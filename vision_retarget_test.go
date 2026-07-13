package main

import (
	"strings"
	"testing"
	"time"
)

func TestSegmentToCropPadClamp(t *testing.T) {
	s := VisionSegment{Label: "person", Score: 0.9, BBox: [4]float64{0.3, 0.2, 0.4, 0.5}}
	c := SegmentToCrop(s, 0.1)
	if !c.Valid() {
		t.Fatal(c)
	}
	if c.X < 0 || c.Y < 0 || c.X+c.W > 1.01 || c.Y+c.H > 1.01 {
		t.Fatalf("clamp %+v", c)
	}
	// edge bbox
	s2 := VisionSegment{BBox: [4]float64{0, 0, 0.2, 0.2}}
	c2 := SegmentToCrop(s2, 0.2)
	if c2.X < 0 || c2.Y < 0 {
		t.Fatal(c2)
	}
}

func TestSelectRetargetSegmentPreferPerson(t *testing.T) {
	segs := []VisionSegment{
		{Label: "text", Score: 0.95, BBox: [4]float64{0.05, 0.8, 0.9, 0.15}},
		{Label: "person", Score: 0.7, BBox: [4]float64{0.25, 0.15, 0.45, 0.65}},
	}
	cfg := RetargetConfig{MinScore: 0.45, Prefer: "person,face"}
	got, ok := SelectRetargetSegment(segs, cfg)
	if !ok || got.Label != "person" {
		t.Fatalf("%v %+v", ok, got)
	}
}

func TestCropIoU(t *testing.T) {
	a := VisionCrop{X: 0.2, Y: 0.2, W: 0.4, H: 0.4}
	b := VisionCrop{X: 0.25, Y: 0.25, W: 0.4, H: 0.4}
	if a.IoU(b) < 0.5 {
		t.Fatal(a.IoU(b))
	}
	if a.IoU(VisionCrop{X: 0.8, Y: 0.8, W: 0.1, H: 0.1}) != 0 {
		t.Fatal("disjoint")
	}
}

func TestFFmpegCropFilter(t *testing.T) {
	c := VisionCrop{X: 0.1, Y: 0.2, W: 0.5, H: 0.4}
	vf := c.FFmpegCropFilter(64, 36, 3)
	if !strings.Contains(vf, "crop=") || !strings.Contains(vf, "scale=64:36") || !strings.Contains(vf, "fps=3") {
		t.Fatal(vf)
	}
}

func TestParseCropToken(t *testing.T) {
	c, ok := parseCropToken("crop=0.1,0.2,0.5,0.6")
	if !ok || c.X != 0.1 || c.W != 0.5 {
		t.Fatal(c, ok)
	}
	c2, ok := parseCropToken("0.1:0.2:0.3:0.4")
	if !ok || c2.H != 0.4 {
		t.Fatal(c2, ok)
	}
}

func TestParseMediaRetarget(t *testing.T) {
	a, ok := ParseMediaLine("MEDIA retarget focus crop=0.2,0.1,0.5,0.7")
	if !ok || a.Op != VisionMediaRetarget {
		t.Fatal(a, ok)
	}
	if a.CropX != 0.2 || a.CropW != 0.5 {
		t.Fatal(a)
	}
}

func TestCropFramePixels(t *testing.T) {
	f := &FramePixels{W: 100, H: 50, RGB: make([]byte, 100*50*3)}
	for i := range f.RGB {
		f.RGB[i] = byte(i % 200)
	}
	c := VisionCrop{X: 0.1, Y: 0.1, W: 0.4, H: 0.4}
	out := CropFramePixels(f, c)
	if out == nil || out.W < 10 || out.H < 10 {
		t.Fatal(out)
	}
	if len(out.RGB) != out.W*out.H*3 {
		t.Fatal(len(out.RGB))
	}
}

func TestAttachRetargetToTake(t *testing.T) {
	t.Setenv("GY_VISION_RETARGET", "1")
	t.Setenv("GY_VISION_MEDIA", "1")
	// reset last so IoU skip doesn't fire
	globalRetarget.mu.Lock()
	globalRetarget.last = VisionCrop{}
	globalRetarget.lastAt = time.Time{}
	globalRetarget.mu.Unlock()

	res := &VisionResult{
		Take: GrokTake{Vision: true},
		Segments: []VisionSegment{
			{Label: "person", Score: 0.88, BBox: [4]float64{0.2, 0.15, 0.45, 0.6}},
		},
	}
	AttachRetargetToTake(&Model{}, res)
	if len(res.Take.Media) != 1 || res.Take.Media[0].Op != VisionMediaRetarget {
		t.Fatalf("%+v", res.Take.Media)
	}
	if res.Take.Media[0].CropW < 0.3 {
		t.Fatal(res.Take.Media[0])
	}
	// after recording same crop, DeriveSAMRetarget should skip (IoU)
	crop := SegmentToCrop(res.Segments[0], LoadRetargetConfig().Pad)
	recordRetarget(crop, "t")
	act := DeriveSAMRetarget(&Model{}, res.Segments)
	if act != nil {
		t.Fatalf("expected IoU skip, got %s", act.Raw)
	}
}

func TestDeriveSAMRetargetDisabled(t *testing.T) {
	t.Setenv("GY_VISION_RETARGET", "0")
	act := DeriveSAMRetarget(nil, []VisionSegment{
		{Label: "person", Score: 0.9, BBox: [4]float64{0.2, 0.2, 0.4, 0.4}},
	})
	if act != nil {
		t.Fatal("should be disabled")
	}
}

func TestFormatRetargetDoctor(t *testing.T) {
	doc := FormatRetargetDoctor()
	if !strings.Contains(doc, "retarget") || !strings.Contains(doc, "SAM") {
		t.Fatal(doc)
	}
}

func TestActionFromCrop(t *testing.T) {
	a := ActionFromCrop(VisionCrop{X: 0.1, Y: 0.2, W: 0.3, H: 0.4, Label: "face"}, "focus")
	if a.Op != VisionMediaRetarget || a.CropX != 0.1 || a.Source != "face" {
		t.Fatal(a)
	}
}
