package main

import (
	"strings"
	"testing"
)

func TestClampAndStable(t *testing.T) {
	line := "\x1b[38;2;255;0;0m\x1b[48;2;0;0;0m" + strings.Repeat("▀", 100) + "\x1b[0m"
	out := clampCells(line, 40)
	base := strings.TrimSuffix(out, "\x1b[0m")
	if cellWidth(base) > 40 {
		t.Fatalf("width %d", cellWidth(base))
	}
	block := line + "\n" + line + "\n" + line
	fit := fitHalfBlock(block, 20, 2)
	lines := strings.Split(fit, "\n")
	if len(lines) != 2 {
		t.Fatalf("rows %d", len(lines))
	}
	for _, ln := range lines {
		b := strings.TrimSuffix(ln, "\x1b[0m")
		if cellWidth(b) > 20 {
			t.Fatalf("line too wide %d", cellWidth(b))
		}
	}
	view := stableView(strings.Repeat("hello\n", 50), 30, 12)
	n := strings.Count(view, "\n") + 1
	if n != 12 {
		t.Fatalf("lines %d want 12", n)
	}
}

func TestSafeColsNeverInflates(t *testing.T) {
	if safeCols(80) != 79 {
		t.Fatalf("safeCols(80)=%d", safeCols(80))
	}
	if safeCols(1) != 1 {
		t.Fatalf("safeCols(1)")
	}
}

func TestRenderHalfNeverWiderThanCols(t *testing.T) {
	// synthetic frame
	w, h := 64, 8
	rgb := make([]byte, w*h*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	f := &FramePixels{W: w, H: h, RGB: rgb}
	for _, cols := range []int{12, 28, 40, 79} {
		body := RenderFrame(f, PixelHalf, cols)
		for _, ln := range strings.Split(body, "\n") {
			if cellWidth(strings.TrimSuffix(ln, "\x1b[0m")) > cols {
				t.Fatalf("cols=%d line width=%d", cols, cellWidth(ln))
			}
		}
		fit := fitHalfBlock(body, cols, 2)
		for _, ln := range strings.Split(fit, "\n") {
			cw := cellWidth(strings.TrimSuffix(ln, "\x1b[0m"))
			if cw > cols {
				t.Fatalf("fit cols=%d got %d", cols, cw)
			}
		}
	}
}

func TestStableViewNoInflatedWidth(t *testing.T) {
	// body wider than terminal must clamp
	wide := strings.Repeat("▀", 200)
	view := stableView(wide, 40, 5)
	for _, ln := range strings.Split(view, "\n") {
		if cellWidth(strings.TrimSuffix(ln, "\x1b[0m")) > 39 { // safeCols(40)=39
			t.Fatalf("line wider than safe: %d", cellWidth(ln))
		}
	}
}

func TestFrameToGlyph25(t *testing.T) {
	w, h := 40, 40
	rgb := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			// bright center
			if (x-20)*(x-20)+(y-20)*(y-20) < 100 {
				rgb[i], rgb[i+1], rgb[i+2] = 220, 180, 160
			}
		}
	}
	f := &FramePixels{W: w, H: h, RGB: rgb}
	g := FrameToGlyph(f, GlyphPhone3)
	if g.N != 25 || len(g.Data) != 625 {
		t.Fatalf("glyph size %d %d", g.N, len(g.Data))
	}
	// center should be brighter than corner
	if g.Data[12*25+12] <= g.Data[0] {
		t.Fatalf("expected bright center")
	}
	ints := g.IntColors()
	if len(ints) != 625 {
		t.Fatal("int colors")
	}
}

func TestCompanionBodyFitsTerminal(t *testing.T) {
	m := NewModel(Options{Nick: "t", Host: "127.0.0.1:9", MIDI: false, Translate: false})
	m.width, m.height = 80, 40
	m.compact = true
	m.camOn = true
	m.videoOn = true
	// frame large enough that renderHalf produces many rows when scaled
	m.frame = &FramePixels{W: 80, H: 48, RGB: make([]byte, 80*48*3)}
	m.live = nil
	body := m.renderCompanion(safeCols(80), 40)
	n := strings.Count(body, "\n") + 1
	// body may be ≤ h before stableView; renderCompanion itself should not explode past h
	if n > 40 {
		t.Fatalf("companion body taller than term: %d", n)
	}
	sc := m.computeVideoScale(80, 40)
	if !sc.Active || sc.HalfRows < 6 {
		t.Fatalf("expected full-scale video halfRows>=6 got %+v", sc)
	}
	for _, ln := range strings.Split(body, "\n") {
		if cellWidth(strings.TrimSuffix(ln, "\x1b[0m")) > 79 {
			t.Fatalf("line overflow")
		}
	}
}
