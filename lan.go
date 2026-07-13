package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LAN discovery for phone ↔ terminal on the same Wi‑Fi.
//
// HTTP:  GET /api/lan  → join URLs + local IPs
// UDP:   broadcast/multicast "GYWHO1" → unicast "GYHUB1"+JSON
//
// Default UDP discovery port = hub port + 1 (9877 when hub is 9876).

const (
	lanWhoMagic = "GYWHO1"
	lanHubMagic = "GYHUB1"
	// multicast group for LAN discovery (link-local admin)
	lanMulticast = "239.255.76.67"
)

// LanInfo is advertise payload for phones and other peers on the LAN.
type LanInfo struct {
	Type    string   `json:"type"` // gy-hub
	V       int      `json:"v"`
	Port    int      `json:"port"`
	UDP     int      `json:"udp,omitempty"`
	WS      string   `json:"ws"`      // preferred ws://IP:port/
	HTTP    string   `json:"http"`    // preferred http://IP:port/
	Phone   string   `json:"phone"`   // cast page for mobile browsers
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
	return LanInfo{
		Type:    "gy-hub",
		V:       1,
		Port:    port,
		UDP:     udp,
		WS:      fmt.Sprintf("ws://%s:%d/", ip, port),
		HTTP:    fmt.Sprintf("http://%s:%d/", ip, port),
		Phone:   fmt.Sprintf("http://%s:%d/phone.html", ip, port),
		Burst:   fmt.Sprintf("http://%s:%d/burst.html", ip, port),
		Glyph:   fmt.Sprintf("http://%s:%d/grokglyph.html", ip, port),
		Room:    room,
		Version: Version,
		Host:    host,
		IPs:     ips,
	}
}

// FormatLanBanner multi-line for gy serve / /lan TUI.
func FormatLanBanner(info LanInfo) string {
	var b strings.Builder
	b.WriteString("same Wi‑Fi · phone → terminal\n")
	b.WriteString(fmt.Sprintf("  phone cast  %s\n", info.Phone))
	b.WriteString(fmt.Sprintf("  mesh WS     %s?role=phone&nick=phone\n", strings.TrimRight(info.WS, "/")))
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
		info.WS = fmt.Sprintf("ws://%s:%d/", ip, info.Port)
		info.HTTP = fmt.Sprintf("http://%s:%d/", ip, info.Port)
		info.Phone = fmt.Sprintf("http://%s:%d/phone.html", ip, info.Port)
		info.Burst = fmt.Sprintf("http://%s:%d/burst.html", ip, info.Port)
		info.Glyph = fmt.Sprintf("http://%s:%d/grokglyph.html", ip, info.Port)
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
