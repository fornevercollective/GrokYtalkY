package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseGrokTakeThemeMute(t *testing.T) {
	raw := `STYLE neon
CAPTION Markets surge on data
THEME markets
MUTE_HINT quiet
`
	take := ParseGrokTake(raw)
	if take.Theme != "markets" {
		t.Fatalf("theme %q", take.Theme)
	}
	if take.MuteHint != "quiet" {
		t.Fatalf("mute %q", take.MuteHint)
	}
	if !strings.Contains(take.TakeSummary(), "theme=markets") {
		t.Fatal(take.TakeSummary())
	}
}

func TestNormalizeThemeToken(t *testing.T) {
	if normalizeThemeToken("Breaking") != "breaking" {
		t.Fatal()
	}
	if normalizeThemeToken("scenic") != "earthcam" {
		t.Fatal()
	}
	if normalizeThemeToken("nope") != "unsorted" {
		t.Fatal(normalizeThemeToken("nope"))
	}
}

func TestFrameToJPEGBase64(t *testing.T) {
	f := &FramePixels{W: 40, H: 20, RGB: make([]byte, 40*20*3)}
	for i := range f.RGB {
		f.RGB[i] = byte(i % 200)
	}
	url, n, err := FrameToJPEGBase64(f, 320, 180, 72)
	if err != nil {
		t.Fatal(err)
	}
	if n < 50 || !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Fatalf("n=%d url=%s", n, url[:min(40, len(url))])
	}
	// tiny budget still works
	_, n2, err := FrameToJPEGBase64(f, 64, 36, 50)
	if err != nil || n2 < 20 {
		t.Fatal(err, n2)
	}
}

func TestVisionBusBackpressure(t *testing.T) {
	v := &VisionBus{cfg: VisionConfig{MaxInflight: 1, Interval: time.Hour}, themes: map[string]string{}}
	if !v.TryBegin() {
		t.Fatal("first should pass")
	}
	if v.TryBegin() {
		t.Fatal("second should drop (inflight)")
	}
	v.End()
	// still interval throttle
	v.lastAt = time.Now()
	if v.TryBegin() {
		t.Fatal("interval should drop")
	}
	if v.Snapshot().Drops < 2 {
		t.Fatal(v.Snapshot().Drops)
	}
}

func TestVisionDoctor(t *testing.T) {
	doc := FormatVisionDoctor(Vision())
	if !strings.Contains(doc, "vision") || !strings.Contains(doc, "GY_VISION") {
		t.Fatal(doc)
	}
}

func TestBuildVisionUserPrompt(t *testing.T) {
	u := BuildVisionUserPrompt(FeedOrchestrateContext{
		Mode: "news", Active: "Al Jazeera", Kind: "news", Live: true, NewsCount: 8,
	})
	if !strings.Contains(u, "Vision take") || !strings.Contains(u, "Al Jazeera") {
		t.Fatal(u)
	}
}
