package main

import (
	"fmt"
	"math"
	"strings"
)

// Glyph matrix sizes from Nothing Glyph Matrix Developer Kit.
// https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit
// Phone (3) DEVICE_23112 = 25×25 · Phone (4a) Pro DEVICE_25111p = 13×13.
// Spec SVGs (25111_spec / 23112_spec): circular active LED disk, square LEDs
// with pitch > LED size (~0.73 fill), dark housing between LEDs.
const (
	GlyphPhone3  = 25
	GlyphPhone4a = 13
	// Terminal hi-res (not hardware): denser circular matrices for large terms.
	GlyphRes37 = 37
	GlyphRes49 = 49
	// LED pitch fill ratio from official SVG (~6.93 / 9.47).
	glyphLEDFill = 0.73
	// Display scale: cells per LED pitch (1 = exact N×N, 2+ = scale-up).
	GlyphScaleMin = 1
	GlyphScaleMax = 8
	// Siri-sized orb: fixed terminal footprint (popup, not full dock)
	OrbCols = 28
	OrbRows = 11
)

// GlyphResLadder is matrix N options: hardware first, then terminal resolution increases.
var GlyphResLadder = []int{GlyphPhone4a, GlyphPhone3, GlyphRes37, GlyphRes49}

// NormalizeGlyphN clamps user preference to a ladder size (13/25/37/49).
func NormalizeGlyphN(n int) int {
	switch n {
	case GlyphPhone4a, GlyphPhone3, GlyphRes37, GlyphRes49:
		return n
	}
	if n >= 40 {
		return GlyphRes49
	}
	if n >= 30 {
		return GlyphRes37
	}
	if n >= 18 {
		return GlyphPhone3
	}
	if n >= 8 {
		return GlyphPhone4a
	}
	return GlyphPhone3
}

// ClampGlyphDisplayN allows any matrix side for terminal fit (e.g. 7–96).
// Used when auto-fitting full circles into small windows like 80×24.
func ClampGlyphDisplayN(n int) int {
	if n < 5 {
		return 5
	}
	if n > 96 {
		return 96
	}
	return n
}

// GlyphDeviceN returns the hardware matrix length for mesh / Nothing SDK.
// Hi-res terminal N still maps to Phone (3)=25 or Phone (4a)=13 for ship.
func GlyphDeviceN(displayN int) int {
	displayN = NormalizeGlyphN(displayN)
	if displayN <= GlyphPhone4a {
		return GlyphPhone4a
	}
	return GlyphPhone3
}

// GlyphRadius is the active-disk radius in LED indices matching official specs:
// 13×13 → r=6.5 (137 LEDs), 25×25 → r=12.5 (489 LEDs). General: n/2.
func GlyphRadius(n int) float64 {
	if n < 1 {
		n = GlyphPhone3
	}
	return float64(n) / 2
}

// ClampGlyphScale bounds display cells-per-LED pitch.
func ClampGlyphScale(s int) int {
	if s < GlyphScaleMin {
		return GlyphScaleMin
	}
	if s > GlyphScaleMax {
		return GlyphScaleMax
	}
	return s
}

// BurstChromeRows is fixed chrome outside the dual matrix panel
// (header, status, vu, hint). Panel gets the rest of the terminal.
const BurstChromeRows = 4

// BurstPanelBudget returns cells available for the dual title+matrix block.
// Never invents space — standard 80×24 → panel 20 rows.
func BurstPanelBudget(cols, rows int) (panelCols, panelRows int) {
	panelCols = cols
	if panelCols < 1 {
		panelCols = 1
	}
	panelRows = rows - BurstChromeRows
	if panelRows < 3 {
		panelRows = 3
	}
	return panelCols, panelRows
}

// maxDualDiam is the largest full-circle diameter (cells) that fits dual
// circles centered in terminal halves: width and height both constrain.
// panelRows includes 1 title line → matrix diam ≤ panelRows-1.
func maxDualDiam(cols, panelRows int) int {
	halfW := cols / 2
	if cols-halfW < halfW {
		halfW = cols - halfW
	}
	maxH := panelRows - 1 // title
	if maxH < 1 {
		maxH = 1
	}
	d := halfW
	if maxH < d {
		d = maxH
	}
	if d < 1 {
		d = 1
	}
	return d
}

// glyphFitsDual reports whether glyphN×ledScale full circles fit in cols×panelRows.
func glyphFitsDual(cols, panelRows, glyphN, ledScale int) bool {
	if glyphN < 1 || ledScale < 1 {
		return false
	}
	diam := glyphN * ledScale
	return diam <= maxDualDiam(cols, panelRows)
}

// AutoGlyphScale picks the largest LED pitch that fits dual (or single) circles.
// Never invents rows/cols — on 80×24 with N=25 returns 0-fit handled by FitGlyphDual.
func AutoGlyphScale(cols, rows, glyphN int, dual bool) int {
	glyphN = NormalizeGlyphN(glyphN)
	_, panelRows := BurstPanelBudget(cols, rows)
	availH := panelRows - 1 // title
	if availH < 1 {
		availH = 1
	}
	availW := cols
	if dual {
		availW = cols / 2
	}
	if availW < 1 {
		availW = 1
	}
	sH := availH / glyphN
	sW := availW / glyphN
	s := sH
	if sW < s {
		s = sW
	}
	if s < 1 {
		return 0 // does not fit — caller must drop N via FitGlyphDual
	}
	return ClampGlyphScale(s)
}

// FitGlyphDual chooses matrix N and LED scale so both full circles fit the
// real terminal. Standard 80×24 cannot host dual 25×25 (needs ~54×31); it
// auto-selects 13×13 (full disks). PreferN/PreferScale are soft targets.
// Returns displayN, ledScale (≥1), and whether preferN was downgraded.
func FitGlyphDual(cols, rows, preferN, preferScale int) (displayN, ledScale int, downgraded bool) {
	preferN = NormalizeGlyphN(preferN)
	_, panelRows := BurstPanelBudget(cols, rows)
	maxD := maxDualDiam(cols, panelRows)

	// Candidates: prefer, then ladder high→low, then step down to 7
	seen := map[int]bool{}
	var cands []int
	add := func(n int) {
		n = ClampGlyphDisplayN(n)
		if seen[n] {
			return
		}
		seen[n] = true
		cands = append(cands, n)
	}
	add(preferN)
	for i := len(GlyphResLadder) - 1; i >= 0; i-- {
		add(GlyphResLadder[i])
	}
	for n := maxD; n >= 7; n-- {
		add(n)
	}

	tryScale := func(n, want int) int {
		if n > maxD {
			return 0
		}
		maxS := maxD / n
		if maxS < 1 {
			return 0
		}
		maxS = ClampGlyphScale(maxS)
		if want <= 0 {
			return maxS // auto = largest full-circle scale
		}
		want = ClampGlyphScale(want)
		if want > maxS {
			return maxS
		}
		return want
	}

	for _, n := range cands {
		s := tryScale(n, preferScale)
		if s >= 1 {
			return n, s, n < preferN
		}
	}
	// last resort: diam = maxD (odd if possible)
	n := maxD
	if n < 5 {
		n = 5
	}
	return n, 1, true
}

// GlyphDisplayDiam returns terminal cells across one matrix at the given scale.
func GlyphDisplayDiam(glyphN, ledScale int) int {
	glyphN = ClampGlyphDisplayN(glyphN)
	ledScale = ClampGlyphScale(ledScale)
	return glyphN * ledScale
}

// cycleGlyphRes steps through hardware + terminal hi-res matrix sizes.
func cycleGlyphRes(n int) int {
	n = NormalizeGlyphN(n)
	for i, v := range GlyphResLadder {
		if v == n {
			return GlyphResLadder[(i+1)%len(GlyphResLadder)]
		}
	}
	return GlyphPhone3
}

// nudgeGlyphScale adjusts display scale. From auto (0), first nudge locks to fit±1.
func nudgeGlyphScale(cur, cols, rows, glyphN, delta int) int {
	_, auto, _ := FitGlyphDual(cols, rows, glyphN, 0)
	if auto < 1 {
		auto = 1
	}
	if cur <= 0 {
		cur = auto
	}
	return ClampGlyphScale(cur + delta)
}

func glyphScaleStatus(scale, glyphN, cols, rows int) string {
	dn, eff, down := FitGlyphDual(cols, rows, glyphN, scale)
	note := ""
	if down {
		note = fmt.Sprintf(" · display %d", dn)
	}
	if scale <= 0 {
		return fmt.Sprintf("glyph scale auto→%d · diam %d · prefer %d%s",
			eff, GlyphDisplayDiam(dn, eff), glyphN, note)
	}
	return fmt.Sprintf("glyph scale %d · diam %d · prefer %d%s",
		eff, GlyphDisplayDiam(dn, eff), glyphN, note)
}

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
// N may be hardware (13/25) or terminal hi-res (37/49).
// Source is center-cropped to a square so the face fills the circular matrix
// centered (not stretched / not half-frame).
func FrameToGlyphRGB(f *FramePixels, n int) GlyphRGB {
	if n < 5 {
		n = GlyphPhone3
	}
	if n > 96 {
		n = 96 // hard cap for terminal / memory
	}
	out := GlyphRGB{N: n, RGB: make([]byte, n*n*3)}
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < f.W*f.H*3 {
		return out
	}
	// center-crop to square
	side := f.W
	if f.H < side {
		side = f.H
	}
	if side < 1 {
		return out
	}
	ox := (f.W - side) / 2
	oy := (f.H - side) / 2
	for y := 0; y < n; y++ {
		sy0 := oy + y*side/n
		sy1 := oy + (y+1)*side/n
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		if sy1 > oy+side {
			sy1 = oy + side
		}
		if sy1 > f.H {
			sy1 = f.H
		}
		for x := 0; x < n; x++ {
			sx0 := ox + x*side/n
			sx1 := ox + (x+1)*side/n
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sx1 > ox+side {
				sx1 = ox + side
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
// Occupancy matches official Nothing specs (25111_spec 13×13, 23112_spec 25×25):
// radius = n/2 about cell centers ((n-1)/2, (n-1)/2).
func glyphInCircle(x, y, n int) bool {
	if n < 1 {
		return false
	}
	cx := float64(n-1) / 2
	cy := float64(n-1) / 2
	return math.Hypot(float64(x)-cx, float64(y)-cy) <= GlyphRadius(n)
}

// glyphActiveCount returns number of active LEDs for matrix N (spec: 137 @13, 489 @25).
func glyphActiveCount(n int) int {
	n = NormalizeGlyphN(n)
	c := 0
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if glyphInCircle(x, y, n) {
				c++
			}
		}
	}
	return c
}

// RenderBurstDual paints two Glyph Matrices (default Phone3, auto scale).
func RenderBurstDual(cols, rows int, local, peer *FramePixels, tx bool, rx, you string, style PixelMode) string {
	return RenderBurstDualGlyph(cols, rows, local, peer, tx, rx, you, GlyphPhone3)
}

// RenderBurstDualGlyph renders dual circles at exact device N with auto LED scale.
func RenderBurstDualGlyph(cols, rows int, local, peer *FramePixels, tx bool, rx, you string, glyphN int) string {
	return RenderBurstDualGlyphScaled(cols, rows, local, peer, tx, rx, you, glyphN, 0)
}

// RenderBurstDualGlyphScaled: Nothing-spec circular dual Glyph Matrices.
// Terminal is split into two equal halves; each circle is centered in its half
// (left=you, right=peer). Always draws full disks — if prefer N is too large for
// the window (e.g. dual 25 on 80×24), FitGlyphDual downgrades N/scale to fit.
// Full truecolor FG+BG LEDs; gaps when scale≥2 (SVG pitch).
//
// cols/rows here are the panel budget (not full terminal). Caller should pass
// BurstPanelBudget results, or full size (we treat rows as panel height).
func RenderBurstDualGlyphScaled(cols, rows int, local, peer *FramePixels, tx bool, rx, you string, glyphN, ledScale int) string {
	if cols < 8 {
		cols = 8
	}
	if rows < 4 {
		rows = 4
	}

	// Fit full circles into this panel (rows = panel height, includes title).
	// Prefer glyphN/ledScale; may downgrade N on small windows (80×24 → 13).
	preferN := glyphN
	if preferN < 5 {
		preferN = GlyphPhone3
	}
	displayN, displayScale, _ := fitGlyphInPanel(cols, rows, preferN, ledScale)
	glyphN = ClampGlyphDisplayN(displayN)
	ledScale = ClampGlyphScale(displayScale)
	diam := GlyphDisplayDiam(glyphN, ledScale)

	// Two equal halves of the real terminal frame
	halfW := cols / 2
	if halfW < 1 {
		halfW = 1
	}
	rightHalfW := cols - halfW

	// safety: never wider/taller than half / body
	if diam > halfW {
		diam = halfW
	}
	if diam > rightHalfW {
		diam = rightHalfW
	}
	if diam > rows-1 && rows > 1 {
		// should not happen after fit — last resort shrink
		for ledScale > 1 && glyphN*ledScale > rows-1 {
			ledScale--
		}
		for glyphN > 5 && glyphN*ledScale > rows-1 {
			glyphN--
		}
		diam = GlyphDisplayDiam(glyphN, ledScale)
		if diam > rows-1 {
			diam = rows - 1
		}
	}

	// Center each circle in its half of the frame
	leftOrigin := (halfW - diam) / 2
	if leftOrigin < 0 {
		leftOrigin = 0
	}
	rightOrigin := halfW + (rightHalfW-diam)/2
	if rightOrigin < halfW {
		rightOrigin = halfW
	}
	_ = rightOrigin

	youLabel := "you"
	if you != "" {
		youLabel = truncate(you, max(4, diam-2))
	}
	peerLabel := "peer"
	if rx != "" {
		peerLabel = truncate(rx, max(4, diam-2))
	}
	leftTitle := styDim().Render("◎ " + youLabel)
	if tx {
		leftTitle = styErr().Reverse(true).Render(" TX ") + styTitle().Render("◎ "+youLabel)
	}
	rightTitle := styDim().Render("◎ " + peerLabel)
	if rx != "" {
		rightTitle = styLive().Render(" RX ") + styTitle().Render("◎ "+peerLabel)
	}

	// Title: each label centered over its half
	title := placeInHalf(leftTitle, halfW, leftOrigin, diam) +
		placeInHalf(rightTitle, rightHalfW, (rightHalfW-diam)/2, diam)
	title = padOrTrim(title, cols)

	leftCircle := renderGlyphMatrix(local, glyphN, ledScale, tx, false)
	rightCircle := renderGlyphMatrix(peer, glyphN, ledScale, false, rx != "")

	ll := strings.Split(leftCircle, "\n")
	rl := strings.Split(rightCircle, "\n")
	for len(ll) < diam {
		ll = append(ll, strings.Repeat(" ", diam))
	}
	for len(rl) < diam {
		rl = append(rl, strings.Repeat(" ", diam))
	}

	// Vertical center matrix under title within rows
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
		// left half + right half; each circle centered in its half
		leftPart := placeGlyphLine(ll[i], halfW, leftOrigin, diam)
		rightPart := placeGlyphLine(rl[i], rightHalfW, (rightHalfW-diam)/2, diam)
		lines = append(lines, clampCells(leftPart+rightPart, cols))
	}
	for len(lines) < rows {
		lines = append(lines, strings.Repeat(" ", cols))
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return strings.Join(lines, "\n")
}

// placeInHalf centers content of visual width ≤ diam at origin within a half of width halfW.
func placeInHalf(content string, halfW, origin, diam int) string {
	if halfW < 1 {
		return ""
	}
	if origin < 0 {
		origin = 0
	}
	inner := padOrTrim(content, diam)
	var b strings.Builder
	if origin > 0 {
		b.WriteString(strings.Repeat(" ", origin))
	}
	b.WriteString(inner)
	// pad rest of half
	used := origin + diam
	if used < halfW {
		b.WriteString(strings.Repeat(" ", halfW-used))
	}
	return padOrTrim(b.String(), halfW)
}

// placeGlyphLine places one matrix row (exactly diam cells) centered in a half.
func placeGlyphLine(row string, halfW, origin, diam int) string {
	if halfW < 1 {
		return ""
	}
	if origin < 0 {
		origin = 0
	}
	// row already has exact diam cell width (ANSI truecolor LEDs)
	row = padOrTrim(row, diam)
	var b strings.Builder
	if origin > 0 {
		b.WriteString(strings.Repeat(" ", origin))
	}
	b.WriteString(row)
	used := origin + diam
	if used < halfW {
		b.WriteString(strings.Repeat(" ", halfW-used))
	}
	// hard cap half width (never spill into the other half)
	return padOrTrim(b.String(), halfW)
}

// fitGlyphInPanel like FitGlyphDual but panelRows is the dual block height
// (title + matrix), not full terminal. Used by the dual renderer.
func fitGlyphInPanel(cols, panelRows, preferN, preferScale int) (displayN, ledScale int, downgraded bool) {
	preferN = NormalizeGlyphN(preferN)
	maxD := maxDualDiam(cols, panelRows)

	seen := map[int]bool{}
	var cands []int
	add := func(n int) {
		n = ClampGlyphDisplayN(n)
		if seen[n] {
			return
		}
		seen[n] = true
		cands = append(cands, n)
	}
	add(preferN)
	for i := len(GlyphResLadder) - 1; i >= 0; i-- {
		add(GlyphResLadder[i])
	}
	for n := maxD; n >= 5; n-- {
		add(n)
	}

	tryScale := func(n, want int) int {
		if n > maxD {
			return 0
		}
		maxS := maxD / n
		if maxS < 1 {
			return 0
		}
		maxS = ClampGlyphScale(maxS)
		if want <= 0 {
			return maxS
		}
		want = ClampGlyphScale(want)
		if want > maxS {
			return maxS
		}
		return want
	}

	for _, n := range cands {
		s := tryScale(n, preferScale)
		if s >= 1 {
			return n, s, n < preferN
		}
	}
	n := maxD
	if n < 5 {
		n = 5
	}
	return n, 1, true
}

// renderGlyphMatrix paints one Nothing-spec circular Glyph Matrix.
// Layout matches 25111_spec / 23112_spec: circular LED allocation, square LEDs,
// dark housing in gaps (when ledScale≥2), full truecolor active LEDs.
// Output is exactly (N*ledScale) lines × (N*ledScale) cells.
func renderGlyphMatrix(f *FramePixels, glyphN, ledScale int, hotTX, hotRX bool) string {
	glyphN = ClampGlyphDisplayN(glyphN)
	ledScale = ClampGlyphScale(ledScale)
	gc := FrameToGlyphRGB(f, glyphN)
	hasFrame := f != nil && f.W > 0 && f.H > 0 && len(gc.RGB) >= glyphN*glyphN*3

	// LED body size within each pitch cell (SVG fill ≈ 0.73)
	ledBody := ledScale
	if ledScale >= 2 {
		ledBody = int(math.Round(float64(ledScale) * glyphLEDFill))
		if ledBody < 1 {
			ledBody = 1
		}
		if ledBody >= ledScale {
			ledBody = ledScale - 1 // at least 1-cell gap between LEDs
		}
	}
	pad := (ledScale - ledBody) / 2 // center LED body in pitch

	// Housing color from official SVG (#1C1C1C)
	const houseR, houseG, houseB byte = 0x1c, 0x1c, 0x1c

	diam := glyphN * ledScale
	// Precompute lines as builders
	lines := make([]strings.Builder, diam)
	for y := 0; y < diam; y++ {
		lines[y].Grow(diam * 24)
	}

	for ly := 0; ly < glyphN; ly++ {
		for lx := 0; lx < glyphN; lx++ {
			active := glyphInCircle(lx, ly, glyphN)
			var r, g, bl byte
			if active {
				if hasFrame {
					i := (ly*glyphN + lx) * 3
					r, g, bl = gc.RGB[i], gc.RGB[i+1], gc.RGB[i+2]
				} else {
					r, g, bl = 28, 28, 36 // unlit LED
				}
				if glyphOnBezel(lx, ly, glyphN) {
					if hotTX {
						r, g, bl = tintRGB(r, g, bl, 255, 60, 60, 0.45)
					} else if hotRX {
						r, g, bl = tintRGB(r, g, bl, 60, 220, 120, 0.40)
					}
				}
			}
			// paint pitch block: housing + optional LED body
			baseY := ly * ledScale
			for dy := 0; dy < ledScale; dy++ {
				sy := baseY + dy
				for dx := 0; dx < ledScale; dx++ {
					if !active {
						// outside circular allocation — empty (transparent to terminal bg)
						lines[sy].WriteByte(' ')
						continue
					}
					// gap vs LED body
					inBody := dx >= pad && dx < pad+ledBody && dy >= pad && dy < pad+ledBody
					if !inBody && ledScale >= 2 {
						// inter-LED housing (black gap like spec SVG)
						lines[sy].WriteString(truecolorLED(houseR, houseG, houseB))
						continue
					}
					lines[sy].WriteString(truecolorLED(r, g, bl))
				}
			}
		}
	}

	out := make([]string, diam)
	for i := range lines {
		out[i] = lines[i].String() + "\x1b[0m"
	}
	return strings.Join(out, "\n")
}

// renderGlyphCircleExact is the scale=1 path (1 cell = 1 LED). Kept for tests/callers.
func renderGlyphCircleExact(f *FramePixels, diam, glyphN int, hotTX, hotRX bool) string {
	_ = diam
	return renderGlyphMatrix(f, glyphN, 1, hotTX, hotRX)
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
// Uses space + FG/BG truecolor (always width 1). Full-block █ is ambiguous-width
// under EastAsianWidth and was clamping each circle to half the frame.
func truecolorLED(r, g, b byte) string {
	// near-black still uses truecolor (dark lattice LED), never mono-only path
	if int(r)+int(g)+int(b) < 18 {
		r, g, b = 22, 22, 28
	}
	var sb strings.Builder
	// FG + BG like renderHalf; space is always 1 cell (solid LED via BG)
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
	sb.WriteString("m ")
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
