package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// Builtin VisionPlugin: theme-reactive style painter on vision-take (v1.73).
//
// Listens for vision-take events, stores theme, and paints focus/lab frames
// with a theme-keyed grade (breaking→scan/red, markets→hex green, …).
// When auto-style is on, vision takes set lab PluginStyle → "theme-vision".
//
// Env:
//
//	GY_VISION_THEME_STYLE=1   auto-set PluginStyle on vision-take (default on)
//	GY_VISION_THEME_PIXEL=1   also map theme → PixelMode on focus/news tiles
//	GY_PLUGIN_THEME=0         disable plugin at register (or /plugin off theme-vision)

const themeVisionPluginName = "theme-vision"
const themeVisionStyleName = "theme-vision"

// themeVisionPlugin is Plugin + VisionHook + StylePainter owner.
type themeVisionPlugin struct {
	mu      sync.RWMutex
	on      bool
	theme   string
	feed    string
	style   string // last STYLE from take
	caption string
	takes   int64
	// auto: set lab PluginStyle on take
	autoStyle bool
	autoPixel bool
}

var (
	themeVisionOnce   sync.Once
	themeVisionInst   *themeVisionPlugin
	metricThemeVision atomic.Int64
)

// ThemeVision returns the singleton theme-vision plugin (for apply/doctor).
func ThemeVision() *themeVisionPlugin {
	themeVisionOnce.Do(func() {
		themeVisionInst = newThemeVisionPlugin()
	})
	return themeVisionInst
}

func newThemeVisionPlugin() *themeVisionPlugin {
	p := &themeVisionPlugin{
		on:        true,
		autoStyle: true,
		autoPixel: true,
	}
	if v := strings.TrimSpace(os.Getenv("GY_PLUGIN_THEME")); v != "" {
		p.on = envTruthy("GY_PLUGIN_THEME")
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_THEME_STYLE")); v != "" {
		p.autoStyle = envTruthy("GY_VISION_THEME_STYLE")
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_THEME_PIXEL")); v != "" {
		p.autoPixel = envTruthy("GY_VISION_THEME_PIXEL")
	}
	return p
}

func (p *themeVisionPlugin) Name() string { return themeVisionPluginName }
func (p *themeVisionPlugin) Description() string {
	return "vision · theme-reactive style painter (vision-take → grade)"
}
func (p *themeVisionPlugin) Enabled() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.on
}
func (p *themeVisionPlugin) SetEnabled(v bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.on = v
	p.mu.Unlock()
}
func (p *themeVisionPlugin) Style() StylePainter {
	if p == nil {
		return nil
	}
	return &themeReactiveStyle{p: p}
}
func (p *themeVisionPlugin) Mesh() MeshHook { return nil }

// VisionHook auto-registers with Vision().Registry() at bus init.
func (p *themeVisionPlugin) VisionHook() VisionHook { return p }

// OnVision reacts to vision-take / vision-error.
func (p *themeVisionPlugin) OnVision(ev VisionEvent) {
	if p == nil || !p.Enabled() {
		return
	}
	if ev.Type != "vision-take" && ev.Type != "" {
		if ev.Type == "vision-error" {
			return
		}
	}
	theme := ev.Theme
	if theme == "" && ev.Take.Theme != "" {
		theme = ev.Take.Theme
	}
	if theme == "" {
		return
	}
	p.mu.Lock()
	p.theme = normalizeThemeToken(theme)
	if ev.Feed != "" {
		p.feed = ev.Feed
	}
	if ev.Style != "" {
		p.style = ev.Style
	} else if ev.Take.Style != "" {
		p.style = ev.Take.Style
	}
	if ev.Caption != "" {
		p.caption = ev.Caption
	} else if ev.Take.Caption != "" {
		p.caption = ev.Take.Caption
	}
	p.takes++
	p.mu.Unlock()
	metricThemeVision.Add(1)
}

// Theme snapshot.
func (p *themeVisionPlugin) Theme() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.theme
}

// Snapshot for doctor.
type ThemeVisionSnapshot struct {
	Enabled   bool
	Theme     string
	Feed      string
	Style     string
	Caption   string
	Takes     int64
	AutoStyle bool
	AutoPixel bool
	PixelHint string
}

// Snapshot copies state.
func (p *themeVisionPlugin) Snapshot() ThemeVisionSnapshot {
	if p == nil {
		return ThemeVisionSnapshot{}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := ThemeVisionSnapshot{
		Enabled: p.on, Theme: p.theme, Feed: p.feed, Style: p.style,
		Caption: p.caption, Takes: p.takes,
		AutoStyle: p.autoStyle, AutoPixel: p.autoPixel,
	}
	if s.Theme != "" {
		if mode, ok := ThemeToPixelMode(s.Theme); ok {
			s.PixelHint = mode.String()
		}
	}
	return s
}

// ── StylePainter ───────────────────────────────────────────

// themeReactiveStyle grades frames by last vision theme (and per-feed theme).
type themeReactiveStyle struct {
	p *themeVisionPlugin
}

func (themeReactiveStyle) Name() string { return themeVisionStyleName }
func (themeReactiveStyle) Cost() int    { return 2 }
func (themeReactiveStyle) Paint(*FramePixels, StyleGeom) string {
	// empty → half-block after Preprocess (tinted RGB)
	return ""
}

func (s themeReactiveStyle) Preprocess(f *FramePixels, _ StyleGeom) {
	if f == nil {
		return
	}
	theme := ""
	if s.p != nil && s.p.Enabled() {
		// prefer per-feed theme from Vision bus when Source is news:Label
		if f.Source != "" {
			lab := f.Source
			if strings.HasPrefix(lab, "news:") {
				lab = strings.TrimPrefix(lab, "news:")
			}
			if t := Vision().ThemeForFeed(lab); t != "" {
				theme = t
			}
		}
		if theme == "" {
			theme = s.p.Theme()
		}
	}
	if theme == "" {
		theme = Vision().LastTheme()
	}
	if theme == "" {
		theme = "unsorted"
	}
	applyThemeGrade(f, theme)
}

// ThemeGrade is RGB multiply + lift for a theme token.
type ThemeGrade struct {
	// channel gains (1 = identity)
	R, G, B float64
	// lift added after multiply (0–40)
	LiftR, LiftG, LiftB float64
	// contrast around mid (1 = flat)
	Contrast float64
	// scanline darken every N rows (0 = off)
	Scan int
	// edge boost 0–1
	Edge float64
}

// ThemeGradeFor maps theme → grade.
func ThemeGradeFor(theme string) ThemeGrade {
	theme = normalizeThemeToken(theme)
	switch theme {
	case "breaking":
		return ThemeGrade{R: 1.25, G: 0.75, B: 0.7, LiftR: 12, Contrast: 1.15, Scan: 2, Edge: 0.25}
	case "politics":
		return ThemeGrade{R: 0.75, G: 0.85, B: 1.2, LiftB: 10, Contrast: 1.05}
	case "conflict":
		return ThemeGrade{R: 1.35, G: 0.65, B: 0.55, LiftR: 18, Contrast: 1.2, Edge: 0.4}
	case "markets":
		return ThemeGrade{R: 0.55, G: 1.3, B: 0.7, LiftG: 8, Contrast: 1.1}
	case "weather":
		return ThemeGrade{R: 0.7, G: 0.95, B: 1.25, LiftB: 14, LiftG: 4}
	case "health":
		return ThemeGrade{R: 0.85, G: 1.15, B: 0.95, LiftG: 10, Contrast: 0.98}
	case "science":
		return ThemeGrade{R: 0.9, G: 0.75, B: 1.3, LiftB: 12, LiftR: 4, Contrast: 1.08}
	case "local":
		return ThemeGrade{R: 1.15, G: 0.95, B: 0.7, LiftR: 8, LiftG: 4}
	case "culture":
		return ThemeGrade{R: 1.2, G: 0.7, B: 1.15, LiftR: 6, LiftB: 10, Contrast: 1.05}
	case "earthcam", "scenic":
		return ThemeGrade{R: 0.85, G: 1.2, B: 0.95, LiftG: 6, Edge: 0.15, Contrast: 1.08}
	default: // unsorted
		return ThemeGrade{R: 1, G: 1, B: 1, Contrast: 1.02}
	}
}

// ThemeToPixelMode maps theme → preferred terminal PixelMode.
func ThemeToPixelMode(theme string) (PixelMode, bool) {
	switch normalizeThemeToken(theme) {
	case "breaking":
		return PixelScan, true
	case "politics":
		return PixelBraille, true
	case "conflict":
		return PixelNeon, true
	case "markets":
		return PixelHex, true
	case "weather":
		return PixelDither, true
	case "health":
		return PixelHalf, true
	case "science":
		return PixelPoints, true
	case "local":
		return PixelBlocks, true
	case "culture":
		return PixelPoster, true
	case "earthcam", "scenic":
		return PixelNeon, true
	default:
		return PixelHex, true
	}
}

func applyThemeGrade(f *FramePixels, theme string) {
	if f == nil || len(f.RGB) < 3 {
		return
	}
	g := ThemeGradeFor(theme)
	if g.R == 0 {
		g.R = 1
	}
	if g.G == 0 {
		g.G = 1
	}
	if g.B == 0 {
		g.B = 1
	}
	if g.Contrast == 0 {
		g.Contrast = 1
	}
	for y := 0; y < f.H; y++ {
		scanMul := 1.0
		if g.Scan > 0 && y%g.Scan == g.Scan-1 {
			scanMul = 0.55
		}
		for x := 0; x < f.W; x++ {
			i := (y*f.W + x) * 3
			if i+2 >= len(f.RGB) {
				return
			}
			rf := (float64(f.RGB[i])*g.R + g.LiftR) * scanMul
			gf := (float64(f.RGB[i+1])*g.G + g.LiftG) * scanMul
			bf := (float64(f.RGB[i+2])*g.B + g.LiftB) * scanMul
			// contrast around 128
			rf = (rf-128)*g.Contrast + 128
			gf = (gf-128)*g.Contrast + 128
			bf = (bf-128)*g.Contrast + 128
			// mild edge: boost if local lum jump (sample left)
			if g.Edge > 0 && x > 0 {
				L := 0.299*rf + 0.587*gf + 0.114*bf
				j := i - 3
				Lp := 0.299*float64(f.RGB[j]) + 0.587*float64(f.RGB[j+1]) + 0.114*float64(f.RGB[j+2])
				if d := L - Lp; d > 20 || d < -20 {
					boost := 1 + g.Edge
					rf *= boost
					gf *= boost
					bf *= boost
				}
			}
			f.RGB[i] = clampByte(rf)
			f.RGB[i+1] = clampByte(gf)
			f.RGB[i+2] = clampByte(bf)
		}
	}
}

func clampByte(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// ApplyThemeVisionPlugin sets lab PluginStyle + optional PixelMode from take theme.
// Called from applyGrokTake when take has Theme (vision or orch).
func ApplyThemeVisionPlugin(m *Model, take GrokTake) []string {
	p := ThemeVision()
	if p == nil || !p.Enabled() || take.Theme == "" {
		return nil
	}
	// keep plugin state in sync even if hook order raced
	p.OnVision(VisionEvent{
		Type: "vision-take", Theme: take.Theme, Style: take.Style,
		Caption: take.Caption, Take: take, Feed: p.feed,
	})
	p.mu.RLock()
	autoStyle, autoPixel := p.autoStyle, p.autoPixel
	p.mu.RUnlock()

	var applied []string
	theme := normalizeThemeToken(take.Theme)

	if autoStyle && m != nil {
		if m.lab != nil && m.lab.On {
			m.lab.PluginStyle = themeVisionStyleName
			// stamp active / all news tiles so mosaic uses painter
			for i := range m.lab.Feeds {
				if m.lab.Feeds[i].Kind == "news" || i == m.lab.Active {
					m.lab.Feeds[i].PluginStyle = themeVisionStyleName
				}
			}
			applied = append(applied, "plugin="+themeVisionStyleName)
		}
	}

	if autoPixel && m != nil {
		if mode, ok := ThemeToPixelMode(theme); ok {
			// don't override explicit STYLE line already applied — caller order:
			// we run after style apply if take.Style set; only fill when empty
			if take.Style == "" {
				m.pixelMode = mode
				if m.lab != nil && m.lab.On {
					m.lab.Style = mode
					if m.lab.News != nil && m.lab.News.On {
						for i := range m.lab.Feeds {
							if m.lab.Feeds[i].Kind == "news" {
								m.lab.Feeds[i].TileStyle = mode
							}
						}
					}
				}
				applied = append(applied, "theme·pixel="+mode.String())
			} else if m.lab != nil && m.lab.On {
				// style already set — still attach plugin grade on top via PluginStyle
				applied = append(applied, "theme·grade="+theme)
			}
		}
	}

	if len(applied) > 0 {
		applied = append([]string{"theme-vision="+theme}, applied...)
	}
	return applied
}

// FormatThemeVisionDoctor multi-line for /plugin / vision doctor.
func FormatThemeVisionDoctor() string {
	s := ThemeVision().Snapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "plugin·theme-vision · enabled=%v takes=%d\n", s.Enabled, s.Takes)
	fmt.Fprintf(&b, "  theme     %s · feed %s\n", emptyDash(s.Theme), emptyDash(s.Feed))
	fmt.Fprintf(&b, "  style     %s · pixel_hint %s\n", emptyDash(s.Style), emptyDash(s.PixelHint))
	if s.Caption != "" {
		fmt.Fprintf(&b, "  caption   %s\n", truncate(s.Caption, 56))
	}
	fmt.Fprintf(&b, "  auto      style=%v pixel=%v\n", s.AutoStyle, s.AutoPixel)
	b.WriteString("  paint     theme-vision StylePainter · grade by THEME on vision-take\n")
	b.WriteString("  map       breaking→scan · markets→hex · conflict→neon · weather→dither …\n")
	b.WriteString("  env       GY_VISION_THEME_STYLE · GY_VISION_THEME_PIXEL · /plugin off theme-vision\n")
	return b.String()
}
