package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestST2110SuiteTableCovers30AndPTP(t *testing.T) {
	var ids []string
	for _, e := range ST2110SuiteTable() {
		ids = append(ids, e.ID)
	}
	s := strings.Join(ids, " ")
	for _, want := range []string{ST2110_10, ST2110_20, ST2110_30, ST2059_1, ST2059_2} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %v", want, ids)
		}
	}
}

func TestPTPDependencyBasis(t *testing.T) {
	s := FormatPTPDependencyBasis()
	for _, want := range []string{"ST 2059-2", "2110-30", "offset = 0", "facility GM", "blackburst"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestSyncClockReportGaps(t *testing.T) {
	r := DefaultSyncClockReport()
	if r.Compliant {
		t.Fatal("free-run must not claim compliant")
	}
	if r.PTP.Profile != PTPProfileST2059 {
		t.Fatal(r.PTP.Profile)
	}
	if len(r.Gaps) < 2 {
		t.Fatal(r.Gaps)
	}
	out := FormatSyncClockReport(r)
	if !strings.Contains(out, "free-run") {
		t.Fatal(out)
	}
}

func TestSyncClockLocked(t *testing.T) {
	r := SyncClockWithPTPLocked(127, 200, "eth0")
	if r.PTP.Mode != PTPLocked {
		t.Fatal(r.PTP.Mode)
	}
	if !r.Compliant {
		t.Fatal("200ns should be compliant")
	}
	r2 := SyncClockWithPTPLocked(127, 50000, "eth0")
	if r2.Compliant {
		t.Fatal("large offset not compliant")
	}
}

func TestCameraTetherMatrixMajors(t *testing.T) {
	mfrs := map[string]bool{}
	native2110 := 0
	for _, c := range CameraTetherMatrix() {
		mfrs[c.Mfr] = true
		if c.ST2110 == "native" {
			native2110++
		}
		if len(c.Tether) == 0 || c.GYPath == "" {
			t.Fatalf("incomplete %+v", c)
		}
	}
	for _, want := range []string{"Sony", "ARRI", "RED", "Blackmagic", "Canon", "Panasonic", "Grass Valley / Mirage"} {
		if !mfrs[want] {
			t.Fatalf("missing mfr %s", want)
		}
	}
	if native2110 < 2 {
		t.Fatalf("expected multiple native 2110 families, got %d", native2110)
	}
	text := FormatCameraTetherMatrix()
	if !strings.Contains(text, "Blackmagic") || !strings.Contains(text, "PTP dependency") {
		t.Fatal(text[:200])
	}
}

func TestST211030SDP(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "30.sdp")
	opts := ST211030Opts{
		Rate: 48000, Channels: 2, Depth: 24, PtimeMs: 1,
		Level: ST211030LevelA, ChannelOrder: "SMPTE2110.(ST)",
	}
	if err := writeST211030SDP(p, "239.1.1.1", 5006, opts); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{
		"L24/48000/2",
		"channel-order=SMPTE2110.(ST)",
		"x-gy-profile:2110-30",
		PTPProfileST2059,
		"ptime:1",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestST2110MultiEssenceSDP(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bundle.sdp")
	audio := ST211030Opts{Rate: 48000, Channels: 2, Depth: 24, PtimeMs: 1, Level: "A", ChannelOrder: "SMPTE2110.(ST)"}
	sync := DefaultSyncClockReport()
	if err := writeST2110MultiEssenceSDP(p, "239.1.1.1", 5004, "239.1.1.1", 5006, 1280, 720, 30, audio, sync); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"m=video", "m=audio", "raw/90000", "L24/48000/2", "group:FID", "2110-20,2110-30"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestBuildVenueSink211030(t *testing.T) {
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "st2110-30", Quiet: true,
		AudioRTP: "rtp://127.0.0.1:5006",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "st2110-30" {
		t.Fatal(s.Name())
	}
	_ = s.Close()
}

func TestST2110WithAudioCompanion(t *testing.T) {
	dir := t.TempDir()
	s, err := NewST2110VenueSink(ST2110Opts{
		RTP: "rtp://127.0.0.1:5004", AudioRTP: "rtp://127.0.0.1:5006",
		SDPPath: filepath.Join(dir, "v.sdp"), MultiSDP: filepath.Join(dir, "b.sdp"),
		Width: 64, Height: 36, FPS: 15, Quiet: true,
		Profile: ST2110Profile211020, MetaDir: dir,
		Sync: DefaultSyncClockReport(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// multi sink name
	if !strings.Contains(s.Name(), "st2110") {
		t.Fatal(s.Name())
	}
	b, err := os.ReadFile(filepath.Join(dir, "b.sdp"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "m=audio") {
		t.Fatal(string(b))
	}
}
