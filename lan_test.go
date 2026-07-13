package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildLanInfo(t *testing.T) {
	info := BuildLanInfo(9876, "dojo")
	if info.Port != 9876 || info.UDP != 9877 {
		t.Fatalf("%+v", info)
	}
	if !strings.Contains(info.Phone, "phone.html") {
		t.Fatal(info.Phone)
	}
	if !strings.Contains(info.QR, "/api/lan/qr") {
		t.Fatal(info.QR)
	}
	if !strings.HasPrefix(info.WS, "ws://") {
		t.Fatal(info.WS)
	}
	if info.Room != "dojo" && info.Room != "global" {
		// NormalizeMeshRoom may rewrite empty; with "dojo" should stick
		if info.Room != NormalizeMeshRoom("dojo") {
			t.Fatal(info.Room)
		}
	}
	b := FormatLanBanner(info)
	if !strings.Contains(b, "phone cast") || !strings.Contains(b, "same Wi") {
		t.Fatal(b)
	}
	if !strings.Contains(b, "quick QR") || !strings.Contains(b, "scan tip") {
		t.Fatal(b)
	}
}

func TestEncodePhoneQR(t *testing.T) {
	png, err := EncodePhoneQR("http://192.168.1.10:9876/phone.html", 128)
	if err != nil {
		t.Fatal(err)
	}
	if len(png) < 100 {
		t.Fatalf("png too small: %d", len(png))
	}
	// PNG magic
	if png[0] != 0x89 || string(png[1:4]) != "PNG" {
		t.Fatalf("not png magic: %x", png[:8])
	}
	if _, err := EncodePhoneQR("", 64); err == nil {
		t.Fatal("expected empty content error")
	}
}

func TestIsSafeLanQRContent(t *testing.T) {
	if !isSafeLanQRContent("http://192.168.1.1:9876/phone.html") {
		t.Fatal("http")
	}
	if !isSafeLanQRContent("ws://10.0.0.2:9876/") {
		t.Fatal("ws")
	}
	if isSafeLanQRContent("javascript:alert(1)") {
		t.Fatal("js")
	}
	if isSafeLanQRContent("ftp://x") {
		t.Fatal("ftp")
	}
}

func TestParseHubPort(t *testing.T) {
	if ParseHubPort("0.0.0.0:9876") != 9876 {
		t.Fatal()
	}
	if ParseHubPort("127.0.0.1:9000") != 9000 {
		t.Fatal()
	}
	if ParseHubPort("") != 9876 {
		t.Fatal()
	}
}

func TestIsWhoProbe(t *testing.T) {
	if !isWhoProbe(lanWhoMagic) {
		t.Fatal("magic")
	}
	if !isWhoProbe(`{"type":"gy-who"}`) {
		t.Fatal("json")
	}
	if isWhoProbe("hello") {
		t.Fatal("noise")
	}
}

func TestLanDiscoverRoundTrip(t *testing.T) {
	info := BuildLanInfo(19876, "global")
	// use high port to avoid clash
	info.UDP = 19877
	info.Port = 19876
	d, err := StartLanDiscoverer(info)
	if err != nil {
		// CI / sandbox may block bind — skip
		t.Skip(err)
	}
	defer d.Close()
	time.Sleep(50 * time.Millisecond)

	// direct unicast probe to 127.0.0.1
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.WriteToUDP([]byte(lanWhoMagic), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 19877})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Skip("no reply (firewall?): ", err)
	}
	raw := string(buf[:n])
	if !strings.HasPrefix(raw, lanHubMagic) {
		t.Fatalf("bad reply %q", raw[:min(40, len(raw))])
	}
	var got LanInfo
	if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, lanHubMagic)), &got); err != nil {
		t.Fatal(err)
	}
	if got.Port != 19876 {
		t.Fatalf("%+v", got)
	}
}

func TestLocalLANIPsNoPanic(t *testing.T) {
	_ = LocalLANIPs()
	_ = PreferredLANIP()
}

func TestIsPrivateIPv4(t *testing.T) {
	if !isPrivateIPv4(net.ParseIP("192.168.1.5")) {
		t.Fatal()
	}
	if !isPrivateIPv4(net.ParseIP("10.0.0.2")) {
		t.Fatal()
	}
	if isPrivateIPv4(net.ParseIP("8.8.8.8")) {
		t.Fatal()
	}
}

func TestLanQRHTTPEndpoint(t *testing.T) {
	h := NewHub("127.0.0.1:9876", true, "")
	if h == nil || h.server == nil || h.server.Handler == nil {
		t.Fatal("hub mux")
	}
	// JSON /api/lan includes qr
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/lan", nil)
	h.server.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("lan status %d", rr.Code)
	}
	var info LanInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.QR, "/api/lan/qr") || !strings.Contains(info.Phone, "phone.html") {
		t.Fatalf("%+v", info)
	}
	// PNG QR
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/lan/qr?size=96", nil)
	h.server.Handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("qr status %d body=%s", rr2.Code, rr2.Body.String())
	}
	if ct := rr2.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatal(ct)
	}
	body, _ := io.ReadAll(rr2.Body)
	if len(body) < 50 || body[0] != 0x89 {
		t.Fatalf("bad png len=%d", len(body))
	}
	// reject unsafe url
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/lan/qr?url=javascript:alert(1)", nil)
	h.server.Handler.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rr3.Code)
	}
}
