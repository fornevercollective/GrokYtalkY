package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── helpers ──────────────────────────────────────────────────

type ancCounterSink struct {
	mu    sync.Mutex
	anc   []ANCPacket
	progs int
	name  string
}

func (s *ancCounterSink) Name() string { return s.name }
func (s *ancCounterSink) OnProgram(ProgramBus) {
	s.mu.Lock()
	s.progs++
	s.mu.Unlock()
}
func (s *ancCounterSink) OnGlyph(VenueGlyphFrame) {}
func (s *ancCounterSink) OnBlack(ProgramBus)      {}
func (s *ancCounterSink) OnHold(ProgramBus)       {}
func (s *ancCounterSink) OnANC(pkts []ANCPacket) {
	s.mu.Lock()
	s.anc = append(s.anc, pkts...)
	s.mu.Unlock()
}
func (s *ancCounterSink) Close() error { return nil }

func (s *ancCounterSink) snapshot() (anc []ANCPacket, progs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	anc = append([]ANCPacket(nil), s.anc...)
	return anc, s.progs
}

func (s *ancCounterSink) reset() {
	s.mu.Lock()
	s.anc = nil
	s.progs = 0
	s.mu.Unlock()
}

func findANC(pkts []ANCPacket, kind string) (ANCPacket, bool) {
	for _, p := range pkts {
		if p.Kind == kind {
			return p, true
		}
	}
	return ANCPacket{}, false
}

func feedProgram(rt *VenueRuntime, bus ProgramBus, from string) {
	raw, _ := json.Marshal(bus.MeshJSON(from))
	rt.handleHubRaw(raw)
}

// conductorClaimTake builds model with forge slots, claims, takes slot.
func conductorClaimTake(t *testing.T, paths []string, takeArg string) *Model {
	t.Helper()
	m := NewModel(Options{Nick: "dir", Host: "127.0.0.1:0"})
	if len(paths) > 0 {
		_, _ = m.startMultiPcapForge(paths)
	}
	_, _ = m.handleConductorCmd("claim")
	if !m.conductor {
		t.Fatal("claim failed")
	}
	if takeArg != "" {
		_, _ = m.handleTakeCmd(takeArg)
	}
	return m
}

// ── 1. claim → take → mark/tally match ───────────────────────

func TestIntegrationClaimTakeANCMarkTally(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path, path}, "1")

	if m.program.Program.Mark == "" {
		t.Fatal("take should set forge mark")
	}
	if m.program.Program.Slot != 1 {
		t.Fatalf("slot %d", m.program.Program.Slot)
	}
	if m.program.Seq < 1 {
		t.Fatal("seq")
	}

	// venue sees hub program envelope
	sink := &ancCounterSink{name: "v"}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}
	feedProgram(rt, m.program, m.nick)

	anc, progs := sink.snapshot()
	if progs != 1 {
		t.Fatalf("OnProgram count %d", progs)
	}
	mark, ok := findANC(anc, "mark")
	if !ok {
		t.Fatalf("no mark ANC in %+v", anc)
	}
	if string(mark.UDW) != m.program.Program.Mark {
		t.Fatalf("mark UDW %q want %q", mark.UDW, m.program.Program.Mark)
	}
	if mark.SDID != ANC_SDID_MARK || mark.DID != ANC_DID_GY {
		t.Fatal(mark.DID, mark.SDID)
	}
	if mark.Seq != m.program.Seq {
		t.Fatalf("mark seq %d bus %d", mark.Seq, m.program.Seq)
	}

	tally, ok := findANC(anc, "tally")
	if !ok {
		t.Fatal("no tally")
	}
	mode, slot, flags, cond := ParseTallyUDW(tally.UDW)
	if mode != ANCTallyLive {
		t.Fatalf("mode %d", mode)
	}
	if slot != 1 {
		t.Fatalf("slot %d", slot)
	}
	if flags&1 == 0 {
		t.Fatal("flag has-mark")
	}
	if cond != "dir" {
		t.Fatalf("conductor %q", cond)
	}
	if _, ok := findANC(anc, "bus"); !ok {
		t.Fatal("no bus snapshot ANC")
	}
}

// ── 2. hold / black flips tally mode ─────────────────────────

func TestIntegrationHoldBlackTallyFlip(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path}, "1")
	markID := m.program.Program.Mark

	sink := &ancCounterSink{name: "v"}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}

	// live take
	feedProgram(rt, m.program, "dir")
	anc, _ := sink.snapshot()
	tally, _ := findANC(anc, "tally")
	mode, _, _, _ := ParseTallyUDW(tally.UDW)
	if mode != ANCTallyLive {
		t.Fatal(mode)
	}

	// hold
	sink.reset()
	_, _ = m.handleProgramMode(ProgramModeHold)
	feedProgram(rt, m.program, "dir")
	anc, _ = sink.snapshot()
	tally, ok := findANC(anc, "tally")
	if !ok {
		t.Fatal("hold tally")
	}
	mode, _, _, _ = ParseTallyUDW(tally.UDW)
	if mode != ANCTallyHold {
		t.Fatalf("hold mode %d", mode)
	}
	// mark preserved on hold
	if m.program.Program.Mark != markID {
		t.Fatal("mark lost on hold")
	}
	if mk, ok := findANC(anc, "mark"); ok && string(mk.UDW) != markID {
		t.Fatal("hold mark mismatch")
	}

	// black
	sink.reset()
	_, _ = m.handleProgramMode(ProgramModeBlack)
	feedProgram(rt, m.program, "dir")
	anc, _ = sink.snapshot()
	tally, ok = findANC(anc, "tally")
	if !ok {
		t.Fatal("black tally")
	}
	mode, _, _, _ = ParseTallyUDW(tally.UDW)
	if mode != ANCTallyBlack {
		t.Fatalf("black mode %d", mode)
	}
}

// ── 3. monotonic seq · stale program ignored ─────────────────

func TestIntegrationStaleSeqNoANCSpam(t *testing.T) {
	sink := &ancCounterSink{name: "v"}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}

	mk := NewForgeMark(1, "a.pcap", []byte("s"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	// force known seq
	bus.Seq = 5
	feedProgram(rt, bus, "dir")
	anc1, _ := sink.snapshot()
	n1 := len(anc1)
	if n1 < 2 {
		t.Fatal(n1)
	}

	// stale lower seq — must not emit
	sink.reset()
	stale := bus
	stale.Seq = 3
	stale.Mode = ProgramModeBlack // would change tally if applied
	feedProgram(rt, stale, "dir")
	anc2, progs := sink.snapshot()
	if len(anc2) != 0 || progs != 0 {
		t.Fatalf("stale applied anc=%d progs=%d", len(anc2), progs)
	}
	// runtime bus still seq 5
	rt.mu.Lock()
	if rt.bus.Seq != 5 {
		t.Fatalf("bus seq %d", rt.bus.Seq)
	}
	if rt.bus.Mode == ProgramModeBlack {
		t.Fatal("stale black applied")
	}
	rt.mu.Unlock()

	// equal seq is allowed through (re-emit / refresh) — document behavior
	// higher seq must apply
	sink.reset()
	next := bus
	next.Seq = 6
	next.Hold("dir")
	feedProgram(rt, next, "dir")
	anc3, _ := sink.snapshot()
	if len(anc3) < 2 {
		t.Fatal("seq 6 should emit")
	}
	tally, _ := findANC(anc3, "tally")
	mode, _, _, _ := ParseTallyUDW(tally.UDW)
	if mode != ANCTallyHold {
		t.Fatal(mode)
	}
}

// ── 4. multi-sink consistency ────────────────────────────────

func TestIntegrationMultiSinkANCConsistency(t *testing.T) {
	a := &ancCounterSink{name: "a"}
	b := &ancCounterSink{name: "b"}
	multi := &multiVenueSink{sinks: []VenueSink{a, b}}
	rt := &VenueRuntime{sink: multi, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}

	mk := NewForgeMark(3, "dojo.pcap", []byte("m"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	feedProgram(rt, bus, "dir")

	aa, _ := a.snapshot()
	bb, _ := b.snapshot()
	if len(aa) != len(bb) || len(aa) < 3 {
		t.Fatalf("a=%d b=%d", len(aa), len(bb))
	}
	for i := range aa {
		if aa[i].Kind != bb[i].Kind || aa[i].SDID != bb[i].SDID || aa[i].Seq != bb[i].Seq {
			t.Fatalf("mismatch i=%d %+v vs %+v", i, aa[i], bb[i])
		}
		if string(aa[i].UDW) != string(bb[i].UDW) {
			t.Fatalf("udw i=%d", i)
		}
	}
}

// log + real st2110-40 both receive one emission set
func TestIntegrationLogAnd40SingleFire(t *testing.T) {
	dir := t.TempDir()
	counter := &ancCounterSink{name: "c"}
	anc40, err := NewST211040Sink(ST211040Opts{Quiet: true, MetaDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer anc40.Close()
	multi := &multiVenueSink{sinks: []VenueSink{counter, anc40}}
	rt := &VenueRuntime{sink: multi, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}

	mk := NewForgeMark(1, "x.pcap", []byte("1"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	feedProgram(rt, bus, "dir")

	// counter should see exactly one batch (3 packets), not double
	anc, progs := counter.snapshot()
	if progs != 1 {
		t.Fatalf("double OnProgram? progs=%d", progs)
	}
	// mark+tally+bus = 3
	if len(anc) != 3 {
		t.Fatalf("want 3 ANC got %d (double fire?)", len(anc))
	}
	// jsonl should have 3 lines
	path := filepath.Join(dir, "st2110-40-anc.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("jsonl lines %d want 3", lines)
	}
}

// ── 5. late-join / re-emit on next take ──────────────────────

func TestIntegrationLateJoinThenTakeReemit(t *testing.T) {
	// hub stores last program for late joiners — model claim/take produces bus
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path, path}, "1")
	firstSeq := m.program.Seq
	firstMark := m.program.Program.Mark

	// late venue starts empty, receives stored program (hub would push this)
	sink := &ancCounterSink{name: "late"}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}
	feedProgram(rt, m.program, "dir")
	anc1, _ := sink.snapshot()
	if len(anc1) < 2 {
		t.Fatal("late join program should emit ANC")
	}
	mk1, _ := findANC(anc1, "mark")
	if string(mk1.UDW) != firstMark {
		t.Fatal(string(mk1.UDW))
	}

	// next take different slot → new seq + re-emit
	sink.reset()
	_, _ = m.handleTakeCmd("2")
	if m.program.Seq <= firstSeq {
		t.Fatalf("seq %d not advanced from %d", m.program.Seq, firstSeq)
	}
	if m.program.Program.Mark == firstMark {
		// slots may share content hash path but slot is in commitment — IDs differ
		// if same path different slots, marks must differ
		t.Logf("marks %s vs %s", firstMark, m.program.Program.Mark)
	}
	if m.program.Program.Slot != 2 {
		t.Fatal(m.program.Program.Slot)
	}
	feedProgram(rt, m.program, "dir")
	anc2, _ := sink.snapshot()
	if len(anc2) < 2 {
		t.Fatal("re-take should re-emit ANC")
	}
	tally, _ := findANC(anc2, "tally")
	_, slot, _, _ := ParseTallyUDW(tally.UDW)
	if slot != 2 {
		t.Fatalf("tally slot %d", slot)
	}
	if tally.Seq != m.program.Seq {
		t.Fatal(tally.Seq)
	}
}

// ── 6. claim publishes program (even before take) ────────────

func TestIntegrationClaimEmitsProgramEnvelope(t *testing.T) {
	m := NewModel(Options{Nick: "dir", Host: "127.0.0.1:0"})
	_, _ = m.handleConductorCmd("claim")
	// claim calls publishProgramBus — bus has conductor, slate program
	if m.program.Conductor != "dir" {
		t.Fatal(m.program.Conductor)
	}
	raw, _ := json.Marshal(m.program.MeshJSON(m.nick))
	var msg map[string]any
	_ = json.Unmarshal(raw, &msg)
	if msg["type"] != "program" {
		t.Fatal(msg["type"])
	}
	bus, ok := ParseProgramBus(msg)
	if !ok {
		t.Fatal("parse")
	}
	// claim alone: slate may have no mark — still tally+bus ANC
	pkts := ProgramBusToANC(bus)
	if _, ok := findANC(pkts, "tally"); !ok {
		t.Fatal("claim slate should still produce tally ANC")
	}
}

// ── 7. end-to-end mesh JSON round-trip authority ─────────────

func TestIntegrationConductorPublishParseANC(t *testing.T) {
	path := dojoPcapPath(t)
	m := conductorClaimTake(t, []string{path}, "1")
	// publishProgramBus shape
	env := m.program.MeshJSON(m.nick)
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if json.Unmarshal(raw, &msg) != nil {
		t.Fatal("unmarshal")
	}
	bus, ok := ParseProgramBus(msg)
	if !ok {
		t.Fatal("ParseProgramBus")
	}
	if bus.Program.Mark != m.program.Program.Mark {
		t.Fatal("mark round-trip")
	}
	pkts := ProgramBusToANC(bus)
	mark, ok := findANC(pkts, "mark")
	if !ok || string(mark.UDW) != bus.Program.Mark {
		t.Fatal("ANC mark")
	}
}
