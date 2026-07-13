package main

import (
	"encoding/json"
	"testing"
)

func TestForgeMarkRoundTrip(t *testing.T) {
	m := NewForgeMark(2, "dojo.pcap", []byte("content-seed"))
	if m.Forge != ForgeName {
		t.Fatal(m.Forge)
	}
	if m.ID == "" || m.Slot != 2 {
		t.Fatalf("%+v", m)
	}
	p := m.Packet(1)
	if p.Kind != KindMeta {
		t.Fatal(p.KindName())
	}
	got, ok := ParseForgeMark(p.Payload)
	if !ok || got.ID != m.ID {
		t.Fatalf("parse %+v", got)
	}
	mesh := m.MeshJSON("alice")
	b, _ := json.Marshal(mesh)
	var msg map[string]any
	_ = json.Unmarshal(b, &msg)
	got2, ok := ParseForgeFromMesh(msg)
	if !ok || got2.ID != m.ID {
		t.Fatalf("mesh %+v", got2)
	}
}

func TestStampHexLumCorner(t *testing.T) {
	n := 25
	lum := make([]byte, n*n)
	for i := range lum {
		lum[i] = 100
	}
	m := NewForgeMark(1, "a.pcap", []byte{1, 2, 3})
	StampHexLum(lum, n, m)
	// corner changed from 100
	if lum[0] == 100 && lum[1] == 100 {
		t.Fatal("expected watermark corner")
	}
	// bottom-right slot mark
	if lum[n*n-1] < 30 {
		t.Fatal("slot mark")
	}
	// two marks different IDs
	m2 := NewForgeMark(3, "b.pcap", []byte{9, 9, 9})
	if m.ID == m2.ID {
		t.Fatal("ids should differ")
	}
}

func TestStampFrameRGB(t *testing.T) {
	f := &FramePixels{W: 40, H: 24, RGB: make([]byte, 40*24*3)}
	for i := range f.RGB {
		f.RGB[i] = 80
	}
	m := NewForgeMark(1, "x.pcap", []byte("x"))
	StampFrame(f, m)
	// top-left stamped with cyan-ish watermark
	if f.RGB[0] == 80 && f.RGB[1] == 80 {
		t.Fatal("expected corner stamp")
	}
}
