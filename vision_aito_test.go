package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAitoMockProviders(t *testing.T) {
	t.Setenv("GY_VISION_AITO_MOCK", "1")
	f := &FramePixels{W: 32, H: 24, RGB: make([]byte, 32*24*3)}
	for i := range f.RGB {
		f.RGB[i] = 100
	}
	url, _, err := FrameToJPEGBase64(f, 64, 48, 70)
	if err != nil {
		t.Fatal(err)
	}
	vf := VisionFrame{Frame: f, Feed: "test", Kind: "news", DataURL: url}

	sam := AitoSAMProvider{}
	if !sam.Available() {
		t.Fatal("sam mock available")
	}
	res, err := sam.Infer(context.Background(), vf, FeedOrchestrateContext{})
	if err != nil || len(res.Segments) < 1 {
		t.Fatal(err, res.Segments)
	}

	pose := AitoPoseProvider{}
	res2, err := pose.Infer(context.Background(), vf, FeedOrchestrateContext{})
	if err != nil || res2.Pose == nil || res2.Pose.Hands < 1 {
		t.Fatal(err, res2.Pose)
	}

	gs := AitoGsplatBoothProvider{}
	res3, err := gs.Infer(context.Background(), vf, FeedOrchestrateContext{})
	if err != nil || res3.Depth == nil || !strings.Contains(res3.Depth.Backend, "gsplat") {
		t.Fatal(err, res3.Depth)
	}

	d, err := aitoDepthInfer(context.Background(), vf)
	if err != nil || d == nil {
		t.Fatal(err)
	}
}

func TestParseAitoSegments(t *testing.T) {
	raw := `{"segments":[{"id":"a","label":"person","score":0.9,"bbox":[0.1,0.1,0.4,0.6]}]}`
	segs, err := parseAitoSegments([]byte(raw))
	if err != nil || len(segs) != 1 || segs[0].Label != "person" {
		t.Fatal(err, segs)
	}
	// bare array
	raw2 := `[{"label":"car","score":0.5,"bbox":[0,0,1,1]}]`
	segs2, err := parseAitoSegments([]byte(raw2))
	if err != nil || segs2[0].Label != "car" {
		t.Fatal(err, segs2)
	}
}

func TestParseAitoPose(t *testing.T) {
	raw := `{"hands":2,"joints":{"nose":[0.5,0.2,0.9]}}`
	p, err := parseAitoPose([]byte(raw))
	if err != nil || p.Hands != 2 || p.Joints["nose"][0] != 0.5 {
		t.Fatal(err, p)
	}
}

func TestEnrichTakeFromSideChannels(t *testing.T) {
	res := &VisionResult{
		Take:     GrokTake{},
		Pose:     &VisionPose{Hands: 2, Joints: map[string][3]float64{"nose": {0.5, 0.2, 1}}},
		Depth:    &VisionDepthHint{Backend: "gsplat-booth", Mean: 0.4},
		Segments: []VisionSegment{{Label: "person", Score: 0.9}},
	}
	enrichTakeFromSideChannels(res)
	if res.Take.MuteHint != "talking" {
		t.Fatal(res.Take.MuteHint)
	}
	if res.Take.Depth != "gsplat" {
		t.Fatal(res.Take.Depth)
	}
	if !strings.Contains(res.Take.Caption, "person") {
		t.Fatal(res.Take.Caption)
	}
}

func TestAitoHTTPSegmentServer(t *testing.T) {
	t.Setenv("GY_VISION_AITO_MOCK", "0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/segment":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"segments": []map[string]any{
					{"id": "1", "label": "desk", "score": 0.7, "bbox": []float64{0.2, 0.2, 0.5, 0.5}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("GY_VISION_AITO_URL", srv.URL)

	// Available hits /health
	if !(AitoSAMProvider{}).Available() {
		t.Fatal("health")
	}
	f := &FramePixels{W: 16, H: 12, RGB: make([]byte, 16*12*3)}
	url, _, _ := FrameToJPEGBase64(f, 64, 48, 70)
	res, err := (AitoSAMProvider{}).Infer(context.Background(), VisionFrame{Frame: f, DataURL: url}, FeedOrchestrateContext{})
	if err != nil || len(res.Segments) != 1 || res.Segments[0].Label != "desk" {
		t.Fatal(err, res.Segments)
	}
}

func TestRegistryHasAitoSides(t *testing.T) {
	r := newVisionRegistry()
	want := []string{"aito-sam", "aito-pose", "aito-gsplat", "aito-depth"}
	for _, n := range want {
		if r.Provider(n) == nil {
			t.Fatalf("missing %s", n)
		}
	}
}

func TestVisionEventMeshSideChannels(t *testing.T) {
	ev := VisionEvent{
		Type: "vision-take", Feed: "CNN",
		Segments: []VisionSegment{{Label: "person", Score: 0.9}},
		Pose:     &VisionPose{Hands: 1, Joints: map[string][3]float64{"nose": {0.5, 0.2, 1}}},
		Depth:    &VisionDepthHint{Backend: "gsplat-booth", Mean: 0.33},
		Take:     GrokTake{Media: []VisionMediaAction{{Op: "restart"}}},
	}
	m := ev.MeshJSON("qbit")
	if m["segments"] != 1 || m["segment_top"] != "person" {
		t.Fatal(m)
	}
	if m["pose_hands"] != 1 {
		t.Fatal(m)
	}
	if m["media_ops"] != 1 {
		t.Fatal(m)
	}
}
