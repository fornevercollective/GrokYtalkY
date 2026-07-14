package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LAN discovery for phone ↔ terminal on the same Wi‑Fi.
//
// HTTP:  GET /api/lan     → join URLs + local IPs + qr path
//        GET /api/lan/qr  → HTML scan page (client-side QR; no Go QR dep)
//                          optional PNG if system `qrencode` is on PATH + Accept/format=png
//        GET /qr.html     → same scan UI (static)
// UDP:   broadcast/multicast "GYWHO1" → unicast "GYHUB1"+JSON
//
// Default UDP discovery port = hub port + 1 (9877 when hub is 9876).
//
// Platform note: QR rendering is owned in site/qrcode-generator.js (MIT,
// Kazuhiko Arase, vendored). Go does not depend on github.com/skip2/go-qrcode.

const (
	lanWhoMagic = "GYWHO1"
	lanHubMagic = "GYHUB1"
	// multicast group for LAN discovery (link-local admin)
	lanMulticast = "239.255.76.67"
	// default module scale for optional system qrencode PNG
	lanQRDefaultSize = 8
)

// LanInfo is advertise payload for phones and other peers on the LAN.
type LanInfo struct {
	Type    string   `json:"type"` // gy-hub
	V       int      `json:"v"`
	Port    int      `json:"port"`
	UDP     int      `json:"udp,omitempty"`
	WS      string   `json:"ws"`    // preferred ws://IP:port/
	HTTP    string   `json:"http"`  // preferred http://IP:port/
	Phone   string   `json:"phone"` // cast page for mobile browsers
	Film    string   `json:"film,omitempty"` // GrokGlyph multi-cam phone seat (L1 + auto hub)
	QR      string   `json:"qr,omitempty"` // scan page (HTML; client-side QR)
	Sphere  string   `json:"sphere,omitempty"` // Sphere Glyph live seat viewer
	Burst   string   `json:"burst,omitempty"`
	Glyph   string   `json:"glyph,omitempty"`
	Room    string   `json:"room"`
	Version string   `json:"version"`
	Nick    string   `json:"nick,omitempty"` // host machine hint
	IPs     []string `json:"ips"`
	Host    string   `json:"host,omitempty"` // hostname
}

// LocalLANIPs returns non-loopback IPv4 addresses suitable for same-WiFi join.
func LocalLANIPs() []string {
	var out []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// skip obvious virtual / docker / bridge when possible
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") ||
			strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "vmnet") ||
			strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "awdl") ||
			strings.HasPrefix(name, "llw") || strings.Contains(name, "vpn") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			// skip link-local 169.254
			if ip.IsLinkLocalUnicast() {
				continue
			}
			s := ip.String()
			// prefer private ranges
			if isPrivateIPv4(ip) {
				out = append(out, s)
			}
		}
	}
	// also include any remaining non-private if empty
	if len(out) == 0 {
		ifaces, _ = net.Interfaces()
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				var ip net.IP
				switch v := a.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
					out = append(out, ip.String())
				}
			}
		}
	}
	return uniqueStrings(out)
}

func isPrivateIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// 10/8, 172.16/12, 192.168/16
	if ip4[0] == 10 {
		return true
	}
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return true
	}
	if ip4[0] == 192 && ip4[1] == 168 {
		return true
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// PreferredLANIP picks the first private IPv4 for advertise.
func PreferredLANIP() string {
	ips := LocalLANIPs()
	if len(ips) == 0 {
		return "127.0.0.1"
	}
	return ips[0]
}

// BuildLanInfo constructs advertise payload for a hub listening on port.
func BuildLanInfo(port int, room string) LanInfo {
	if port < 1 {
		port = 9876
	}
	if room == "" {
		room = NormalizeMeshRoom(os.Getenv("GY_ROOM"))
	}
	ip := PreferredLANIP()
	ips := LocalLANIPs()
	if len(ips) == 0 {
		ips = []string{ip}
	}
	host, _ := os.Hostname()
	udp := port + 1
	httpBase := fmt.Sprintf("http://%s:%d", ip, port)
	if room == "" {
		room = "global"
	}
	// Phone multi-cam film link (GrokGlyph seat L1 + auto hub)
	filmL1 := fmt.Sprintf("%s/grokglyph.html?role=phone&slot=L1&room=%s&nick=phone-L1&hub=1&connect=1",
		httpBase, room)
	return LanInfo{
		Type:    "gy-hub",
		V:       1,
		Port:    port,
		UDP:     udp,
		WS:      fmt.Sprintf("ws://%s:%d/", ip, port),
		HTTP:    httpBase + "/",
		Phone:   httpBase + "/phone.html?room=" + room + "&quick=1",
		Film:    filmL1,
		QR:      httpBase + "/api/lan/qr",
		Sphere:  httpBase + "/sphere.html",
		Burst:   httpBase + "/burst.html",
		Glyph:   httpBase + "/grokglyph.html?role=laptop&slot=C&room=" + room + "&hub=1",
		Room:    room,
		Version: Version,
		Host:    host,
		IPs:     ips,
	}
}

// EncodePhoneQRPNG optionally encodes a PNG via system `qrencode` (libqrencode).
// No Go QR library — returns error if qrencode is missing. size is module pixels (1–20).
func EncodePhoneQRPNG(content string, size int) ([]byte, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty QR content")
	}
	if size <= 0 {
		size = lanQRDefaultSize
	}
	if size > 20 {
		size = 20
	}
	path, err := exec.LookPath("qrencode")
	if err != nil {
		return nil, fmt.Errorf("qrencode not on PATH (install libqrencode) — use HTML scan page /api/lan/qr")
	}
	// qrencode -t PNG -s N -o - CONTENT
	cmd := exec.Command(path, "-t", "PNG", "-s", strconv.Itoa(size), "-m", "2", "-o", "-", content)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("qrencode: %w", err)
	}
	if len(out) < 8 || out[0] != 0x89 {
		return nil, fmt.Errorf("qrencode: not a PNG")
	}
	return out, nil
}

// FormatLanQRHTML is a self-contained scan page for laptop → phone.
// QR is drawn client-side from vendored site/qrcode-generator.js (no Go QR dep).
// Payload is base64 in the page so raw content cannot break out of <script>.
func FormatLanQRHTML(content, phoneURL string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		content = phoneURL
	}
	esc := html.EscapeString(content)
	phoneEsc := html.EscapeString(phoneURL)
	// b64 avoids </script> breakout inside inline JS
	b64 := base64.StdEncoding.EncodeToString([]byte(content))
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8"/>
<meta name="viewport" content="width=device-width, initial-scale=1"/>
<meta name="theme-color" content="#0a0a0c"/>
<title>GrokYtalkY · scan to phone</title>
<style>
  :root { --bg:#0a0a0c; --card:#14141a; --accent:#6ee7b7; --muted:#8b8b9a; }
  * { box-sizing: border-box; }
  body { margin:0; min-height:100dvh; background:var(--bg); color:#eee;
    font-family: system-ui, sans-serif; display:flex; flex-direction:column;
    align-items:center; justify-content:center; padding:1.5rem; text-align:center; }
  h1 { font-size:1.15rem; font-weight:600; margin:0 0 .35rem; letter-spacing:-.02em; }
  .sub { color:var(--muted); font-size:.85rem; margin-bottom:1.25rem; }
  #qr { background:#fff; padding:14px; border-radius:16px; line-height:0;
    box-shadow:0 0 0 1px #333; }
  #qr img, #qr canvas, #qr table { display:block; margin:0 auto; }
  .url { margin-top:1rem; font-family: ui-monospace, monospace; font-size:.72rem;
    color:var(--accent); word-break:break-all; max-width:28rem; }
  a.btn { display:inline-block; margin-top:1.1rem; padding:.7rem 1.1rem;
    background:var(--accent); color:#042; text-decoration:none; border-radius:12px;
    font-weight:700; font-size:.95rem; }
  .hint { margin-top:1rem; color:var(--muted); font-size:.75rem; line-height:1.45; max-width:26rem; }
</style>
</head>
<body>
  <h1>Scan → phone</h1>
  <p class="sub">same Wi‑Fi · quick connect</p>
  <div id="qr" aria-label="QR code"></div>
  <p class="url" id="url">` + esc + `</p>
  <a class="btn" id="open" href="` + phoneEsc + `">Open phone cast</a>
  <p class="hint">Phone camera scans this code → opens cast page → <strong>Quick connect</strong>.
  QR is rendered in-browser (vendored MIT encoder). No third-party Go QR package.</p>
<script src="/qrcode-generator.js"></script>
<script>
(function(){
  var text = atob("` + b64 + `");
  var el = document.getElementById("qr");
  if (typeof qrcode !== "function") {
    el.innerHTML = "<p style='color:#f87171;padding:1rem'>qrcode-generator.js missing</p>";
    return;
  }
  try {
    var q = qrcode(0, "M");
    q.addData(text);
    q.make();
    el.innerHTML = q.createImgTag(5, 2);
    var img = el.querySelector("img");
    if (img) { img.alt = "QR phone cast"; img.style.width = "min(72vw, 280px)"; img.style.height = "auto"; }
  } catch (e) {
    el.innerHTML = "<p style='color:#f87171;padding:1rem'>encode failed</p>";
  }
})();
</script>
</body>
</html>
`
}

// FormatLanBanner multi-line for gy serve / /lan TUI.
func FormatLanBanner(info LanInfo) string {
	var b strings.Builder
	b.WriteString("same Wi‑Fi · phone ↔ laptop mesh\n")
	b.WriteString(fmt.Sprintf("  laptop      %s\n", info.Glyph))
	if info.Film != "" {
		b.WriteString(fmt.Sprintf("  phone film  %s\n", info.Film))
	}
	b.WriteString(fmt.Sprintf("  phone cast  %s\n", info.Phone))
	b.WriteString(fmt.Sprintf("  sphere      %s\n", info.Sphere))
	b.WriteString(fmt.Sprintf("  quick QR    %s\n", info.QR))
	b.WriteString("  scan tip    open GrokGlyph on laptop · Show phone QR · both cast live\n")
	b.WriteString(fmt.Sprintf("  mesh WS     %s?role=phone&nick=phone&room=%s\n", strings.TrimRight(info.WS, "/"), info.Room))
	if len(info.IPs) > 1 {
		b.WriteString("  also        ")
		for i, ip := range info.IPs {
			if i > 0 {
				b.WriteString(" · ")
			}
			b.WriteString(fmt.Sprintf("http://%s:%d/phone.html", ip, info.Port))
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("  discover    UDP %s:%d  (probe %s)\n", lanMulticast, info.UDP, lanWhoMagic))
	b.WriteString("  terminal    gy  (or gy join " + strings.TrimPrefix(strings.TrimPrefix(info.WS, "ws://"), "/") + ")\n")
	return b.String()
}

// LanDiscoverer responds to UDP who-probes while hub is up.
type LanDiscoverer struct {
	Port int // UDP listen port
	Info LanInfo
	mu   sync.Mutex
	conn *net.UDPConn
	stop chan struct{}
}

// StartLanDiscoverer listens for GYWHO probes and answers with hub info.
func StartLanDiscoverer(info LanInfo) (*LanDiscoverer, error) {
	udpPort := info.UDP
	if udpPort < 1 {
		udpPort = info.Port + 1
	}
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: udpPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}
	// Broadcast + unicast probes land on 0.0.0.0:udpPort without multicast join.
	// Multicast 239.x may still arrive on some stacks when bound to all interfaces.
	_ = conn.SetReadBuffer(64 * 1024)

	d := &LanDiscoverer{Port: udpPort, Info: info, conn: conn, stop: make(chan struct{})}
	go d.loop()
	return d, nil
}

func (d *LanDiscoverer) loop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-d.stop:
			return
		default:
		}
		_ = d.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, raddr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-d.stop:
				return
			default:
				continue
			}
		}
		if n < 4 || raddr == nil {
			continue
		}
		raw := strings.TrimSpace(string(buf[:n]))
		if !isWhoProbe(raw) {
			continue
		}
		d.reply(raddr)
	}
}

func isWhoProbe(raw string) bool {
	if strings.HasPrefix(raw, lanWhoMagic) {
		return true
	}
	// JSON probe
	if strings.Contains(raw, "gy-who") || strings.Contains(raw, `"type":"who"`) {
		return true
	}
	return false
}

func (d *LanDiscoverer) reply(to *net.UDPAddr) {
	d.mu.Lock()
	info := d.Info
	// refresh IPs each reply (roam / interface change)
	info.IPs = LocalLANIPs()
	if len(info.IPs) > 0 {
		ip := info.IPs[0]
		base := fmt.Sprintf("http://%s:%d", ip, info.Port)
		info.WS = fmt.Sprintf("ws://%s:%d/", ip, info.Port)
		info.HTTP = base + "/"
		info.Phone = base + "/phone.html"
		info.QR = base + "/api/lan/qr"
		info.Sphere = base + "/sphere.html"
		info.Burst = base + "/burst.html"
		info.Glyph = base + "/grokglyph.html"
	}
	d.Info = info
	d.mu.Unlock()

	payload, _ := json.Marshal(info)
	msg := append([]byte(lanHubMagic), payload...)
	_, _ = d.conn.WriteToUDP(msg, to)
}

// Close stops the discoverer.
func (d *LanDiscoverer) Close() error {
	if d == nil {
		return nil
	}
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

// DiscoverHubsOnLAN broadcasts a who-probe and collects hub replies (timeout).
func DiscoverHubsOnLAN(udpPort int, timeout time.Duration) ([]LanInfo, error) {
	if udpPort < 1 {
		udpPort = 9877
	}
	if timeout < 100*time.Millisecond {
		timeout = 1500 * time.Millisecond
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	probe := []byte(lanWhoMagic + "\n")
	// broadcast
	_, _ = conn.WriteToUDP(probe, &net.UDPAddr{IP: net.IPv4bcast, Port: udpPort})
	// multicast
	if g := net.ParseIP(lanMulticast); g != nil {
		_, _ = conn.WriteToUDP(probe, &net.UDPAddr{IP: g, Port: udpPort})
	}
	// also probe common local subnets via directed broadcast is hard; rely on bcast

	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	var found []LanInfo
	seen := map[string]bool{}
	buf := make([]byte, 4096)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		raw := string(buf[:n])
		if !strings.HasPrefix(raw, lanHubMagic) {
			continue
		}
		var info LanInfo
		if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, lanHubMagic)), &info); err != nil {
			continue
		}
		key := info.WS
		if key == "" {
			key = fmt.Sprintf("%v", info.IPs)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if info.Type == "" {
			info.Type = "gy-hub"
		}
		found = append(found, info)
	}
	return found, nil
}

// isSafeLanQRContent allows only http(s)/ws(s) join URLs for QR generation.
func isSafeLanQRContent(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "ws://") || strings.HasPrefix(s, "wss://")
}

// ParseHubPort extracts port from "host:port" or addr string.
func ParseHubPort(addr string) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return 9876
	}
	// "[::]:9876" or "0.0.0.0:9876"
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		p, err := strconv.Atoi(addr[i+1:])
		if err == nil && p > 0 {
			return p
		}
	}
	return 9876
}
