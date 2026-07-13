package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinPluginsRegistered(t *testing.T) {
	r := Plugins()
	list := r.List()
	if len(list) < 4 {
		t.Fatalf("expected builtins, got %d", len(list))
	}
	for _, name := range []string{"invert", "mirror", "threshold", "heatmap", "mesh-tag", "quiet-roster", "theme-vision"} {
		if r.Get(name) == nil {
			t.Fatalf("missing %s", name)
		}
	}
	doc := r.FormatPluginList()
	if !strings.Contains(doc, "plugins") || !strings.Contains(doc, "invert") {
		t.Fatal(doc)
	}
}

func TestStylePainterInvert(t *testing.T) {
	sp := Plugins().FindStyle("invert")
	if sp == nil {
		t.Fatal("invert style")
	}
	f := &FramePixels{W: 2, H: 1, RGB: []byte{0, 10, 255, 128, 128, 128}}
	sp.Preprocess(f, StyleGeom{Cols: 2, Rows: 1})
	if f.RGB[0] != 255 || f.RGB[1] != 245 || f.RGB[2] != 0 {
		t.Fatalf("invert %v", f.RGB[:3])
	}
}

func TestStylePainterMirror(t *testing.T) {
	sp := Plugins().FindStyle("mirror")
	if sp == nil {
		t.Fatal("mirror")
	}
	// two pixels: red | green → green | red
	f := &FramePixels{W: 2, H: 1, RGB: []byte{255, 0, 0, 0, 255, 0}}
	sp.Preprocess(f, StyleGeom{})
	if f.RGB[0] != 0 || f.RGB[1] != 255 || f.RGB[3] != 255 {
		t.Fatalf("mirror %v", f.RGB)
	}
}

func TestMeshInboundQuietRoster(t *testing.T) {
	r := &PluginRegistry{by: make(map[string]Plugin)}
	registerBuiltinPlugins(r)
	_ = r.SetEnabled("quiet-roster", true)
	msg := map[string]any{"type": "roster", "peers": []any{}}
	if out := r.ApplyMeshInbound(msg); out != nil {
		t.Fatal("expected drop")
	}
	chat := map[string]any{"type": "chat", "text": "hi"}
	if out := r.ApplyMeshInbound(chat); out == nil {
		t.Fatal("chat should pass")
	}
}

func TestMeshTagPrefix(t *testing.T) {
	r := &PluginRegistry{by: make(map[string]Plugin)}
	registerBuiltinPlugins(r)
	_ = r.SetEnabled("mesh-tag", true)
	msg := map[string]any{"type": "chat", "text": "yo"}
	out := r.ApplyMeshInbound(msg)
	if out == nil {
		t.Fatal("nil")
	}
	text, _ := out["text"].(string)
	if !strings.HasPrefix(text, "[mesh] ") {
		t.Fatalf("got %q", text)
	}
}

func TestLoadDirManifest(t *testing.T) {
	dir := t.TempDir()
	man := `{
  "name": "test-pfx",
  "description": "unit test prefix",
  "enabled": true,
  "mesh": "chat-prefix",
  "prefix": "[T] "
}`
	if err := os.WriteFile(filepath.Join(dir, "test-pfx.json"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &PluginRegistry{by: make(map[string]Plugin)}
	if err := r.LoadDir(dir); err != nil {
		t.Fatal(err)
	}
	p := r.Get("test-pfx")
	if p == nil || p.Mesh() == nil {
		t.Fatal("manifest not loaded")
	}
	out := r.ApplyMeshInbound(map[string]any{"type": "chat", "text": "a"})
	if text, _ := out["text"].(string); text != "[T] a" {
		t.Fatalf("got %v", out)
	}
}

func TestRenderFrameNamedPlugin(t *testing.T) {
	// solid mid gray → invert → still paints
	f := &FramePixels{W: 4, H: 4, RGB: make([]byte, 4*4*3)}
	for i := range f.RGB {
		f.RGB[i] = 80
	}
	s := RenderFrameNamed(f, "invert", PixelHalf, 4, 2)
	if s == "" || strings.Contains(s, "no video") {
		t.Fatal(s)
	}
	// unknown plugin name falls back to mode
	s2 := RenderFrameNamed(f, "not-a-plugin", PixelHalf, 4, 2)
	if s2 == "" {
		t.Fatal("fallback empty")
	}
}

func TestSetEnabledMissing(t *testing.T) {
	r := Plugins()
	if err := r.SetEnabled("nope-xyz", true); err == nil {
		t.Fatal("expected error")
	}
}
