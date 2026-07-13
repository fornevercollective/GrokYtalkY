package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGlyphLumToRGB24LatticePreserved(t *testing.T) {
	n := 25
	lum := make([]byte, n*n)
	for i := range lum {
		lum[i] = 80
	}
	// stamp-like corner
	lum[0] = 200
	lum[1] = 40
	rgb := glyphLumToRGB24(lum, n, 100, 100)
	if len(rgb) != 100*100*3 {
		t.Fatalf("len %d", len(rgb))
	}
	// top-left block should be 200
	if rgb[0] != 200 || rgb[1] != 200 {
		t.Fatalf("corner %d %d", rgb[0], rgb[1])
	}
}

func TestBuildVenueSinkLog(t *testing.T) {
	s, err := BuildVenueSink(VenueOpts{SinkKind: "log", Quiet: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "log-stub" {
		t.Fatal(s.Name())
	}
}

func TestBuildVenueSinkNDI(t *testing.T) {
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "ndi", Quiet: true,
		NDIName: "Test-PGM", Width: 64, Height: 64, FPS: 10,
		NDIFallback: "udp://127.0.0.1:19999",
	})
	if err != nil {
		t.Fatal(err)
	}
	// name is ndi or ndi-fallback-mpegts
	n := s.Name()
	if n != "ndi" && n != "ndi-fallback-mpegts" {
		t.Fatal(n)
	}
	_ = s.Close()
}

func TestBuildVenueSinkST2110(t *testing.T) {
	dir := t.TempDir()
	sdp := filepath.Join(dir, "t.sdp")
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "st2110", Quiet: true,
		RTP: "rtp://127.0.0.1:5004", SDPPath: sdp,
		Width: 64, Height: 36, FPS: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "st2110" {
		t.Fatal(s.Name())
	}
	b, err := os.ReadFile(sdp)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 20 || string(b[0:2]) != "v=" {
		t.Fatalf("sdp %q", b[:min(40, len(b))])
	}
	_ = s.Close()
}

func TestBuildVenueSinkMulti(t *testing.T) {
	dir := t.TempDir()
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "log,st2110", Quiet: true,
		RTP: "rtp://127.0.0.1:5005", SDPPath: filepath.Join(dir, "m.sdp"),
		Width: 32, Height: 18, FPS: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "log-stub+st2110" {
		t.Fatal(s.Name())
	}
	_ = s.Close()
}

func TestNewVenueSinkUnknown(t *testing.T) {
	_, err := NewVenueSink("nope", VenueOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseRTPURL(t *testing.T) {
	h, p, err := parseRTPURL("rtp://239.1.1.1:5004")
	if err != nil || h != "239.1.1.1" || p != 5004 {
		t.Fatal(h, p, err)
	}
}

func TestNDIOnGlyphWithoutFFmpegSoft(t *testing.T) {
	// ensureStarted fails soft if no ffmpeg — OnGlyph must not panic
	s, err := NewNDIVenueSink(NDIOpts{
		Name: "T", Width: 32, Height: 32, FPS: 5, Quiet: true,
		FallbackUDP: "udp://127.0.0.1:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.OnProgram(NewProgramBus())
	s.OnGlyph(VenueGlyphFrame{N: 4, Data: make([]byte, 16), OnAir: true})
	s.OnBlack(NewProgramBus())
	s.OnHold(NewProgramBus())
}

func TestST2110OnGlyphSoft(t *testing.T) {
	dir := t.TempDir()
	s, err := NewST2110VenueSink(ST2110Opts{
		RTP: "rtp://127.0.0.1:5006", SDPPath: filepath.Join(dir, "x.sdp"),
		Width: 32, Height: 18, FPS: 5, Quiet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bus := NewProgramBus()
	mk := NewForgeMark(1, "a.pcap", []byte("v"))
	bus.Take(SourceFromForge("p", &mk, LaneGlyph), "c")
	s.OnProgram(bus)
	lum := make([]byte, 25*25)
	StampHexLum(lum, 25, mk)
	s.OnGlyph(VenueGlyphFrame{N: 25, Data: lum, Mark: mk.ID, OnAir: true})
}
