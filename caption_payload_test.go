package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCaptionPlainUDWRoundTrip(t *testing.T) {
	c := CaptionPayload{Text: "HELLO DOJO"}
	udw := EncodeCaptionUDW(c)
	if udw[0] == CaptionUDWMagic {
		t.Fatal("plain should not use magic")
	}
	if string(udw) != "HELLO DOJO" {
		t.Fatal(string(udw))
	}
	got := ParseCaptionUDW(udw)
	if got.Text != "HELLO DOJO" {
		t.Fatal(got)
	}
}

func TestCaptionRichUDWRoundTrip(t *testing.T) {
	c := CaptionPayload{
		Text: "Next take", Lang: "en", Role: CaptionRoleLower,
		Speaker: "dir", Source: CaptionSourceManual,
	}
	udw := EncodeCaptionUDW(c)
	if len(udw) == 0 || udw[0] != CaptionUDWMagic {
		t.Fatalf("want magic got %v", udw)
	}
	if len(udw) > ANCCaptionMax {
		t.Fatal(len(udw))
	}
	got := ParseCaptionUDW(udw)
	if got.Text != "Next take" || got.Lang != "en" || got.Role != "lower" || got.Speaker != "dir" {
		t.Fatalf("%+v", got)
	}
}

func TestCaptionRichFallsBackWhenHuge(t *testing.T) {
	c := CaptionPayload{
		Text: strings.Repeat("W", 100),
		Lang: "en", Role: "lower",
		Speaker: strings.Repeat("s", 40),
		Source:  "bridge",
	}
	udw := EncodeCaptionUDW(c)
	if len(udw) > ANCCaptionMax {
		t.Fatal(len(udw))
	}
	// must still yield some text
	got := ParseCaptionUDW(udw)
	if got.IsEmpty() {
		t.Fatal("empty after encode")
	}
}

func TestParseCaptionArg(t *testing.T) {
	_, clear, err := ParseCaptionArg("clear")
	if err != nil || !clear {
		t.Fatal(clear, err)
	}
	c, clear, err := ParseCaptionArg("lang=en role=crawl speaker=alice Hello world")
	if err != nil || clear {
		t.Fatal(err, clear)
	}
	if c.Lang != "en" || c.Role != "crawl" || c.Speaker != "alice" || c.Text != "Hello world" {
		t.Fatalf("%+v", c)
	}
	c, _, err = ParseCaptionArg("es: Hola mundo")
	if err != nil || c.Lang != "es" || c.Text != "Hola mundo" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestProgramBusCaptionRichMeshAndANC(t *testing.T) {
	bus := NewProgramBus()
	bus.SetCaptionRich(CaptionPayload{
		Text: "TAKE NEXT", Lang: "en", Role: CaptionRoleChyron, Source: CaptionSourceProgram,
	}, "dir")
	if bus.Caption != "TAKE NEXT" || bus.CapMeta == nil || bus.CapMeta.Role != "chyron" {
		t.Fatalf("%+v", bus)
	}
	// mesh round-trip
	raw, _ := json.Marshal(bus.MeshJSON("dir"))
	var msg map[string]any
	_ = json.Unmarshal(raw, &msg)
	got, ok := ParseProgramBus(msg)
	if !ok || got.Caption != "TAKE NEXT" {
		t.Fatalf("%+v ok=%v", got, ok)
	}
	if got.CapMeta == nil || got.CapMeta.Lang != "en" || got.CapMeta.Role != "chyron" {
		t.Fatalf("meta %+v", got.CapMeta)
	}
	pkts := ProgramBusToANC(got)
	cap, ok := findANC(pkts, "caption")
	if !ok {
		t.Fatal("caption packet")
	}
	parsed := ParseCaptionUDW(cap.UDW)
	if parsed.Text != "TAKE NEXT" || parsed.Lang != "en" {
		t.Fatalf("%+v udw=%q", parsed, cap.UDW)
	}
	tally, _ := findANC(pkts, "tally")
	_, _, flags, _, _ := ParseTallyUDWEx(tally.UDW)
	if flags&ANCFlagHasCaption == 0 {
		t.Fatal("flag")
	}
	// clear
	bus.SetCaptionRich(CaptionPayload{}, "dir")
	if bus.Caption != "" || bus.CapMeta != nil {
		t.Fatal("clear")
	}
	if _, ok := findANC(ProgramBusToANC(bus), "caption"); ok {
		t.Fatal("packet after clear")
	}
}

func TestLegacyCaptionStillPlainANC(t *testing.T) {
	bus := NewProgramBus()
	bus.SetCaption("LEGACY", "dir")
	pkts := ProgramBusToANC(bus)
	cap, ok := findANC(pkts, "caption")
	if !ok || string(cap.UDW) != "LEGACY" {
		t.Fatalf("%+v", cap)
	}
	if cap.UDW[0] == CaptionUDWMagic {
		t.Fatal("legacy must stay plain")
	}
}

func TestHandleCaptionCmdRich(t *testing.T) {
	m := NewModel(Options{Nick: "dir", Host: "127.0.0.1:0"})
	_, _ = m.handleConductorCmd("claim")
	_, _ = m.handleCaptionCmd("lang=en role=lower speaker=host Welcome")
	eff := m.program.EffectiveCaption()
	if eff.Text != "Welcome" || eff.Lang != "en" || eff.Speaker != "host" {
		t.Fatalf("%+v", eff)
	}
	_, _ = m.handleCaptionCmd("clear")
	if !m.program.EffectiveCaption().IsEmpty() {
		t.Fatal("clear")
	}
}

func TestFormatCaptionLine(t *testing.T) {
	s := FormatCaptionLine(CaptionPayload{Text: "Hi", Lang: "en", Role: "lower"})
	if !strings.Contains(s, "Hi") || !strings.Contains(s, "en") {
		t.Fatal(s)
	}
}
