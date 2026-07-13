package main

import (
	"encoding/json"
	"testing"
)

func TestApplyProgramCaptionPreservesProgramSource(t *testing.T) {
	bus := NewProgramBus()
	mk := NewForgeMark(2, "a.pcap", []byte("x"))
	bus.Take(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	bus.SetPreview(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	seq0 := bus.Seq
	stored := bus.MeshJSON("dir")

	cap := CaptionPayload{
		Text: "Hello room", Lang: "en", Role: CaptionRoleLower,
		Speaker: "alice", Source: CaptionSourceChat,
	}
	next := ApplyProgramCaption(stored, "caption-bridge", cap)
	if next["type"] != "program" {
		t.Fatal(next["type"])
	}
	got, ok := ParseProgramBus(next)
	if !ok {
		t.Fatal("parse")
	}
	if got.Program.Mark != mk.ID {
		t.Fatalf("program mark lost: %s", got.Program.Mark)
	}
	if got.Preview == nil {
		t.Fatal("preview cleared")
	}
	if got.Mode != ProgramModeLive {
		t.Fatal(got.Mode)
	}
	if got.Conductor != "dir" {
		// must not steal conductor
		t.Fatalf("conductor %q", got.Conductor)
	}
	if got.Seq <= seq0 {
		t.Fatal("seq")
	}
	eff := got.EffectiveCaption()
	if eff.Text != "Hello room" || eff.Source != CaptionSourceChat || eff.Speaker != "alice" {
		t.Fatalf("%+v", eff)
	}
	// ANC caption rich
	pkts := ProgramBusToANC(got)
	c, ok := findANC(pkts, "caption")
	if !ok {
		t.Fatal("anc caption")
	}
	parsed := ParseCaptionUDW(c.UDW)
	if parsed.Text != "Hello room" || parsed.Lang != "en" {
		t.Fatalf("%+v", parsed)
	}
}

func TestApplyProgramCaptionClear(t *testing.T) {
	bus := NewProgramBus()
	bus.SetCaption("X", "dir")
	stored := bus.MeshJSON("dir")
	next := ApplyProgramCaption(stored, "bridge", CaptionPayload{})
	got, _ := ParseProgramBus(next)
	if !got.EffectiveCaption().IsEmpty() {
		t.Fatal(got.Caption)
	}
}

func TestApplyProgramCaptionNilStored(t *testing.T) {
	next := ApplyProgramCaption(nil, "bridge", CaptionPayload{Text: "solo", Source: CaptionSourceChat})
	got, ok := ParseProgramBus(next)
	if !ok || got.Caption != "solo" {
		t.Fatalf("%+v", got)
	}
}

func TestCaptionFromChatLine(t *testing.T) {
	c := CaptionFromChatLine("alice", "Hello DOJO")
	if c.Text != "Hello DOJO" || c.Speaker != "alice" || c.Source != CaptionSourceChat {
		t.Fatalf("%+v", c)
	}
	c = CaptionFromChatLine("bob", "en: Hola")
	if c.Lang != "en" || c.Text != "Hola" || c.Speaker != "bob" {
		t.Fatalf("%+v", c)
	}
	c = CaptionFromChatLine("x", "lang=es role=crawl Breaking")
	if c.Lang != "es" || c.Role != "crawl" || c.Text != "Breaking" {
		t.Fatalf("%+v", c)
	}
}

func TestParseCaptionFromMesh(t *testing.T) {
	msg := map[string]any{
		"type": "program-caption", "from": "alice",
		"text": "Hi", "lang": "en", "role": "lower", "source": "chat",
	}
	c, ok := ParseCaptionFromMesh(msg)
	if !ok || c.Text != "Hi" || c.Speaker != "alice" {
		t.Fatalf("%+v ok=%v", c, ok)
	}
	// caption_meta wins
	msg2 := map[string]any{
		"caption_meta": map[string]any{
			"text": "Meta", "speaker": "z", "source": "chat", "lang": "ja",
		},
	}
	c, ok = ParseCaptionFromMesh(msg2)
	if !ok || c.Text != "Meta" || c.Lang != "ja" {
		t.Fatalf("%+v", c)
	}
}

func TestProgramCaptionMeshJSONRoundTrip(t *testing.T) {
	bus := NewProgramBus()
	mk := NewForgeMark(1, "p.pcap", []byte("h"))
	bus.Take(SourceFromForge("dir", &mk, LaneHex), "dir")
	stored := bus.MeshJSON("dir")
	raw, _ := json.Marshal(stored)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	next := ApplyProgramCaption(m, "caption-bridge", CaptionFromChatLine("host1", "en: On air now"))
	b, _ := json.Marshal(next)
	var again map[string]any
	_ = json.Unmarshal(b, &again)
	got, ok := ParseProgramBus(again)
	if !ok || got.Program.Lane != LaneHex {
		t.Fatalf("%+v", got)
	}
	if got.EffectiveCaption().Lang != "en" {
		t.Fatal(got.EffectiveCaption())
	}
}
