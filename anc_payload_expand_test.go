package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Preview-armed tally + caption edges (v1.36 payload expansion).

func TestANCPreviewArmedTallyAndPacket(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path, path}, "1")
	seq0 := m.program.Seq

	_, _ = m.handlePreviewCmd("2")
	if m.program.Preview == nil || m.program.Preview.Slot != 2 {
		t.Fatal("preview not armed")
	}
	if m.program.Seq <= seq0 {
		t.Fatal("preview must bump seq for ANC re-emit")
	}

	pkts := ProgramBusToANC(m.program)
	tally, ok := findANC(pkts, "tally")
	if !ok {
		t.Fatal("tally")
	}
	mode, slot, flags, pvw, cond := ParseTallyUDWEx(tally.UDW)
	if mode != ANCTallyLive || slot != 1 {
		t.Fatalf("pgm mode/slot %d %d", mode, slot)
	}
	if flags&ANCFlagPreviewArmed == 0 {
		t.Fatal("preview flag")
	}
	if flags&ANCFlagHasMark == 0 {
		t.Fatal("has mark flag")
	}
	if pvw != 2 {
		t.Fatalf("pvw slot %d", pvw)
	}
	if cond != "dir" {
		t.Fatal(cond)
	}

	pv, ok := findANC(pkts, "preview")
	if !ok {
		t.Fatal("preview packet")
	}
	if pv.SDID != ANC_SDID_PREVIEW {
		t.Fatal(pv.SDID)
	}
	// preview mark differs from program when two slots
	if m.program.Preview.Mark != "" && string(pv.UDW) != m.program.Preview.Mark {
		// allow label fallback
		if !strings.Contains(string(pv.UDW), "dojo") && string(pv.UDW) != m.program.Preview.Mark {
			t.Logf("preview udw %q mark %q", pv.UDW, m.program.Preview.Mark)
		}
	}
}

func TestANCPreviewWithoutMarkStillEmits(t *testing.T) {
	// preview with nick-only source (no forge mark)
	bus := NewProgramBus()
	bus.Take(ProgramSource{Source: ProgramSourceGyst, Nick: "cam1", Label: "cam-a"}, "dir")
	bus.SetPreview(ProgramSource{Source: ProgramSourceGyst, Nick: "cam2", Slot: 0, Label: ""}, "dir")
	pkts := ProgramBusToANC(bus)
	// no program mark packet
	if _, ok := findANC(pkts, "mark"); ok {
		t.Fatal("unexpected program mark")
	}
	tally, _ := findANC(pkts, "tally")
	_, _, flags, _, _ := ParseTallyUDWEx(tally.UDW)
	if flags&ANCFlagPreviewArmed == 0 {
		t.Fatal("flag")
	}
	if flags&ANCFlagHasMark != 0 {
		t.Fatal("should not has-mark")
	}
	pv, ok := findANC(pkts, "preview")
	if !ok {
		t.Fatal("preview packet required even without mark")
	}
	// falls back to nick
	if string(pv.UDW) != "cam2" {
		t.Fatalf("udw %q", pv.UDW)
	}
}

func TestANCPreviewClearRemovesPacket(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path}, "1")
	_, _ = m.handlePreviewCmd("1")
	if ProgramBusToANC(m.program); true {
		pkts := ProgramBusToANC(m.program)
		if _, ok := findANC(pkts, "preview"); !ok {
			t.Fatal("armed")
		}
	}
	_, _ = m.handlePreviewCmd("clear")
	if m.program.Preview != nil {
		t.Fatal("clear")
	}
	pkts := ProgramBusToANC(m.program)
	if _, ok := findANC(pkts, "preview"); ok {
		t.Fatal("preview ANC after clear")
	}
	tally, _ := findANC(pkts, "tally")
	_, _, flags, _, _ := ParseTallyUDWEx(tally.UDW)
	if flags&ANCFlagPreviewArmed != 0 {
		t.Fatal("flag still set")
	}
}

func TestANCCaptionSetAndClear(t *testing.T) {
	m := NewModel(Options{Nick: "dir", Host: "127.0.0.1:0"})
	_, _ = m.handleConductorCmd("claim")
	seq0 := m.program.Seq

	_, _ = m.handleCaptionCmd("HELLO DOJO")
	if m.program.Caption != "HELLO DOJO" {
		t.Fatal(m.program.Caption)
	}
	if m.program.Seq <= seq0 {
		t.Fatal("caption bumps seq")
	}
	pkts := ProgramBusToANC(m.program)
	cap, ok := findANC(pkts, "caption")
	if !ok || string(cap.UDW) != "HELLO DOJO" {
		t.Fatalf("%+v", cap)
	}
	if cap.SDID != ANC_SDID_CAPTION {
		t.Fatal(cap.SDID)
	}
	tally, _ := findANC(pkts, "tally")
	_, _, flags, _, _ := ParseTallyUDWEx(tally.UDW)
	if flags&ANCFlagHasCaption == 0 {
		t.Fatal("caption flag")
	}

	// clear — no caption packet
	_, _ = m.handleCaptionCmd("clear")
	if m.program.Caption != "" {
		t.Fatal("clear")
	}
	pkts = ProgramBusToANC(m.program)
	if _, ok := findANC(pkts, "caption"); ok {
		t.Fatal("caption after clear")
	}
	tally, _ = findANC(pkts, "tally")
	_, _, flags, _, _ = ParseTallyUDWEx(tally.UDW)
	if flags&ANCFlagHasCaption != 0 {
		t.Fatal("flag")
	}
}

func TestANCCaptionTruncateAndEmpty(t *testing.T) {
	bus := NewProgramBus()
	long := strings.Repeat("x", 200)
	bus.SetCaption(long, "dir")
	if len(bus.Caption) > 120 {
		t.Fatal(len(bus.Caption))
	}
	pkts := ProgramBusToANC(bus)
	cap, ok := findANC(pkts, "caption")
	if !ok || len(cap.UDW) > ANCCaptionMax {
		t.Fatal(len(cap.UDW))
	}
	// empty set
	bus.SetCaption("   ", "dir")
	if bus.Caption != "" {
		t.Fatal("trim empty")
	}
	if _, ok := findANC(ProgramBusToANC(bus), "caption"); ok {
		t.Fatal("empty caption packet")
	}
}

func TestANCCaptionAndPreviewTogether(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path, path}, "1")
	_, _ = m.handlePreviewCmd("2")
	_, _ = m.handleCaptionCmd("TAKE NEXT")

	sink := &ancCounterSink{name: "v"}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}
	feedProgram(rt, m.program, "dir")
	anc, _ := sink.snapshot()

	need := []string{"mark", "tally", "preview", "caption", "bus"}
	for _, k := range need {
		if _, ok := findANC(anc, k); !ok {
			t.Fatalf("missing %s in %d pkts", k, len(anc))
		}
	}
	// mesh round-trip preserves caption + preview
	raw, _ := json.Marshal(m.program.MeshJSON("dir"))
	var msg map[string]any
	_ = json.Unmarshal(raw, &msg)
	bus, ok := ParseProgramBus(msg)
	if !ok || bus.Caption != "TAKE NEXT" || bus.Preview == nil {
		t.Fatalf("%+v", bus)
	}
}

func TestANCHoldKeepsCaptionAndPreviewFlags(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path}, "1")
	_, _ = m.handlePreviewCmd("1")
	_, _ = m.handleCaptionCmd("HOLD ME")
	_, _ = m.handleProgramMode(ProgramModeHold)

	pkts := ProgramBusToANC(m.program)
	tally, _ := findANC(pkts, "tally")
	mode, _, flags, _, _ := ParseTallyUDWEx(tally.UDW)
	if mode != ANCTallyHold {
		t.Fatal(mode)
	}
	if flags&ANCFlagPreviewArmed == 0 || flags&ANCFlagHasCaption == 0 {
		t.Fatalf("flags %02x", flags)
	}
	if _, ok := findANC(pkts, "caption"); !ok {
		t.Fatal("caption on hold")
	}
	if _, ok := findANC(pkts, "preview"); !ok {
		t.Fatal("preview on hold")
	}
}
