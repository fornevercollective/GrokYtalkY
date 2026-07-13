package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVisionRegistryProviders(t *testing.T) {
	r := newVisionRegistry()
	names := map[string]bool{}
	for _, p := range r.ListProviders() {
		names[p.Name()] = true
	}
	for _, want := range []string{"grok", "offline", "aito-depth", "aito-sam", "aito-pose", "aito-gsplat", "depth-proxy"} {
		if !names[want] {
			t.Fatalf("missing provider %s in %v", want, names)
		}
	}
	off := r.Provider("offline")
	if off == nil || !off.Available() {
		// offline available when no key or GY_VISION_OFFLINE
		t.Setenv("GY_VISION_OFFLINE", "1")
		if !(&OfflineVisionProvider{}).Available() {
			t.Fatal("offline")
		}
	}
}

func TestOfflineVisionInfer(t *testing.T) {
	f := &FramePixels{W: 16, H: 12, RGB: make([]byte, 16*12*3)}
	for i := range f.RGB {
		f.RGB[i] = 180
	}
	url, _, err := FrameToJPEGBase64(f, 64, 48, 70)
	if err != nil {
		t.Fatal(err)
	}
	vf := VisionFrame{Frame: f, Feed: "test-news", Kind: "news", DataURL: url}
	res, err := (OfflineVisionProvider{}).Infer(context.Background(), vf, FeedOrchestrateContext{
		Mode: "news", Active: "test-news", Kind: "news", Live: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Take.Style == "" || res.Take.Theme == "" {
		t.Fatalf("%+v", res.Take)
	}
	if !res.Take.Vision {
		t.Fatal("vision flag")
	}
}

func TestDepthProxyInfer(t *testing.T) {
	f := &FramePixels{W: 32, H: 24, RGB: make([]byte, 32*24*3)}
	res, err := (DepthProxyProvider{}).Infer(context.Background(), VisionFrame{Frame: f}, FeedOrchestrateContext{})
	if err != nil || res.Depth == nil || res.Depth.Backend != "gsplat-proxy" {
		t.Fatal(err, res.Depth)
	}
	if len(res.Depth.Preview) != 64 {
		t.Fatalf("preview %d", len(res.Depth.Preview))
	}
}

func TestVisionEventMeshJSON(t *testing.T) {
	ev := VisionEvent{
		Type: "vision-take", Feed: "Al Jazeera", Theme: "breaking",
		Caption: "live", Style: "neon", Provider: "offline",
		LatencyMs: 12, At: time.Now(),
	}
	m := ev.MeshJSON("qbit")
	if m["type"] != "vision-take" || m["theme"] != "breaking" {
		t.Fatal(m)
	}
}

func TestVisionSubscribe(t *testing.T) {
	r := newVisionRegistry()
	var got string
	r.Subscribe(func(ev VisionEvent) { got = ev.Theme })
	r.Emit(VisionEvent{Type: "vision-take", Theme: "weather"})
	if got != "weather" {
		t.Fatal(got)
	}
}

func TestFormatVisionBackboneDoctor(t *testing.T) {
	doc := FormatVisionBackboneDoctor(Vision())
	if !strings.Contains(doc, "backbone") || !strings.Contains(doc, "grok") {
		t.Fatal(doc)
	}
}
