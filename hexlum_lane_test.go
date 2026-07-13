package main

import (
	"encoding/json"
	"testing"
)

func TestVburstGlyphToHexLumMesh(t *testing.T) {
	glyph := make([]any, 25*25)
	for i := range glyph {
		glyph[i] = float64((i * 3) % 256)
	}
	msg := map[string]any{
		"type":   "vburst-frame",
		"from":   "cam-alice",
		"glyph":  glyph,
		"glyphN": float64(25),
		"t":      float64(1_700_000_000_123),
		"seq":    float64(42),
		"b64":    "not-used-for-lattice",
		"fmt":    "jpeg",
	}
	out, ok := VburstGlyphToHexLumMesh(msg)
	if !ok {
		t.Fatal("expected hexlum promote")
	}
	if out["type"] != MeshTypeGYST {
		t.Fatalf("type %v", out["type"])
	}
	if out["kind"] != "hexlum" {
		t.Fatalf("kind %v", out["kind"])
	}
	if out["lane"] != LaneHex {
		t.Fatalf("lane %v", out["lane"])
	}
	if out["via"] != "vburst-promote" {
		t.Fatalf("via %v", out["via"])
	}
	if out["from"] != "cam-alice" {
		t.Fatalf("from %v", out["from"])
	}
	// JSON round-trip like hub fan-out
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	pkt, err := MeshToPacket(m)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Kind != KindHexLum || pkt.Width != 25 || len(pkt.Payload) != 625 {
		t.Fatalf("pkt kind=%s %dx%d len=%d", pkt.KindName(), pkt.Width, pkt.Height, len(pkt.Payload))
	}
	if pkt.Payload[0] != 0 || pkt.Payload[1] != 3 {
		t.Fatalf("payload[0]=%d [1]=%d", pkt.Payload[0], pkt.Payload[1])
	}
	// SFU bridge shape: gyst hexlum extract
	data, n, err := gystHexlumBytes(m)
	if err != nil || n != 25 || len(data) != 625 {
		t.Fatalf("gystHexlumBytes n=%d len=%d err=%v", n, len(data), err)
	}
}

func TestVburstGlyphSkipWhenHexLane(t *testing.T) {
	msg := map[string]any{
		"type":     "vburst-frame",
		"from":     "gg",
		"hex_lane": true,
		"glyph":    []any{float64(1), float64(2), float64(3), float64(4)},
		"glyphN":   float64(2),
	}
	if _, ok := VburstGlyphToHexLumMesh(msg); ok {
		t.Fatal("should skip dual-published frames")
	}
}

func TestVburstNoGlyphNoPromote(t *testing.T) {
	msg := map[string]any{
		"type": "vburst-frame",
		"from": "x",
		"b64":  "aaaa",
		"fmt":  "jpeg",
	}
	if _, ok := VburstGlyphToHexLumMesh(msg); ok {
		t.Fatal("jpeg-only should not promote")
	}
}

func TestExtractGlyph13(t *testing.T) {
	g := make([]any, 13*13)
	for i := range g {
		g[i] = float64(i % 200)
	}
	msg := map[string]any{"glyph": g, "glyphN": float64(13)}
	data, n, ok := extractGlyphLattice(msg)
	if !ok || n != 13 || len(data) != 169 {
		t.Fatalf("n=%d len=%d ok=%v", n, len(data), ok)
	}
}

func TestFormatHexLumLaneLine(t *testing.T) {
	s := FormatHexLumLaneLine(25, "alice")
	if s == "" || len(s) < 10 {
		t.Fatal(s)
	}
}
