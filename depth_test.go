package main

import "testing"

func TestEstimateZipLite(t *testing.T) {
	w, h := 32, 24
	rgb := make([]byte, w*h*3)
	// bright center blob (should be "near" / high z when inverse-lum weighted)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			if (x-16)*(x-16)+(y-12)*(y-12) < 40 {
				rgb[i], rgb[i+1], rgb[i+2] = 220, 200, 180
			} else {
				rgb[i], rgb[i+1], rgb[i+2] = 30, 30, 40
			}
		}
	}
	dm := EstimateZipLite(rgb, w, h)
	if dm == nil || len(dm.Z) != w*h {
		t.Fatal("map")
	}
	if dm.Via != "zip-lite" {
		t.Fatalf("via %s", dm.Via)
	}
}

func TestGsplatAndColorize(t *testing.T) {
	w, h := 16, 12
	rgb := make([]byte, w*h*3)
	for i := range rgb {
		rgb[i] = 80
	}
	f := &FramePixels{W: w, H: h, RGB: append([]byte(nil), rgb...)}
	dm := EstimateGsplatProxy(rgb, w, h, defaultGsplat, 1.2)
	if dm == nil {
		t.Fatal("gsplat")
	}
	ApplyDepthColorize(f, dm)
	ApplyGsplatStack(f, dm, defaultGsplat, 0.5)
	gm := DepthToGlyph(dm, 9)
	if len(gm.Data) != 81 {
		t.Fatal("glyph")
	}
}

func TestDepthSessionCycle(t *testing.T) {
	s := newDepthSession()
	if s.Mode() != DepthOff {
		t.Fatal("start off")
	}
	s.Cycle()
	if s.Mode() != DepthZipLite {
		t.Fatal(s.Mode())
	}
}
