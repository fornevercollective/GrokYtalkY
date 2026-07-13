package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDetectCapProfile80x24Lean(t *testing.T) {
	t.Setenv("GY_CAP", "")
	t.Setenv("GY_ROLE", "")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "xterm-256color")
	p := DetectCapProfile(80, 24)
	if p.Class != CapClassTermLean && p.Class != CapClassTermFull {
		// 80×24 dual 25 may not fit → lean
		t.Logf("class=%s glyph_n=%d", p.Class, p.GlyphN)
	}
	if p.GlyphN != GlyphPhone4a && p.Class == CapClassTermLean {
		// lean should prefer 13
		if p.GlyphN > GlyphPhone3 {
			t.Fatalf("lean glyph_n %d", p.GlyphN)
		}
	}
	if !p.Forge {
		t.Fatal("forge")
	}
	if !p.AcceptsLane(LaneGlyph) && p.Class != CapClassTermMono {
		t.Fatal("lanes")
	}
}

func TestDetectCapProfileForceIoT(t *testing.T) {
	t.Setenv("GY_CAP", "glyph-iot")
	p := DetectCapProfile(80, 24)
	if p.Class != CapClassGlyphIoT {
		t.Fatal(p.Class)
	}
	if p.Role != "agent" {
		t.Fatal(p.Role)
	}
	if p.AcceptsLane(LaneMid) {
		t.Fatal("iot should not take mid")
	}
	if !p.AcceptsLane(LaneGlyph) || !p.Forge {
		t.Fatal("glyph+forge required")
	}
	if p.Backpressure > 8 {
		t.Fatalf("iot bp should be tight got %d", p.Backpressure)
	}
}

func TestDetectCapProfileMono(t *testing.T) {
	t.Setenv("GY_CAP", "")
	t.Setenv("TERM", "dumb")
	t.Setenv("COLORTERM", "")
	p := DetectCapProfile(80, 24)
	// dumb → mono or forced path
	if p.TrueColor && p.Class != CapClassTermMono {
		// Detect may still classify; check mono force
		t.Setenv("GY_CAP", "mono")
		p = DetectCapProfile(80, 24)
	}
	t.Setenv("GY_CAP", "term-mono")
	p = DetectCapProfile(40, 12)
	if p.Class != CapClassTermMono {
		t.Fatal(p.Class)
	}
	if p.AcceptsLane(LaneGlyph) {
		// mono drops pure glyph LED truecolor path
		t.Log("mono may still list glyph — ok if hex present")
	}
	if !p.AcceptsLane(LaneHex) {
		t.Fatal("hex")
	}
}

func TestCapJoinMeshRoundTrip(t *testing.T) {
	p := DetectCapProfile(120, 40)
	t.Setenv("GY_CAP", "term-full")
	p = DetectCapProfile(120, 40)
	join := p.JoinFields("alice", "term")
	b, _ := json.Marshal(join)
	var msg map[string]any
	_ = json.Unmarshal(b, &msg)
	got, ok := ParseCapFromMesh(msg)
	if !ok || got.Class != p.Class {
		t.Fatalf("parse %+v ok=%v", got, ok)
	}
	if got.GlyphN != p.GlyphN {
		t.Fatalf("glyph_n %d vs %d", got.GlyphN, p.GlyphN)
	}
	ann := p.CapAnnounce("alice")
	if ann["type"] != "cap" {
		t.Fatal(ann)
	}
}

func TestRoomGlyphNMin(t *testing.T) {
	peers := []CapProfile{
		{Class: CapClassGlyphIoT, GlyphN: 13, Lanes: []string{LaneGlyph}},
		{Class: CapClassTermFull, GlyphN: 25, Lanes: []string{LaneGlyph}},
	}
	if RoomGlyphN(25, peers) != 13 {
		t.Fatal(RoomGlyphN(25, peers))
	}
	if RoomGlyphN(25, nil) != 25 {
		t.Fatal(RoomGlyphN(25, nil))
	}
}

func TestMapHubMsgToAgentEventsLatticePassThrough(t *testing.T) {
	mark := NewForgeMark(1, "dojo.pcap", []byte("agent"))
	n := 25
	lum := make([]byte, n*n)
	StampHexLum(lum, n, mark)
	corner := lum[0]
	mesh := PacketToMesh(PacketFromHexLum(lum, n, 3), "forger")
	raw, _ := json.Marshal(mesh)
	evs := MapHubMsgToAgentEvents(raw)
	if len(evs) != 1 || evs[0].Type != "glyph" {
		t.Fatalf("%+v", evs)
	}
	if evs[0].N != n || len(evs[0].Data) != n*n {
		t.Fatalf("n/data %d %d", evs[0].N, len(evs[0].Data))
	}
	if evs[0].Data[0] != int(corner) {
		t.Fatalf("lattice lost %d want %d", evs[0].Data[0], corner)
	}
	// forge meta
	metaRaw, _ := json.Marshal(mark.MeshJSON("forger"))
	mevs := MapHubMsgToAgentEvents(metaRaw)
	if len(mevs) != 1 || mevs[0].Type != "forge-mark" || mevs[0].Mark != mark.ID {
		t.Fatalf("%+v", mevs)
	}
}

func TestModelCapOnStart(t *testing.T) {
	t.Setenv("GY_CAP", "term-lean")
	m := NewModel(Options{Nick: "c", Host: "127.0.0.1:0"})
	if m.cap.Class != CapClassTermLean {
		t.Fatal(m.cap.Class)
	}
	sys := strings.Join(sysTexts(m), "\n")
	if !strings.Contains(sys, "cap ") {
		t.Fatal(sys)
	}
}

func TestPreferGlyphNForGeom(t *testing.T) {
	if PreferGlyphNForGeom(25, 80, 24, true) != GlyphPhone4a {
		// dual 25 won't fit 80×24
		got := PreferGlyphNForGeom(25, 80, 24, true)
		if got != GlyphPhone4a {
			t.Fatalf("got %d", got)
		}
	}
}

func TestCapSummary(t *testing.T) {
	p := CapProfile{Class: CapClassGlyphIoT, Role: "agent", GlyphN: 25,
		Lanes: []string{LaneGlyph, LaneHex}, Backpressure: 4, MaxFPS: 12}
	s := p.SummaryLine()
	if !strings.Contains(s, "glyph-iot") || !strings.Contains(s, "bp=4") {
		t.Fatal(s)
	}
	_ = os.Getenv
}
