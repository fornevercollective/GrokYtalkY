package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Plugin system — in-process hooks for mesh messages and glyph/pixel styles.
// Go plugins (.so) are not used (fragile cross-version); register built-ins
// and load JSON manifests from GY_PLUGIN_DIR / ~/.config/grokytalky/plugins.

// StylePainter is a visual plugin: preprocess and/or custom paint.
type StylePainter interface {
	Name() string
	// Cost 1–5 for stream throttle (same scale as PixelMode.StyleCost).
	Cost() int
	// Paint renders work frame into ANSI string honoring geom.
	// If empty string returned, caller falls back to half-block after Preprocess.
	Paint(f *FramePixels, geom StyleGeom) string
	// Preprocess mutates f before Paint / half fallback.
	Preprocess(f *FramePixels, geom StyleGeom)
}

// MeshHook observes or mutates mesh JSON (inbound from hub / outbound to hub).
type MeshHook interface {
	Name() string
	// OnInbound: return nil to drop, or modified/same map.
	OnInbound(msg map[string]any) map[string]any
	// OnOutbound: return nil to drop outbound, or modified/same.
	OnOutbound(msg map[string]any) map[string]any
}

// Plugin is a named unit that may provide style and/or mesh hooks.
type Plugin interface {
	Name() string
	// Enabled is toggled at runtime (/plugin on|off).
	Enabled() bool
	SetEnabled(bool)
	// Description one-line for /plugin list.
	Description() string
	// Style optional custom painter.
	Style() StylePainter
	// Mesh optional mesh hook.
	Mesh() MeshHook
}

// PluginRegistry holds registered plugins.
type PluginRegistry struct {
	mu   sync.RWMutex
	by   map[string]Plugin
	order []string
}

var (
	pluginsOnce sync.Once
	pluginsReg  *PluginRegistry
)

// Plugins returns the global registry (built-ins registered on first use).
func Plugins() *PluginRegistry {
	pluginsOnce.Do(func() {
		pluginsReg = &PluginRegistry{by: make(map[string]Plugin)}
		registerBuiltinPlugins(pluginsReg)
		_ = pluginsReg.LoadDir(defaultPluginDir())
	})
	return pluginsReg
}

func defaultPluginDir() string {
	if d := strings.TrimSpace(os.Getenv("GY_PLUGIN_DIR")); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "grokytalky", "plugins")
}

// Register adds a plugin (idempotent by name).
func (r *PluginRegistry) Register(p Plugin) {
	if r == nil || p == nil || p.Name() == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	name := strings.ToLower(p.Name())
	if _, ok := r.by[name]; !ok {
		r.order = append(r.order, name)
	}
	r.by[name] = p
}

// Get looks up by name.
func (r *PluginRegistry) Get(name string) Plugin {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.by[strings.ToLower(strings.TrimSpace(name))]
}

// List returns plugins in registration order.
func (r *PluginRegistry) List() []Plugin {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Plugin, 0, len(r.order))
	for _, n := range r.order {
		if p := r.by[n]; p != nil {
			out = append(out, p)
		}
	}
	return out
}

// StyleNames lists enabled custom style plugin names.
func (r *PluginRegistry) StyleNames() []string {
	var out []string
	for _, p := range r.List() {
		if p.Enabled() && p.Style() != nil {
			out = append(out, p.Style().Name())
		}
	}
	sort.Strings(out)
	return out
}

// FindStyle returns an enabled style painter by name.
func (r *PluginRegistry) FindStyle(name string) StylePainter {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, p := range r.List() {
		if !p.Enabled() || p.Style() == nil {
			continue
		}
		if strings.ToLower(p.Style().Name()) == name {
			return p.Style()
		}
	}
	return nil
}

// ApplyMeshInbound runs all enabled mesh hooks in order.
func (r *PluginRegistry) ApplyMeshInbound(msg map[string]any) map[string]any {
	if r == nil || msg == nil {
		return msg
	}
	for _, p := range r.List() {
		if !p.Enabled() || p.Mesh() == nil {
			continue
		}
		msg = p.Mesh().OnInbound(msg)
		if msg == nil {
			return nil
		}
	}
	return msg
}

// ApplyMeshOutbound runs all enabled mesh hooks in reverse order (LIFO egress).
func (r *PluginRegistry) ApplyMeshOutbound(msg map[string]any) map[string]any {
	if r == nil || msg == nil {
		return msg
	}
	list := r.List()
	for i := len(list) - 1; i >= 0; i-- {
		p := list[i]
		if !p.Enabled() || p.Mesh() == nil {
			continue
		}
		msg = p.Mesh().OnOutbound(msg)
		if msg == nil {
			return nil
		}
	}
	return msg
}

// SetEnabled toggles a plugin by name.
func (r *PluginRegistry) SetEnabled(name string, on bool) error {
	p := r.Get(name)
	if p == nil {
		return fmt.Errorf("plugin %q not found", name)
	}
	p.SetEnabled(on)
	return nil
}

// FormatPluginList for /plugin list / doctor.
func (r *PluginRegistry) FormatPluginList() string {
	var b strings.Builder
	b.WriteString("plugins\n")
	list := r.List()
	if len(list) == 0 {
		b.WriteString("  (none registered)\n")
		return b.String()
	}
	for _, p := range list {
		mark := "·"
		if p.Enabled() {
			mark = "✓"
		}
		kind := ""
		if p.Style() != nil {
			kind += "style "
		}
		if p.Mesh() != nil {
			kind += "mesh "
		}
		fmt.Fprintf(&b, "  %s %-12s  %s— %s\n", mark, p.Name(), kind, p.Description())
	}
	b.WriteString("  dir: " + defaultPluginDir() + "\n")
	b.WriteString("  /plugin on|off <name> · /plugin list · GY_PLUGIN_DIR\n")
	return b.String()
}

// ── base plugin shell ────────────────────────────────────────

type basePlugin struct {
	name, desc string
	on         bool
	style      StylePainter
	mesh       MeshHook
}

func (p *basePlugin) Name() string           { return p.name }
func (p *basePlugin) Description() string    { return p.desc }
func (p *basePlugin) Enabled() bool          { return p.on }
func (p *basePlugin) SetEnabled(v bool)      { p.on = v }
func (p *basePlugin) Style() StylePainter    { return p.style }
func (p *basePlugin) Mesh() MeshHook         { return p.mesh }

// ── JSON manifest plugins ────────────────────────────────────

// pluginManifest is a declarative plugin from disk.
type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"` // default true
	// style: invert | mirror | threshold | (empty = mesh-only)
	Style string `json:"style"`
	// mesh: chat-prefix | type-filter | (empty = style-only)
	Mesh string `json:"mesh"`
	// options
	Prefix   string   `json:"prefix"`    // chat-prefix mesh
	Types    []string `json:"types"`     // type-filter allow list
	DropTypes []string `json:"drop_types"`
	Level    int      `json:"level"`     // threshold 0-255
}

// LoadDir loads *.json manifests from dir (non-fatal if missing).
func (r *PluginRegistry) LoadDir(dir string) error {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err // caller ignores
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var man pluginManifest
		if json.Unmarshal(data, &man) != nil || man.Name == "" {
			continue
		}
		p := pluginFromManifest(man)
		if p != nil {
			r.Register(p)
		}
	}
	return nil
}

// Reload re-reads GY_PLUGIN_DIR / default config plugins (overwrites same names).
func (r *PluginRegistry) Reload() (int, error) {
	if r == nil {
		return 0, fmt.Errorf("no registry")
	}
	before := len(r.List())
	dir := defaultPluginDir()
	err := r.LoadDir(dir)
	after := len(r.List())
	// count net new (or at least how many files we attempted)
	n := after - before
	if n < 0 {
		n = 0
	}
	// also count overwrites: re-scan dir for file count
	if dir != "" {
		if entries, e2 := os.ReadDir(dir); e2 == nil {
			c := 0
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
					c++
				}
			}
			if c > n {
				n = c
			}
		}
	}
	return n, err
}

func pluginFromManifest(m pluginManifest) Plugin {
	on := true
	if m.Enabled != nil {
		on = *m.Enabled
	}
	bp := &basePlugin{
		name: m.Name,
		desc: m.Description,
		on:   on,
	}
	if bp.desc == "" {
		bp.desc = "manifest plugin"
	}
	switch strings.ToLower(m.Style) {
	case "invert":
		bp.style = &invertStyle{}
	case "mirror":
		bp.style = &mirrorStyle{}
	case "threshold":
		lv := m.Level
		if lv <= 0 {
			lv = 128
		}
		bp.style = &thresholdStyle{level: lv}
	case "heatmap":
		bp.style = &heatmapStyle{}
	}
	switch strings.ToLower(m.Mesh) {
	case "chat-prefix":
		pfx := m.Prefix
		if pfx == "" {
			pfx = "[" + m.Name + "] "
		}
		bp.mesh = &chatPrefixMesh{prefix: pfx, name: m.Name + "-mesh"}
	case "type-filter":
		bp.mesh = &typeFilterMesh{
			name: m.Name + "-mesh",
			allow: toSet(m.Types),
			drop:  toSet(m.DropTypes),
		}
	}
	if bp.style == nil && bp.mesh == nil {
		return nil
	}
	return bp
}

func toSet(ss []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range ss {
		m[strings.ToLower(strings.TrimSpace(s))] = true
	}
	return m
}

// ── built-in style painters ──────────────────────────────────

type invertStyle struct{}

func (invertStyle) Name() string { return "invert" }
func (invertStyle) Cost() int    { return 1 }
func (invertStyle) Paint(*FramePixels, StyleGeom) string { return "" }
func (s invertStyle) Preprocess(f *FramePixels, _ StyleGeom) {
	if f == nil {
		return
	}
	for i := range f.RGB {
		f.RGB[i] = 255 - f.RGB[i]
	}
}

type mirrorStyle struct{}

func (mirrorStyle) Name() string { return "mirror" }
func (mirrorStyle) Cost() int    { return 1 }
func (mirrorStyle) Paint(*FramePixels, StyleGeom) string { return "" }
func (mirrorStyle) Preprocess(f *FramePixels, _ StyleGeom) {
	if f == nil || f.W < 2 {
		return
	}
	row := make([]byte, f.W*3)
	for y := 0; y < f.H; y++ {
		off := y * f.W * 3
		copy(row, f.RGB[off:off+f.W*3])
		for x := 0; x < f.W; x++ {
			sx := f.W - 1 - x
			di := off + x*3
			si := sx * 3
			f.RGB[di], f.RGB[di+1], f.RGB[di+2] = row[si], row[si+1], row[si+2]
		}
	}
}

type thresholdStyle struct{ level int }

func (t thresholdStyle) Name() string { return "threshold" }
func (thresholdStyle) Cost() int      { return 1 }
func (thresholdStyle) Paint(*FramePixels, StyleGeom) string { return "" }
func (t thresholdStyle) Preprocess(f *FramePixels, _ StyleGeom) {
	if f == nil {
		return
	}
	lv := t.level
	if lv <= 0 {
		lv = 128
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			L := f.lum(x, y) * 255
			v := byte(0)
			if int(L) >= lv {
				v = 255
			}
			i := (y*f.W + x) * 3
			f.RGB[i], f.RGB[i+1], f.RGB[i+2] = v, v, v
		}
	}
}

type heatmapStyle struct{}

func (heatmapStyle) Name() string { return "heatmap" }
func (heatmapStyle) Cost() int    { return 2 }
func (heatmapStyle) Paint(*FramePixels, StyleGeom) string { return "" }
func (heatmapStyle) Preprocess(f *FramePixels, _ StyleGeom) {
	if f == nil {
		return
	}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			L := f.lum(x, y)
			// blue → cyan → yellow → red
			var r, g, b byte
			switch {
			case L < 0.25:
				t := L / 0.25
				b = byte(100 + 155*t)
			case L < 0.5:
				t := (L - 0.25) / 0.25
				g = byte(255 * t)
				b = byte(255 * (1 - t))
			case L < 0.75:
				t := (L - 0.5) / 0.25
				r = byte(255 * t)
				g = 255
			default:
				t := (L - 0.75) / 0.25
				r = 255
				g = byte(255 * (1 - t))
			}
			i := (y*f.W + x) * 3
			f.RGB[i], f.RGB[i+1], f.RGB[i+2] = r, g, b
		}
	}
}

// ── built-in mesh hooks ──────────────────────────────────────

type chatPrefixMesh struct {
	name, prefix string
}

func (c *chatPrefixMesh) Name() string { return c.name }
func (c *chatPrefixMesh) OnInbound(msg map[string]any) map[string]any {
	if t, _ := msg["type"].(string); t == "chat" {
		if text, ok := msg["text"].(string); ok {
			msg["text"] = c.prefix + text
		}
	}
	return msg
}
func (c *chatPrefixMesh) OnOutbound(msg map[string]any) map[string]any { return msg }

type typeFilterMesh struct {
	name        string
	allow, drop map[string]bool
}

func (t *typeFilterMesh) Name() string { return t.name }
func (t *typeFilterMesh) OnInbound(msg map[string]any) map[string]any {
	typ, _ := msg["type"].(string)
	typ = strings.ToLower(typ)
	if len(t.drop) > 0 && t.drop[typ] {
		return nil
	}
	if len(t.allow) > 0 && !t.allow[typ] {
		return nil
	}
	return msg
}
func (t *typeFilterMesh) OnOutbound(msg map[string]any) map[string]any { return msg }

// ── register built-ins ───────────────────────────────────────

func registerBuiltinPlugins(r *PluginRegistry) {
	r.Register(&basePlugin{
		name: "invert", desc: "style · RGB invert (GrokGlyph alt)", on: true,
		style: &invertStyle{},
	})
	r.Register(&basePlugin{
		name: "mirror", desc: "style · horizontal mirror", on: true,
		style: &mirrorStyle{},
	})
	r.Register(&basePlugin{
		name: "threshold", desc: "style · hard B/W threshold", on: true,
		style: &thresholdStyle{level: 128},
	})
	r.Register(&basePlugin{
		name: "heatmap", desc: "style · false-color luminance heatmap", on: true,
		style: &heatmapStyle{},
	})
	r.Register(&basePlugin{
		name: "mesh-tag", desc: "mesh · prefix inbound chat with [mesh] ", on: false,
		mesh: &chatPrefixMesh{name: "mesh-tag", prefix: "[mesh] "},
	})
	// drop noisy mid-lane hooks by default off
	r.Register(&basePlugin{
		name: "quiet-roster", desc: "mesh · drop roster floods (inbound)", on: false,
		mesh: &typeFilterMesh{name: "quiet-roster", drop: map[string]bool{"roster": true}},
	})
}
