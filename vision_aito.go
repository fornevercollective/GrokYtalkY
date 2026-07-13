package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// Aito side-channel providers — SAM segment · MediaPipe pose · gsplat booth.
// Heavy ML stays in aito / aito-living-canvas / aito-mac sidecars.
// GrokYtalkY POSTs focus JPEGs (or RGB) and fills VisionResult side channels.
//
// Base URL: GY_VISION_AITO_URL (default http://127.0.0.1:8766)
// Optional path overrides:
//
//	GY_VISION_AITO_SEGMENT=/segment
//	GY_VISION_AITO_POSE=/pose
//	GY_VISION_AITO_GSPLAT=/gsplat
//	GY_VISION_AITO_DEPTH=/depth
//
// Mock (tests / no sidecar): GY_VISION_AITO_MOCK=1 enables local geometry mocks.

// aitoBaseURL resolves sidecar root.
func aitoBaseURL() string {
	url := strings.TrimSpace(os.Getenv("GY_VISION_AITO_URL"))
	if url == "" {
		url = "http://127.0.0.1:8766"
	}
	return strings.TrimRight(url, "/")
}

func aitoPath(envKey, def string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		if !strings.HasPrefix(v, "/") {
			v = "/" + v
		}
		return v
	}
	return def
}

func aitoMock() bool {
	return envTruthy("GY_VISION_AITO_MOCK")
}

// aitoHealth reports whether base /health answers (cached loosely via Available).
func aitoHealth(ctx context.Context) bool {
	if aitoMock() {
		return true
	}
	c, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, aitoBaseURL()+"/health", nil)
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

// aitoPostJSON posts JSON body and returns response bytes.
func aitoPostJSON(ctx context.Context, path string, body any) ([]byte, int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aitoBaseURL()+path, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GrokYtalkY/"+Version)
	client := &http.Client{Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return b, res.StatusCode, nil
}

// aitoPostRGB posts binary depth-style frame: u32le w | u32le h | RGB888.
func aitoPostRGB(ctx context.Context, path string, f *FramePixels) ([]byte, int, error) {
	if f == nil || f.W < 1 || f.H < 1 || len(f.RGB) < f.W*f.H*3 {
		return nil, 0, fmt.Errorf("no rgb frame")
	}
	// downsample for sidecar budget
	work := f
	if f.W > 320 || f.H > 180 {
		work = DownsampleFrame(f, 320, 180)
		if work == nil {
			work = f
		}
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(work.W))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(work.H))
	buf.Write(work.RGB[:work.W*work.H*3])
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, aitoBaseURL()+path, &buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "GrokYtalkY/"+Version)
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return b, res.StatusCode, nil
}

// ── SAM segment (aito-living-canvas lineage) ───────────────

// AitoSAMProvider calls POST /segment (or mock) for region masks/bboxes.
type AitoSAMProvider struct{}

func (AitoSAMProvider) Name() string { return "aito-sam" }
func (AitoSAMProvider) Kind() string { return "segment" }
func (AitoSAMProvider) Available() bool {
	if envTruthy("GY_VISION_NO_SAM") {
		return false
	}
	if aitoMock() {
		return true
	}
	return aitoHealth(context.Background())
}

func (AitoSAMProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = orch
	if aitoMock() {
		return VisionResult{Provider: "aito-sam", Segments: mockSegments(frame)}, nil
	}
	path := aitoPath("GY_VISION_AITO_SEGMENT", "/segment")
	body := map[string]any{
		"image": frame.DataURL,
		"feed":  frame.Feed,
		"kind":  frame.Kind,
	}
	b, code, err := aitoPostJSON(ctx, path, body)
	if err != nil {
		return VisionResult{}, err
	}
	if code >= 300 {
		// try RGB binary fallback
		if frame.Frame != nil {
			b2, code2, err2 := aitoPostRGB(ctx, path, frame.Frame)
			if err2 != nil || code2 >= 300 {
				return VisionResult{}, fmt.Errorf("aito-sam %d: %s", code, truncate(string(b), 120))
			}
			b = b2
		} else {
			return VisionResult{}, fmt.Errorf("aito-sam %d: %s", code, truncate(string(b), 120))
		}
	}
	segs, err := parseAitoSegments(b)
	if err != nil {
		return VisionResult{}, err
	}
	return VisionResult{Provider: "aito-sam", Segments: segs}, nil
}

func parseAitoSegments(b []byte) ([]VisionSegment, error) {
	var wrap struct {
		Segments []struct {
			ID    string     `json:"id"`
			Label string     `json:"label"`
			Score float64    `json:"score"`
			BBox  [4]float64 `json:"bbox"`
			// alternate field names
			X float64 `json:"x"`
			Y float64 `json:"y"`
			W float64 `json:"w"`
			H float64 `json:"h"`
		} `json:"segments"`
		// some sidecars use "masks" or top-level array
		Masks []struct {
			ID    string     `json:"id"`
			Label string     `json:"label"`
			Score float64    `json:"score"`
			BBox  [4]float64 `json:"bbox"`
		} `json:"masks"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		// bare array
		var arr []struct {
			ID    string     `json:"id"`
			Label string     `json:"label"`
			Score float64    `json:"score"`
			BBox  [4]float64 `json:"bbox"`
		}
		if err2 := json.Unmarshal(b, &arr); err2 != nil {
			return nil, fmt.Errorf("segment json: %w", err)
		}
		out := make([]VisionSegment, 0, len(arr))
		for i, s := range arr {
			id := s.ID
			if id == "" {
				id = fmt.Sprintf("seg-%d", i)
			}
			out = append(out, VisionSegment{ID: id, Label: s.Label, Score: s.Score, BBox: s.BBox})
		}
		return out, nil
	}
	src := wrap.Segments
	if len(src) == 0 {
		for _, m := range wrap.Masks {
			src = append(src, struct {
				ID    string     `json:"id"`
				Label string     `json:"label"`
				Score float64    `json:"score"`
				BBox  [4]float64 `json:"bbox"`
				X     float64    `json:"x"`
				Y     float64    `json:"y"`
				W     float64    `json:"w"`
				H     float64    `json:"h"`
			}{ID: m.ID, Label: m.Label, Score: m.Score, BBox: m.BBox})
		}
	}
	out := make([]VisionSegment, 0, len(src))
	for i, s := range src {
		id := s.ID
		if id == "" {
			id = fmt.Sprintf("seg-%d", i)
		}
		bb := s.BBox
		if bb[2] == 0 && bb[3] == 0 && (s.W > 0 || s.H > 0) {
			bb = [4]float64{s.X, s.Y, s.W, s.H}
		}
		out = append(out, VisionSegment{ID: id, Label: s.Label, Score: s.Score, BBox: bb})
	}
	return out, nil
}

func mockSegments(frame VisionFrame) []VisionSegment {
	// center person + lower-third chyron-ish region
	_ = frame
	return []VisionSegment{
		{ID: "person-0", Label: "person", Score: 0.82, BBox: [4]float64{0.25, 0.15, 0.5, 0.7}},
		{ID: "chyron", Label: "text", Score: 0.55, BBox: [4]float64{0.05, 0.82, 0.9, 0.14}},
	}
}

// ── MediaPipe pose / IK ────────────────────────────────────

// AitoPoseProvider calls POST /pose for joint map + hand count.
type AitoPoseProvider struct{}

func (AitoPoseProvider) Name() string { return "aito-pose" }
func (AitoPoseProvider) Kind() string { return "pose" }
func (AitoPoseProvider) Available() bool {
	if envTruthy("GY_VISION_NO_POSE") {
		return false
	}
	if aitoMock() {
		return true
	}
	return aitoHealth(context.Background())
}

func (AitoPoseProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = orch
	if aitoMock() {
		return VisionResult{Provider: "aito-pose", Pose: mockPose(frame)}, nil
	}
	path := aitoPath("GY_VISION_AITO_POSE", "/pose")
	body := map[string]any{"image": frame.DataURL, "feed": frame.Feed}
	b, code, err := aitoPostJSON(ctx, path, body)
	if err != nil {
		return VisionResult{}, err
	}
	if code >= 300 {
		return VisionResult{}, fmt.Errorf("aito-pose %d: %s", code, truncate(string(b), 120))
	}
	pose, err := parseAitoPose(b)
	if err != nil {
		return VisionResult{}, err
	}
	return VisionResult{Provider: "aito-pose", Pose: pose}, nil
}

func parseAitoPose(b []byte) (*VisionPose, error) {
	var wrap struct {
		Joints map[string][]float64 `json:"joints"`
		Hands  int                  `json:"hands"`
		// alternate: landmarks array
		Landmarks []struct {
			Name string  `json:"name"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
			C    float64 `json:"c"`
			Conf float64 `json:"conf"`
		} `json:"landmarks"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	pose := &VisionPose{Joints: map[string][3]float64{}, Hands: wrap.Hands}
	for name, v := range wrap.Joints {
		var j [3]float64
		for i := 0; i < 3 && i < len(v); i++ {
			j[i] = v[i]
		}
		if len(v) == 2 {
			j[2] = 1
		}
		pose.Joints[name] = j
	}
	for _, lm := range wrap.Landmarks {
		c := lm.C
		if c == 0 {
			c = lm.Conf
		}
		if c == 0 {
			c = 1
		}
		pose.Joints[lm.Name] = [3]float64{lm.X, lm.Y, c}
	}
	if len(pose.Joints) == 0 && pose.Hands == 0 {
		return nil, fmt.Errorf("empty pose")
	}
	return pose, nil
}

func mockPose(frame VisionFrame) *VisionPose {
	_ = frame
	return &VisionPose{
		Hands: 1,
		Joints: map[string][3]float64{
			"nose":       {0.5, 0.28, 0.9},
			"left_wrist": {0.35, 0.55, 0.7},
			"right_wrist":{0.65, 0.52, 0.7},
			"left_shoulder":  {0.38, 0.35, 0.8},
			"right_shoulder": {0.62, 0.35, 0.8},
		},
	}
}

// ── gsplat booth (aito-mac) ────────────────────────────────

// AitoGsplatBoothProvider calls POST /gsplat (or /booth) for depth-stack preview.
type AitoGsplatBoothProvider struct{}

func (AitoGsplatBoothProvider) Name() string { return "aito-gsplat" }
func (AitoGsplatBoothProvider) Kind() string { return "depth" }
func (AitoGsplatBoothProvider) Available() bool {
	if envTruthy("GY_VISION_NO_GSPLAT") {
		return false
	}
	if aitoMock() {
		return true
	}
	return aitoHealth(context.Background())
}

func (AitoGsplatBoothProvider) Infer(ctx context.Context, frame VisionFrame, orch FeedOrchestrateContext) (VisionResult, error) {
	_ = orch
	if aitoMock() {
		return VisionResult{
			Provider: "aito-gsplat",
			Depth:    mockGsplatDepth(frame),
		}, nil
	}
	path := aitoPath("GY_VISION_AITO_GSPLAT", "/gsplat")
	body := map[string]any{"image": frame.DataURL, "mode": "booth"}
	b, code, err := aitoPostJSON(ctx, path, body)
	if err != nil || code >= 300 {
		// try /booth alias
		path2 := aitoPath("GY_VISION_AITO_BOOTH", "/booth")
		b2, code2, err2 := aitoPostJSON(ctx, path2, body)
		if err2 != nil || code2 >= 300 {
			if err != nil {
				return VisionResult{}, err
			}
			return VisionResult{}, fmt.Errorf("aito-gsplat %d: %s", code, truncate(string(b), 120))
		}
		b = b2
	}
	d, err := parseAitoDepth(b, "gsplat-booth")
	if err != nil {
		return VisionResult{}, err
	}
	return VisionResult{Provider: "aito-gsplat", Depth: d}, nil
}

func mockGsplatDepth(frame VisionFrame) *VisionDepthHint {
	mean := frameMeanLum(frame.Frame)
	prev := make([]float64, 64)
	for i := range prev {
		prev[i] = math.Mod(mean+float64(i)*0.01, 1)
	}
	return &VisionDepthHint{Backend: "gsplat-booth-mock", Mean: 1 - mean, Preview: prev}
}

func parseAitoDepth(b []byte, defaultBackend string) (*VisionDepthHint, error) {
	var wrap struct {
		Backend string    `json:"backend"`
		Mean    float64   `json:"mean"`
		Preview []float64 `json:"preview"`
		// full depth map optional — we only keep mean + tiny preview
		Depth []float64 `json:"depth"`
		W     int       `json:"w"`
		H     int       `json:"h"`
	}
	if err := json.Unmarshal(b, &wrap); err != nil {
		return nil, err
	}
	d := &VisionDepthHint{Backend: wrap.Backend, Mean: wrap.Mean, Preview: wrap.Preview}
	if d.Backend == "" {
		d.Backend = defaultBackend
	}
	if d.Mean == 0 && len(wrap.Depth) > 0 {
		var s float64
		for _, v := range wrap.Depth {
			s += v
		}
		d.Mean = s / float64(len(wrap.Depth))
	}
	if len(d.Preview) == 0 && len(wrap.Depth) > 0 {
		// downsample to 8×8
		n := len(wrap.Depth)
		step := n / 64
		if step < 1 {
			step = 1
		}
		for i := 0; i < 64 && i*step < n; i++ {
			d.Preview = append(d.Preview, wrap.Depth[i*step])
		}
	}
	return d, nil
}

// Enhance AitoDepthProvider Infer to real POST /depth when frame present.
// (Kept in this file as helper used by updated provider in vision_providers.go)

// applyVisionSideChannels uses last SAM/pose/depth on the bus to nudge stage
// (depth mode, status, soft caption) without requiring extra take lines.
func applyVisionSideChannels(m *Model, take GrokTake) []string {
	if m == nil {
		return nil
	}
	s := Vision().Snapshot()
	var applied []string
	// depth booth → enable depth style mode when take asked or strong gsplat
	if take.Depth == "" && s.LastDepthB != "" && strings.Contains(s.LastDepthB, "gsplat") && m.depth != nil {
		if m.depth.Mode() == DepthOff {
			m.depth.SetMode(DepthGsplat)
			applied = append(applied, "depth=gsplat(side)")
		}
	}
	// pose hands → status cue
	if s.LastHands > 0 {
		m.status = fmt.Sprintf("pose hands=%d", s.LastHands)
		applied = append(applied, fmt.Sprintf("pose=h%d", s.LastHands))
	}
	// SAM segments → sys note
	if s.LastSegN > 0 {
		applied = append(applied, fmt.Sprintf("sam=%d", s.LastSegN))
		if take.Caption == "" {
			m.pushSys(fmt.Sprintf("vision·sam %d segments", s.LastSegN))
		}
	}
	if s.LastDepthB != "" {
		applied = append(applied, "depth·"+truncate(s.LastDepthB, 16))
	}
	return applied
}

// aitoDepthInfer posts binary RGB to /depth (ZipDepth protocol).
func aitoDepthInfer(ctx context.Context, frame VisionFrame) (*VisionDepthHint, error) {
	if aitoMock() {
		return &VisionDepthHint{Backend: "aito-mock", Mean: frameMeanLum(frame.Frame)}, nil
	}
	path := aitoPath("GY_VISION_AITO_DEPTH", "/depth")
	if frame.Frame == nil {
		// health-only fallback
		return &VisionDepthHint{Backend: "aito", Mean: 0.5}, nil
	}
	b, code, err := aitoPostRGB(ctx, path, frame.Frame)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		// JSON image fallback
		b2, code2, err2 := aitoPostJSON(ctx, path, map[string]any{"image": frame.DataURL})
		if err2 != nil || code2 >= 300 {
			return nil, fmt.Errorf("aito-depth %d: %s", code, truncate(string(b), 120))
		}
		b = b2
	}
	return parseAitoDepth(b, "zipdepth")
}
