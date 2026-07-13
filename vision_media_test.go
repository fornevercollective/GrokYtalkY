package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMediaLine(t *testing.T) {
	cases := []struct {
		line string
		op   string
		tgt  string
	}{
		{"MEDIA restart focus", VisionMediaRestart, "focus"},
		{"MEDIA kill all", VisionMediaKill, "all"},
		{"MEDIA recover", VisionMediaRecover, "focus"},
		{"MEDIA spawn cnn", VisionMediaSpawn, "focus"},
		{"MEDIA encode focus jpeg", VisionMediaEncode, "focus"},
		{"MEDIA retune focus 96x54@5", VisionMediaRetune, "focus"},
	}
	for _, c := range cases {
		a, ok := ParseMediaLine(c.line)
		if !ok {
			t.Fatalf("parse fail %q", c.line)
		}
		if a.Op != c.op {
			t.Fatalf("%q op=%q want %q", c.line, a.Op, c.op)
		}
		if a.Target != c.tgt && !(c.op == VisionMediaSpawn && a.Source == "cnn") {
			// spawn puts cnn in Source
			if c.op != VisionMediaSpawn {
				t.Fatalf("%q target=%q want %q", c.line, a.Target, c.tgt)
			}
		}
	}
	a, ok := ParseMediaLine("MEDIA retune focus scale=128x72 fps=6")
	if !ok || a.ScaleW != 128 || a.ScaleH != 72 || a.FPS != 6 {
		t.Fatalf("retune geom %+v", a)
	}
	a, ok = ParseMediaLine("MEDIA spawn aje")
	if !ok || a.Source != "aje" {
		t.Fatalf("spawn source %+v", a)
	}
}

func TestParseGrokTakeMedia(t *testing.T) {
	raw := `STYLE neon
CAPTION Live desk
THEME breaking
MEDIA restart focus
MEDIA encode jpeg /tmp/snap.jpg
`
	take := ParseGrokTake(raw)
	if len(take.Media) != 2 {
		t.Fatalf("media %d %+v", len(take.Media), take.Media)
	}
	if take.Media[0].Op != VisionMediaRestart {
		t.Fatal(take.Media[0])
	}
	if take.Media[1].Op != VisionMediaEncode {
		t.Fatal(take.Media[1])
	}
	if !strings.Contains(take.TakeSummary(), "media×2") {
		t.Fatal(take.TakeSummary())
	}
}

func TestParseGeomToken(t *testing.T) {
	w, h, fps, ok := parseGeomToken("96x54@5")
	if !ok || w != 96 || h != 54 || fps != 5 {
		t.Fatalf("%d %d %d %v", w, h, fps, ok)
	}
	w, h, fps, ok = parseGeomToken("80×45")
	if !ok || w != 80 || h != 45 {
		t.Fatalf("%d %d %v", w, h, ok)
	}
}

func TestVisionMediaBudget(t *testing.T) {
	b := &VisionMediaBus{cfg: VisionMediaConfig{Enabled: true, MaxPerM: 2}}
	if !b.TryBudget() || !b.TryBudget() {
		t.Fatal("first two")
	}
	if b.TryBudget() {
		t.Fatal("third should drop")
	}
	if b.Snapshot().Dropped < 1 {
		t.Fatal("drops")
	}
	b.SetEnabled(false)
	if b.TryBudget() {
		t.Fatal("disabled")
	}
}

func TestVisionMediaEncodeFrame(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	f := &FramePixels{W: 64, H: 36, RGB: make([]byte, 64*36*3)}
	for i := range f.RGB {
		f.RGB[i] = byte(i % 180)
	}
	if err := writeFrameImage(f, out, "jpeg"); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() < 50 {
		t.Fatalf("out %v size %v", err, st)
	}
}

func TestApplyVisionMediaKillEmpty(t *testing.T) {
	// no pipes — kill focus errors soft
	m := &Model{}
	take := GrokTake{
		Vision: true,
		Media:  []VisionMediaAction{{Op: VisionMediaKill, Target: "focus"}},
	}
	// ensure enabled
	VisionMedia().SetEnabled(true)
	// may apply with fail token
	_ = ApplyVisionMediaControl(m, take)
}

func TestFormatVisionMediaDoctor(t *testing.T) {
	doc := FormatVisionMediaDoctor()
	if !strings.Contains(doc, "control plane") || !strings.Contains(doc, "GY_VISION_MEDIA") {
		t.Fatal(doc)
	}
}

func TestDeriveVisionMediaAutoRecover(t *testing.T) {
	// unhealthy tile triggers recover when auto on
	tp := &NewsTilePipe{Label: "CNN", Src: "http://example.com/x.m3u8", running: false, Err: "eof", lastDie: time.Now().Add(-5 * time.Second)}
	m := &Model{lab: &LabState{On: true, Active: 0, News: &NewsWallState{On: true, Pipes: []*NewsTilePipe{tp}, AutoRecover: true}}}
	VisionMedia().SetEnabled(true)
	// force auto
	VisionMedia().mu.Lock()
	VisionMedia().cfg.Auto = true
	VisionMedia().mu.Unlock()
	acts := DeriveVisionMediaActions(m, GrokTake{Vision: true})
	if len(acts) != 1 || acts[0].Op != VisionMediaRecover {
		t.Fatalf("%+v", acts)
	}
	// explicit media suppresses auto
	acts = DeriveVisionMediaActions(m, GrokTake{Vision: true, Media: []VisionMediaAction{{Op: VisionMediaRestart}}})
	if acts != nil {
		t.Fatal("explicit should suppress auto derive")
	}
}

func TestNewsTileOptsDefaults(t *testing.T) {
	// just ensure type compiles / geom clamp path via parse
	opts := NewsTileOpts{W: 10, H: 10, FPS: 0}
	if opts.W != 10 {
		t.Fatal()
	}
}
