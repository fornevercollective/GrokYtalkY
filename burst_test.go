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
	// title + 25 matrix rows
	if len(lines) < 26 {
		t.Fatalf("rows %d want ≥26 for exact 25", len(lines))
	}
	// half-style full color: both FG 38;2 and BG 48;2 truecolor
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Fatalf("expected FG truecolor 38;2")
	}
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("expected BG truecolor 48;2 (half-style full color density)")
	}
	if !strings.Contains(out, "█") {
		t.Fatalf("expected full-block LEDs")
	}
}

func TestFrameToGlyphRGBExact(t *testing.T) {
	f := genSimFrame(50, 50, 0, 1)
	gc := FrameToGlyphRGB(f, 25)
	if gc.N != 25 || len(gc.RGB) != 25*25*3 {
		t.Fatalf("N=%d len=%d", gc.N, len(gc.RGB))
	}
	// not all black
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

func TestGlyphInCircleCornersOff(t *testing.T) {
	n := 25
	// corners of square are outside circular matrix
	if glyphInCircle(0, 0, n) {
		t.Fatal("corner should be off")
	}
	if !glyphInCircle(12, 12, n) {
		t.Fatal("center on")
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

func TestRenderGlyphCircleExactWidthAndColor(t *testing.T) {
	// synthetic red-ish frame so LEDs are clearly full color
	f := &FramePixels{W: 40, H: 40, RGB: make([]byte, 40*40*3)}
	for i := 0; i < 40*40; i++ {
		f.RGB[i*3] = 200
		f.RGB[i*3+1] = 40
		f.RGB[i*3+2] = 80
	}
	for _, n := range []int{GlyphPhone3, GlyphPhone4a} {
		out := renderGlyphCircleExact(f, n, n, false, false)
		lines := strings.Split(out, "\n")
		if len(lines) != n {
			t.Fatalf("n=%d rows=%d want exact N", n, len(lines))
		}
		for y, ln := range lines {
			cw := cellWidth(strings.TrimSuffix(ln, "\x1b[0m"))
			if cw != n {
				t.Fatalf("n=%d y=%d cellWidth=%d want exact N (1 cell=1 LED)", n, y, cw)
			}
		}
		// interior center must be truecolor FG+BG (not mono shade)
		mid := lines[n/2]
		if !strings.Contains(mid, "\x1b[38;2;") || !strings.Contains(mid, "\x1b[48;2;") {
			t.Fatalf("n=%d center row missing half-style truecolor FG+BG", n)
		}
		if !strings.Contains(mid, "█") {
			t.Fatalf("n=%d center missing full-block LED", n)
		}
		// mono glyphShade path must not be used for active LEDs
		plain := stripANSI(mid)
		for _, bad := range []string{"@", "O", "*", "+"} {
			if strings.Contains(plain, bad) {
				t.Fatalf("n=%d mono shade %q leaked into full-color matrix", n, bad)
			}
		}
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
	if !strings.Contains(s, "█") {
		t.Fatal("full block LED")
	}
	// near-black still truecolor capable, not mono-only
	dark := truecolorLED(0, 0, 0)
	if !strings.Contains(dark, "\x1b[38;2;") || !strings.Contains(dark, "\x1b[48;2;") {
		t.Fatalf("dark LED must stay full truecolor path: %q", dark)
	}
}
