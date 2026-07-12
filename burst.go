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
type GlyphMatrix struct {
	N    int
	Data []byte // len = N*N, brightness
}

// FrameToGlyph downsamples RGB frame → N×N luminance (Nothing matrix ready).
func FrameToGlyph(f *FramePixels, n int) GlyphMatrix {
	if n < 5 {
		n = GlyphPhone3
	}
	gm := GlyphMatrix{N: n, Data: make([]byte, n*n)}
	if f == nil || f.W < 1 || f.H < 1 {
		return gm
	}
	for y := 0; y < n; y++ {
		sy := y * f.H / n
		if sy >= f.H {
			sy = f.H - 1
		}
		for x := 0; x < n; x++ {
			sx := x * f.W / n
			if sx >= f.W {
				sx = f.W - 1
			}
			// sample 2×2 average when possible
			var sum float64
			cnt := 0
			for dy := 0; dy < 2 && sy+dy < f.H; dy++ {
				for dx := 0; dx < 2 && sx+dx < f.W; dx++ {
					sum += f.lum(sx+dx, sy+dy)
					cnt++
				}
			}
			if cnt == 0 {
				cnt = 1
			}
			v := sum / float64(cnt)
			// boost midtones so faces read on LED matrix
			v = math.Pow(v, 0.85)
			if v > 1 {
				v = 1
			}
			gm.Data[y*n+x] = byte(v * 255)
		}
	}
	return gm
}

// IntColors returns Nothing SDK int[] style brightness (0–255 per LED).
func (g GlyphMatrix) IntColors() []int {
	out := make([]int, len(g.Data))
	for i, b := range g.Data {
		out[i] = int(b)
	}
	return out
}

// RenderOrb paints a Siri-sized popup: ring + center glyph face + status.
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
