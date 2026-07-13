package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFormatProgramLineIncludesCaption(t *testing.T) {
	bus := NewProgramBus()
	bus.SetCaptionRich(CaptionPayload{
		Text: "On air now", Lang: "en", Source: CaptionSourceChat, Speaker: "host",
	}, "dir")
	line := FormatProgramLine(bus)
	if !strings.Contains(line, "On air") && !strings.Contains(line, "caption") {
		t.Fatal(line)
	}
	if !strings.Contains(line, "program") {
		t.Fatal(line)
	}
}

func TestApplyProgramBusAppliesCaption(t *testing.T) {
	m := NewModel(Options{Nick: "viewer", Host: "127.0.0.1:0"})
	bus := NewProgramBus()
	bus.Take(ProgramSource{Source: ProgramSourceGyst, Nick: "cam1", Label: "cam"}, "dir")
	bus.SetCaptionRich(CaptionPayload{
		Text: "Hello mesh", Source: CaptionSourceChat, Speaker: "dir",
	}, "")
	m.applyProgramBus(bus, "caption-bridge")
	if m.program.Caption != "Hello mesh" {
		t.Fatalf("caption not applied: %q", m.program.Caption)
	}
	eff := m.program.EffectiveCaption()
	if eff.Speaker != "dir" || eff.Source != CaptionSourceChat {
		t.Fatalf("%+v", eff)
	}
}

func TestGlyphAgentProgramEmitsCaptionEvent(t *testing.T) {
	bus := NewProgramBus()
	bus.SetCaptionRich(CaptionPayload{Text: "Agent cap", Source: CaptionSourceManual}, "dir")
	msg := bus.MeshJSON("dir")
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	// re-encode so bus is map (same as wire)
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(wire)
	evs := MapHubMsgToAgentEvents(raw)
	var sawProg, sawCap bool
	for _, e := range evs {
		if e.Type == "program" {
			sawProg = true
			if e.Meta["caption"] != "Agent cap" {
				t.Fatalf("meta caption %v", e.Meta["caption"])
			}
		}
		if e.Type == "caption" {
			sawCap = true
		}
	}
	if !sawProg || !sawCap {
		t.Fatalf("prog=%v cap=%v n=%d", sawProg, sawCap, len(evs))
	}
}

func TestParseCaptionSoftEvent(t *testing.T) {
	msg := map[string]any{
		"type": "caption", "from": "alice", "text": "Soft line", "source": "chat",
	}
	c, ok := ParseCaptionFromMesh(msg)
	if !ok || c.Text != "Soft line" {
		t.Fatalf("%+v", c)
	}
}
