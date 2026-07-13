package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Vision-first backbone — staged pipeline + pluggable providers + event stream.
//
// Stages: capture → encode → infer → parse → apply → emit
// Providers (swap without new primitives):
//   - grok     : xAI multimodal take (default, production path)
//   - offline  : deterministic mock take (tests / GY_VISION_OFFLINE)
//   - aito-depth : optional zipdepth sidecar HTTP (aito-mac)
//   - depth-proxy: local gsplat-style depth hint (no neural net)
//
// Future providers (interfaces ready, not required to ship):
//   - sam / mediapipe / gsplat-booth via GY_VISION_AITO_URL sidecars
//
// Plugins subscribe via VisionHook; mesh emits type:vision-take.

// VisionStage names pipeline steps for observability.
type VisionStage string

const (
	VisionStageCapture VisionStage = "capture"
	VisionStageEncode  VisionStage = "encode"
	VisionStageInfer   VisionStage = "infer"
	VisionStageParse   VisionStage = "parse"
	VisionStageApply   VisionStage = "apply"
	VisionStageEmit    VisionStage = "emit"
)

// VisionFrame is one focus-feed capture prepared for inference.
type VisionFrame struct {
	Frame      *FramePixels
	Feed       string
	Kind       string
	DataURL    string // set after encode
	JPEGBytes  int
	CapturedAt time.Time
}

// VisionSegment is a backbone slot for SAM / region masks (optional).
type VisionSegment struct {
	ID     string  `json:"id"`
	Label  string  `json:"label,omitempty"`
	Score  float64 `json:"score,omitempty"`
	// BBox normalized 0–1: x,y,w,h
	BBox [4]float64 `json:"bbox,omitempty"`
}

// VisionPose is a backbone slot for MediaPipe / IK (optional).
type VisionPose struct {
	// Joints map name → normalized x,y,conf
	Joints map[string][3]float64 `json:"joints,omitempty"`
	// Hands optional fingertip hints
	Hands int `json:"hands,omitempty"`
}

// VisionDepthHint is a backbone slot for zipdepth / gsplat proxy.
type VisionDepthHint struct {
	Backend string  `json:"backend"` // zip-lite|zipdepth|gsplat-proxy|aito
	Mean    float64 `json:"mean,omitempty"`
	// Optional tiny preview (downsampled depths 0–1)
	Preview []float64 `json:"preview,omitempty"`
}

// VisionResult is the full pipeline output (take + optional side channels).
type VisionResult struct {
	Take     GrokTake
	Provider string
	Latency  time.Duration
	Stages   map[VisionStage]time.Duration
	Segments []VisionSegment
	Pose     *VisionPose
	Depth    *VisionDepthHint
	Frame    VisionFrame
	// Err non-nil if pipeline failed after capture
	Err error
}

// VisionEvent is emitted after a successful (or failed) pipeline run.
// Plugins and mesh consumers listen here — the "real event stream".
type VisionEvent struct {
	Type      string    `json:"type"` // vision-take | vision-error
	At        time.Time `json:"t"`
	Feed      string    `json:"feed"`
	Kind      string    `json:"kind,omitempty"`
	Provider  string    `json:"provider"`
	Take      GrokTake  `json:"take,omitempty"`
	Theme     string    `json:"theme,omitempty"`
	MuteHint  string    `json:"mute_hint,omitempty"`
	Caption   string    `json:"caption,omitempty"`
	Style     string    `json:"style,omitempty"`
	LatencyMs int64     `json:"latency_ms,omitempty"`
	JPEGBytes int       `json:"jpeg_bytes,omitempty"`
	// Side channels (empty until providers fill them)
	Segments []VisionSegment  `json:"segments,omitempty"`
	Pose     *VisionPose      `json:"pose,omitempty"`
	Depth    *VisionDepthHint `json:"depth,omitempty"`
	Error    string           `json:"error,omitempty"`
}

// VisionProvider is a pluggable inference backend.
type VisionProvider interface {
	Name() string
	// Kind: take | depth | segment | pose | multi
	Kind() string
	Available() bool
	// Infer may fill Take and/or side channels (depth/pose/segments).
	Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error)
}

// VisionHook is a plugin-facing subscriber (in-process).
type VisionHook interface {
	Name() string
	OnVision(ev VisionEvent)
}

// VisionSubscriber is a function hook (tests / lightweight listeners).
type VisionSubscriber func(ev VisionEvent)

// ── registry ───────────────────────────────────────────────

// VisionRegistry holds providers + hooks for the backbone.
type VisionRegistry struct {
	mu        sync.RWMutex
	providers map[string]VisionProvider
	order     []string
	hooks     []VisionHook
	subs      []VisionSubscriber
	// primary take provider name (default grok)
	primary string
}

func newVisionRegistry() *VisionRegistry {
	r := &VisionRegistry{
		providers: make(map[string]VisionProvider),
		primary:   "grok",
	}
	// built-in providers
	r.RegisterProvider(&GrokVisionProvider{})
	r.RegisterProvider(&OfflineVisionProvider{})
	r.RegisterProvider(&AitoDepthProvider{})
	r.RegisterProvider(&DepthProxyProvider{})
	if p := strings.TrimSpace(os.Getenv("GY_VISION_PROVIDER")); p != "" {
		r.primary = strings.ToLower(p)
	}
	return r
}

// RegisterProvider adds/replaces a provider.
func (r *VisionRegistry) RegisterProvider(p VisionProvider) {
	if r == nil || p == nil || p.Name() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	name := strings.ToLower(p.Name())
	if _, ok := r.providers[name]; !ok {
		r.order = append(r.order, name)
	}
	r.providers[name] = p
}

// RegisterHook adds a VisionHook (plugin).
func (r *VisionRegistry) RegisterHook(h VisionHook) {
	if r == nil || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = append(r.hooks, h)
}

// Subscribe adds a function listener.
func (r *VisionRegistry) Subscribe(fn VisionSubscriber) {
	if r == nil || fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs = append(r.subs, fn)
}

// Provider looks up by name.
func (r *VisionRegistry) Provider(name string) VisionProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[strings.ToLower(name)]
}

// PrimaryTakeProvider returns the configured take provider (fallback offline/grok).
func (r *VisionRegistry) PrimaryTakeProvider() VisionProvider {
	if r == nil {
		return &OfflineVisionProvider{}
	}
	r.mu.RLock()
	name := r.primary
	r.mu.RUnlock()
	if p := r.Provider(name); p != nil && p.Available() {
		return p
	}
	// prefer grok if key present
	if p := r.Provider("grok"); p != nil && p.Available() {
		return p
	}
	return r.Provider("offline")
}

// ListProviders for doctor.
func (r *VisionRegistry) ListProviders() []VisionProvider {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]VisionProvider, 0, len(r.order))
	for _, n := range r.order {
		if p := r.providers[n]; p != nil {
			out = append(out, p)
		}
	}
	return out
}

// Emit dispatches event to hooks + subscribers (never panics).
func (r *VisionRegistry) Emit(ev VisionEvent) {
	if r == nil {
		return
	}
	r.mu.RLock()
	hooks := append([]VisionHook(nil), r.hooks...)
	subs := append([]VisionSubscriber(nil), r.subs...)
	r.mu.RUnlock()
	for _, h := range hooks {
		func() {
			defer func() { _ = recover() }()
			h.OnVision(ev)
		}()
	}
	for _, fn := range subs {
		func() {
			defer func() { _ = recover() }()
			fn(ev)
		}()
	}
}

// MeshJSON for hub fan-out type:vision-take.
func (ev VisionEvent) MeshJSON(from string) map[string]any {
	m := map[string]any{
		"type":     "vision-take",
		"from":     from,
		"feed":     ev.Feed,
		"kind":     ev.Kind,
		"provider": ev.Provider,
		"theme":    ev.Theme,
		"mute_hint": ev.MuteHint,
		"caption":  ev.Caption,
		"style":    ev.Style,
		"latency_ms": ev.LatencyMs,
		"t":        ev.At.UnixMilli(),
	}
	if ev.Error != "" {
		m["type"] = "vision-error"
		m["error"] = ev.Error
	}
	if ev.Depth != nil {
		m["depth"] = ev.Depth
	}
	if len(ev.Segments) > 0 {
		m["segments"] = len(ev.Segments) // don't flood mesh with masks
	}
	if ev.Pose != nil {
		m["pose"] = true
	}
	return m
}

// ── pipeline runner ────────────────────────────────────────

// RunVisionPipeline executes capture→encode→infer→parse side-channels→emit.
// Does NOT apply to Model (caller applies take). Respects VisionBus backpressure.
func RunVisionPipeline(m *Model, hint string) (VisionResult, error) {
	bus := Vision()
	reg := bus.Registry()
	res := VisionResult{Stages: make(map[VisionStage]time.Duration)}
	t0 := time.Now()

	if !bus.TryBegin() {
		return res, fmt.Errorf("vision drop · backpressure")
	}
	defer bus.End()

	// CAPTURE
	tCap := time.Now()
	frame, label, kind := FocusFrameFromModel(m)
	res.Stages[VisionStageCapture] = time.Since(tCap)
	if frame == nil {
		err := fmt.Errorf("no focus frame")
		bus.RecordError(err.Error())
		reg.Emit(VisionEvent{Type: "vision-error", At: time.Now(), Error: err.Error(), Provider: "capture"})
		return res, err
	}
	vf := VisionFrame{Frame: frame, Feed: label, Kind: kind, CapturedAt: time.Now()}

	// ENCODE
	tEnc := time.Now()
	cfg := bus.Config()
	dataURL, nB, err := FrameToJPEGBase64(frame, cfg.MaxW, cfg.MaxH, cfg.JPEGQ)
	res.Stages[VisionStageEncode] = time.Since(tEnc)
	if err != nil {
		bus.RecordError(err.Error())
		reg.Emit(VisionEvent{Type: "vision-error", At: time.Now(), Feed: label, Error: err.Error(), Provider: "encode"})
		return res, err
	}
	vf.DataURL = dataURL
	vf.JPEGBytes = nB
	res.Frame = vf

	orch := FeedOrchestrateContext{}
	if m != nil {
		orch = m.feedOrchestrateContext(hint)
		orch.Active = label
		if kind != "" {
			orch.Kind = kind
		}
		orch.Live = true
	}

	// INFER primary take provider
	prov := reg.PrimaryTakeProvider()
	if prov == nil {
		err := fmt.Errorf("no vision provider")
		bus.RecordError(err.Error())
		return res, err
	}
	tInf := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := prov.Infer(ctx, vf, orch)
	res.Stages[VisionStageInfer] = time.Since(tInf)
	res.Provider = prov.Name()
	if err != nil {
		bus.RecordError(err.Error())
		reg.Emit(VisionEvent{
			Type: "vision-error", At: time.Now(), Feed: label, Kind: kind,
			Provider: prov.Name(), Error: err.Error(), JPEGBytes: nB,
		})
		return res, err
	}
	res.Take = out.Take
	res.Take.Vision = true
	res.Segments = out.Segments
	res.Pose = out.Pose
	res.Depth = out.Depth

	// optional side providers (depth) — best-effort, never fail the take
	if dp := reg.Provider("aito-depth"); dp != nil && dp.Available() && res.Depth == nil {
		if side, e2 := dp.Infer(ctx, vf, orch); e2 == nil && side.Depth != nil {
			res.Depth = side.Depth
		}
	} else if dp := reg.Provider("depth-proxy"); dp != nil && dp.Available() && res.Depth == nil {
		if side, e2 := dp.Infer(ctx, vf, orch); e2 == nil && side.Depth != nil {
			res.Depth = side.Depth
		}
	}

	res.Latency = time.Since(t0)
	bus.RecordSuccessFull(label, res.Take, nB, res.Provider, res.Latency.Milliseconds())

	// EMIT event stream (plugins + subscribers)
	ev := VisionEvent{
		Type: "vision-take", At: time.Now(),
		Feed: label, Kind: kind, Provider: res.Provider,
		Take: res.Take, Theme: res.Take.Theme, MuteHint: res.Take.MuteHint,
		Caption: res.Take.Caption, Style: res.Take.Style,
		LatencyMs: res.Latency.Milliseconds(), JPEGBytes: nB,
		Segments: res.Segments, Pose: res.Pose, Depth: res.Depth,
	}
	tEm := time.Now()
	reg.Emit(ev)
	res.Stages[VisionStageEmit] = time.Since(tEm)

	return res, nil
}

// FormatVisionBackboneDoctor lists providers + last event summary.
func FormatVisionBackboneDoctor(v *VisionBus) string {
	if v == nil {
		v = Vision()
	}
	var b strings.Builder
	b.WriteString(FormatVisionDoctor(v))
	b.WriteString("  backbone  capture→encode→infer→apply→emit\n")
	b.WriteString("  providers\n")
	for _, p := range v.Registry().ListProviders() {
		mark := "·"
		if p.Available() {
			mark = "✓"
		}
		prim := ""
		if v.Registry().PrimaryTakeProvider() != nil && p.Name() == v.Registry().PrimaryTakeProvider().Name() {
			prim = "  ← primary"
		}
		fmt.Fprintf(&b, "    %s %-12s  kind=%-8s%s\n", mark, p.Name(), p.Kind(), prim)
	}
	b.WriteString("  hooks     plugin VisionHook · Subscribe() · mesh type:vision-take\n")
	b.WriteString("  aito      GY_VISION_AITO_URL=http://127.0.0.1:8766  (zipdepth sidecar)\n")
	b.WriteString("  future    SAM/MediaPipe/gsplat via aito sidecars (interfaces ready)\n")
	b.WriteString(FormatVisionMediaDoctor())
	return b.String()
}
