package main

import (
	"encoding/json"
	"testing"

	"github.com/fornevercollective/grokytalky/strudel"
)

func TestMeshMIDIRoundTrip(t *testing.T) {
	msg := BuildMeshMIDINote("alice", MeshMIDINoteOn, 9, 36, 100, "strudel")
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	got, ok := ParseMeshMIDI(m)
	if !ok {
		t.Fatal("parse")
	}
	if got.From != "alice" || got.Note != 36 || got.Kind != MeshMIDINoteOn {
		t.Fatalf("%+v", got)
	}
	cc := BuildMeshMIDICC("bob", 0, 11, 64, "walkie")
	if cc["kind"] != MeshMIDICC {
		t.Fatal(cc)
	}
	tmp := BuildMeshMIDITempo("jam", 0.5, 12)
	if tmp["cps"].(float64) != 0.5 {
		t.Fatal(tmp)
	}
}

func TestStrudelHitToMIDI(t *testing.T) {
	n, ch, vel := strudelHitToMIDI(strudel.Event{Kind: "drum", Sound: "bd", MIDI: 36, Vel: 100})
	if n != 36 || ch != 9 || vel != 100 {
		t.Fatalf("%d %d %d", n, ch, vel)
	}
	n, ch, vel = strudelHitToMIDI(strudel.Event{Kind: "drum", Sound: "sd", Vel: 80})
	if n != 38 || ch != 9 {
		t.Fatalf("sd %d %d", n, ch)
	}
	n, _, _ = strudelHitToMIDI(strudel.Event{Kind: "fx", Sound: "zzz"})
	if n != -1 {
		t.Fatal(n)
	}
}

func TestDuckPCM(t *testing.T) {
	// full scale s16
	pcm := make([]byte, 4)
	pcm[0], pcm[1] = 0x00, 0x40 // 0x4000
	out := duckPCM(pcm, 0.5)
	if len(out) != 4 {
		t.Fatal(len(out))
	}
	silent := duckPCM(pcm, 0)
	if silent[0] != 0 || silent[1] != 0 {
		t.Fatal("mute")
	}
}

func TestGrokOverlayThrottle(t *testing.T) {
	s := newGrokOverlayState()
	if s.CanAuto() {
		t.Fatal("auto off")
	}
	s.Auto = true
	if !s.CanAuto() {
		t.Fatal("should allow first")
	}
	s.Record("hello", "p1")
	if s.CanAuto() {
		t.Fatal("just recorded — throttle")
	}
	if s.StatusLine() == "" {
		t.Fatal("status")
	}
	cap := OverlayReplyToCaption("line one\nline two", "grok")
	if cap.Text != "line one" || cap.Source != "grok-overlay" {
		t.Fatalf("%+v", cap)
	}
	if NormalizeOverlayMode("fx") != OverlayEffect {
		t.Fatal("mode")
	}
}
