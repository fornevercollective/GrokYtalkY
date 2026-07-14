package main

import (
	"strings"
	"testing"
)

func TestParseXRIngest(t *testing.T) {
	cases := []struct {
		in, scheme string
		ok         bool
	}{
		{"xr:quest", "xr", true},
		{"xr:vision", "xr", true},
		{"webxr:", "webxr", true},
		{"stereo:sbs:device:0", "stereo", true},
		{"stereo:ou:srt://x", "stereo", true},
		{"quest:", "xr", true},
		{"https://x.com/a", "", false},
	}
	for _, c := range cases {
		sch, _, ok := ParseXRIngest(c.in)
		if ok != c.ok || (ok && sch != c.scheme) {
			t.Fatalf("%q → %q %v want %q %v", c.in, sch, ok, c.scheme, c.ok)
		}
	}
}

func TestClassifyXRLabel(t *testing.T) {
	if b := ClassifyXRLabel("Meta Quest 3"); b == nil || b.ID != "quest" {
		t.Fatalf("%v", b)
	}
	if b := ClassifyXRLabel("Apple Vision Pro"); b == nil || b.ID != "vision" {
		t.Fatalf("%v", b)
	}
	if b := ClassifyXRLabel("XREAL Air 2"); b == nil || b.ID != "xreal" {
		t.Fatalf("%v", b)
	}
	if ClassifyXRLabel("FaceTime HD Camera") != nil {
		t.Fatal("facetime is not XR")
	}
}

func TestListXRSourcesNonEmpty(t *testing.T) {
	list := ListXRSources()
	if len(list) < 5 {
		t.Fatal(len(list))
	}
	var hasQuest bool
	for _, s := range list {
		if strings.Contains(strings.ToLower(s.Label), "quest") {
			hasQuest = true
		}
	}
	if !hasQuest {
		t.Fatal("expected Meta Quest in catalog")
	}
}

func TestIsIngestSourceXR(t *testing.T) {
	if !IsIngestSource("xr:quest") || !IsIngestSource("stereo:sbs:device:0") {
		t.Fatal("xr should be ingest")
	}
}

func TestResolveXRWebXR(t *testing.T) {
	r, err := ResolveXR("webxr:")
	if err != nil || r.Via != "ingest-webxr" {
		t.Fatal(err, r)
	}
}

func TestResolveStereoNeedsSource(t *testing.T) {
	_, err := ResolveXR("stereo:sbs:")
	if err == nil {
		t.Fatal("expected error for empty stereo source")
	}
}
