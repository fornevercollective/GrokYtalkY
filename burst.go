package main

import (
	"fmt"
	"math"
	"strings"
)

// Glyph matrix sizes from Nothing Glyph Matrix Developer Kit.
// Phone (3) = 25×25, Phone (4a) Pro = 13×13.
const (
	GlyphPhone3  = 25
	GlyphPhone4a = 13
	// Siri-sized orb: fixed terminal footprint (popup, not full dock)
	OrbCols = 28
	OrbRows = 11
)

// BurstKind is a short-lived video+audio walkie transmission.
// Hold → stream tiny frames + PCM; release → end. Like PTT with a face.
type BurstKind string

const (
	BurstStart BurstKind = "vburst-start"
	BurstFrame BurstKind = "vburst-frame"
	BurstAudio BurstKind = "vburst-audio" // same payload shape as audio
	BurstEnd   BurstKind = "vburst-end"
)

// GlyphMatrix is a flat brightness grid (0–255), row-major, N×N.
// Used for Nothing SDK / mesh (hardware LEDs are brightness-only).
type GlyphMatrix struct {
	N    int
	Data []byte // len = N*N, brightness
}

// GlyphRGB is exact N×N full-color matrix (terminal truecolor display).
// 1 cell = 1 LED at exact Glyph Matrix scale.
type GlyphRGB struct {
	N   int
	RGB []byte // len = N*N*3
}

// FrameToGlyph downsamples RGB frame → N×N luminance (Nothing matrix ready).
func FrameToGlyph(f *FramePixels, n int) GlyphMatrix {
	gc := FrameToGlyphRGB(f, n)
	gm := GlyphMatrix{N: gc.N, Data: make([]byte, gc.N*gc.N)}
	for i := 0; i < gc.N*gc.N; i++ {
		r, g, b := gc.RGB[i*3], gc.RGB[i*3+1], gc.RGB[i*3+2]
		// same luma as mesh
		L := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		gm.Data[i] = byte(L)
	}
	return gm
}

// FrameToGlyphRGB downsamples to exact N×N full-color LEDs (1:1 matrix cells).
func FrameToGlyphRGB(f *FramePixels, n int) GlyphRGB {
	if n < 5 {
		n = GlyphPhone3
	}
	out := GlyphRGB{N: n, RGB: make([]byte, n*n*3)}
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < f.W*f.H*3 {
		return out
	}
	for y := 0; y < n; y++ {
		sy0 := y * f.H / n
		sy1 := (y + 1) * f.H / n
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		if sy1 > f.H {
			sy1 = f.H
		}
		for x := 0; x < n; x++ {
			sx0 := x * f.W / n
			sx1 := (x + 1) * f.W / n
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sx1 > f.W {
				sx1 = f.W
			}
			var rs, gs, bs, cnt int
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					r, g, b := f.at(sx, sy)
					rs += int(r)
					gs += int(g)
					bs += int(b)
					cnt++
				}
			}
			if cnt < 1 {
				cnt = 1
			}
			i := (y*n + x) * 3
			out.RGB[i] = byte(rs / cnt)
			out.RGB[i+1] = byte(gs / cnt)
			out.RGB[i+2] = byte(bs / cnt)
		}
	}
	return out
}

// IntColors returns Nothing SDK int[] style brightness (0–255 per LED).
func (g GlyphMatrix) IntColors() []int {
	out := make([]int, len(g.Data))
	for i, b := range g.Data {
		out[i] = int(b)
	}
	return out
}

// glyphInCircle reports whether matrix cell (x,y) is inside the circular Glyph disk.
// Matches Nothing Phone (3) matrix: square grid with circular active region.
func glyphInCircle(x, y, n int) bool {
	cx := float64(n-1) / 2
	cy := float64(n-1) / 2
	// radius covers the disk; corners of the N×N square are off
	r := cx + 0.35
	return math.Hypot(float64(x)-cx, float64(y)-cy) <= r
}

// RenderBurstDual paints two exact-scale circular full-color Glyph Matrices.
func RenderBurstDual(cols, rows int, local, peer *FramePixels, tx bool, rx, you string, style PixelMode) string {
	return RenderBurstDualGlyph(cols, rows, local, peer, tx, rx, you, GlyphPhone3)
}

// RenderBurstDualGlyph: each circle is exactly glyphN×glyphN cells (1 LED = 1 cell),
// full truecolor like half-block style. Requires terminal ≥ 2*N+gutter wide, N+2 tall.
func RenderBurstDualGlyph(cols, rows int, local, peer *FramePixels, tx bool, rx, you string, glyphN int) string {
	if glyphN != GlyphPhone3 && glyphN != GlyphPhone4a {
		glyphN = GlyphPhone3
	}
	// exact scale: diameter = N cells
	diam := glyphN
	gutter := 2
	needW := diam*2 + gutter
	needH := diam + 2 // title + matrix
	if cols < needW {
		cols = needW
	}
	if rows < needH {
		rows = needH
	}

	tileW := diam + 1 // 1 cell padding for bezel breathing room optional — keep exact diam width
	// center two exact-N tiles in cols
	total := diam*2 + gutter
	xPad := (cols - total) / 2
	if xPad < 0 {
		xPad = 0
	}

	youLabel := "you"
	if you != "" {
		youLabel = truncate(you, diam-2)
	}
	peerLabel := "peer"
	if rx != "" {
		peerLabel = truncate(rx, diam-2)
	}
	leftTitle := styDim().Render("◎ " + youLabel)
	if tx {
		leftTitle = styErr().Reverse(true).Render(" TX ") + styTitle().Render("◎ "+youLabel)
	}
	rightTitle := styDim().Render("◎ " + peerLabel)
	if rx != "" {
		rightTitle = styLive().Render(" RX ") + styTitle().Render("◎ "+peerLabel)
	}
	gap := strings.Repeat(" ", gutter)
	title := strings.Repeat(" ", xPad) +
		padOrTrim(leftTitle, diam) + gap + padOrTrim(rightTitle, diam)

	leftCircle := renderGlyphCircleExact(local, diam, glyphN, tx, false)
	rightCircle := renderGlyphCircleExact(peer, diam, glyphN, false, rx != "")

	ll := strings.Split(leftCircle, "\n")
	rl := strings.Split(rightCircle, "\n")
	for len(ll) < diam {
		ll = append(ll, strings.Repeat(" ", diam))
	}
	for len(rl) < diam {
		rl = append(rl, strings.Repeat(" ", diam))
	}

	// vertical center matrix in remaining rows under title
	bodyBudget := rows - 1
	vPad := (bodyBudget - diam) / 2
	if vPad < 0 {
		vPad = 0
	}

	var lines []string
	lines = append(lines, clampCells(title, cols))
	for i := 0; i < vPad; i++ {
		lines = append(lines, strings.Repeat(" ", cols))
	}
	for i := 0; i < diam; i++ {
		line := strings.Repeat(" ", xPad) +
			padOrTrim(ll[i], diam) + gap + padOrTrim(rl[i], diam)
		lines = append(lines, clampCells(line, cols))
	}
	for len(lines) < rows {
		lines = append(lines, strings.Repeat(" ", cols))
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	_ = tileW
	return strings.Join(lines, "\n")
}

// renderGlyphCircleExact: exact N×N cells, 1:1 LEDs, full truecolor (half-style).
// Every active LED is truecolor RGB (FG+BG █ like half ▀ density). Circular mask
// matches Nothing Glyph Matrix; corners of the square are off. TX/RX only tints
// the outer ring slightly — never replaces color with mono.
func renderGlyphCircleExact(f *FramePixels, diam, glyphN int, hotTX, hotRX bool) string {
	// diam must equal glyphN for exact scale
	if diam != glyphN {
		diam = glyphN
	}
	gc := FrameToGlyphRGB(f, glyphN)
	hasFrame := f != nil && f.W > 0 && f.H > 0 && len(gc.RGB) >= glyphN*glyphN*3

	var lines []string
	for y := 0; y < glyphN; y++ {
		var b strings.Builder
		for x := 0; x < glyphN; x++ {
			if !glyphInCircle(x, y, glyphN) {
				// outside circular matrix — empty (Nothing corner cutouts)
				b.WriteByte(' ')
				continue
			}

			var r, g, bl byte
			if hasFrame {
				i := (y*glyphN + x) * 3
				r, g, bl = gc.RGB[i], gc.RGB[i+1], gc.RGB[i+2]
			} else {
				// unlit LED lattice (dark truecolor, still full-color capable)
				r, g, bl = 28, 28, 36
			}

			// TX/RX: soft tint on outer ring only — keep full RGB content
			if glyphOnBezel(x, y, glyphN) {
				if hotTX {
					r, g, bl = tintRGB(r, g, bl, 255, 60, 60, 0.45)
				} else if hotRX {
					r, g, bl = tintRGB(r, g, bl, 60, 220, 120, 0.40)
				}
			}

			// full-color LED (truecolor FG+BG █ — same family as half style)
			b.WriteString(truecolorLED(r, g, bl))
		}
		// exact width = glyphN cells (ANSI excluded from cell width via clamp later)
		lines = append(lines, b.String()+"\x1b[0m")
	}
	return strings.Join(lines, "\n")
}

// glyphOnBezel — cell is on the outer ring of the circular matrix.
func glyphOnBezel(x, y, n int) bool {
	if !glyphInCircle(x, y, n) {
		return false
	}
	// edge if any 4-neighbor is outside the circle
	for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= n || ny >= n || !glyphInCircle(nx, ny, n) {
			return true
		}
	}
	return false
}

// tintRGB blends base toward accent (keeps full-color path; used for TX/RX ring).
func tintRGB(r, g, b, ar, ag, ab byte, amount float64) (byte, byte, byte) {
	if amount <= 0 {
		return r, g, b
	}
	if amount > 1 {
		amount = 1
	}
	mix := func(c, a byte) byte {
		return byte(float64(c)*(1-amount) + float64(a)*amount)
	}
	return mix(r, ar), mix(g, ag), mix(b, ab)
}

// truecolorLED renders one full-color Glyph LED at exact 1-cell scale.
// Matches half-block “full color style”: both FG and BG truecolor so the cell
// is a solid colored LED (█), not mono brightness shading.
func truecolorLED(r, g, b byte) string {
	// near-black still uses truecolor (dark lattice LED), never mono-only path
	if int(r)+int(g)+int(b) < 18 {
		r, g, b = 22, 22, 28
	}
	var sb strings.Builder
	// FG + BG like renderHalf (▀ uses both); solid █ fills the cell with color
	sb.WriteString("\x1b[38;2;")
	writeU8(&sb, r)
	sb.WriteByte(';')
	writeU8(&sb, g)
	sb.WriteByte(';')
	writeU8(&sb, b)
	sb.WriteString("m\x1b[48;2;")
	writeU8(&sb, r)
	sb.WriteByte(';')
	writeU8(&sb, g)
	sb.WriteByte(';')
	writeU8(&sb, b)
	sb.WriteString("m█")
	return sb.String()
}

// RenderOrb paints a single Siri-sized popup (legacy / compact).
// Always returns exactly OrbRows lines of at most cols cells.
func RenderOrb(cols, rows int, face *FramePixels, level float64, bands []float64, tx bool, rx string, nick string) string {
	if cols < 16 {
		cols = OrbCols
	}
	if rows < 8 {
		rows = OrbRows
	}
	// glyph face size inside orb
	n := 9
	if cols < 24 {
		n = 7
	}
	gm := FrameToGlyph(face, n)

	cx := float64(cols-1) / 2
	cy := float64(rows-1) / 2
	// ring radius in cells
	rxR := float64(cols)/2 - 1.2
	ryR := float64(rows)/2 - 1.0

	var lines []string
	for y := 0; y < rows; y++ {
		var b strings.Builder
		for x := 0; x < cols; x++ {
			// normalize to ellipse
			nx := (float64(x) - cx) / rxR
			ny := (float64(y) - cy) / ryR
			d := math.Hypot(nx, ny)

			// center face region
			fx := x - (cols-n)/2
			fy := y - (rows-n)/2
			inFace := fx >= 0 && fy >= 0 && fx < n && fy < n

			if inFace && d < 0.72 {
				lum := float64(gm.Data[fy*n+fx]) / 255
				ch := glyphShade(lum)
				// TX warm, RX cool, idle neutral
				var cell string
				switch {
				case tx:
					cell = styErr().Render(ch)
				case rx != "":
					cell = styAccent().Render(ch)
				default:
					cell = styDim().Render(ch)
				}
				if lum > 0.35 {
					if tx {
						cell = styErr().Bold(true).Render(ch)
					} else if rx != "" {
						cell = styLive().Render(ch)
					} else {
						cell = styText().Render(ch)
					}
				}
				b.WriteString(cell)
				continue
			}

			// spectrum ring (cliamp / Siri-orb edge)
			if d > 0.78 && d < 1.05 {
				// angle → band
				ang := math.Atan2(ny, nx) // -pi..pi
				t := (ang + math.Pi) / (2 * math.Pi)
				bi := 0
				if len(bands) > 0 {
					bi = int(t * float64(len(bands)))
					if bi >= len(bands) {
						bi = len(bands) - 1
					}
				}
				lv := level
				if len(bands) > 0 {
					lv = bands[bi]
				}
				// ring thickness modulated by level
				ringOn := d < 0.78+0.12+lv*0.18
				if ringOn {
					ch := "·"
					if lv > 0.25 {
						ch = "•"
					}
					if lv > 0.55 {
						ch = "●"
					}
					if tx {
						b.WriteString(styErr().Render(ch))
					} else if rx != "" {
						b.WriteString(styLive().Render(ch))
					} else {
						b.WriteString(specStyle(lv).Render(ch))
					}
					continue
				}
			}

			b.WriteByte(' ')
		}
		lines = append(lines, clampCells(b.String(), cols))
	}

	// status under orb is separate — caller composes
	_ = nick
	return strings.Join(lines, "\n")
}

func glyphShade(lum float64) string {
	// dense → sparse for LED-like face
	switch {
	case lum < 0.08:
		return " "
	case lum < 0.18:
		return "·"
	case lum < 0.32:
		return ":"
	case lum < 0.48:
		return "+"
	case lum < 0.62:
		return "*"
	case lum < 0.78:
		return "o"
	case lum < 0.90:
		return "O"
	default:
		return "@"
	}
}

// RenderGlyphASCII dumps N×N for debugging / AOD preview.
func (g GlyphMatrix) RenderGlyphASCII() string {
	var b strings.Builder
	for y := 0; y < g.N; y++ {
		for x := 0; x < g.N; x++ {
			b.WriteString(glyphShade(float64(g.Data[y*g.N+x]) / 255))
		}
		if y+1 < g.N {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// BurstStatusLine one-liner under the orb.
func BurstStatusLine(w int, tx bool, rx, nick string, peers int) string {
	var left string
	switch {
	case tx:
		left = styErr().Reverse(true).Render(" BURST ") + styDim().Render(" hold space · video+mic")
	case rx != "":
		left = styLive().Render("● "+rx) + styDim().Render(" receiving")
	default:
		left = styTitle().Render("◈ burst") + styDim().Render(fmt.Sprintf(" · %s · space=PTT video", truncate(nick, 12)))
	}
	right := styDim().Render(fmt.Sprintf("%d peer", peers))
	if peers != 1 {
		right = styDim().Render(fmt.Sprintf("%d peers", peers))
	}
	need := cellWidth(stripANSI(left)) + cellWidth(stripANSI(right)) + 1
	if need > w {
		return clampCells(left, w)
	}
	gap := w - need
	if gap < 1 {
		gap = 1
	}
	return clampCells(left+strings.Repeat(" ", gap)+right, w)
}
