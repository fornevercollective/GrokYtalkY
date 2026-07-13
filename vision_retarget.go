package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Closed-loop retarget: SAM bbox → ffmpeg crop + retune focus encode (v1.72).
//
// After aito-sam fills VisionSegment bboxes, vision can retarget the focus
// news/watch pipeline so the supervised FFmpeg pipe crops to the subject
// before scale/fps — not observe-only.
//
// Env:
//
//	GY_VISION_RETARGET=1        enable auto SAM→crop retune (default ON with media)
//	GY_VISION_RETARGET_PAD=0.08 expand bbox (0–0.4)
//	GY_VISION_RETARGET_MIN=0.45 min segment score
//	GY_VISION_RETARGET_LABEL=   prefer label substring (default person|face|human)
//	GY_VISION_RETARGET_IOU=0.82 skip if crop ≈ current (IoU)

const (
	VisionMediaRetarget = "retarget"
)

// VisionCrop is a normalized crop window (0–1) for ffmpeg crop=iw*w:ih*h:iw*x:ih*y.
type VisionCrop struct {
	X, Y, W, H float64
	Label      string
	Score      float64
	Pad        float64
}

// Valid reports a usable crop (non-degenerate, not full-frame).
func (c VisionCrop) Valid() bool {
	if c.W < 0.04 || c.H < 0.04 {
		return false
	}
	if c.W > 0.98 && c.H > 0.98 {
		return false // nothing to gain
	}
	if c.X < 0 || c.Y < 0 || c.X+c.W > 1.02 || c.Y+c.H > 1.02 {
		// allow slight overshoot; still ok if mostly in range
		if c.W <= 0 || c.H <= 0 {
			return false
		}
	}
	return c.W > 0 && c.H > 0
}

// Area normalized area.
func (c VisionCrop) Area() float64 { return c.W * c.H }

// IoU with another crop (axis-aligned).
func (c VisionCrop) IoU(o VisionCrop) float64 {
	x1 := math.Max(c.X, o.X)
	y1 := math.Max(c.Y, o.Y)
	x2 := math.Min(c.X+c.W, o.X+o.W)
	y2 := math.Min(c.Y+c.H, o.Y+o.H)
	if x2 <= x1 || y2 <= y1 {
		return 0
	}
	inter := (x2 - x1) * (y2 - y1)
	union := c.Area() + o.Area() - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// FFmpegCropFilter builds crop+scale+fps+rgb24 chain.
// Uses relative expressions so source resolution is independent of tile size.
func (c VisionCrop) FFmpegCropFilter(outW, outH, fps int) string {
	if outW < 16 {
		outW = newsTileW
	}
	if outH < 9 {
		outH = newsTileH
	}
	if outH%2 != 0 {
		outH++
	}
	if fps < 1 {
		fps = newsTileFPS
	}
	// even dimensions via floor(*2)/2 pattern
	// crop=w:h:x:y with relative iw/ih
	cx, cy, cw, ch := c.X, c.Y, c.W, c.H
	// clamp
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx+cw > 1 {
		cw = 1 - cx
	}
	if cy+ch > 1 {
		ch = 1 - cy
	}
	if cw < 0.02 {
		cw = 0.02
	}
	if ch < 0.02 {
		ch = 0.02
	}
	// floor(iw*W/2)*2 keeps even width
	crop := fmt.Sprintf(
		"crop=floor(iw*%.4f/2)*2:floor(ih*%.4f/2)*2:floor(iw*%.4f/2)*2:floor(ih*%.4f/2)*2",
		cw, ch, cx, cy,
	)
	return fmt.Sprintf("%s,scale=%d:%d:flags=fast_bilinear,fps=%d,format=rgb24", crop, outW, outH, fps)
}

// RetargetConfig from env.
type RetargetConfig struct {
	Enabled  bool
	Pad      float64
	MinScore float64
	Prefer   string // comma labels
	MinIoU   float64 // skip if IoU with current >= this
}

// LoadRetargetConfig reads GY_VISION_RETARGET_*.
func LoadRetargetConfig() RetargetConfig {
	c := RetargetConfig{
		Enabled:  true,
		Pad:      0.08,
		MinScore: 0.45,
		Prefer:   "person,face,human,speaker,anchor",
		MinIoU:   0.82,
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_RETARGET")); v != "" {
		c.Enabled = envTruthy("GY_VISION_RETARGET")
	}
	// if media plane hard-off, retarget follows
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MEDIA")); v != "" && !envTruthy("GY_VISION_MEDIA") {
		c.Enabled = false
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_RETARGET_PAD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 0.4 {
			c.Pad = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_RETARGET_MIN")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			c.MinScore = f
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_RETARGET_LABEL")); v != "" {
		c.Prefer = strings.ToLower(v)
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_RETARGET_IOU")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0.5 && f <= 0.99 {
			c.MinIoU = f
		}
	}
	return c
}

// last retarget state (doctor + IoU skip)
type retargetState struct {
	mu      sync.Mutex
	last    VisionCrop
	lastAt  time.Time
	lastLab string
	count   int64
	skipped int64
}

var globalRetarget retargetState

// SelectRetargetSegment picks best SAM segment for closed-loop crop.
// Prefer preferred labels with score ≥ min; else highest score meeting min.
func SelectRetargetSegment(segs []VisionSegment, cfg RetargetConfig) (VisionSegment, bool) {
	if len(segs) == 0 {
		return VisionSegment{}, false
	}
	prefer := strings.Split(cfg.Prefer, ",")
	for i := range prefer {
		prefer[i] = strings.TrimSpace(prefer[i])
	}
	var best VisionSegment
	bestScore := -1.0
	preferHit := false
	for _, s := range segs {
		if s.Score < cfg.MinScore && s.Score > 0 {
			// score 0 may mean sidecar omitted score — allow if bbox valid
			if s.Score != 0 {
				continue
			}
		}
		bb := s.BBox
		// normalize if coords look like pixels (unlikely) — assume 0-1
		if bb[2] <= 0 || bb[3] <= 0 {
			continue
		}
		// if values look like absolute pixels, skip (require normalized)
		if bb[0] > 1.5 || bb[1] > 1.5 || bb[2] > 1.5 || bb[3] > 1.5 {
			continue
		}
		lab := strings.ToLower(s.Label)
		isPref := false
		for _, p := range prefer {
			if p != "" && strings.Contains(lab, p) {
				isPref = true
				break
			}
		}
		score := s.Score
		if score == 0 {
			score = 0.5
		}
		if isPref {
			score += 0.25 // boost preferred classes
		}
		if preferHit && !isPref {
			continue
		}
		if isPref && !preferHit {
			preferHit = true
			best = s
			bestScore = score
			continue
		}
		if score > bestScore {
			best = s
			bestScore = score
		}
	}
	if bestScore < 0 {
		return VisionSegment{}, false
	}
	return best, true
}

// SegmentToCrop expands bbox by pad and clamps to [0,1].
func SegmentToCrop(s VisionSegment, pad float64) VisionCrop {
	x, y, w, h := s.BBox[0], s.BBox[1], s.BBox[2], s.BBox[3]
	if pad > 0 {
		px, py := w*pad, h*pad
		// also pad by absolute fraction of frame
		px = math.Max(px, pad*0.5)
		py = math.Max(py, pad*0.5)
		x -= px
		y -= py
		w += 2 * px
		h += 2 * py
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > 1 {
		w = 1 - x
	}
	if y+h > 1 {
		h = 1 - y
	}
	if w < 0.02 {
		w = 0.02
	}
	if h < 0.02 {
		h = 0.02
	}
	// keep aspect roughly for glyph tiles (16:9-ish) by expanding shorter side
	targetAR := 16.0 / 9.0
	ar := w / h
	if ar < targetAR*0.85 {
		// too tall — widen
		nw := h * targetAR
		if nw > 1 {
			nw = 1
		}
		x -= (nw - w) / 2
		if x < 0 {
			x = 0
		}
		if x+nw > 1 {
			x = 1 - nw
		}
		w = nw
	} else if ar > targetAR*1.2 {
		// too wide — heighten
		nh := w / targetAR
		if nh > 1 {
			nh = 1
		}
		y -= (nh - h) / 2
		if y < 0 {
			y = 0
		}
		if y+nh > 1 {
			y = 1 - nh
		}
		h = nh
	}
	return VisionCrop{X: x, Y: y, W: w, H: h, Label: s.Label, Score: s.Score, Pad: pad}
}

// CropFromPipe reads current crop on a news tile (if any).
func CropFromPipe(tp *NewsTilePipe) VisionCrop {
	if tp == nil {
		return VisionCrop{}
	}
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if !tp.HasCrop {
		return VisionCrop{}
	}
	return VisionCrop{X: tp.CropX, Y: tp.CropY, W: tp.CropW, H: tp.CropH}
}

// ActionFromCrop builds a MEDIA retarget action.
func ActionFromCrop(crop VisionCrop, target string) VisionMediaAction {
	if target == "" {
		target = "focus"
	}
	return VisionMediaAction{
		Op:     VisionMediaRetarget,
		Target: target,
		CropX:  crop.X,
		CropY:  crop.Y,
		CropW:  crop.W,
		CropH:  crop.H,
		Source: crop.Label,
		Raw: fmt.Sprintf("MEDIA retarget %s crop=%.3f,%.3f,%.3f,%.3f # %s",
			target, crop.X, crop.Y, crop.W, crop.H, crop.Label),
	}
}

// DeriveSAMRetarget builds auto retarget action from segments (or nil).
func DeriveSAMRetarget(m *Model, segs []VisionSegment) *VisionMediaAction {
	cfg := LoadRetargetConfig()
	if !cfg.Enabled {
		return nil
	}
	seg, ok := SelectRetargetSegment(segs, cfg)
	if !ok {
		globalRetarget.mu.Lock()
		globalRetarget.skipped++
		globalRetarget.mu.Unlock()
		return nil
	}
	crop := SegmentToCrop(seg, cfg.Pad)
	if !crop.Valid() {
		return nil
	}
	// skip if similar to last / current pipe crop
	var cur VisionCrop
	if m != nil && m.lab != nil && m.lab.News != nil && m.lab.News.On {
		i := m.lab.Active
		if i >= 0 && i < len(m.lab.News.Pipes) {
			cur = CropFromPipe(m.lab.News.Pipes[i])
		}
	}
	globalRetarget.mu.Lock()
	last := globalRetarget.last
	globalRetarget.mu.Unlock()
	if cur.Valid() && crop.IoU(cur) >= cfg.MinIoU {
		globalRetarget.mu.Lock()
		globalRetarget.skipped++
		globalRetarget.mu.Unlock()
		return nil
	}
	if last.Valid() && crop.IoU(last) >= cfg.MinIoU && time.Since(globalRetarget.lastAt) < 20*time.Second {
		globalRetarget.mu.Lock()
		globalRetarget.skipped++
		globalRetarget.mu.Unlock()
		return nil
	}
	a := ActionFromCrop(crop, "focus")
	return &a
}

// AttachRetargetToTake appends MEDIA retarget from SAM segments when enabled.
// Called from RunVisionPipeline after side channels.
func AttachRetargetToTake(m *Model, res *VisionResult) {
	if res == nil || len(res.Segments) == 0 {
		return
	}
	// skip if take already has retarget/retune with crop
	for _, a := range res.Take.Media {
		if a.Op == VisionMediaRetarget || (a.Op == VisionMediaRetune && a.CropW > 0) {
			return
		}
	}
	act := DeriveSAMRetarget(m, res.Segments)
	if act == nil {
		return
	}
	res.Take.Media = append(res.Take.Media, *act)
	// also stamp note for status
	if res.Take.Note == "" {
		res.Take.Note = fmt.Sprintf("retarget %s", act.Source)
	}
}

// recordRetarget stores last crop for doctor / IoU.
func recordRetarget(crop VisionCrop, label string) {
	globalRetarget.mu.Lock()
	globalRetarget.last = crop
	globalRetarget.lastAt = time.Now()
	globalRetarget.lastLab = label
	globalRetarget.count++
	globalRetarget.mu.Unlock()
}

// FormatRetargetDoctor one-liner + detail for vision doctor.
func FormatRetargetDoctor() string {
	cfg := LoadRetargetConfig()
	globalRetarget.mu.Lock()
	last := globalRetarget.last
	lab := globalRetarget.lastLab
	n := globalRetarget.count
	sk := globalRetarget.skipped
	at := globalRetarget.lastAt
	globalRetarget.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "vision·retarget · enabled=%v pad=%.2f min_score=%.2f\n", cfg.Enabled, cfg.Pad, cfg.MinScore)
	fmt.Fprintf(&b, "  prefer    %s · iou_skip %.2f\n", cfg.Prefer, cfg.MinIoU)
	fmt.Fprintf(&b, "  applied   %d  skipped %d\n", n, sk)
	if last.Valid() {
		fmt.Fprintf(&b, "  last      %s  crop=[%.2f,%.2f,%.2f,%.2f] score=%.2f",
			emptyDash(lab), last.X, last.Y, last.W, last.H, last.Score)
		if !at.IsZero() {
			fmt.Fprintf(&b, " · %s", at.Format(time.RFC3339))
		}
		b.WriteByte('\n')
		fmt.Fprintf(&b, "  vf        %s\n", truncate(last.FFmpegCropFilter(newsTileW, newsTileH, newsTileFPS), 72))
	}
	b.WriteString("  env       GY_VISION_RETARGET=1 · PAD · MIN · LABEL · IOU\n")
	b.WriteString("  loop      SAM bbox → MEDIA retarget → ffmpeg crop+scale → focus encode\n")
	return b.String()
}

// CropFramePixels crops RGB frame by normalized bbox (for encode path without re-ffmpeg source).
func CropFramePixels(f *FramePixels, crop VisionCrop) *FramePixels {
	if f == nil || !crop.Valid() {
		return f
	}
	x0 := int(crop.X * float64(f.W))
	y0 := int(crop.Y * float64(f.H))
	x1 := int((crop.X + crop.W) * float64(f.W))
	y1 := int((crop.Y + crop.H) * float64(f.H))
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > f.W {
		x1 = f.W
	}
	if y1 > f.H {
		y1 = f.H
	}
	cw, ch := x1-x0, y1-y0
	if cw < 2 || ch < 2 {
		return f
	}
	if ch%2 != 0 {
		ch--
	}
	if cw%2 != 0 {
		cw--
	}
	rgb := make([]byte, cw*ch*3)
	for y := 0; y < ch; y++ {
		srcOff := ((y0+y)*f.W + x0) * 3
		dstOff := y * cw * 3
		copy(rgb[dstOff:dstOff+cw*3], f.RGB[srcOff:srcOff+cw*3])
	}
	return &FramePixels{
		W: cw, H: ch, RGB: rgb,
		Source: f.Source + "+crop",
		Stamp:  f.Stamp,
	}
}

// parseCropToken parses crop=x,y,w,h or crop=x:y:w:h
func parseCropToken(s string) (VisionCrop, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.ToLower(s), "crop=")
	s = strings.ReplaceAll(s, ":", ",")
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return VisionCrop{}, false
	}
	var v [4]float64
	for i := 0; i < 4; i++ {
		f, err := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
		if err != nil {
			return VisionCrop{}, false
		}
		v[i] = f
	}
	c := VisionCrop{X: v[0], Y: v[1], W: v[2], H: v[3]}
	return c, c.W > 0 && c.H > 0
}
