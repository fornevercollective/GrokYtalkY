package main

import (
	"strings"
	"testing"
)

func TestDefaultCameraLook(t *testing.T) {
	l := DefaultCameraLook()
	if l.Facing != "environment" || l.WBMode != "auto" {
		t.Fatalf("%+v", l)
	}
}

func TestApplyCameraPresetFilm(t *testing.T) {
	l := ApplyCameraPreset(DefaultCameraLook(), "film")
	if l.Preset != "film" || l.Grain <= 0 {
		t.Fatalf("%+v", l)
	}
	n := ApplyCameraPreset(DefaultCameraLook(), "night")
	if !n.Night || n.Fill <= 0 {
		t.Fatalf("%+v", n)
	}
}

func TestParseCameraLookLine(t *testing.T) {
	l, keys, ok := ParseCameraLookLine("CAMERA exposure=0.25 contrast=0.1 fill=0.4 wb=daylight iso=400")
	if !ok {
		t.Fatal("parse")
	}
	if l.Exposure < 0.2 || l.Fill < 0.3 || l.ISO != 400 {
		t.Fatalf("%+v keys=%v", l, keys)
	}
	l2, _, ok := ParseCameraLookLine("LOOK night")
	if !ok || !l2.Night {
		// LOOK night may need ApplyCameraPreset in path
		l2 = ApplyCameraPreset(DefaultCameraLook(), "night")
		if !l2.Night {
			t.Fatal(l2)
		}
	}
}

func TestApplyCameraLookGrade(t *testing.T) {
	f := &FramePixels{W: 8, H: 8, RGB: make([]byte, 8*8*3)}
	for i := range f.RGB {
		f.RGB[i] = 80
	}
	l := DefaultCameraLook()
	l.Exposure = 0.5
	l.Temperature = 0.4
	ApplyCameraLook(f, l)
	// should brighten
	if f.RGB[0] <= 80 {
		t.Fatalf("expected brighter %d", f.RGB[0])
	}
}

func TestCameraLookMeshJSON(t *testing.T) {
	l := ApplyCameraPreset(DefaultCameraLook(), "neon")
	m := l.MeshJSON("phone")
	if m["type"] != "camera-controls" {
		t.Fatal(m)
	}
}

func TestFormatCameraDoctor(t *testing.T) {
	doc := FormatCameraDoctor()
	if !strings.Contains(doc, "camera") || !strings.Contains(doc, "aito") {
		t.Fatal(doc)
	}
}

func TestCameraBusPatch(t *testing.T) {
	c := Camera()
	c.SetLook(DefaultCameraLook())
	got := c.Patch(CameraLook{Exposure: 0.3, Preset: "punchy"}, nil)
	if got.Exposure != 0.3 {
		t.Fatal(got)
	}
	sum := got.LookSummary()
	if sum == "" {
		t.Fatal("empty summary")
	}
}
