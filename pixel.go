package main

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"sync"
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
	PixelEdges                 // sobel outline
	PixelPoster                // posterize / quantize
	PixelScan                  // CRT scanlines
	PixelDither                // ordered Bayer dither
	PixelNeon                  // high-sat bloom edges
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
	case PixelEdges:
		return "edges"
	case PixelPoster:
		return "poster"
	case PixelScan:
		return "scan"
	case PixelDither:
		return "dither"
	case PixelNeon:
		return "neon"
	default:
		return "?"
	}
}

// StyleCost ranks CPU load for stream throttling (1 = light, 5 = heavy).
func (m PixelMode) StyleCost() int {
	switch m {
	case PixelHalf, PixelASCII, PixelHex, PixelPoster, PixelScan, PixelDither:
		return 1
	case PixelBlocks, PixelPoints, PixelBraille, PixelEdges, PixelNeon:
		return 2
	case PixelHalftone:
		return 3
	case PixelDepth:
		return 4
	case PixelGsplat:
		return 5
	default:
		return 2
	}
}

// HeavyStyle is true when preprocess should downsample + throttle under stream.
func (m PixelMode) HeavyStyle() bool {
	return m.StyleCost() >= 3
}

// AllStyles lists style names for UI/docs.
func AllStyles() []string {
	out := make([]string, 0, PixelCount)
	for i := PixelMode(0); i < PixelCount; i++ {
		out = append(out, i.String())
	}
	return out
}

// StyleGeom is the terminal paint budget for a style pass.
// All effects must honor Cols × Rows so scale never clips inconsistently.
type StyleGeom struct {
	Cols int // terminal cells wide
	Rows int // output lines (half-block rows for half-family)
	Cell int // preprocess cell size (blocks/points/halftone)
}

// StyleGeomFromBudget derives geometry from display width + max half-rows + frame size.
func StyleGeomFromBudget(widthCols, maxHalfRows, frameW, frameH int) StyleGeom {
	cols := widthCols
	if cols < 1 {
		cols = 1
	}
	rows := maxHalfRows
	if rows < 1 {
		// full frame half-rows
		if frameH > 0 {
			rows = max(1, frameH/2)
		} else {
			rows = 8
		}
	}
	// Cell size scales with display: larger tiles → coarser preprocess blocks
	cell := max(2, cols/14)
	if cell > 14 {
		cell = 14
	}
	if cols < 24 {
		cell = max(2, cell-1)
	}
	return StyleGeom{Cols: cols, Rows: rows, Cell: cell}
}

// FramePixels holds a decoded grayscale/RGB buffer for terminal paint.
type FramePixels struct {
	W, H   int
	RGB    []byte // len = W*H*3
	Source string
	// Stamp for stream cache invalidation (UnixMilli or packet seq)
	Stamp int64
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
	return &FramePixels{W: dw, H: dh, RGB: rgb, Stamp: time.Now().UnixMilli()}, nil
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

// DownsampleFrame nearest-neighbor resize for style preprocess (stream mitigation).
func DownsampleFrame(f *FramePixels, tw, th int) *FramePixels {
	if f == nil || tw < 1 || th < 1 {
		return f
	}
	if f.W <= tw && f.H <= th {
		return f.Clone()
	}
	if th%2 != 0 {
		th++
	}
	rgb := make([]byte, tw*th*3)
	for y := 0; y < th; y++ {
		sy := y * f.H / th
		if sy >= f.H {
			sy = f.H - 1
		}
		for x := 0; x < tw; x++ {
			sx := x * f.W / tw
			if sx >= f.W {
				sx = f.W - 1
			}
			r, g, b := f.at(sx, sy)
			i := (y*tw + x) * 3
			rgb[i], rgb[i+1], rgb[i+2] = r, g, b
		}
	}
	return &FramePixels{W: tw, H: th, RGB: rgb, Source: f.Source, Stamp: f.Stamp}
}

// styleWorkFrame picks a process resolution that matches paint budget (mitigates stream lag).
func styleWorkFrame(f *FramePixels, mode PixelMode, geom StyleGeom) *FramePixels {
	if f == nil {
		return nil
	}
	// target: ~2 source pixels per half-cell row/col
	tw := geom.Cols * 2
	th := geom.Rows * 2
	if tw < 16 {
		tw = 16
	}
	if th < 8 {
		th = 8
	}
	// light styles can keep more detail
	if mode.StyleCost() <= 1 {
		tw = max(tw, min(f.W, geom.Cols*3))
		th = max(th, min(f.H, geom.Rows*3))
	}
	// heavy styles hard-cap for FPS
	if mode.HeavyStyle() {
		if tw > 96 {
			tw = 96
		}
		if th > 64 {
			th = 64
		}
	} else {
		if tw > 160 {
			tw = 160
		}
		if th > 96 {
			th = 96
		}
	}
	if f.W <= tw && f.H <= th {
		return f.Clone()
	}
	return DownsampleFrame(f, tw, th)
}

// ── depth cache (stream: don't recompute zip-lite every paint) ──

var (
	depthCacheMu   sync.Mutex
	depthCacheKey  string
	depthCacheMap  *DepthMap
	depthCacheWhen time.Time
)

func depthCacheGet(f *FramePixels, mode PixelMode) *DepthMap {
	if f == nil {
		return nil
	}
	key := mode.String() + ":" + itoa(f.W) + "x" + itoa(f.H) + ":" + itoa64(f.Stamp)
	depthCacheMu.Lock()
	defer depthCacheMu.Unlock()
	// reuse within 80ms for same stamp (streaming same frame)
	if depthCacheKey == key && depthCacheMap != nil && time.Since(depthCacheWhen) < 120*time.Millisecond {
		return depthCacheMap
	}
	// same geometry, recent — reuse if stamp close (stream continuity)
	if depthCacheMap != nil && depthCacheMap.W == f.W && depthCacheMap.H == f.H &&
		time.Since(depthCacheWhen) < 80*time.Millisecond {
		return depthCacheMap
	}
	dm := EstimateZipLite(f.RGB, f.W, f.H)
	depthCacheKey = key
	depthCacheMap = dm
	depthCacheWhen = time.Now()
	return dm
}

// RenderFrame paints frame into a string using the selected pixel mode + lipgloss.
func RenderFrame(f *FramePixels, mode PixelMode, widthCols int) string {
	return RenderFrameH(f, mode, widthCols, 0)
}

// RenderFrameH paints with vertical budget. All styles honor widthCols × maxHalfRows.
func RenderFrameH(f *FramePixels, mode PixelMode, widthCols, maxHalfRows int) string {
	if f == nil || f.W == 0 || f.H == 0 {
		return dimStyle.Render("  no video — waiting for frames / c cam / a sim  ")
	}
	geom := StyleGeomFromBudget(widthCols, maxHalfRows, f.W, f.H)

	// work frame: downsample for heavy styles under stream
	work := styleWorkFrame(f, mode, geom)
	if work == nil {
		return dimStyle.Render("  no video  ")
	}

	// preprocess (may mutate work)
	switch mode {
	case PixelBlocks, PixelPoints, PixelHalftone, PixelDepth, PixelGsplat,
		PixelEdges, PixelPoster, PixelScan, PixelDither, PixelNeon:
		applyPixelStyleGeom(work, mode, geom)
	}

	var body string
	switch mode {
	case PixelHex:
		body = renderHex(work, geom)
	case PixelBraille:
		body = renderBraille(work, geom)
	case PixelASCII:
		body = renderASCII(work, geom)
	default:
		// half-family + preprocess styles
		body = renderHalf(work, geom.Cols)
	}
	// hard clamp all styles to budget
	body = fitHalfBlock(body, geom.Cols, geom.Rows)
	return body
}

// Clone deep-copies RGB buffer.
func (f *FramePixels) Clone() *FramePixels {
	if f == nil {
		return nil
	}
	cp := make([]byte, len(f.RGB))
	copy(cp, f.RGB)
	return &FramePixels{W: f.W, H: f.H, RGB: cp, Source: f.Source, Stamp: f.Stamp}
}

// applyPixelStyleGeom mutates f with scale-aware cell sizes.
func applyPixelStyleGeom(f *FramePixels, mode PixelMode, geom StyleGeom) {
	if f == nil {
		return
	}
	cell := geom.Cell
	switch mode {
	case PixelBlocks:
		applyBlocks(f, cell)
	case PixelPoints:
		applyPoints(f, max(3, cell+1))
	case PixelHalftone:
		applyHalftone(f, max(3, cell+2))
	case PixelDepth:
		if dm := depthCacheGet(f, mode); dm != nil {
			ApplyDepthColorize(f, dm)
		}
	case PixelGsplat:
		if dm := depthCacheGet(f, mode); dm != nil {
			ApplyGsplatStack(f, dm, defaultGsplat, float64(time.Now().UnixMilli())/1000)
		}
	case PixelEdges:
		applyEdges(f)
	case PixelPoster:
		applyPoster(f, 5)
	case PixelScan:
		applyScanlines(f)
	case PixelDither:
		applyDither(f)
	case PixelNeon:
		applyNeon(f)
	}
}

// applyPixelStyle keeps old API for tests/callers without geom.
func applyPixelStyle(f *FramePixels, mode PixelMode) {
	applyPixelStyleGeom(f, mode, StyleGeomFromBudget(max(16, f.W/2), max(4, f.H/2), f.W, f.H))
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

func applyEdges(f *FramePixels) {
	src := f.Clone()
	for y := 1; y < f.H-1; y++ {
		for x := 1; x < f.W-1; x++ {
			// sobel-ish on luminance
			tl := src.lum(x-1, y-1)
			tc := src.lum(x, y-1)
			tr := src.lum(x+1, y-1)
			ml := src.lum(x-1, y)
			mr := src.lum(x+1, y)
			bl := src.lum(x-1, y+1)
			bc := src.lum(x, y+1)
			br := src.lum(x+1, y+1)
			gx := -tl + tr - 2*ml + 2*mr - bl + br
			gy := -tl - 2*tc - tr + bl + 2*bc + br
			mag := math.Min(1, math.Hypot(gx, gy)*1.4)
			v := byte(mag * 255)
			i := (y*f.W + x) * 3
			// tint cyan on edges over dark
			f.RGB[i] = v / 3
			f.RGB[i+1] = v
			f.RGB[i+2] = byte(min(255, int(v)+40))
		}
	}
}

func applyPoster(f *FramePixels, levels int) {
	if levels < 2 {
		levels = 2
	}
	step := 256 / levels
	if step < 1 {
		step = 1
	}
	for i := 0; i < len(f.RGB); i++ {
		v := int(f.RGB[i])
		f.RGB[i] = byte((v/step)*step + step/2)
	}
}

func applyScanlines(f *FramePixels) {
	for y := 0; y < f.H; y++ {
		if y%2 == 1 {
			for x := 0; x < f.W; x++ {
				i := (y*f.W + x) * 3
				f.RGB[i] = f.RGB[i] / 3
				f.RGB[i+1] = f.RGB[i+1] / 3
				f.RGB[i+2] = f.RGB[i+2] / 3
			}
		}
	}
}

// Bayer 4×4 ordered dither to mono green-terminal look.
func applyDither(f *FramePixels) {
	bayer := [4][4]float64{
		{0, 8, 2, 10},
		{12, 4, 14, 6},
		{3, 11, 1, 9},
		{15, 7, 13, 5},
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			L := f.lum(x, y)
			t := (bayer[y%4][x%4] + 0.5) / 16
			on := L > t
			i := (y*f.W + x) * 3
			if on {
				f.RGB[i], f.RGB[i+1], f.RGB[i+2] = 40, 220, 120
			} else {
				f.RGB[i], f.RGB[i+1], f.RGB[i+2] = 8, 12, 10
			}
		}
	}
}

func applyNeon(f *FramePixels) {
	src := f.Clone()
	// base dim + edge bloom
	for i := 0; i < len(f.RGB); i += 3 {
		f.RGB[i] = src.RGB[i] / 5
		f.RGB[i+1] = src.RGB[i+1] / 5
		f.RGB[i+2] = src.RGB[i+2] / 4
	}
	for y := 1; y < f.H-1; y++ {
		for x := 1; x < f.W-1; x++ {
			c := src.lum(x, y)
			n := (src.lum(x-1, y) + src.lum(x+1, y) + src.lum(x, y-1) + src.lum(x, y+1)) / 4
			edge := math.Abs(c - n)
			if edge < 0.08 {
				continue
			}
			boost := byte(min(255, int(edge*400)))
			r, g, b := src.at(x, y)
			i := (y*f.W + x) * 3
			f.RGB[i] = byte(min(255, int(r)/2+int(boost)))
			f.RGB[i+1] = byte(min(255, int(g)/3+int(boost)))
			f.RGB[i+2] = byte(min(255, int(b)+int(boost)))
		}
	}
}

func renderHalf(f *FramePixels, widthCols int) string {
	cols := widthCols
	if cols < 1 {
		cols = 1
	}
	var b strings.Builder
	b.Grow(cols * (f.H/2 + 1) * 20)
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

// renderHex — mosaic glyphs; fully fills geom.Cols × geom.Rows.
func renderHex(f *FramePixels, geom StyleGeom) string {
	glyphs := []string{"·", "▫", "▪", "◆", "◈", "◉", "●", "█"}
	cols := geom.Cols
	rows := geom.Rows
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	var b strings.Builder
	for row := 0; row < rows; row++ {
		sy := 0
		if f.H > 0 {
			sy = row * f.H / rows
			if sy >= f.H {
				sy = f.H - 1
			}
		}
		for x := 0; x < cols; x++ {
			sx := 0
			if f.W > 0 {
				sx = x * f.W / cols
				if sx >= f.W {
					sx = f.W - 1
				}
			}
			r, g, bl := f.at(sx, sy)
			lum := f.lum(sx, sy)
			idx := int(lum * float64(len(glyphs)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(glyphs) {
				idx = len(glyphs) - 1
			}
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
		if row+1 < rows {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderBraille(f *FramePixels, geom StyleGeom) string {
	cols := geom.Cols
	rows := geom.Rows
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	var b strings.Builder
	// map each output cell to 2×4 source region
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			// source origin for this cell
			sx0 := col * f.W / cols
			sy0 := row * f.H / rows
			sxStep := max(1, f.W/(cols*2))
			syStep := max(1, f.H/(rows*4))
			var bits rune
			dots := [4][2]rune{
				{0x01, 0x08},
				{0x02, 0x10},
				{0x04, 0x20},
				{0x40, 0x80},
			}
			var sr, sg, sb, n int
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx := sx0 + dx*sxStep
					sy := sy0 + dy*syStep
					if sx >= f.W {
						sx = f.W - 1
					}
					if sy >= f.H {
						sy = f.H - 1
					}
					if f.lum(sx, sy) > 0.42 {
						bits |= dots[dy][dx]
					}
					r, g, bl := f.at(sx, sy)
					sr += int(r)
					sg += int(g)
					sb += int(bl)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			ch := string(rune(0x2800 | bits))
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(rgbHex(byte(sr/n), byte(sg/n), byte(sb/n)))).
				Render(ch)
			b.WriteString(cell)
		}
		if row+1 < rows {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func renderASCII(f *FramePixels, geom StyleGeom) string {
	ramp := []rune(" .'`^\",:;Il!i><~+_-?][}{1)(|\\/tfjrxnuvczXYUJCLQ0OZmwqpdbkhao*#MW&8%B@$")
	cols := geom.Cols
	rows := geom.Rows
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	var b strings.Builder
	for row := 0; row < rows; row++ {
		sy := 0
		if f.H > 0 {
			sy = row * f.H / rows
			if sy >= f.H {
				sy = f.H - 1
			}
		}
		for col := 0; col < cols; col++ {
			sx := 0
			if f.W > 0 {
				sx = col * f.W / cols
				if sx >= f.W {
					sx = f.W - 1
				}
			}
			lum := f.lum(sx, sy)
			idx := int(lum * float64(len(ramp)-1))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(ramp) {
				idx = len(ramp) - 1
			}
			r, g, bl := f.at(sx, sy)
			cell := lipgloss.NewStyle().
				Foreground(lipgloss.Color(rgbHex(r, g, bl))).
				Render(string(ramp[idx]))
			b.WriteString(cell)
		}
		if row+1 < rows {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// StyleStreamInterval returns min time between lab ticks for this style under stream.
func StyleStreamInterval(mode PixelMode, baseFPS int) time.Duration {
	if baseFPS < 1 {
		baseFPS = 12
	}
	// heavy styles reduce effective FPS
	cost := mode.StyleCost()
	fps := baseFPS
	switch {
	case cost >= 5:
		fps = min(baseFPS, 6)
	case cost >= 4:
		fps = min(baseFPS, 8)
	case cost >= 3:
		fps = min(baseFPS, 10)
	}
	if fps < 1 {
		fps = 1
	}
	return time.Second / time.Duration(fps)
}

// StyleDecodeBudget caps JPEG/decode resolution while a style is active.
// Keeps stream handling responsive under filters (depth/gsplat especially).
func StyleDecodeBudget(mode PixelMode, baseW, baseH int) (int, int) {
	if baseW < 8 {
		baseW = 48
	}
	if baseH < 4 {
		baseH = 32
	}
	cost := mode.StyleCost()
	maxW, maxH := baseW, baseH
	switch {
	case cost >= 5:
		maxW, maxH = 64, 48
	case cost >= 4:
		maxW, maxH = 80, 56
	case cost >= 3:
		maxW, maxH = 96, 64
	case cost >= 2:
		maxW, maxH = 128, 80
	}
	if baseW < maxW {
		maxW = baseW
	}
	if baseH < maxH {
		maxH = baseH
	}
	if maxH%2 != 0 {
		maxH++
	}
	return maxW, maxH
}

// StyleSimBudget caps procedural sim frame size under heavy filters.
func StyleSimBudget(mode PixelMode, scale int) (w, h int) {
	pw := min(scale, 96)
	if pw < 24 {
		pw = 24
	}
	if mode.HeavyStyle() {
		pw = min(pw, 64)
	} else if mode.StyleCost() >= 2 {
		pw = min(pw, 80)
	}
	ph := max(12, pw*10/16)
	if ph%2 != 0 {
		ph++
	}
	return pw, ph
}

func itoa64(n int64) string {
	return itoa(int(n))
}

// unused import guard for color package used indirectly
var _ = color.Black
