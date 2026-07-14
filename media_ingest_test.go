package main

import (
	"strings"
	"testing"
)

func TestParseIngestScheme(t *testing.T) {
	cases := []struct {
		in, scheme, ref string
		ok              bool
	}{
		{"ndi:Studio Camera", "ndi", "Studio Camera", true},
		{"decklink:0", "decklink", "0", true},
		{"blackmagic:1", "decklink", "1", true},
		{"device:avfoundation:0", "device", "avfoundation:0", true},
		{"device:0", "device", "0", true},
		{"pgm:", "pgm", "", true},
		{"pgm:dojo", "pgm", "dojo", true},
		{"srt://10.0.0.5:9000", "srt", "srt://10.0.0.5:9000", true},
		{"rtmp://live/app/key", "rtmp", "rtmp://live/app/key", true},
		{"https://x.com/a", "", "", false},
		{"@user", "", "", false},
	}
	for _, c := range cases {
		sch, ref, ok := ParseIngestScheme(c.in)
		if ok != c.ok || sch != c.scheme {
			t.Fatalf("%q → %q %q %v want %q %v", c.in, sch, ref, ok, c.scheme, c.ok)
		}
		if ok && c.scheme != "srt" && c.scheme != "rtmp" && ref != c.ref {
			// srt ref is full URL
			if ref != c.ref {
				t.Fatalf("%q ref %q want %q", c.in, ref, c.ref)
			}
		}
	}
}

func TestIsIngestSource(t *testing.T) {
	if !IsIngestSource("ndi:Cam") || !IsIngestSource("decklink:0") {
		t.Fatal("expected true")
	}
	if IsIngestSource("https://youtube.com/watch?v=1") {
		t.Fatal("youtube not ingest scheme")
	}
}

func TestListIngestSourcesHasPGMAndBlackmagic(t *testing.T) {
	list := ListIngestSources()
	if len(list) < 2 {
		t.Fatal(len(list))
	}
	var pgm, bmd bool
	for _, s := range list {
		if s.Scheme == "pgm" {
			pgm = true
		}
		if s.Brand == "Blackmagic" || s.Scheme == "decklink" {
			bmd = true
		}
	}
	if !pgm || !bmd {
		t.Fatalf("pgm=%v bmd=%v list=%+v", pgm, bmd, list)
	}
}

func TestResolveIngestSRT(t *testing.T) {
	r, err := ResolveIngest("srt://127.0.0.1:9000")
	if err != nil {
		t.Fatal(err)
	}
	if r.Video != "srt://127.0.0.1:9000" || !strings.Contains(r.Via, "srt") {
		t.Fatalf("%+v", r)
	}
}

func TestResolveIngestPGMNoURL(t *testing.T) {
	t.Setenv("GY_PGM_PLAY_URL", "")
	r, err := ResolveIngest("pgm:")
	if err != nil {
		t.Fatal(err)
	}
	if r.Via != "ingest-pgm" {
		t.Fatal(r.Via)
	}
}

func TestResolveIngestPGMWithURL(t *testing.T) {
	t.Setenv("GY_PGM_PLAY_URL", "http://127.0.0.1:9876/api/media/play/x")
	r, err := ResolveIngest("pgm:dojo")
	if err != nil {
		t.Fatal(err)
	}
	if r.Video == "" {
		t.Fatal("expected play url")
	}
}

func TestNormalizeDeviceRef(t *testing.T) {
	d := normalizeDeviceRef("0")
	if d == "" || !strings.Contains(d, ":") {
		t.Fatal(d)
	}
}

func TestRegexpDeviceLine(t *testing.T) {
	m := regexpDeviceLine.FindStringSubmatch(`[AVFoundation indev @ 0x1] [0] FaceTime HD Camera (Built-in)`)
	if len(m) != 3 || m[1] != "0" || !strings.Contains(m[2], "FaceTime") {
		t.Fatalf("%v", m)
	}
}

func TestThreeCamSourcesShape(t *testing.T) {
	// may be empty in CI without cameras; just ensure no panic
	_ = ThreeCamSources()
}
