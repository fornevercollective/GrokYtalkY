package main

import (
	"encoding/json"
	"sync"
	"testing"
)

// recordSink captures VenueSink calls for tests.
type recordSink struct {
	mu     sync.Mutex
	progs  []ProgramBus
	glyphs []VenueGlyphFrame
	blacks int
	holds  int
}

func (r *recordSink) Name() string { return "record" }
func (r *recordSink) OnProgram(b ProgramBus) {
	r.mu.Lock()
	r.progs = append(r.progs, b)
	r.mu.Unlock()
}
func (r *recordSink) OnGlyph(f VenueGlyphFrame) {
	r.mu.Lock()
	r.glyphs = append(r.glyphs, f)
	r.mu.Unlock()
}
func (r *recordSink) OnBlack(ProgramBus) {
	r.mu.Lock()
	r.blacks++
	r.mu.Unlock()
}
func (r *recordSink) OnHold(ProgramBus) {
	r.mu.Lock()
	r.holds++
	r.mu.Unlock()
}
func (r *recordSink) Close() error { return nil }

func TestVenueOnAirFilter(t *testing.T) {
	bus := NewProgramBus()
	mk := NewForgeMark(1, "dojo.pcap", []byte("v"))
	bus.Take(SourceFromForge("forger", &mk, LaneGlyph), "dir")

	if !venueOnAir(bus, "forger", mk.ID, 1) {
		t.Fatal("on air mark")
	}
	if venueOnAir(bus, "other", "cgf:ffffffffffffffff", 9) {
		t.Fatal("off air")
	}
	bus.Black("dir")
	if venueOnAir(bus, "forger", mk.ID, 1) {
		t.Fatal("black")
	}
}

func TestVenueRuntimeProgramAndGlyph(t *testing.T) {
	sink := &recordSink{}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}

	mk := NewForgeMark(2, "dojo.pcap", []byte("lat"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("forger", &mk, LaneGlyph), "dir")
	raw, _ := json.Marshal(bus.MeshJSON("dir"))
	rt.handleHubRaw(raw)

	sink.mu.Lock()
	if len(sink.progs) != 1 || sink.progs[0].Program.Mark != mk.ID {
		t.Fatalf("progs %+v", sink.progs)
	}
	sink.mu.Unlock()

	// stamped hexlum from program nick
	n := 13
	lum := make([]byte, n*n)
	StampHexLum(lum, n, mk)
	corner := lum[0]
	mesh := PacketToMesh(PacketFromHexLum(lum, n, 1), "forger")
	mesh["mark"] = mk.ID
	mesh["slot"] = float64(2)
	graw, _ := json.Marshal(mesh)
	rt.handleHubRaw(graw)

	sink.mu.Lock()
	if len(sink.glyphs) != 1 {
		t.Fatalf("glyphs %d", len(sink.glyphs))
	}
	g := sink.glyphs[0]
	sink.mu.Unlock()
	if g.N != n || g.Data[0] != corner {
		t.Fatalf("lattice lost n=%d d0=%d want %d", g.N, g.Data[0], corner)
	}
	if !g.OnAir {
		t.Fatal("on_air")
	}

	// off-air publisher ignored
	mesh2 := PacketToMesh(PacketFromHexLum(lum, n, 2), "nobody")
	graw2, _ := json.Marshal(mesh2)
	rt.handleHubRaw(graw2)
	sink.mu.Lock()
	if len(sink.glyphs) != 1 {
		t.Fatalf("should ignore off-air got %d", len(sink.glyphs))
	}
	sink.mu.Unlock()

	// black
	bus.Black("dir")
	braw, _ := json.Marshal(bus.MeshJSON("dir"))
	rt.handleHubRaw(braw)
	sink.mu.Lock()
	if sink.blacks != 1 {
		t.Fatal(sink.blacks)
	}
	sink.mu.Unlock()

	// glyph while black dropped
	rt.handleHubRaw(graw)
	sink.mu.Lock()
	if len(sink.glyphs) != 1 {
		t.Fatal("glyph during black")
	}
	sink.mu.Unlock()
}

func TestVenueHoldRedeliversLast(t *testing.T) {
	sink := &recordSink{}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}
	mk := NewForgeMark(1, "a.pcap", []byte("h"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("p", &mk, LaneGlyph), "c")
	raw, _ := json.Marshal(bus.MeshJSON("c"))
	rt.handleHubRaw(raw)

	lum := make([]byte, 25*25)
	StampHexLum(lum, 25, mk)
	mesh := PacketToMesh(PacketFromHexLum(lum, 25, 1), "p")
	mesh["mark"] = mk.ID
	graw, _ := json.Marshal(mesh)
	rt.handleHubRaw(graw)

	bus.Hold("c")
	hraw, _ := json.Marshal(bus.MeshJSON("c"))
	rt.handleHubRaw(hraw)

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.holds != 1 {
		t.Fatal(sink.holds)
	}
	// hold redelivers last glyph
	if len(sink.glyphs) < 2 {
		t.Fatalf("glyphs %d want redelivery", len(sink.glyphs))
	}
	if sink.glyphs[len(sink.glyphs)-1].Mode != ProgramModeHold {
		t.Fatal(sink.glyphs[len(sink.glyphs)-1].Mode)
	}
}

func TestVenueOnAirOpenSlate(t *testing.T) {
	bus := NewProgramBus() // slate, no nick/mark
	if !venueOnAir(bus, "anyone", "", 0) {
		t.Fatal("open slate should accept live glyph")
	}
}

func TestNewVenueSinkFallback(t *testing.T) {
	s := NewVenueSink("ndi", false, true)
	if s.Name() != "log-stub" {
		t.Fatal(s.Name())
	}
}
