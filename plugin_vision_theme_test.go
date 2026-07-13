package main

import (
	"strings"
	"testing"
)

func TestThemeVisionPluginRegistered(t *testing.T) {
	r := Plugins()
	p := r.Get("theme-vision")
	if p == nil {
		t.Fatal("theme-vision missing")
	}
	if !p.Enabled() {
		t.Fatal("should be on by default")
	}
	if p.Style() == nil || p.Style().Name() != "theme-vision" {
		t.Fatal(p.Style())
	}
	vh, ok := p.(interface{ VisionHook() VisionHook })
	if !ok || vh.VisionHook() == nil {
		t.Fatal("VisionHook")
	}
	doc := r.FormatPluginList()
	if !strings.Contains(doc, "theme-vision") || !strings.Contains(doc, "vision") {
		t.Fatal(doc)
	}
}

func TestThemeToPixelMode(t *testing.T) {
	cases := map[string]PixelMode{
		"breaking": PixelScan,
		"markets":  PixelHex,
		"conflict": PixelNeon,
		"weather":  PixelDither,
		"earthcam": PixelNeon,
	}
	for th, want := range cases {
		got, ok := ThemeToPixelMode(th)
		if !ok || got != want {
			t.Fatalf("%s → %v want %v", th, got, want)
		}
	}
}

func TestApplyThemeGradeMarkets(t *testing.T) {
	f := &FramePixels{W: 4, H: 2, RGB: make([]byte, 4*2*3)}
	for i := range f.RGB {
		f.RGB[i] = 100
	}
	applyThemeGrade(f, "markets")
	// green channel should dominate after markets grade
	if f.RGB[1] <= f.RGB[0] {
		t.Fatalf("expected green bias %v", f.RGB[:3])
	}
}

func TestThemeReactiveStylePreprocess(t *testing.T) {
	p := ThemeVision()
	p.SetEnabled(true)
	p.OnVision(VisionEvent{Type: "vision-take", Theme: "conflict", Feed: "CNN"})
	if p.Theme() != "conflict" {
		t.Fatal(p.Theme())
	}
	sp := p.Style()
	f := &FramePixels{W: 8, H: 4, RGB: make([]byte, 8*4*3), Source: "news:CNN"}
	for i := range f.RGB {
		f.RGB[i] = 80
	}
	// stamp bus theme for feed
	Vision().mu.Lock()
	Vision().themes["CNN"] = "conflict"
	Vision().mu.Unlock()
	sp.Preprocess(f, StyleGeom{})
	// conflict boosts red
	if f.RGB[0] < 80 {
		t.Fatalf("red should rise %v", f.RGB[:3])
	}
}

func TestApplyThemeVisionPlugin(t *testing.T) {
	p := ThemeVision()
	p.SetEnabled(true)
	p.mu.Lock()
	p.autoStyle = true
	p.autoPixel = true
	p.mu.Unlock()

	m := &Model{
		lab: &LabState{
			On: true, Active: 0,
			Feeds: []FeedSlot{{Kind: "news", Label: "BBC"}, {Kind: "news", Label: "CNN"}},
		},
	}
	take := GrokTake{Vision: true, Theme: "markets"} // no STYLE → pixel hex
	got := ApplyThemeVisionPlugin(m, take)
	if len(got) == 0 {
		t.Fatal("expected applied")
	}
	if m.lab.PluginStyle != "theme-vision" {
		t.Fatal(m.lab.PluginStyle)
	}
	if m.lab.Feeds[0].PluginStyle != "theme-vision" {
		t.Fatal(m.lab.Feeds[0].PluginStyle)
	}
	if m.pixelMode != PixelHex {
		t.Fatalf("pixel %v", m.pixelMode)
	}
	// explicit STYLE wins for pixel
	m2 := &Model{lab: &LabState{On: true, Feeds: []FeedSlot{{Kind: "news"}}}, pixelMode: PixelHalf}
	take2 := GrokTake{Vision: true, Theme: "breaking", Style: "neon"}
	// simulate style already applied
	if st, ok := ParsePixelStyleName(take2.Style); ok {
		m2.pixelMode = st
	}
	_ = ApplyThemeVisionPlugin(m2, take2)
	if m2.pixelMode != PixelNeon {
		t.Fatalf("should keep explicit style pixel %v", m2.pixelMode)
	}
	if m2.lab.PluginStyle != "theme-vision" {
		t.Fatal("plugin style still set")
	}
}

func TestRenderFrameNamedThemeVision(t *testing.T) {
	p := ThemeVision()
	p.SetEnabled(true)
	p.OnVision(VisionEvent{Type: "vision-take", Theme: "science"})
	f := &FramePixels{W: 16, H: 10, RGB: make([]byte, 16*10*3)}
	for i := range f.RGB {
		f.RGB[i] = byte(i % 180)
	}
	s := RenderFrameNamed(f, "theme-vision", PixelHalf, 8, 4)
	if s == "" || strings.Contains(s, "no video") {
		t.Fatal(s)
	}
}

func TestFormatThemeVisionDoctor(t *testing.T) {
	doc := FormatThemeVisionDoctor()
	if !strings.Contains(doc, "theme-vision") || !strings.Contains(doc, "THEME") {
		t.Fatal(doc)
	}
}

func TestThemeGradeForKnown(t *testing.T) {
	for _, th := range []string{"breaking", "markets", "weather", "unsorted"} {
		g := ThemeGradeFor(th)
		if g.R == 0 && g.G == 0 {
			t.Fatal(th)
		}
	}
}
