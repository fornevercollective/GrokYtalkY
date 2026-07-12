package main

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// PixelMode selects how frames render (cliamp + vwall styles).
type PixelMode int

const (
	PixelHalf PixelMode = iota // ▀ half-blocks (truecolor)
	PixelHex                   // hex mosaic
	PixelBraille               // braille density
	PixelASCII                 // shade ramp glyphs
	PixelBlocks                // chunky pixel blocks
	PixelPoints                // pointillist dots
	PixelHalftone              // AM newspaper dots
	PixelDepth                 // zip-lite depth false-color
	PixelGsplat                // gsplat depth stack
	PixelCount
)

func (m PixelMode) String() string {
	switch m {
	case PixelHalf:
		return "half"
	case PixelHex:
		return "hex"
	case PixelBraille:
		return "braille"
	case PixelASCII:
		return "ascii"
	case PixelBlocks:
		return "blocks"
	case PixelPoints:
		return "points"
	case PixelHalftone:
		return "halftone"
	case PixelDepth:
		return "depth"
	case PixelGsplat:
		return "gsplat"
	default:
		return "?"
	}
}

// AllStyles lists style names for UI/docs.
func AllStyles() []string {
	out := make([]string, 0, PixelCount)
	for i := PixelMode(0); i < PixelCount; i++ {
		out = append(out, i.String())
	}
	return out
}

// FramePixels holds a decoded grayscale/RGB buffer for terminal paint.
type FramePixels struct {
	W, H   int
	RGB    []byte // len = W*H*3
	Source string
}

func decodeFrameJPEG(b64data []byte, maxW, maxH int) (*FramePixels, error) {
	img, _, err := image.Decode(bytes.NewReader(b64data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return nil, err
	}
	// Target terminal cell grid: widthCols × (half-block rows*2)
	// Stretch to fill panel so the camera is readable (prefer width fill).
	if maxW < 8 {
		maxW = 80
	}
	if maxH < 4 {
		maxH = 40
	}
	dw := maxW
	dh := int(float64(dw) * float64(sh) / float64(sw))
	if dh > maxH {
		dh = maxH
		dw = int(float64(dh) * float64(sw) / float64(sh))
	}
	if dw < 1 {
		dw = 1
	}
	if dh < 2 {
		dh = 2
	}
	if dh%2 != 0 {
		dh++
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	// sample source with floating coords (better than pure nearest for faces)
	for y := 0; y < dh; y++ {
		sy := b.Min.Y + y*sh/dh
		if sy >= b.Max.Y {
			sy = b.Max.Y - 1
		}
		for x := 0; x < dw; x++ {
			sx := b.Min.X + x*sw/dw
			if sx >= b.Max.X {
				sx = b.Max.X - 1
			}
			dst.Set(x, y, img.At(sx, sy))
		}
	}
	rgb := make([]byte, dw*dh*3)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			r, g, bb, _ := dst.At(x, y).RGBA()
			i := (y*dw + x) * 3
			rgb[i] = byte(r >> 8)
			rgb[i+1] = byte(g >> 8)
			rgb[i+2] = byte(bb >> 8)
		}
	}
	return &FramePixels{W: dw, H: dh, RGB: rgb}, nil
}

func (f *FramePixels) at(x, y int) (r, g, b byte) {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return 0, 0, 0
	}
	i := (y*f.W + x) * 3
	return f.RGB[i], f.RGB[i+1], f.RGB[i+2]
}

func (f *FramePixels) lum(x, y int) float64 {
	r, g, b := f.at(x, y)
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255
}

func rgbHex(r, g, b byte) string {
	return colorful.Color{
		R: float64(r) / 255,
		G: float64(g) / 255,
		B: float64(b) / 255,
	}.Hex()
}

// RenderFrame paints frame into a string using the selected pixel mode + lipgloss.
// maxHalfRows limits vertical size (0 = full frame height).
func RenderFrame(f *FramePixels, mode PixelMode, widthCols int) string {
	return RenderFrameH(f, mode, widthCols, 0)
}

// RenderFrameH same as RenderFrame but caps half-block rows.
func RenderFrameH(f *FramePixels, mode PixelMode, widthCols, maxHalfRows int) string {
	if f == nil || f.W == 0 || f.H == 0 {
		return dimStyle.Render("  no video — waiting for frames / c cam / a sim  ")
	}
	// style preprocess on a copy for modes that mutate RGB
	src := f
	switch mode {
	case PixelBlocks, PixelPoints, PixelHalftone, PixelDepth, PixelGsplat:
		src = f.Clone()
		applyPixelStyle(src, mode)
	}
	var body string
	switch mode {
	case PixelHex:
		body = renderHex(src, widthCols)
	case PixelBraille:
		body = renderBraille(src, widthCols)
	case PixelASCII:
		body = renderASCII(src, widthCols)
	default:
		// half / blocks / points / halftone / depth / gsplat all paint as half-blocks after preprocess
		body = renderHalf(src, widthCols)
	}
	if maxHalfRows > 0 {
		body = fitHalfBlock(body, widthCols, maxHalfRows)
	}
	return body
}

// Clone deep-copies RGB buffer.
func (f *FramePixels) Clone() *FramePixels {
	if f == nil {
		return nil
	}
	cp := make([]byte, len(f.RGB))
	copy(cp, f.RGB)
	return &FramePixels{W: f.W, H: f.H, RGB: cp, Source: f.Source}
}

// applyPixelStyle mutates f.RGB in place for lab styles.
func applyPixelStyle(f *FramePixels, mode PixelMode) {
	if f == nil {
		return
	}
	switch mode {
	case PixelBlocks:
		applyBlocks(f, 4)
	case PixelPoints:
		applyPoints(f, 5)
	case PixelHalftone:
		applyHalftone(f, 6)
	case PixelDepth:
		if dm := EstimateZipLite(f.RGB, f.W, f.H); dm != nil {
			ApplyDepthColorize(f, dm)
		}
	case PixelGsplat:
		if dm := EstimateZipLite(f.RGB, f.W, f.H); dm != nil {
			ApplyGsplatStack(f, dm, defaultGsplat, float64(time.Now().UnixMilli())/1000)
		}
	}
}

func applyBlocks(f *FramePixels, cell int) {
	if cell < 2 {
		cell = 2
	}
	for y := 0; y < f.H; y += cell {
		for x := 0; x < f.W; x += cell {
			var r, g, b, n int
			for dy := 0; dy < cell && y+dy < f.H; dy++ {
				for dx := 0; dx < cell && x+dx < f.W; dx++ {
					rr, gg, bb := f.at(x+dx, y+dy)
					r += int(rr)
					g += int(gg)
					b += int(bb)
					n++
				}
			}
			if n == 0 {
				continue
			}
			r /= n
			g /= n
			b /= n
			for dy := 0; dy < cell && y+dy < f.H; dy++ {
				for dx := 0; dx < cell && x+dx < f.W; dx++ {
					i := ((y+dy)*f.W + (x + dx)) * 3
					f.RGB[i] = byte(r)
					f.RGB[i+1] = byte(g)
					f.RGB[i+2] = byte(b)
				}
			}
		}
	}
}

func applyPoints(f *FramePixels, cell int) {
	if cell < 3 {
		cell = 3
	}
	src := f.Clone()
	for i := 0; i < len(f.RGB); i += 3 {
		f.RGB[i], f.RGB[i+1], f.RGB[i+2] = 8, 8, 12
	}
	for y := cell / 2; y < f.H; y += cell {
		for x := cell / 2; x < f.W; x += cell {
			r, g, b := src.at(x, y)
			L := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
			rad := float64(cell) * 0.45 * (0.15 + L*0.95)
			r2 := rad * rad
			for dy := -cell; dy <= cell; dy++ {
				for dx := -cell; dx <= cell; dx++ {
					if float64(dx*dx+dy*dy) > r2 {
						continue
					}
					xx, yy := x+dx, y+dy
					if xx < 0 || yy < 0 || xx >= f.W || yy >= f.H {
						continue
					}
					i := (yy*f.W + xx) * 3
					f.RGB[i], f.RGB[i+1], f.RGB[i+2] = r, g, b
				}
			}
		}
	}
}

func applyHalftone(f *FramePixels, cell int) {
	if cell < 3 {
		cell = 3
	}
	src := f.Clone()
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			cx := (x/cell)*cell + cell/2
			cy := (y/cell)*cell + cell/2
			if cx >= f.W {
				cx = f.W - 1
			}
			if cy >= f.H {
				cy = f.H - 1
			}
			r, g, b := src.at(cx, cy)
			L := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 255
			dist := math.Hypot(float64(x-cx), float64(y-cy))
			maxR := float64(cell) * 0.48 * (1 - L)
			ink := byte(245)
			if dist <= maxR {
				ink = 0
			}
			i := (y*f.W + x) * 3
			f.RGB[i], f.RGB[i+1], f.RGB[i+2] = ink, ink, ink
		}
	}
}

func renderHalf(f *FramePixels, widthCols int) string {
	// Exactly widthCols cells per line — never wider (prevents wrap-spool).
	cols := widthCols
	if cols < 1 {
		cols = 1
	}
	if cols > f.W && f.W > 0 {
		// still emit `cols` cells by resampling
	}
	halfRows := f.H / 2
	if halfRows < 1 {
		halfRows = 1
	}
	var b strings.Builder
	b.Grow(cols * halfRows * 20)
	for y := 0; y < f.H; y += 2 {
		var pr, pg, pb, pbr, pbg, pbb byte
		var have bool
		for x := 0; x < cols; x++ {
			sx := 0
			if f.W > 0 {
				sx = x * f.W / cols
				if sx >= f.W {
					sx = f.W - 1
				}
			}
			tr, tg, tb := f.at(sx, y)
			br, bg, bb := byte(0), byte(0), byte(0)
			if y+1 < f.H {
				br, bg, bb = f.at(sx, y+1)
			}
			if !have || tr != pr || tg != pg || tb != pb || br != pbr || bg != pbg || bb != pbb {
				b.WriteString("\x1b[38;2;")
				writeU8(&b, tr)
				b.WriteByte(';')
				writeU8(&b, tg)
				b.WriteByte(';')
				writeU8(&b, tb)
				b.WriteString("m\x1b[48;2;")
				writeU8(&b, br)
				b.WriteByte(';')
				writeU8(&b, bg)
				b.WriteByte(';')
				writeU8(&b, bb)
				b.WriteByte('m')
				pr, pg, pb, pbr, pbg, pbb = tr, tg, tb, br, bg, bb
				have = true
			}
			b.WriteString("▀")
		}
		b.WriteString("\x1b[0m")
		if y+2 < f.H {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeU8(b *strings.Builder, v byte) {
	// small int write without fmt
	if v >= 100 {
		b.WriteByte('0' + v/100)
		v %= 100
		b.WriteByte('0' + v/10)
		b.WriteByte('0' + v%10)
		return
	}
	if v >= 10 {
		b.WriteByte('0' + v/10)
		b.WriteByte('0' + v%10)
		return
	}
	b.WriteByte('0' + v)
}

// renderHex — hexcast-inspired mosaic: block characters tinted by local luminance bands.
func renderHex(f *FramePixels, widthCols int) string {
	// sample coarser grid; use hex-ish glyphs
	glyphs := []string{"·", "▫", "▪", "◆", "◈", "◉", "●", "█"}
	cols := min(f.W, widthCols)
	stepY := max(1, f.H/(max(1, cols/2)))
	var b strings.Builder
	rows := 0
	maxRows := 12
	for y := 0; y < f.H && rows < maxRows; y += stepY {
		for x := 0; x < cols; x++ {
			r, g, bl := f.at(x, y)
			lum := f.lum(x, y)
			// hexcast prefix-ish: color by channel dominance + glyph by lum
			idx := int(lum * float64(len(glyphs)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(glyphs) {
				idx = len(glyphs) - 1
			}
			// slight channel boost for "hex" feel
			cr, cg, cb := r, g, bl
			if r > g && r > bl {
				cr = byte(min(255, int(r)+20))
			} else if g > r && g > bl {
				cg = byte(min(255, int(g)+20))
			} else if bl > r && bl > g {
				cb = byte(min(255, int(bl)+20))
			}
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(rgbHex(cr, cg, cb))).
				Render(glyphs[idx])
			b.WriteString(cell)
		}
		rows++
		if y+stepY < f.H && rows < maxRows {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderBraille(f *FramePixels, widthCols int) string {
	// 2x4 dots per braille cell
	cols := min(f.W/2, widthCols)
	var b strings.Builder
	for y := 0; y+3 < f.H; y += 4 {
		for x := 0; x < cols*2; x += 2 {
			var bits rune
			// braille bit map
			dots := [4][2]rune{
				{0x01, 0x08},
				{0x02, 0x10},
				{0x04, 0x20},
				{0x40, 0x80},
			}
			var sr, sg, sb int
			n := 0
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					if f.lum(x+dx, y+dy) > 0.45 {
						bits |= dots[dy][dx]
					}
					r, g, bl := f.at(x+dx, y+dy)
					sr += int(r)
					sg += int(g)
					sb += int(bl)
					n++
				}
			}
			ch := string(rune(0x2800 | bits))
			if n == 0 {
				n = 1
			}
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(rgbHex(byte(sr/n), byte(sg/n), byte(sb/n)))).
				Render(ch)
			b.WriteString(cell)
		}
		if y+4 < f.H {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderASCII(f *FramePixels, widthCols int) string {
	ramp := []rune(" .:-=+*#%@")
	cols := min(f.W, widthCols)
	var b strings.Builder
	step := max(1, f.H/12)
	for y := 0; y < f.H; y += step {
		for x := 0; x < cols; x++ {
			lum := f.lum(x, y)
			idx := int(lum * float64(len(ramp)-1))
			r, g, bl := f.at(x, y)
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(rgbHex(r, g, bl))).
				Render(string(ramp[idx]))
			b.WriteString(cell)
		}
		if y+step < f.H {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// unused import guard for color package used indirectly
var _ = color.Black
