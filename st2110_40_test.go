package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgramBusToANC(t *testing.T) {
	mk := NewForgeMark(2, "dojo.pcap", []byte("anc"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("dir", &mk, LaneGlyph), "dir")
	pkts := ProgramBusToANC(bus)
	if len(pkts) < 3 {
		t.Fatalf("want mark+tally+bus got %d", len(pkts))
	}
	var kinds []string
	for _, p := range pkts {
		kinds = append(kinds, p.Kind)
		if p.DID != ANC_DID_GY {
			t.Fatal(p.DID)
		}
		raw := p.Packed()
		if len(raw) < 4 {
			t.Fatal("packed")
		}
	}
	s := strings.Join(kinds, ",")
	if !strings.Contains(s, "mark") || !strings.Contains(s, "tally") || !strings.Contains(s, "bus") {
		t.Fatal(s)
	}
	// mark UDW is cgf id
	for _, p := range pkts {
		if p.Kind == "mark" && !strings.HasPrefix(string(p.UDW), "cgf:") {
			t.Fatal(string(p.UDW))
		}
		if p.Kind == "tally" {
			mode, slot, _, cond := ParseTallyUDW(p.UDW)
			if mode != ANCTallyLive || slot != 2 || cond != "dir" {
				t.Fatalf("tally %d %d %q", mode, slot, cond)
			}
		}
	}
}

func TestProgramBusToANCBlack(t *testing.T) {
	bus := NewProgramBus()
	bus.Black("dir")
	pkts := ProgramBusToANC(bus)
	for _, p := range pkts {
		if p.Kind == "tally" {
			mode, _, _, _ := ParseTallyUDW(p.UDW)
			if mode != ANCTallyBlack {
				t.Fatal(mode)
			}
		}
	}
}

func TestVenueRuntimeEmitsANC(t *testing.T) {
	sink := &recordSink{}
	rt := &VenueRuntime{sink: sink, bus: NewProgramBus(), opts: VenueOpts{Quiet: true}}
	mk := NewForgeMark(1, "a.pcap", []byte("x"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("p", &mk, LaneGlyph), "c")
	raw, _ := json.Marshal(bus.MeshJSON("c"))
	rt.handleHubRaw(raw)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.anc) < 2 {
		t.Fatalf("anc %d", len(sink.anc))
	}
}

func TestST211040SinkJSONL(t *testing.T) {
	dir := t.TempDir()
	s, err := NewST211040Sink(ST211040Opts{
		// no RTP — jsonl only
		Quiet: true, MetaDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	mk := NewForgeMark(1, "a.pcap", []byte("j"))
	bus := NewProgramBus()
	bus.Take(SourceFromForge("x", &mk, LaneGlyph), "c")
	s.OnProgram(bus)
	path := filepath.Join(dir, "st2110-40-anc.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "mark") {
		t.Fatal(string(b))
	}
}

func TestWriteST211040SDP(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "40.sdp")
	if err := WriteST211040SDP(p, "239.1.1.1", 5008, DefaultSyncClockReport()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"smpte291", "x-gy-profile:2110-40", "0x5F", "2110"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestBuildVenueSink40(t *testing.T) {
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "st2110-40", Quiet: true,
		AncRTP: "rtp://127.0.0.1:5008",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "st2110-40" {
		t.Fatal(s.Name())
	}
	_ = s.Close()
}

func TestEncodeANCPayload(t *testing.T) {
	pkts := ProgramBusToANC(NewProgramBus())
	raw := EncodeANCPayload(pkts)
	if len(raw) < 8 {
		t.Fatal(len(raw))
	}
}
