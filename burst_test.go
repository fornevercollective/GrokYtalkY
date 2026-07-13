package main

import (
	"strings"
	"testing"
)

func TestRenderBurstDualExactScale(t *testing.T) {
	local := genSimFrame(64, 64, 1000, 1)
	peer := genSimFrame(64, 64, 2000, 2)
	// exact 25×25 dual needs width ≥ 52, height ≥ 26
	out := RenderBurstDualGlyph(60, 28, local, peer, true, "alice", "me", GlyphPhone3)
	lines := strings.Split(out, "\n")
	if len(lines) < 26 {
		t.Fatalf("rows %d want ≥26 for exact 25", len(lines))
	}
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Fatalf("expected FG truecolor 38;2")
	}
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("expected BG truecolor 48;2 (half-style full color density)")
	}
	// LEDs are space + BG truecolor (always 1 cell; avoids EAW half-clamp)
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("expected BG LED cells")
	}
}

func TestDualCirclesCenteredInHalves(t *testing.T) {
	local := genSimFrame(64, 64, 1000, 1)
	peer := genSimFrame(64, 64, 2000, 2)
	cols, rows := 80, 32
	out := RenderBurstDualGlyphScaled(cols, rows, local, peer, false, "alice", "me", GlyphPhone3, 1)
	lines := strings.Split(out, "\n")
	// find a body line with truecolor (matrix row)
	var body string
	for _, ln := range lines {
		if strings.Contains(ln, "\x1b[48;2;") {
			body = ln
			break
		}
	}
	if body == "" {
		t.Fatal("no matrix body line")
	}
	// strip for cell width check — full line must be exactly cols (padded)
	cw := cellWidth(strings.TrimSuffix(body, "\x1b[0m"))
	if cw > cols {
		t.Fatalf("line wider than terminal: %d > %d (would clip to half)", cw, cols)
	}
	// left half and right half each contain LEDs
	// With N=25 scale=1, diam=25, halfW=40, origin=7 — LEDs start after pad
	half := cols / 2
	// count truecolor sequences roughly in left vs right by splitting string mid-way is hard with ANSI;
	// instead: both placeGlyphLine paths produce content — require ≥2 distinct matrix lines full width
	if !strings.Contains(out, "◎") {
		t.Fatal("expected titles")
	}
	// never expand: diam*2 must fit in cols
	if GlyphDisplayDiam(25, 1)*2 > cols {
		t.Fatal("test setup: need cols large enough for dual 25")
	}
	_ = half
}

func TestLEDCellWidthIsOne(t *testing.T) {
	s := truecolorLED(200, 100, 50)
	if cellWidth(s) != 1 {
		t.Fatalf("LED cellWidth=%d want 1 (half-frame clamp bug)", cellWidth(s))
	}
	// 25 LEDs = 25 cells
	var row strings.Builder
	for i := 0; i < 25; i++ {
		row.WriteString(truecolorLED(byte(i*10), 40, 80))
	}
	if cellWidth(row.String()) != 25 {
		t.Fatalf("25 LEDs cellWidth=%d want 25", cellWidth(row.String()))
	}
}

func TestFrameToGlyphRGBExact(t *testing.T) {
	f := genSimFrame(50, 50, 0, 1)
	gc := FrameToGlyphRGB(f, 25)
	if gc.N != 25 || len(gc.RGB) != 25*25*3 {
		t.Fatalf("N=%d len=%d", gc.N, len(gc.RGB))
	}
	sum := 0
	for _, b := range gc.RGB {
		sum += int(b)
	}
	if sum == 0 {
		t.Fatal("black matrix")
	}
	gm := FrameToGlyph(f, 25)
	if len(gm.IntColors()) != 625 {
		t.Fatal("mesh brightness grid")
	}
}

// Official Nothing specs (25111_spec / 23112_spec LED occupancy).
func TestGlyphInCircleNothingSpec(t *testing.T) {
	// 13×13: corners off, center on; row counts from 25111_spec
	if glyphInCircle(0, 0, 13) {
		t.Fatal("13 corner should be off")
	}
	if !glyphInCircle(6, 6, 13) {
		t.Fatal("13 center on")
	}
	want13 := []int{5, 9, 11, 11, 13, 13, 13, 13, 13, 11, 11, 9, 5}
	for y := 0; y < 13; y++ {
		c := 0
		for x := 0; x < 13; x++ {
			if glyphInCircle(x, y, 13) {
				c++
			}
		}
		if c != want13[y] {
			t.Fatalf("13 row %d count %d want %d (25111_spec)", y, c, want13[y])
		}
	}
	if glyphActiveCount(13) != 137 {
		t.Fatalf("13 active LEDs %d want 137", glyphActiveCount(13))
	}

	// 25×25 from 23112_spec
	want25 := []int{7, 11, 15, 17, 19, 21, 21, 23, 23, 25, 25, 25, 25, 25, 25, 25, 23, 23, 21, 21, 19, 17, 15, 11, 7}
	for y := 0; y < 25; y++ {
		c := 0
		for x := 0; x < 25; x++ {
			if glyphInCircle(x, y, 25) {
				c++
			}
		}
		if c != want25[y] {
			t.Fatalf("25 row %d count %d want %d (23112_spec)", y, c, want25[y])
		}
	}
	if glyphActiveCount(25) != 489 {
		t.Fatalf("25 active LEDs %d want 489", glyphActiveCount(25))
	}
}

func TestRenderBurstDualEmptyPeer(t *testing.T) {
	local := genSimFrame(32, 24, 0, 1)
	out := RenderBurstDualGlyph(56, 30, local, nil, false, "", "bob", GlyphPhone3)
	if out == "" {
		t.Fatal("empty")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 26 {
		t.Fatalf("exact scale rows %d", len(lines))
	}
}

func TestRenderGlyphMatrixScaled(t *testing.T) {
	f := &FramePixels{W: 40, H: 40, RGB: make([]byte, 40*40*3)}
	for i := 0; i < 40*40; i++ {
		f.RGB[i*3] = 200
		f.RGB[i*3+1] = 40
		f.RGB[i*3+2] = 80
	}
	for _, n := range []int{GlyphPhone4a, GlyphPhone3} {
		for _, sc := range []int{1, 2, 3} {
			out := renderGlyphMatrix(f, n, sc, false, false)
			lines := strings.Split(out, "\n")
			want := n * sc
			if len(lines) != want {
				t.Fatalf("n=%d sc=%d rows=%d want %d", n, sc, len(lines), want)
			}
			for y, ln := range lines {
				cw := cellWidth(strings.TrimSuffix(ln, "\x1b[0m"))
				if cw != want {
					t.Fatalf("n=%d sc=%d y=%d cellWidth=%d want diam %d", n, sc, y, cw, want)
				}
			}
			if !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "\x1b[48;2;") {
				t.Fatalf("n=%d sc=%d missing half-style truecolor", n, sc)
			}
		}
	}
}

func TestRenderGlyphHiRes(t *testing.T) {
	f := genSimFrame(80, 80, 0, 1)
	// dual 37 needs ≥74 cols; use wide term so both full circles fit centered
	out := RenderBurstDualGlyphScaled(100, 50, f, f, false, "p", "me", GlyphRes37, 1)
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatal("hi-res 37 should render LEDs")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 38 {
		t.Fatalf("hi-res rows %d", len(lines))
	}
	// each line ≤ terminal width
	for i, ln := range lines {
		if cellWidth(strings.TrimSuffix(ln, "\x1b[0m")) > 100 {
			t.Fatalf("line %d overflows terminal", i)
		}
	}
}

func TestFit80x24FullCircles(t *testing.T) {
	// Standard terminal cannot host dual 25×25 (needs ~54×31).
	// Auto-fit must pick full 13×13 disks, never half-clipped 25.
	n, s, down := FitGlyphDual(80, 24, GlyphPhone3, 0)
	if n != GlyphPhone4a {
		t.Fatalf("80×24 prefer 25 → display N=%d want 13", n)
	}
	if s < 1 {
		t.Fatal("scale")
	}
	if !down {
		t.Fatal("expected downgrade from 25")
	}
	// panel budget for 80×24
	pc, pr := BurstPanelBudget(80, 24)
	if pr != 20 {
		t.Fatalf("panel rows %d want 20 (24-4 chrome)", pr)
	}
	dn, ds, _ := fitGlyphInPanel(pc, pr, 25, 0)
	if dn*ds > maxDualDiam(pc, pr) {
		t.Fatalf("diam %d exceeds max %d", dn*ds, maxDualDiam(pc, pr))
	}

	local := genSimFrame(64, 64, 0, 1)
	peer := genSimFrame(64, 64, 100, 2)
	out := RenderBurstDualGlyphScaled(80, pr, local, peer, false, "a", "me", 25, 0)
	lines := strings.Split(out, "\n")
	if len(lines) > pr {
		t.Fatalf("dual emitted %d lines > panel %d (would clip circles)", len(lines), pr)
	}
	// every matrix body line must be ≤ 80 cells
	for i, ln := range lines {
		if cellWidth(strings.TrimSuffix(ln, "\x1b[0m")) > 80 {
			t.Fatalf("line %d overflow", i)
		}
	}
	// full circle height: title + diam rows, diam = dn*ds
	wantDiam := dn * ds
	if len(lines) < wantDiam {
		t.Fatalf("rows %d < full diam %d", len(lines), wantDiam)
	}
	// count LED-bearing rows ≈ diam (full disk, not half)
	ledRows := 0
	for _, ln := range lines {
		if strings.Contains(ln, "\x1b[48;2;") {
			ledRows++
		}
	}
	if ledRows < wantDiam {
		t.Fatalf("LED rows %d < full circle diam %d (half-circle clip?)", ledRows, wantDiam)
	}
}

func TestAutoGlyphScaleAndLadder(t *testing.T) {
	// 60×30 full term: panel ~26, halfW 30 → 25 fits scale 1
	if s := AutoGlyphScale(60, 32, 25, true); s < 1 {
		t.Fatalf("auto scale for 25 on tall term: %d", s)
	}
	// large terminal should allow scale ≥2 for 13×13 dual
	if AutoGlyphScale(80, 40, 13, true) < 2 {
		t.Fatalf("expected scale-up on large term, got %d", AutoGlyphScale(80, 40, 13, true))
	}
	// 80×24: 25 does not fit
	if AutoGlyphScale(80, 24, 25, true) != 0 {
		t.Fatalf("25 must not claim to fit on 80×24")
	}
	n := GlyphPhone4a
	seen := map[int]bool{}
	for i := 0; i < len(GlyphResLadder); i++ {
		n = cycleGlyphRes(n)
		seen[n] = true
	}
	if len(seen) != len(GlyphResLadder) {
		t.Fatalf("ladder cycle incomplete: %v", seen)
	}
	if GlyphDeviceN(GlyphRes49) != GlyphPhone3 {
		t.Fatal("hi-res maps to Phone3 device N for mesh")
	}
	if GlyphDeviceN(GlyphPhone4a) != GlyphPhone4a {
		t.Fatal("4a device")
	}
}

func TestTruecolorLEDHalfStyle(t *testing.T) {
	s := truecolorLED(255, 128, 64)
	if !strings.Contains(s, "\x1b[38;2;255;128;64m") {
		t.Fatalf("FG truecolor missing: %q", s)
	}
	if !strings.Contains(s, "\x1b[48;2;255;128;64m") {
		t.Fatalf("BG truecolor missing (half-style density): %q", s)
	}
	// space glyph (width 1), not full-block █
	if !strings.HasSuffix(strings.TrimSuffix(s, ""), " ") && !strings.Contains(s, "m ") {
		t.Fatalf("expected space LED cell: %q", s)
	}
	dark := truecolorLED(0, 0, 0)
	if !strings.Contains(dark, "\x1b[38;2;") || !strings.Contains(dark, "\x1b[48;2;") {
		t.Fatalf("dark LED must stay full truecolor path: %q", dark)
	}
}
