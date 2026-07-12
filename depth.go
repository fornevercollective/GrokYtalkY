package main

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Depth / gsplat live path — vocabulary shared with:
//   aito spatial-depth · aito-mac zipdepth-sidecar · overview videoLabEffects
// Real ZipDepth (https://zipdepth.github.io) via optional HTTP sidecar :8766;
// offline path = zip-lite multi-scale mono proxy + gsplat-style depth stack.

// DepthMode selects monocular depth estimate + render style.
type DepthMode int

const (
	DepthOff DepthMode = iota
	DepthZipLite                       // multi-scale CPU (aito-mac zip-lite)
	DepthZipDepth                      // sidecar POST /depth when available
	DepthGsplat                        // gsplat-style depth stack (overview VFL)
	DepthCount
)

func (m DepthMode) String() string {
	switch m {
	case DepthOff:
		return "off"
	case DepthZipLite:
		return "zip-lite"
	case DepthZipDepth:
		return "zipdepth"
	case DepthGsplat:
		return "gsplat"
	default:
		return "?"
	}
}

// GsplatTune mirrors overview GsplatDepthTune (CPU proxy, not real 3DGS).
type GsplatTune struct {
	Radial    float64 // center→edge weight
	Vertical  float64
	Luminance float64
	ZPow      float64
	SweepHz   float64
	SweepZM   float64
}

var defaultGsplat = GsplatTune{
	Radial: 0.36, Vertical: 0.34, Luminance: 0.3,
	ZPow: 1.28, SweepHz: 1.65, SweepZM: 4.25,
}

// DepthMap is a normalized 0..1 inverse-depth field (near=high when graded near-warm).
type DepthMap struct {
	W, H   int
	Z      []float64 // len W*H, 0 far → 1 near-ish (normalized)
	Via    string
	Stamp  time.Time
}

func (d *DepthMap) at(x, y int) float64 {
	if d == nil || d.W < 1 || x < 0 || y < 0 || x >= d.W || y >= d.H {
		return 0.5
	}
	return d.Z[y*d.W+x]
}

// ── zip-lite (from aito-mac booth_zipdepth._pure_lite + multi-scale cues) ─

// EstimateZipLite builds a multi-scale monocular depth proxy from RGB24.
func EstimateZipLite(rgb []byte, w, h int) *DepthMap {
	if w < 2 || h < 2 || len(rgb) < w*h*3 {
		return nil
	}
	n := w * h
	lum := make([]float64, n)
	for i := 0; i < n; i++ {
		o := i * 3
		lum[i] = (0.299*float64(rgb[o]) + 0.587*float64(rgb[o+1]) + 0.114*float64(rgb[o+2])) / 255
	}

	// cheap box-ish blur levels via down/up sample
	b1 := boxBlurLum(lum, w, h, 1)
	b2 := boxBlurLum(b1, w, h, 2)
	b4 := boxBlurLum(b2, w, h, 2)

	z := make([]float64, n)
	var zmin, zmax float64 = 1e9, -1e9
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			L := lum[i]
			// gradients
			xm, xp := max(0, x-1), min(w-1, x+1)
			ym, yp := max(0, y-1), min(h-1, y+1)
			gx := lum[y*w+xp] - lum[y*w+xm]
			gy := lum[yp*w+x] - lum[ym*w+x]
			grad := math.Hypot(gx, gy) * 0.5
			detail := math.Abs(L - b2[i])
			multi := math.Abs(b1[i]-b2[i])*0.45 + math.Abs(b2[i]-b4[i])*0.55
			// strip-ish: deviation from row/col mean (approx with local)
			rowM := (lum[y*w] + lum[y*w+w/2] + lum[y*w+w-1]) / 3
			colM := (lum[x] + lum[(h/2)*w+x] + lum[(h-1)*w+x]) / 3
			strip := math.Abs(L-rowM)*0.55 + math.Abs(L-colM)*0.45

			nx := float64(x) / float64(max(1, w-1))
			ny := float64(y) / float64(max(1, h-1))
			radial := math.Hypot(nx-0.5, ny-0.42)
			base := (1-radial*0.95)*0.32 + (1-L)*0.22 + (1-ny)*0.14 + b4[i]*0.08
			v := base + detail*0.47 + multi*0.55 + grad*0.22 + strip*0.2
			z[i] = v
			if v < zmin {
				zmin = v
			}
			if v > zmax {
				zmax = v
			}
		}
	}
	span := zmax - zmin
	if span < 1e-5 {
		span = 1
	}
	for i := range z {
		z[i] = 0.05 + (z[i]-zmin)/span*1.05
		if z[i] > 1 {
			z[i] = 1
		}
	}
	return &DepthMap{W: w, H: h, Z: z, Via: "zip-lite", Stamp: time.Now()}
}

func boxBlurLum(src []float64, w, h, r int) []float64 {
	if r <= 0 {
		out := make([]float64, len(src))
		copy(out, src)
		return out
	}
	out := make([]float64, len(src))
	// separable-ish 3-tap repeated
	tmp := make([]float64, len(src))
	copy(tmp, src)
	for pass := 0; pass < r; pass++ {
		// horizontal
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := 0.0
				c := 0
				for dx := -1; dx <= 1; dx++ {
					xx := x + dx
					if xx < 0 || xx >= w {
						continue
					}
					s += tmp[y*w+xx]
					c++
				}
				out[y*w+x] = s / float64(c)
			}
		}
		copy(tmp, out)
		// vertical
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s := 0.0
				c := 0
				for dy := -1; dy <= 1; dy++ {
					yy := y + dy
					if yy < 0 || yy >= h {
						continue
					}
					s += tmp[yy*w+x]
					c++
				}
				out[y*w+x] = s / float64(c)
			}
		}
		copy(tmp, out)
	}
	return out
}

// ── gsplat-style depth proxy (overview videoLabEffects) ──────

func EstimateGsplatProxy(rgb []byte, w, h int, tune GsplatTune, tSec float64) *DepthMap {
	if w < 2 || h < 2 || len(rgb) < w*h*3 {
		return nil
	}
	n := w * h
	z := make([]float64, n)
	cx0 := float64(w) * 0.5
	cy0 := float64(h) * 0.5
	maxR := math.Hypot(cx0, cy0)
	if maxR < 1 {
		maxR = 1
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			o := i * 3
			L := (0.299*float64(rgb[o]) + 0.587*float64(rgb[o+1]) + 0.114*float64(rgb[o+2])) / 255
			radial := math.Hypot(float64(x)-cx0, float64(y)-cy0) / maxR
			vert := float64(y) / float64(max(1, h-1))
			zz := tune.Radial*(1-radial) + tune.Vertical*vert + tune.Luminance*(1-L)
			if zz < 0 {
				zz = 0
			}
			if zz > 1 {
				zz = 1
			}
			// subtle temporal shimmer for “live” gsplat
			zz += 0.04 * math.Sin(tSec*tune.SweepHz+zz*math.Pi*tune.SweepZM)
			if zz < 0 {
				zz = 0
			}
			if zz > 1 {
				zz = 1
			}
			z[i] = zz
		}
	}
	return &DepthMap{W: w, H: h, Z: z, Via: "gsplat", Stamp: time.Now()}
}

// ── render depth onto FramePixels (half-block friendly RGB) ──

// ApplyDepthColorize writes a Magma-ish / thermal depth false-color into f.RGB.
func ApplyDepthColorize(f *FramePixels, dm *DepthMap) {
	if f == nil || dm == nil || f.W != dm.W || f.H != dm.H {
		return
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			z := dm.at(x, y)
			r, g, b := depthTurbo(z)
			i := (y*f.W + x) * 3
			f.RGB[i], f.RGB[i+1], f.RGB[i+2] = r, g, b
		}
	}
}

// ApplyGsplatStack blends original RGB with thermal depth stack (overview-style).
func ApplyGsplatStack(f *FramePixels, dm *DepthMap, tune GsplatTune, tSec float64) {
	if f == nil || dm == nil || f.W != dm.W || f.H != dm.H {
		return
	}
	cx0 := float64(f.W) * 0.5
	cy0 := float64(f.H) * 0.5
	maxR := math.Hypot(cx0, cy0)
	if maxR < 1 {
		maxR = 1
	}
	// precompute luminance for edges
	n := f.W * f.H
	L0 := make([]float64, n)
	for i := 0; i < n; i++ {
		o := i * 3
		L0[i] = (0.299*float64(f.RGB[o]) + 0.587*float64(f.RGB[o+1]) + 0.114*float64(f.RGB[o+2])) / 255
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			i := y*f.W + x
			o := i * 3
			z := dm.at(x, y)
			// edge
			xm, xp := max(0, x-1), min(f.W-1, x+1)
			ym, yp := max(0, y-1), min(f.H-1, y+1)
			gx := L0[y*f.W+xp] - L0[y*f.W+xm]
			gy := L0[yp*f.W+x] - L0[ym*f.W+x]
			edge := math.Min(1, math.Hypot(gx, gy)*20)
			px := (float64(x) - cx0) / maxR
			py := (float64(y) - cy0) / maxR
			lightDot := math.Max(0, math.Min(1, 0.55+0.35*px-0.15*py))
			shade := (0.22 + 0.78*lightDot) * (1 - edge*0.38)
			rim := edge * lightDot

			sr, sg, sb := float64(f.RGB[o]), float64(f.RGB[o+1]), float64(f.RGB[o+2])
			sr = sr*shade + rim*52
			sg = sg*shade + rim*38
			sb = sb*shade + rim*28

			sweep := math.Sin(tSec*tune.SweepHz+z*math.Pi*tune.SweepZM)*0.5 + 0.5
			floatFront := math.Pow(1-z, tune.ZPow)
			aT := 0.1 + 0.36*sweep + 0.46*floatFront*(0.48+0.52*sweep)
			if aT > 0.85 {
				aT = 0.85
			}
			tr, tg, tb := depthTurbo(z)
			sr = sr*(1-aT) + float64(tr)*aT
			sg = sg*(1-aT) + float64(tg)*aT
			sb = sb*(1-aT) + float64(tb)*aT

			f.RGB[o] = byte(clamp255(sr))
			f.RGB[o+1] = byte(clamp255(sg))
			f.RGB[o+2] = byte(clamp255(sb))
		}
	}
}

// depthTurbo ≈ turbo/magma for depth false-color (near=warm, far=cool).
func depthTurbo(z float64) (r, g, b byte) {
	if z < 0 {
		z = 0
	}
	if z > 1 {
		z = 1
	}
	// piecewise warm→cool
	// near (1): yellow/white · mid: orange · far (0): deep blue
	t := 1 - z // far high
	rr := 0.15 + 0.85*(1-t)*(1-t) + 0.4*z
	gg := 0.08 + 0.55*z + 0.25*(1-math.Abs(z-0.5)*2)
	bb := 0.35 + 0.65*t
	return byte(clamp255(rr * 255)), byte(clamp255(gg * 255)), byte(clamp255(bb * 255))
}

func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// DepthToGlyph maps depth field → N×N brightness for Glyph Matrix / burst.
func DepthToGlyph(dm *DepthMap, n int) GlyphMatrix {
	gm := GlyphMatrix{N: n, Data: make([]byte, n*n)}
	if dm == nil || dm.W < 1 {
		return gm
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			sx := x * dm.W / n
			sy := y * dm.H / n
			z := dm.at(sx, sy)
			// near = bright LED
			gm.Data[y*n+x] = byte(clamp255(z * 255))
		}
	}
	return gm
}

// ── live depth session (cached map + backend) ────────────────

type depthSession struct {
	mu     sync.Mutex
	mode   DepthMode
	map_   *DepthMap
	gsplat GsplatTune
	t0     time.Time
	via    string
}

func newDepthSession() *depthSession {
	return &depthSession{
		mode:   DepthOff,
		gsplat: defaultGsplat,
		t0:     time.Now(),
	}
}

func (s *depthSession) Mode() DepthMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

func (s *depthSession) SetMode(m DepthMode) {
	s.mu.Lock()
	s.mode = m
	s.mu.Unlock()
}

func (s *depthSession) Cycle() DepthMode {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = (s.mode + 1) % DepthCount
	return s.mode
}

func (s *depthSession) Via() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.via
}

// Process updates depth from frame and optionally recolors RGB in-place.
func (s *depthSession) Process(f *FramePixels) {
	if f == nil || s == nil {
		return
	}
	s.mu.Lock()
	mode := s.mode
	tune := s.gsplat
	s.mu.Unlock()
	if mode == DepthOff {
		return
	}

	tSec := time.Since(s.t0).Seconds()
	var dm *DepthMap
	via := ""

	switch mode {
	case DepthZipDepth:
		if m, err := FetchZipDepth(f.RGB, f.W, f.H); err == nil && m != nil {
			dm = m
			via = m.Via
		} else {
			dm = EstimateZipLite(f.RGB, f.W, f.H)
			via = "zip-lite(fallback)"
		}
		if dm != nil {
			ApplyDepthColorize(f, dm)
		}
	case DepthZipLite:
		dm = EstimateZipLite(f.RGB, f.W, f.H)
		via = "zip-lite"
		if dm != nil {
			ApplyDepthColorize(f, dm)
		}
	case DepthGsplat:
		// base z from zip-lite structure, render with gsplat stack
		base := EstimateZipLite(f.RGB, f.W, f.H)
		if base == nil {
			base = EstimateGsplatProxy(f.RGB, f.W, f.H, tune, tSec)
		}
		dm = base
		via = "gsplat+" + base.Via
		if dm != nil {
			ApplyGsplatStack(f, dm, tune, tSec)
		}
	}

	s.mu.Lock()
	s.map_ = dm
	s.via = via
	s.mu.Unlock()
}

func (s *depthSession) LastMap() *DepthMap {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.map_
}

func depthHelpLine() string {
	return "depth: d cycle · off|zip-lite|zipdepth|gsplat · sidecar :8766 (ZipDepth)"
}

func formatDepthStatus(s *depthSession) string {
	if s == nil {
		return "depth off"
	}
	m := s.Mode()
	if m == DepthOff {
		return "depth off"
	}
	via := s.Via()
	if via == "" {
		via = m.String()
	}
	return fmt.Sprintf("depth %s · %s", m.String(), via)
}

// DepthModesList for doctor/help.
func DepthModesList() string {
	var b strings.Builder
	b.WriteString("depth modes: ")
	for i := DepthMode(0); i < DepthCount; i++ {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(i.String())
	}
	return b.String()
}
