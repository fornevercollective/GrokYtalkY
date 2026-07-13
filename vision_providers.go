package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// Built-in VisionProvider implementations for the vision-first backbone.

// ── Grok (xAI multimodal) ──────────────────────────────────

// GrokVisionProvider is the production take path.
type GrokVisionProvider struct{}

func (GrokVisionProvider) Name() string { return "grok" }
func (GrokVisionProvider) Kind() string { return "take" }
func (GrokVisionProvider) Available() bool {
	cfg := loadGrokConfig()
	return cfg.APIKey != "" && !cfg.Offline
}

func (GrokVisionProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = ctx
	cfg := loadGrokConfig()
	take, err := AskGrokVisionOrchestrate(cfg, orch, frame.DataURL)
	if err != nil {
		return VisionResult{}, err
	}
	take.Vision = true
	return VisionResult{Take: take, Provider: "grok"}, nil
}

// ── Offline mock (tests / no key) ──────────────────────────

// OfflineVisionProvider returns a deterministic take from frame stats.
type OfflineVisionProvider struct{}

func (OfflineVisionProvider) Name() string { return "offline" }
func (OfflineVisionProvider) Kind() string { return "take" }
func (OfflineVisionProvider) Available() bool {
	return envTruthy("GY_VISION_OFFLINE") || loadGrokConfig().APIKey == "" || loadGrokConfig().Offline
}

func (OfflineVisionProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = ctx
	mean := frameMeanLum(frame.Frame)
	theme := "unsorted"
	style := "hex"
	mute := "quiet"
	cap := "offline vision · " + orch.Active
	if orch.Kind == "news" {
		theme = "breaking"
		style = "scan"
		cap = "News wall · " + orch.Active
	}
	if strings.Contains(strings.ToLower(orch.Active+orch.Kind), "earth") ||
		strings.Contains(strings.ToLower(orch.Active), "cam") {
		theme = "earthcam"
		style = "neon"
		cap = "Scenic live cam · " + orch.Active
	}
	if mean > 0.55 {
		mute = "talking"
		style = "neon"
	}
	raw := fmt.Sprintf("STYLE %s\nCAPTION %s\nTHEME %s\nMUTE_HINT %s\nNOTE offline provider",
		style, truncate(cap, 80), theme, mute)
	take := ParseGrokTake(raw)
	take.Vision = true
	take.Raw = raw
	return VisionResult{Take: take, Provider: "offline"}, nil
}

func frameMeanLum(f *FramePixels) float64 {
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < 3 {
		return 0.3
	}
	var sum float64
	n := 0
	step := 3 * 4 // sample every 4th pixel
	for i := 0; i+2 < len(f.RGB); i += step {
		sum += (0.299*float64(f.RGB[i]) + 0.587*float64(f.RGB[i+1]) + 0.114*float64(f.RGB[i+2])) / 255
		n++
	}
	if n == 0 {
		return 0.3
	}
	return sum / float64(n)
}

// ── Aito zipdepth sidecar (optional) ───────────────────────

// AitoDepthProvider probes aito-mac zipdepth-sidecar (:8766) for depth hints.
// Does not run SAM/MediaPipe — only depth HTTP when available.
type AitoDepthProvider struct{}

func (AitoDepthProvider) Name() string { return "aito-depth" }
func (AitoDepthProvider) Kind() string { return "depth" }

func (AitoDepthProvider) Available() bool {
	url := strings.TrimSpace(os.Getenv("GY_VISION_AITO_URL"))
	if url == "" {
		url = "http://127.0.0.1:8766"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/health", nil)
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode < 300
}

func (AitoDepthProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = orch
	url := strings.TrimSpace(os.Getenv("GY_VISION_AITO_URL"))
	if url == "" {
		url = "http://127.0.0.1:8766"
	}
	// health only for now — full depth POST left for aito booth integration
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/health", nil)
	if err != nil {
		return VisionResult{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return VisionResult{}, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	backend := "aito"
	var h map[string]any
	if json.Unmarshal(b, &h) == nil {
		if s, ok := h["backend"].(string); ok && s != "" {
			backend = s
		}
	}
	mean := frameMeanLum(frame.Frame)
	return VisionResult{
		Provider: "aito-depth",
		Depth: &VisionDepthHint{
			Backend: backend,
			Mean:    mean,
		},
	}, nil
}

// ── Local depth proxy (gsplat-style stats, no neural net) ──

// DepthProxyProvider estimates a crude depth mean from luminance gradient
// (aligns with terminal gsplat proxy aesthetics — not a real point cloud).
type DepthProxyProvider struct{}

func (DepthProxyProvider) Name() string { return "depth-proxy" }
func (DepthProxyProvider) Kind() string { return "depth" }
func (DepthProxyProvider) Available() bool {
	return !envTruthy("GY_VISION_NO_DEPTH_PROXY")
}

func (DepthProxyProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = ctx
	_ = orch
	f := frame.Frame
	if f == nil || f.W < 2 || f.H < 2 {
		return VisionResult{Provider: "depth-proxy", Depth: &VisionDepthHint{Backend: "gsplat-proxy", Mean: 0.5}}, nil
	}
	// sample 8×8 preview of inverse center-weighted lum as pseudo-depth
	const N = 8
	prev := make([]float64, N*N)
	var sum float64
	for gy := 0; gy < N; gy++ {
		for gx := 0; gx < N; gx++ {
			x := gx * f.W / N
			y := gy * f.H / N
			if x >= f.W {
				x = f.W - 1
			}
			if y >= f.H {
				y = f.H - 1
			}
			L := f.lum(x, y)
			// edges → farther (higher depth) for wall aesthetic
			cx := float64(gx)/float64(N-1) - 0.5
			cy := float64(gy)/float64(N-1) - 0.5
			edge := math.Min(1, 2*math.Sqrt(cx*cx+cy*cy))
			d := (1-L)*0.6 + edge*0.4
			prev[gy*N+gx] = d
			sum += d
		}
	}
	return VisionResult{
		Provider: "depth-proxy",
		Depth: &VisionDepthHint{
			Backend: "gsplat-proxy",
			Mean:    sum / float64(len(prev)),
			Preview: prev,
		},
	}, nil
}
