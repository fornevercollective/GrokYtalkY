package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Vision bus — focus-feed Grok vision takes (STYLE/CAPTION/THEME/MUTE_HINT).
// Throttled, budgeted, backpressure-safe; reuses orchestrator apply path.
//
// Aito (aito / aito-living-canvas / aito-mac) owns heavy SAM / MediaPipe / gsplat
// booth work. GrokYtalkY keeps a lean xAI vision loop for live stage control.

// VisionConfig from env (GY_VISION_*).
type VisionConfig struct {
	Enabled    bool
	Interval   time.Duration // min time between takes
	MaxW       int           // decode/JPEG budget
	MaxH       int
	JPEGQ      int    // 1–100
	Model      string // vision model (default grok-2-vision-latest)
	Overlay    bool   // also push caption as overlay
	MaxInflight int   // hard backpressure (default 1)
}

// LoadVisionConfig reads GY_VISION_* env.
func LoadVisionConfig() VisionConfig {
	c := VisionConfig{
		Enabled:     envTruthy("GY_VISION") || envTruthy("GY_VISION_ON"),
		Interval:    8 * time.Second,
		MaxW:        320,
		MaxH:        180,
		JPEGQ:       72,
		Model:       firstNonEmpty(os.Getenv("GY_VISION_MODEL"), os.Getenv("XAI_VISION_MODEL"), "grok-2-vision-latest"),
		Overlay:     envTruthy("GY_VISION_OVERLAY"),
		MaxInflight: 1,
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_INTERVAL_MS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1000 {
			c.Interval = time.Duration(n) * time.Millisecond
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MAX_W")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 64 {
			c.MaxW = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MAX_H")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 36 {
			c.MaxH = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_JPEG_Q")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 40 && n <= 95 {
			c.JPEGQ = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MAX_INFLIGHT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 3 {
			c.MaxInflight = n
		}
	}
	return c
}

// VisionBus process-wide vision state (observable) + backbone registry.
type VisionBus struct {
	mu         sync.Mutex
	cfg        VisionConfig
	reg        *VisionRegistry
	inflight   int
	lastTake   GrokTake
	lastTheme  string
	lastMute   string
	lastFeed   string
	lastAt     time.Time
	lastErr    string
	lastBytes  int
	lastProv   string
	lastLatMs  int64
	takes      int64
	drops      int64
	errors     int64
	// last side channels (SAM / pose / depth)
	lastSegN   int
	lastPoseN  int
	lastHands  int
	lastDepthB string
	lastDepthM float64
	// theme ring for news clustering (id → theme)
	themes map[string]string
}

var (
	visionOnce sync.Once
	visionBus  *VisionBus
	// MetricVisionTakes exposed via reliability
	metricVisionTakes atomic.Int64
	metricVisionDrops atomic.Int64
)

// Vision returns the global bus (lazy init from env + provider registry).
func Vision() *VisionBus {
	visionOnce.Do(func() {
		visionBus = &VisionBus{
			cfg:    LoadVisionConfig(),
			reg:    newVisionRegistry(),
			themes: make(map[string]string),
		}
		// bridge plugins that implement VisionHook
		for _, p := range Plugins().List() {
			if vh, ok := p.(interface{ VisionHook() VisionHook }); ok && vh.VisionHook() != nil {
				visionBus.reg.RegisterHook(vh.VisionHook())
			}
		}
		// builtin theme logger hook (observable)
		visionBus.reg.RegisterHook(&visionLogHook{})
	})
	return visionBus
}

// Registry returns the provider/hook registry (never nil after Vision()).
func (v *VisionBus) Registry() *VisionRegistry {
	if v == nil {
		return newVisionRegistry()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.reg == nil {
		v.reg = newVisionRegistry()
	}
	return v.reg
}

// visionLogHook soft-logs takes for doctor (no-op heavy work).
type visionLogHook struct{}

func (visionLogHook) Name() string { return "vision-log" }
func (visionLogHook) OnVision(ev VisionEvent) {
	// keep last provider latency on bus without locking long
	if ev.Type != "vision-take" {
		return
	}
	v := Vision()
	v.mu.Lock()
	v.lastProv = ev.Provider
	v.lastLatMs = ev.LatencyMs
	v.mu.Unlock()
}

// Config snapshot.
func (v *VisionBus) Config() VisionConfig {
	if v == nil {
		return LoadVisionConfig()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cfg
}

// SetEnabled toggles runtime vision loop.
func (v *VisionBus) SetEnabled(on bool) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.cfg.Enabled = on
	v.mu.Unlock()
}

// Enabled reports whether vision takes may run.
func (v *VisionBus) Enabled() bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cfg.Enabled
}

// TryBegin acquires an inflight slot (backpressure). false = drop.
func (v *VisionBus) TryBegin() bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.inflight >= v.cfg.MaxInflight {
		v.drops++
		metricVisionDrops.Add(1)
		return false
	}
	// throttle interval
	if !v.lastAt.IsZero() && time.Since(v.lastAt) < v.cfg.Interval && v.inflight > 0 {
		v.drops++
		metricVisionDrops.Add(1)
		return false
	}
	// still respect min interval even when idle after last take
	if !v.lastAt.IsZero() && time.Since(v.lastAt) < v.cfg.Interval {
		v.drops++
		metricVisionDrops.Add(1)
		return false
	}
	v.inflight++
	return true
}

// End releases inflight (call after take completes).
func (v *VisionBus) End() {
	if v == nil {
		return
	}
	v.mu.Lock()
	if v.inflight > 0 {
		v.inflight--
	}
	v.mu.Unlock()
}

// RecordSuccess stores last take.
func (v *VisionBus) RecordSuccess(feed string, take GrokTake, jpegBytes int) {
	v.RecordSuccessFull(feed, take, jpegBytes, "", 0)
}

// RecordSuccessFull stores take + provider latency.
func (v *VisionBus) RecordSuccessFull(feed string, take GrokTake, jpegBytes int, provider string, latencyMs int64) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastTake = take
	v.lastFeed = feed
	v.lastAt = time.Now()
	v.lastErr = ""
	v.lastBytes = jpegBytes
	if provider != "" {
		v.lastProv = provider
	}
	if latencyMs > 0 {
		v.lastLatMs = latencyMs
	}
	v.takes++
	metricVisionTakes.Add(1)
	if take.Theme != "" {
		v.lastTheme = take.Theme
		if feed != "" {
			v.themes[feed] = take.Theme
		}
	}
	if take.MuteHint != "" {
		v.lastMute = take.MuteHint
	}
	MetricIncr("orch_takes")
}

// RecordError stores last error.
func (v *VisionBus) RecordError(err string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastErr = err
	v.lastAt = time.Now()
	v.errors++
}

// RecordSideChannels stores last SAM/pose/depth summary for doctor.
func (v *VisionBus) RecordSideChannels(segs []VisionSegment, pose *VisionPose, depth *VisionDepthHint) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastSegN = len(segs)
	v.lastPoseN = 0
	v.lastHands = 0
	if pose != nil {
		v.lastPoseN = len(pose.Joints)
		v.lastHands = pose.Hands
	}
	v.lastDepthB = ""
	v.lastDepthM = 0
	if depth != nil {
		v.lastDepthB = depth.Backend
		v.lastDepthM = depth.Mean
	}
}

// ThemeForFeed returns last vision theme for a feed id/label.
func (v *VisionBus) ThemeForFeed(id string) string {
	if v == nil {
		return ""
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.themes[id]
}

// LastTheme global last theme token.
func (v *VisionBus) LastTheme() string {
	if v == nil {
		return ""
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastTheme
}

// Snapshot for doctor / status.
type VisionSnapshot struct {
	Enabled    bool
	Inflight   int
	Takes      int64
	Drops      int64
	Errors     int64
	LastFeed   string
	LastTheme  string
	LastMute   string
	LastErr    string
	LastAt     time.Time
	LastBytes  int
	LastProv   string
	LastLatMs  int64
	Interval   time.Duration
	MaxW       int
	MaxH       int
	Model      string
	Summary    string
	Primary    string
	// side channels
	LastSegN   int
	LastPoseN  int
	LastHands  int
	LastDepthB string
	LastDepthM float64
}

// Snapshot copies bus state.
func (v *VisionBus) Snapshot() VisionSnapshot {
	if v == nil {
		return VisionSnapshot{}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	s := VisionSnapshot{
		Enabled: v.cfg.Enabled, Inflight: v.inflight,
		Takes: v.takes, Drops: v.drops, Errors: v.errors,
		LastFeed: v.lastFeed, LastTheme: v.lastTheme, LastMute: v.lastMute,
		LastErr: v.lastErr, LastAt: v.lastAt, LastBytes: v.lastBytes,
		LastProv: v.lastProv, LastLatMs: v.lastLatMs,
		Interval: v.cfg.Interval, MaxW: v.cfg.MaxW, MaxH: v.cfg.MaxH,
		Model: v.cfg.Model,
		LastSegN: v.lastSegN, LastPoseN: v.lastPoseN, LastHands: v.lastHands,
		LastDepthB: v.lastDepthB, LastDepthM: v.lastDepthM,
	}
	s.Summary = v.lastTake.TakeSummary()
	if v.reg != nil && v.reg.PrimaryTakeProvider() != nil {
		s.Primary = v.reg.PrimaryTakeProvider().Name()
	}
	return s
}

// FormatVisionDoctor multi-line for gy doctor vision / /vision.
func FormatVisionDoctor(v *VisionBus) string {
	if v == nil {
		v = Vision()
	}
	s := v.Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "vision · enabled=%v primary=%s model=%s\n", s.Enabled, emptyDash(s.Primary), s.Model)
	fmt.Fprintf(&b, "  budget    %dx%d jpeg · interval %s · max_inflight %d\n",
		s.MaxW, s.MaxH, s.Interval, v.Config().MaxInflight)
	fmt.Fprintf(&b, "  takes     %d  drops %d  errors %d  inflight %d\n",
		s.Takes, s.Drops, s.Errors, s.Inflight)
	fmt.Fprintf(&b, "  last_feed %s\n", emptyDash(s.LastFeed))
	fmt.Fprintf(&b, "  last_take %s\n", emptyDash(s.Summary))
	fmt.Fprintf(&b, "  last_prov %s · %d ms\n", emptyDash(s.LastProv), s.LastLatMs)
	fmt.Fprintf(&b, "  theme     %s\n", emptyDash(s.LastTheme))
	fmt.Fprintf(&b, "  mute_hint %s\n", emptyDash(s.LastMute))
	fmt.Fprintf(&b, "  sides     sam=%d pose_j=%d hands=%d depth=%s (%.2f)\n",
		s.LastSegN, s.LastPoseN, s.LastHands, emptyDash(s.LastDepthB), s.LastDepthM)
	if !s.LastAt.IsZero() {
		fmt.Fprintf(&b, "  last_at   %s · jpeg %d B\n", s.LastAt.Format(time.RFC3339), s.LastBytes)
	}
	if s.LastErr != "" {
		fmt.Fprintf(&b, "  last_err  %s\n", s.LastErr)
	}
	b.WriteString("  env       GY_VISION=1 · PROVIDER · AITO_URL · AITO_MOCK=1\n")
	b.WriteString("  ffmpeg    GY_VISION_MEDIA=1 control plane · /vision media\n")
	b.WriteString("  aito      SAM/pose/gsplat/depth sidecars · POST /segment /pose /gsplat /depth\n")
	return b.String()
}

// FrameToJPEGBase64 encodes RGB FramePixels as data URL for vision APIs.
// Downsamples to maxW×maxH (throttled budget).
func FrameToJPEGBase64(f *FramePixels, maxW, maxH, quality int) (dataURL string, nBytes int, err error) {
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < f.W*f.H*3 {
		return "", 0, fmt.Errorf("no frame")
	}
	if maxW < 64 {
		maxW = 64
	}
	if maxH < 36 {
		maxH = 36
	}
	if quality < 40 {
		quality = 40
	}
	if quality > 95 {
		quality = 95
	}
	// downsample
	work := f
	if f.W > maxW || f.H > maxH {
		dw, dh := maxW, int(float64(maxW)*float64(f.H)/float64(f.W))
		if dh > maxH {
			dh = maxH
			dw = int(float64(maxH) * float64(f.W) / float64(f.H))
		}
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
		work = DownsampleFrame(f, dw, dh)
		if work == nil {
			return "", 0, fmt.Errorf("downsample failed")
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, work.W, work.H))
	for y := 0; y < work.H; y++ {
		for x := 0; x < work.W; x++ {
			i := (y*work.W + x) * 3
			img.SetRGBA(x, y, color.RGBA{R: work.RGB[i], G: work.RGB[i+1], B: work.RGB[i+2], A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return "", 0, err
	}
	b := buf.Bytes()
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b), len(b), nil
}

// VisionSystemPrompt structured take lines including THEME + MUTE_HINT.
func VisionSystemPrompt() string {
	return `You are Grok Vision inside GrokYtalkY — a live terminal mesh with glyph video walls.
You SEE one focus feed frame. Emit a SHORT "take" using ONLY these lines (omit unused):

STYLE <half|hex|braille|ascii|blocks|scan|neon|dither|poster|edges|points|halftone>
CAPTION <one chyron line, max 80 chars — describe what you see>
THEME <breaking|politics|conflict|markets|weather|health|science|local|culture|earthcam|unsorted>
MUTE_HINT <none|suggest-mute|quiet|talking>  (stage audio/visual activity)
MEDIA <restart|kill|spawn|retune|retarget|encode|recover> [focus|…] [crop=x,y,w,h] [WxH@fps] [source|path]
GLYPH <square|phone-v|13|25|37|49>
DEPTH <off|zip-lite|gsplat>
EFFECT <max 12 words visual cue>
PATTERN <optional strudel mini-notation>
NOTE <optional operator tip, max 60 chars>

Rules: no markdown fences; no preamble; always prefer STYLE+CAPTION+THEME when the frame is live video.
For news/earthcam pick STYLE readable at small tiles (hex, braille, scan, dither, neon).
THEME must be one token from the list. MUTE_HINT=talking if faces/mouths/activity; quiet if static scenic.
MEDIA is the FFmpeg control plane: restart dead tiles, retune scale/fps, spawn catalog sources, encode snapshots.
When the frame is frozen/black or the feed looks dead, emit MEDIA recover focus (or MEDIA restart focus).
When a clear subject/person is visible, SAM auto-retargets (MEDIA retarget crop=…); you may also emit MEDIA retarget focus crop=x,y,w,h.`
}

// BuildVisionUserPrompt text part (image attached separately).
func BuildVisionUserPrompt(ctx FeedOrchestrateContext) string {
	var b strings.Builder
	b.WriteString("Vision take on focus feed frame.\n")
	fmt.Fprintf(&b, "mode=%s active=%s kind=%s style=%s glyph=%d/%s live=%v\n",
		ctx.Mode, ctx.Active, ctx.Kind, ctx.Style, ctx.GlyphN, ctx.GlyphAsp, ctx.Live)
	if ctx.NewsCount > 0 {
		fmt.Fprintf(&b, "news_tiles=%d\n", ctx.NewsCount)
	}
	if ctx.Media != "" {
		fmt.Fprintf(&b, "media=%s\n", ctx.Media)
	}
	if ctx.Hint != "" {
		fmt.Fprintf(&b, "operator_hint: %s\n", ctx.Hint)
	}
	b.WriteString("Look at the image and emit take lines now.")
	return b.String()
}

// AskGrokVisionOrchestrate multimodal take (image + context).
func AskGrokVisionOrchestrate(cfg GrokConfig, ctx FeedOrchestrateContext, imageDataURL string) (GrokTake, error) {
	cfg.System = VisionSystemPrompt()
	if vmodel := Vision().Config().Model; vmodel != "" {
		cfg.Model = vmodel
	}
	user := BuildVisionUserPrompt(ctx)
	reply, err := AskGrokVision(cfg, user, imageDataURL)
	if err != nil {
		return GrokTake{}, err
	}
	return ParseGrokTake(reply), nil
}

// AskGrokVision sends multimodal chat/completions (xAI vision).
func AskGrokVision(cfg GrokConfig, userText, imageDataURL string) (string, error) {
	if cfg.Offline {
		return "", fmt.Errorf("vision disabled offline")
	}
	if cfg.APIKey == "" {
		return "", fmt.Errorf("vision requires XAI_API_KEY (multimodal)")
	}
	if imageDataURL == "" {
		return "", fmt.Errorf("no image")
	}
	// multimodal content parts
	content := []map[string]any{
		{"type": "text", "text": userText},
		{"type": "image_url", "image_url": map[string]any{"url": imageDataURL}},
	}
	msgs := []map[string]any{}
	if cfg.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": cfg.System})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": content})

	model := cfg.Model
	if model == "" || !strings.Contains(strings.ToLower(model), "vision") {
		// force vision-capable model when plain text model configured
		model = firstNonEmpty(Vision().Config().Model, "grok-2-vision-latest")
	}
	body := map[string]any{
		"model":       model,
		"messages":    msgs,
		"temperature": 0.4,
		"stream":      false,
		"max_tokens":  400,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	client := &http.Client{Timeout: 90 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("xAI vision %s: %s", res.Status, truncate(string(b), 300))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty vision response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// FocusFrameFromModel picks one frame for vision (priority: lab active → main frame → burst).
func FocusFrameFromModel(m *Model) (frame *FramePixels, feedLabel, kind string) {
	if m == nil {
		return nil, "", ""
	}
	if m.lab != nil && m.lab.On {
		if af := m.lab.ActiveFeed(); af != nil && af.Frame != nil && !af.IsEmpty() {
			return af.Frame, af.Label, af.Kind
		}
		// first non-empty feed
		for i := range m.lab.Feeds {
			f := &m.lab.Feeds[i]
			if f.Frame != nil && !f.IsEmpty() {
				return f.Frame, f.Label, f.Kind
			}
		}
	}
	if m.frame != nil {
		label := m.frameMeta
		if label == "" {
			label = m.watchPath
		}
		if label == "" {
			label = "main"
		}
		return m.frame, label, "watch"
	}
	if m.burstLocalFrame != nil {
		return m.burstLocalFrame, "burst-local", "burst"
	}
	return nil, "", ""
}
