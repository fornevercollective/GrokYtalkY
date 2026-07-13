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

// CameraControls — standard phone / film image + lighting controls (v1.78).
// Aligned with aito adjustments (exposure/contrast/sat/temp/tint/clarity/…)
// plus phone MediaTrack fields (iso, shutter, wb, focus, torch, zoom).
//
// Applied as software grade on RGB frames (always) and exported on mesh as
// type:camera-controls / look:{} so glyph-cast, phone, and GrokGlyph stay in sync.
// Hardware constraints are browser-side (camera-controls.js applyConstraints).

// CameraLook is one complete look state (hardware intent + grade).
type CameraLook struct {
	// ── phone / film capture intent ──
	Facing      string  `json:"facing,omitempty"`       // user|environment
	FocusMode   string  `json:"focus_mode,omitempty"`  // continuous|manual|single-shot
	FocusDist   float64 `json:"focus_distance,omitempty"` // 0..1 normalized
	ExposureMode string `json:"exposure_mode,omitempty"` // continuous|manual
	EvComp      float64 `json:"ev,omitempty"`          // -3..+3 stops
	Shutter     string  `json:"shutter,omitempty"`     // e.g. 1/125
	ISO         int     `json:"iso,omitempty"`         // 50..12800
	WBMode      string  `json:"wb_mode,omitempty"`     // auto|manual|daylight|…
	ColorTempK  int     `json:"color_temp_k,omitempty"` // 2500..9000
	Torch       bool    `json:"torch,omitempty"`
	Zoom        float64 `json:"zoom,omitempty"` // 1..max
	FPS         int     `json:"fps,omitempty"`
	// ── aito-style grade (software, always) ──
	Exposure    float64 `json:"exposure"`              // -2..+2
	Contrast    float64 `json:"contrast"`              // -1..+1
	Saturation  float64 `json:"saturation"`            // -1..+1
	Temperature float64 `json:"temperature"`           // -1 cool .. +1 warm
	Tint        float64 `json:"tint"`                  // -1 green .. +1 magenta
	Clarity     float64 `json:"clarity"`               // -1..+1
	Sharpen     float64 `json:"sharpen"`               // 0..2
	Vignette    float64 `json:"vignette"`              // -1..+1
	// ── lighting fixes ──
	Brightness  float64 `json:"brightness"`            // -1..+1 soft lift
	Highlights  float64 `json:"highlights"`            // -1..+1 roll
	Shadows     float64 `json:"shadows"`               // -1..+1 lift
	Fill        float64 `json:"fill"`                  // 0..1 fill light
	Key         float64 `json:"key"`                   // 0..1 key intensity (UI meta)
	Night       bool    `json:"night,omitempty"`
	Grain       float64 `json:"grain"`                 // 0..1 film grain
	// ── film / preset ──
	Preset      string  `json:"preset,omitempty"` // daylight|tungsten|cloudy|neon|film|neutral
	LUT         string  `json:"lut,omitempty"`    // optional slug (aito film stock id)
	LUTIntensity float64 `json:"lut_intensity,omitempty"`
}

// DefaultCameraLook neutral phone/film baseline.
func DefaultCameraLook() CameraLook {
	return CameraLook{
		Facing: "environment", FocusMode: "continuous", ExposureMode: "continuous",
		WBMode: "auto", ISO: 0, Zoom: 1, FPS: 30,
		Preset: "neutral",
	}
}

// CameraBus process-wide active look (TUI + mesh).
type CameraBus struct {
	mu   sync.RWMutex
	look CameraLook
}

var (
	cameraOnce sync.Once
	cameraBus  *CameraBus
)

// Camera returns the global look bus.
func Camera() *CameraBus {
	cameraOnce.Do(func() {
		cameraBus = &CameraBus{look: DefaultCameraLook()}
		// optional env seed
		if p := strings.TrimSpace(os.Getenv("GY_CAMERA_PRESET")); p != "" {
			cameraBus.look = ApplyCameraPreset(cameraBus.look, p)
		}
	})
	return cameraBus
}

// Look snapshot.
func (c *CameraBus) Look() CameraLook {
	if c == nil {
		return DefaultCameraLook()
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.look
}

// SetLook replaces look.
func (c *CameraBus) SetLook(l CameraLook) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.look = clampCameraLook(l)
	c.mu.Unlock()
}

// Patch merges non-zero / set fields from patch (partial update).
func (c *CameraBus) Patch(patch CameraLook, keys map[string]bool) CameraLook {
	if c == nil {
		return DefaultCameraLook()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	l := c.look
	if keys == nil {
		// full replace of grade fields always; hardware only if non-zero/non-empty
		l = mergeCameraLook(l, patch)
	} else {
		l = mergeCameraLookKeys(l, patch, keys)
	}
	c.look = clampCameraLook(l)
	return c.look
}

func mergeCameraLook(base, p CameraLook) CameraLook {
	out := base
	if p.Facing != "" {
		out.Facing = p.Facing
	}
	if p.FocusMode != "" {
		out.FocusMode = p.FocusMode
	}
	if p.FocusDist != 0 {
		out.FocusDist = p.FocusDist
	}
	if p.ExposureMode != "" {
		out.ExposureMode = p.ExposureMode
	}
	if p.EvComp != 0 {
		out.EvComp = p.EvComp
	}
	if p.Shutter != "" {
		out.Shutter = p.Shutter
	}
	if p.ISO != 0 {
		out.ISO = p.ISO
	}
	if p.WBMode != "" {
		out.WBMode = p.WBMode
	}
	if p.ColorTempK != 0 {
		out.ColorTempK = p.ColorTempK
	}
	out.Torch = p.Torch // bool always take
	if p.Zoom != 0 {
		out.Zoom = p.Zoom
	}
	if p.FPS != 0 {
		out.FPS = p.FPS
	}
	// grade: always apply (0 is valid neutral — use patch when calling from full set)
	out.Exposure = p.Exposure
	out.Contrast = p.Contrast
	out.Saturation = p.Saturation
	out.Temperature = p.Temperature
	out.Tint = p.Tint
	out.Clarity = p.Clarity
	out.Sharpen = p.Sharpen
	out.Vignette = p.Vignette
	out.Brightness = p.Brightness
	out.Highlights = p.Highlights
	out.Shadows = p.Shadows
	out.Fill = p.Fill
	out.Key = p.Key
	out.Night = p.Night
	out.Grain = p.Grain
	if p.Preset != "" {
		out.Preset = p.Preset
	}
	if p.LUT != "" {
		out.LUT = p.LUT
	}
	out.LUTIntensity = p.LUTIntensity
	return out
}

func mergeCameraLookKeys(base, p CameraLook, keys map[string]bool) CameraLook {
	// for future partial mesh patches
	out := base
	set := func(k string) bool { return keys[k] }
	if set("facing") {
		out.Facing = p.Facing
	}
	if set("exposure") {
		out.Exposure = p.Exposure
	}
	if set("contrast") {
		out.Contrast = p.Contrast
	}
	if set("saturation") {
		out.Saturation = p.Saturation
	}
	if set("temperature") {
		out.Temperature = p.Temperature
	}
	if set("tint") {
		out.Tint = p.Tint
	}
	if set("brightness") {
		out.Brightness = p.Brightness
	}
	if set("highlights") {
		out.Highlights = p.Highlights
	}
	if set("shadows") {
		out.Shadows = p.Shadows
	}
	if set("fill") {
		out.Fill = p.Fill
	}
	if set("grain") {
		out.Grain = p.Grain
	}
	if set("night") {
		out.Night = p.Night
	}
	if set("iso") {
		out.ISO = p.ISO
	}
	if set("ev") {
		out.EvComp = p.EvComp
	}
	if set("wb_mode") {
		out.WBMode = p.WBMode
	}
	if set("color_temp_k") {
		out.ColorTempK = p.ColorTempK
	}
	if set("torch") {
		out.Torch = p.Torch
	}
	if set("preset") {
		out.Preset = p.Preset
	}
	return out
}

func clampCameraLook(l CameraLook) CameraLook {
	cl := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	l.Exposure = cl(l.Exposure, -2, 2)
	l.Contrast = cl(l.Contrast, -1, 1)
	l.Saturation = cl(l.Saturation, -1, 1)
	l.Temperature = cl(l.Temperature, -1, 1)
	l.Tint = cl(l.Tint, -1, 1)
	l.Clarity = cl(l.Clarity, -1, 1)
	l.Sharpen = cl(l.Sharpen, 0, 2)
	l.Vignette = cl(l.Vignette, -1, 1)
	l.Brightness = cl(l.Brightness, -1, 1)
	l.Highlights = cl(l.Highlights, -1, 1)
	l.Shadows = cl(l.Shadows, -1, 1)
	l.Fill = cl(l.Fill, 0, 1)
	l.Key = cl(l.Key, 0, 1)
	l.Grain = cl(l.Grain, 0, 1)
	l.EvComp = cl(l.EvComp, -3, 3)
	l.FocusDist = cl(l.FocusDist, 0, 1)
	if l.Zoom < 1 {
		l.Zoom = 1
	}
	if l.Zoom > 16 {
		l.Zoom = 16
	}
	if l.ISO < 0 {
		l.ISO = 0
	}
	if l.ISO > 12800 {
		l.ISO = 12800
	}
	if l.ColorTempK != 0 {
		if l.ColorTempK < 2500 {
			l.ColorTempK = 2500
		}
		if l.ColorTempK > 9000 {
			l.ColorTempK = 9000
		}
	}
	return l
}

// ApplyCameraPreset sets film/phone lighting presets (aito-compatible names).
func ApplyCameraPreset(base CameraLook, name string) CameraLook {
	l := base
	name = strings.ToLower(strings.TrimSpace(name))
	l.Preset = name
	switch name {
	case "neutral", "reset", "off", "":
		l.Exposure, l.Contrast, l.Saturation = 0, 0, 0
		l.Temperature, l.Tint = 0, 0
		l.Clarity, l.Sharpen, l.Vignette = 0, 0, 0
		l.Brightness, l.Highlights, l.Shadows, l.Fill, l.Grain = 0, 0, 0, 0, 0
		l.Night = false
		l.WBMode = "auto"
	case "daylight", "day":
		l.WBMode = "daylight"
		l.ColorTempK = 5600
		l.Temperature = 0.05
		l.Contrast = 0.08
	case "cloudy":
		l.WBMode = "cloudy"
		l.ColorTempK = 6500
		l.Temperature = 0.2
		l.Tint = -0.05
	case "tungsten", "incandescent", "warm":
		l.WBMode = "tungsten"
		l.ColorTempK = 3200
		l.Temperature = -0.35
		l.Tint = 0.05
	case "fluorescent":
		l.WBMode = "fluorescent"
		l.ColorTempK = 4000
		l.Temperature = -0.15
		l.Tint = -0.2
	case "shade":
		l.ColorTempK = 7500
		l.Temperature = 0.3
		l.Shadows = 0.15
	case "neon", "night-neon":
		l.Temperature = -0.25
		l.Tint = 0.35
		l.Saturation = 0.25
		l.Contrast = 0.2
		l.Night = true
		l.Grain = 0.15
	case "night", "lowlight":
		l.Exposure = 0.45
		l.Shadows = 0.35
		l.Fill = 0.4
		l.Grain = 0.25
		l.Night = true
		l.ISO = 1600
	case "film", "portra", "cinematic":
		l.Contrast = 0.12
		l.Saturation = -0.08
		l.Temperature = 0.12
		l.Tint = 0.04
		l.Vignette = 0.15
		l.Grain = 0.12
		l.Clarity = 0.1
	case "punchy", "vivid":
		l.Contrast = 0.25
		l.Saturation = 0.2
		l.Clarity = 0.2
	case "soft", "skin":
		l.Contrast = -0.12
		l.Clarity = -0.1
		l.Saturation = -0.05
		l.Highlights = -0.1
	case "bleach":
		l.Contrast = 0.35
		l.Saturation = -0.45
		l.Clarity = 0.15
	default:
		// unknown — keep base grade, store name
	}
	return clampCameraLook(l)
}

// ParseCameraLookLine parses CAMERA or LOOK take lines / CLI tokens.
// e.g. CAMERA exposure=0.2 contrast=0.1 wb=daylight iso=400
//
//	LOOK film · LOOK night fill=0.5
func ParseCameraLookLine(line string) (CameraLook, map[string]bool, bool) {
	line = strings.TrimSpace(line)
	up := strings.ToUpper(line)
	if !strings.HasPrefix(up, "CAMERA ") && !strings.HasPrefix(up, "LOOK ") && up != "CAMERA" && up != "LOOK" {
		return CameraLook{}, nil, false
	}
	rest := line
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		rest = strings.TrimSpace(line[i+1:])
	} else {
		rest = ""
	}
	l := DefaultCameraLook()
	keys := map[string]bool{}
	if rest == "" {
		return l, keys, true
	}
	// first bare token may be preset
	fields := strings.Fields(rest)
	for _, tok := range fields {
		if !strings.Contains(tok, "=") {
			l = ApplyCameraPreset(l, tok)
			keys["preset"] = true
			continue
		}
		kv := strings.SplitN(tok, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(kv[0])
		v := kv[1]
		keys[k] = true
		switch k {
		case "exposure", "exp":
			l.Exposure = parseF(v)
		case "contrast", "con":
			l.Contrast = parseF(v)
		case "saturation", "sat":
			l.Saturation = parseF(v)
		case "temperature", "temp", "wb_temp":
			l.Temperature = parseF(v)
		case "tint":
			l.Tint = parseF(v)
		case "clarity":
			l.Clarity = parseF(v)
		case "sharpen", "sharp":
			l.Sharpen = parseF(v)
		case "vignette", "vig":
			l.Vignette = parseF(v)
		case "brightness", "bright":
			l.Brightness = parseF(v)
		case "highlights", "hi":
			l.Highlights = parseF(v)
		case "shadows", "sh":
			l.Shadows = parseF(v)
		case "fill":
			l.Fill = parseF(v)
		case "key":
			l.Key = parseF(v)
		case "grain":
			l.Grain = parseF(v)
		case "ev", "ev_comp":
			l.EvComp = parseF(v)
		case "iso":
			l.ISO = int(parseF(v))
		case "shutter":
			l.Shutter = v
		case "wb", "wb_mode":
			l.WBMode = v
		case "color_temp", "color_temp_k", "kelvin", "k":
			l.ColorTempK = int(parseF(v))
		case "focus", "focus_mode":
			l.FocusMode = v
		case "focus_distance", "fd":
			l.FocusDist = parseF(v)
		case "facing":
			l.Facing = v
		case "torch", "flash":
			l.Torch = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
		case "zoom":
			l.Zoom = parseF(v)
		case "fps":
			l.FPS = int(parseF(v))
		case "night":
			l.Night = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on")
		case "preset", "look":
			l = ApplyCameraPreset(l, v)
		case "lut":
			l.LUT = v
		case "lut_intensity", "lut_i":
			l.LUTIntensity = parseF(v)
		}
	}
	return clampCameraLook(l), keys, true
}

func parseF(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// ApplyCameraLook grades FramePixels in place (software path).
func ApplyCameraLook(f *FramePixels, l CameraLook) {
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < f.W*f.H*3 {
		return
	}
	l = clampCameraLook(l)
	// night boost
	exp := l.Exposure + l.EvComp*0.35
	if l.Night {
		exp += 0.25
	}
	// exposure as power-of-two gain
	gain := math.Pow(2, exp)
	// brightness as additive
	lift := l.Brightness * 40
	// fill lifts shadows
	fill := l.Fill * 35
	// temperature: warm = +R -B, cool reverse
	tr := l.Temperature * 28
	tb := -l.Temperature * 28
	// tint: magenta +R+B -G, green opposite
	tg := -l.Tint * 22
	tr2 := l.Tint * 12
	tb2 := l.Tint * 12
	con := 1 + l.Contrast
	sat := 1 + l.Saturation
	hi := l.Highlights
	sh := l.Shadows
	vig := l.Vignette
	grain := l.Grain
	cx := float64(f.W-1) / 2
	cy := float64(f.H-1) / 2
	maxR := math.Hypot(cx, cy)
	if maxR < 1 {
		maxR = 1
	}

	// clarity: unsharp-ish local contrast via neighbor (simple 4-tap)
	useClarity := math.Abs(l.Clarity) > 0.02
	useSharpen := l.Sharpen > 0.02
	src := f.RGB
	// copy for kernel reads when needed
	var bak []byte
	if useClarity || useSharpen {
		bak = make([]byte, len(src))
		copy(bak, src)
	}

	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			i := (y*f.W + x) * 3
			rf := float64(src[i])
			gf := float64(src[i+1])
			bf := float64(src[i+2])

			if useClarity || useSharpen {
				// sample center from bak
				rf = float64(bak[i])
				gf = float64(bak[i+1])
				bf = float64(bak[i+2])
				var sr, sg, sb float64
				n := 0
				for _, d := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					xx, yy := x+d[0], y+d[1]
					if xx < 0 || yy < 0 || xx >= f.W || yy >= f.H {
						continue
					}
					j := (yy*f.W + xx) * 3
					sr += float64(bak[j])
					sg += float64(bak[j+1])
					sb += float64(bak[j+2])
					n++
				}
				if n > 0 {
					sr /= float64(n)
					sg /= float64(n)
					sb /= float64(n)
					// clarity / sharpen: push away from local mean
					amt := l.Clarity*0.45 + l.Sharpen*0.35
					rf += (rf - sr) * amt
					gf += (gf - sg) * amt
					bf += (bf - sb) * amt
				}
			}

			// exposure + lift
			rf = rf*gain + lift
			gf = gf*gain + lift
			bf = bf*gain + lift

			// luminance for shadow/highlight/fill
			Y := 0.299*rf + 0.587*gf + 0.114*bf
			// shadows lift when Y low
			if sh != 0 || fill > 0 {
				w := (1 - Y/255)
				if w < 0 {
					w = 0
				}
				add := (sh*40 + fill) * w
				rf += add
				gf += add
				bf += add
			}
			// highlights roll when Y high
			if hi != 0 {
				w := Y / 255
				rf += hi * -35 * w
				gf += hi * -35 * w
				bf += hi * -35 * w
			}

			// white balance
			rf += tr + tr2
			gf += tg
			bf += tb + tb2

			// contrast around mid
			rf = (rf-128)*con + 128
			gf = (gf-128)*con + 128
			bf = (bf-128)*con + 128

			// saturation
			Y2 := 0.299*rf + 0.587*gf + 0.114*bf
			rf = Y2 + (rf-Y2)*sat
			gf = Y2 + (gf-Y2)*sat
			bf = Y2 + (bf-Y2)*sat

			// vignette
			if vig != 0 {
				dx := (float64(x) - cx) / maxR
				dy := (float64(y) - cy) / maxR
				d := math.Sqrt(dx*dx + dy*dy)
				// vig>0 darkens edges; vig<0 lightens
				factor := 1 - vig*d*d*0.85
				rf *= factor
				gf *= factor
				bf *= factor
			}

			// grain
			if grain > 0 {
				// deterministic pseudo noise from x,y
				n := float64(((x*374761393 + y*668265263) ^ (x * y)) & 0xff)
				n = (n/255 - 0.5) * grain * 40
				rf += n
				gf += n
				bf += n
			}

			src[i] = clampU8(rf)
			src[i+1] = clampU8(gf)
			src[i+2] = clampU8(bf)
		}
	}
}

func clampU8(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// MeshJSON for type:camera-controls fan-out.
func (l CameraLook) MeshJSON(from string) map[string]any {
	return map[string]any{
		"type": "camera-controls",
		"from": from,
		"look": l,
		"t":    time.Now().UnixMilli(),
	}
}

// LookSummary one-line for TUI status.
func (l CameraLook) LookSummary() string {
	var parts []string
	if l.Preset != "" && l.Preset != "neutral" {
		parts = append(parts, "preset="+l.Preset)
	}
	if l.Exposure != 0 {
		parts = append(parts, fmt.Sprintf("exp=%+.2f", l.Exposure))
	}
	if l.Contrast != 0 {
		parts = append(parts, fmt.Sprintf("con=%+.2f", l.Contrast))
	}
	if l.Saturation != 0 {
		parts = append(parts, fmt.Sprintf("sat=%+.2f", l.Saturation))
	}
	if l.Temperature != 0 {
		parts = append(parts, fmt.Sprintf("temp=%+.2f", l.Temperature))
	}
	if l.Fill > 0 {
		parts = append(parts, fmt.Sprintf("fill=%.2f", l.Fill))
	}
	if l.Night {
		parts = append(parts, "night")
	}
	if l.ISO > 0 {
		parts = append(parts, fmt.Sprintf("iso=%d", l.ISO))
	}
	if l.WBMode != "" && l.WBMode != "auto" {
		parts = append(parts, "wb="+l.WBMode)
	}
	if l.Torch {
		parts = append(parts, "torch")
	}
	if len(parts) == 0 {
		return "look neutral"
	}
	return "look " + strings.Join(parts, " · ")
}

// FormatCameraDoctor multi-line doctor.
func FormatCameraDoctor() string {
	l := Camera().Look()
	var b strings.Builder
	fmt.Fprintf(&b, "camera · %s\n", l.LookSummary())
	fmt.Fprintf(&b, "  grade    exp=%+.2f con=%+.2f sat=%+.2f temp=%+.2f tint=%+.2f\n",
		l.Exposure, l.Contrast, l.Saturation, l.Temperature, l.Tint)
	fmt.Fprintf(&b, "  light    bright=%+.2f hi=%+.2f sh=%+.2f fill=%.2f grain=%.2f night=%v\n",
		l.Brightness, l.Highlights, l.Shadows, l.Fill, l.Grain, l.Night)
	fmt.Fprintf(&b, "  capture  facing=%s focus=%s ev=%+.1f iso=%d shutter=%s wb=%s K=%d torch=%v zoom=%.1f\n",
		emptyDash(l.Facing), emptyDash(l.FocusMode), l.EvComp, l.ISO, emptyDash(l.Shutter),
		emptyDash(l.WBMode), l.ColorTempK, l.Torch, l.Zoom)
	fmt.Fprintf(&b, "  film     preset=%s lut=%s\n", emptyDash(l.Preset), emptyDash(l.LUT))
	b.WriteString("  mesh     type:camera-controls · look{} on vburst/cast\n")
	b.WriteString("  browser  site/camera-controls.js · phone · grokglyph · glyph-cast\n")
	b.WriteString("  aito     exposure/contrast/sat/temp/tint/clarity (store.adjustments)\n")
	b.WriteString("  cli      /camera · /look film|night|daylight · CAMERA exp=0.2 fill=0.3\n")
	b.WriteString("  env      GY_CAMERA_PRESET=film|night|daylight|…\n")
	return b.String()
}


