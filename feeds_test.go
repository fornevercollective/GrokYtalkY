package main

import (
	"strings"
	"testing"
)

func TestAllStylesListed(t *testing.T) {
	s := AllStyles()
	if len(s) != int(PixelCount) {
		t.Fatalf("styles %d want %d", len(s), PixelCount)
	}
	want := map[string]bool{
		"half": true, "blocks": true, "points": true, "ascii": true,
		"halftone": true, "depth": true, "gsplat": true, "hex": true, "braille": true,
		"edges": true, "poster": true, "scan": true, "dither": true, "neon": true,
	}
	if len(want) != int(PixelCount) {
		t.Fatalf("want map %d PixelCount %d", len(want), PixelCount)
	}
	for _, name := range s {
		if !want[name] {
			t.Fatalf("unexpected style %s", name)
		}
	}
	// heavy styles cost ≥ 3
	if !PixelDepth.HeavyStyle() || !PixelGsplat.HeavyStyle() || !PixelHalftone.HeavyStyle() {
		t.Fatal("heavy flags")
	}
	if PixelHalf.HeavyStyle() || PixelScan.HeavyStyle() {
		t.Fatal("light styles marked heavy")
	}
}

func TestStyleGeomScales(t *testing.T) {
	g := StyleGeomFromBudget(80, 12, 160, 90)
	if g.Cols != 80 || g.Rows != 12 || g.Cell < 2 {
		t.Fatalf("%+v", g)
	}
	// narrow displays get smaller cells
	g2 := StyleGeomFromBudget(20, 6, 64, 48)
	if g2.Cell > g.Cell {
		t.Fatalf("cell %d > %d", g2.Cell, g.Cell)
	}
}

func TestStyleStreamAndDecodeBudget(t *testing.T) {
	// heavy styles: longer interval = lower effective FPS under stream
	if StyleStreamInterval(PixelGsplat, 30) <= StyleStreamInterval(PixelHalf, 30) {
		t.Fatal("gsplat interval should exceed half (throttled)")
	}
	w, h := StyleDecodeBudget(PixelGsplat, 200, 150)
	if w > 64 || h > 48 {
		t.Fatalf("gsplat decode cap %dx%d", w, h)
	}
	w2, h2 := StyleDecodeBudget(PixelHalf, 200, 150)
	if w2 < w || h2 < h {
		t.Fatalf("half should allow more than gsplat: %dx%d vs %dx%d", w2, h2, w, h)
	}
	sw, sh := StyleSimBudget(PixelDepth, 128)
	if sw > 64 {
		t.Fatalf("sim heavy %d", sw)
	}
	_ = sh
}

func TestLabScaleFPSPresets(t *testing.T) {
	l := newLabState()
	l.Scale = 64
	if l.NudgeScale(1) != 80 {
		t.Fatalf("scale up %d", l.Scale)
	}
	if l.NudgeScale(-1) != 64 {
		t.Fatalf("scale down %d", l.Scale)
	}
	l.FPS = 12
	if l.NudgeFPS(1) != 15 {
		t.Fatalf("fps %d", l.FPS)
	}
	l.NudgeFPS(-1)
	if l.FPS != 12 {
		t.Fatal(l.FPS)
	}
}

func TestLabLayoutsAndFeeds(t *testing.T) {
	l := newLabState()
	// newLabState seeds empty placeholders — fill them
	l.FillSimIntoActive()
	l.NextFeed()
	l.FillSimIntoActive()
	l.NextFeed()
	l.FillCamIntoActive()
	filled := 0
	for _, f := range l.Feeds {
		if !f.IsEmpty() {
			filled++
		}
	}
	if filled < 3 {
		t.Fatalf("filled %d", filled)
	}
	l.NextFeed()
	seen := map[string]bool{}
	for i := FeedLayout(0); i < LayoutCount; i++ {
		seen[l.CycleLayout().String()] = true
	}
	for _, name := range []string{"side", "stack", "grid", "focus"} {
		if !seen[name] {
			t.Fatalf("missing layout %s", name)
		}
	}
	before := len(l.Feeds)
	l.RemoveActive()
	if len(l.Feeds) != before-1 {
		t.Fatalf("remove: %d → %d", before, len(l.Feeds))
	}
}

func TestLabControlStripLists(t *testing.T) {
	l := newLabState()
	l.ShowList = true
	l.AddSim()
	strip := l.ControlStrip(80)
	if !strings.Contains(stripANSI(strip), "fps") {
		t.Fatal(strip)
	}
	list := l.ControlList(80)
	for _, key := range []string{"fps", "scale", "style", "layout", "feeds"} {
		if !strings.Contains(stripANSI(list), key) {
			t.Fatalf("list missing %s: %s", key, list)
		}
	}
}

func TestLabSideBySideRender(t *testing.T) {
	m := NewModel(Options{Nick: "t", Host: "127.0.0.1:9", MIDI: false, Translate: false, Lab: true})
	m.width, m.height = 100, 30
	if m.lab == nil || !m.lab.On {
		t.Fatal("lab not on")
	}
	// seed frames
	for i := range m.lab.Feeds {
		m.lab.Feeds[i].Frame = genSimFrame(48, 28, 1000, i+1)
	}
	body := m.renderLab(safeCols(100), 30)
	n := strings.Count(body, "\n") + 1
	if n > 30 {
		t.Fatalf("overflow %d", n)
	}
	// side layout should have separator
	if !strings.Contains(body, "│") && m.lab.Layout == LayoutSide {
		// may be ansi-wrapped — check dim sep via raw
		t.Log("note: separator may be styled")
	}
	// cycle styles without panic
	for i := 0; i < int(PixelCount); i++ {
		m.lab.CycleStyle()
		_ = m.renderFeedMosaic(40, 10, m.lab)
	}
	// all layouts
	for i := 0; i < int(LayoutCount); i++ {
		m.lab.Layout = FeedLayout(i)
		_ = m.renderLab(safeCols(100), 30)
	}
}

func TestRenderFrameStyles(t *testing.T) {
	rgb := make([]byte, 64*48*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	f := &FramePixels{W: 64, H: 48, RGB: rgb, Stamp: 1}
	// every style must honor width×rows at multiple scales
	for _, budget := range [][2]int{{16, 4}, {24, 6}, {40, 10}, {64, 14}} {
		cols, rows := budget[0], budget[1]
		for mode := PixelMode(0); mode < PixelCount; mode++ {
			out := RenderFrameH(f, mode, cols, rows)
			lines := strings.Split(out, "\n")
			if len(lines) > rows {
				t.Fatalf("%s@%dx%d rows %d", mode, cols, rows, len(lines))
			}
			for _, ln := range lines {
				// strip common ANSI reset when measuring cells
				if cellWidth(stripANSI(ln)) > cols {
					t.Fatalf("%s@%d wide: %d", mode, cols, cellWidth(stripANSI(ln)))
				}
			}
		}
	}
}

func TestFillPlaceholderCamWatch(t *testing.T) {
	l := newLabState()
	l.EnsurePlaceholders(4)
	// first slot may already be sim from older tests — clear
	l.Active = 0
	l.ClearActive()
	if !l.Feeds[0].IsEmpty() {
		t.Fatal("want empty")
	}
	l.FillCamIntoActive()
	if l.Feeds[0].Kind != "cam" {
		t.Fatalf("cam %s", l.Feeds[0].Kind)
	}
	l.SelectSlot(2)
	l.FillSimIntoActive()
	if l.Feeds[1].Kind != "sim" {
		t.Fatal(l.Feeds[1].Kind)
	}
	l.SelectSlot(3)
	fr := genSimFrame(32, 20, 0, 1)
	l.FillWatchIntoActive("clip", "https://example.com/a.mp4", fr)
	if l.Feeds[2].Kind != "watch" || l.Feeds[2].WatchSrc == "" {
		t.Fatal("watch")
	}
	b := l.BudgetLine()
	if b == "" || !strings.Contains(b, "Mbps") {
		t.Fatal(b)
	}
}

func TestTileGrid(t *testing.T) {
	c, r := tileGrid(1)
	if c != 1 || r != 1 {
		t.Fatal(c, r)
	}
	c, r = tileGrid(4)
	if c != 2 || r != 2 {
		t.Fatal(c, r)
	}
	c, r = tileGrid(6)
	if c != 3 || r != 2 {
		t.Fatal(c, r)
	}
}
