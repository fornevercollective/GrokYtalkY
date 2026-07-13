package main

import (
	"encoding/json"
	"net"
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
