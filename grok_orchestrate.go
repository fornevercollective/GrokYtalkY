package main

import (
	"fmt"
	"strings"
	"time"
)

// Grok orchestration — feed-aware takes applied to style / caption / pattern / glyph.
// Runs on stable media (supervisor healthy) so AI actions don't pile onto dead pipes.

// GrokTake is a parsed set of orchestration actions from a Grok reply.
type GrokTake struct {
	Style    string // pixel style name (neon, hex, scan…)
	Caption  string // on-air / soft chyron
	Pattern  string // strudel mini-notation
	Glyph    string // square | phone-v | 13 | 25 | 37 | 49
	Effect   string // freeform effect line (soft caption source=fx)
	Depth    string // off | zip-lite | gsplat
	Theme    string // breaking|politics|…|earthcam|unsorted (vision/news cluster)
	MuteHint string // none|suggest-mute|quiet|talking
	Note     string // optional operator note (chat only)
	// Media is the FFmpeg control-plane ops (vision-first MEDIA lines).
	Media []VisionMediaAction
	Raw   string // original reply
	// Vision marks take as vision-sourced (metrics / mesh)
	Vision bool
}

// FeedOrchestrateContext is a compact snapshot of what Grok should reason about.
type FeedOrchestrateContext struct {
	Mode      string // chat|lab|news|burst|watch
	Active    string // active feed label
	Kind      string // news|watch|cam|sim
	Style     string
	GlyphN    int
	GlyphAsp  string
	Media     string // media supervisor chrome line
	NewsCount int
	Hint      string
	Live      bool
}

// OrchestrateSystemPrompt tells Grok to emit machine-applicable take lines.
func OrchestrateSystemPrompt() string {
	return `You are Grok inside GrokYtalkY — a live terminal mesh (glyph video wall, Strudel, walkie).
Given the feed context, emit a SHORT "take" using ONLY these lines (omit unused):

STYLE <half|hex|braille|ascii|blocks|scan|neon|dither|poster|edges|points|halftone>
CAPTION <one chyron line, max 80 chars>
PATTERN <strudel mini-notation e.g. s("bd*4, ~ sd") or note("c2 e2")>
GLYPH <square|phone-v|13|25|37|49>
DEPTH <off|zip-lite|gsplat>
EFFECT <max 12 words visual/audio cue>
NOTE <optional operator tip, max 60 chars>

THEME <breaking|politics|conflict|markets|weather|health|science|local|culture|earthcam|unsorted>
MUTE_HINT <none|suggest-mute|quiet|talking>
MEDIA <restart|kill|spawn|retune|retarget|encode|recover> [focus|all|news|watch|label] [crop=x,y,w,h] [scale=WxH|WxH@fps] [source|path]

Rules: no markdown fences unless PATTERN needs them; no preamble; prefer STYLE+CAPTION always when video is live.
For news walls pick STYLE that reads well at small tile size (hex, braille, scan, dither, neon).
THEME/MUTE_HINT optional unless vision frame is attached (then THEME required).
MEDIA drives the FFmpeg control plane (spawn/restart/retune/encode supervised pipes) — use when a tile is dead, needs higher res, or a snapshot is useful.`
}

// BuildOrchestrateUserPrompt packages context for the model.
func BuildOrchestrateUserPrompt(ctx FeedOrchestrateContext) string {
	var b strings.Builder
	b.WriteString("Live take request.\n")
	fmt.Fprintf(&b, "mode=%s active=%s kind=%s style=%s glyph=%d/%s live=%v\n",
		ctx.Mode, ctx.Active, ctx.Kind, ctx.Style, ctx.GlyphN, ctx.GlyphAsp, ctx.Live)
	if ctx.NewsCount > 0 {
		fmt.Fprintf(&b, "news_tiles=%d\n", ctx.NewsCount)
	}
	if ctx.Media != "" {
		fmt.Fprintf(&b, "media=%s\n", ctx.Media)
	}
	if ctx.Hint != "" {
		fmt.Fprintf(&b, "operator_hint: %s\n", ctx.Hint)
	}
	b.WriteString("Emit take lines now.")
	return b.String()
}

// ParseGrokTake extracts orchestration lines from freeform Grok text.
func ParseGrokTake(text string) GrokTake {
	t := GrokTake{Raw: text}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// strip bullets
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "STYLE "):
			t.Style = strings.TrimSpace(line[6:])
		case strings.HasPrefix(up, "CAPTION "):
			t.Caption = strings.TrimSpace(line[8:])
		case strings.HasPrefix(up, "PATTERN "):
			t.Pattern = strings.TrimSpace(line[8:])
		case strings.HasPrefix(up, "GLYPH "):
			t.Glyph = strings.TrimSpace(line[6:])
		case strings.HasPrefix(up, "DEPTH "):
			t.Depth = strings.TrimSpace(line[6:])
		case strings.HasPrefix(up, "EFFECT "):
			t.Effect = strings.TrimSpace(line[7:])
		case strings.HasPrefix(up, "NOTE "):
			t.Note = strings.TrimSpace(line[5:])
		case strings.HasPrefix(up, "THEME "):
			t.Theme = normalizeThemeToken(strings.TrimSpace(line[6:]))
		case strings.HasPrefix(up, "MUTE_HINT "):
			t.MuteHint = normalizeMuteHint(strings.TrimSpace(line[len("MUTE_HINT "):]))
		case strings.HasPrefix(up, "MUTE ") && !strings.HasPrefix(up, "MUTE_"):
			t.MuteHint = normalizeMuteHint(strings.TrimSpace(line[5:]))
		case strings.HasPrefix(up, "MEDIA ") || up == "MEDIA":
			if a, ok := ParseMediaLine(line); ok {
				t.Media = append(t.Media, a)
			}
		case strings.HasPrefix(up, "CAMERA ") || up == "CAMERA" || strings.HasPrefix(up, "LOOK "):
			if look, _, ok := ParseCameraLookLine(line); ok {
				// stash as effect note for apply path via Camera bus
				Camera().SetLook(look)
				if t.Note == "" {
					t.Note = look.LookSummary()
				}
			}
		default:
			// fenced pattern fallback
			if p := extractPattern(line); p != "" && t.Pattern == "" {
				t.Pattern = p
			}
		}
	}
	// whole-text pattern if still empty
	if t.Pattern == "" {
		t.Pattern = extractPattern(text)
	}
	// single-line caption fallback if only prose
	if t.Caption == "" && t.Style == "" && t.Pattern == "" && t.Effect == "" {
		one := strings.TrimSpace(strings.Split(text, "\n")[0])
		one = strings.Trim(one, `"'`)
		if len(one) > 0 && len(one) <= 80 && !strings.Contains(strings.ToLower(one), "style ") {
			t.Caption = one
		}
	}
	return t
}

// ParsePixelStyleName maps Grok STYLE token → PixelMode.
func ParsePixelStyleName(s string) (PixelMode, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	// GrokGlyph aliases
	switch s {
	case "matrix", "half", "truecolor":
		return PixelHalf, true
	case "hex", "hexlum":
		return PixelHex, true
	case "braille":
		return PixelBraille, true
	case "ascii", "shade":
		return PixelASCII, true
	case "blocks", "block":
		return PixelBlocks, true
	case "points", "dots":
		return PixelPoints, true
	case "halftone":
		return PixelHalftone, true
	case "depth":
		return PixelDepth, true
	case "gsplat":
		return PixelGsplat, true
	case "edges", "edge":
		return PixelEdges, true
	case "poster":
		return PixelPoster, true
	case "scan", "scanline", "scanlines":
		return PixelScan, true
	case "dither", "bayer":
		return PixelDither, true
	case "neon", "bloom":
		return PixelNeon, true
	}
	// exact mode name
	for i := PixelMode(0); i < PixelCount; i++ {
		if i.String() == s {
			return i, true
		}
	}
	return 0, false
}

// AskGrokOrchestrate runs a non-stream take request.
func AskGrokOrchestrate(cfg GrokConfig, ctx FeedOrchestrateContext) (GrokTake, error) {
	cfg.System = OrchestrateSystemPrompt()
	user := BuildOrchestrateUserPrompt(ctx)
	reply, err := AskGrok(cfg, nil, user)
	if err != nil {
		return GrokTake{}, err
	}
	return ParseGrokTake(reply), nil
}

// TakeSummary one-line for status bar.
func (t GrokTake) TakeSummary() string {
	var parts []string
	if t.Vision {
		parts = append(parts, "vision")
	}
	if t.Style != "" {
		parts = append(parts, "style="+t.Style)
	}
	if t.Caption != "" {
		parts = append(parts, "cap")
	}
	if t.Theme != "" {
		parts = append(parts, "theme="+t.Theme)
	}
	if t.MuteHint != "" && t.MuteHint != "none" {
		parts = append(parts, "mute="+t.MuteHint)
	}
	if t.Pattern != "" {
		parts = append(parts, "pat")
	}
	if t.Glyph != "" {
		parts = append(parts, "glyph="+t.Glyph)
	}
	if t.Effect != "" {
		parts = append(parts, "fx")
	}
	if len(t.Media) > 0 {
		parts = append(parts, fmt.Sprintf("media×%d", len(t.Media)))
	}
	if len(parts) == 0 {
		return "take (empty)"
	}
	return "take " + strings.Join(parts, " · ")
}

var knownThemes = map[string]bool{
	"breaking": true, "politics": true, "conflict": true, "markets": true,
	"weather": true, "health": true, "science": true, "local": true,
	"culture": true, "earthcam": true, "unsorted": true, "scenic": true,
}

func normalizeThemeToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".,;:")
	if s == "scenic" || s == "cam" || s == "landmark" {
		return "earthcam"
	}
	if knownThemes[s] {
		return s
	}
	// first token only
	if i := strings.IndexAny(s, " \t/"); i > 0 {
		s = s[:i]
	}
	if knownThemes[s] {
		return s
	}
	return "unsorted"
}

func normalizeMuteHint(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "none", "off", "no", "0":
		return "none"
	case "suggest-mute", "suggest", "mute", "muted":
		return "suggest-mute"
	case "quiet", "silent", "still":
		return "quiet"
	case "talking", "speak", "active", "hot":
		return "talking"
	default:
		return s
	}
}

// MediaHealthyEnough gates orchestration so we don't burn API on dead pipes.
func MediaHealthyEnough() bool {
	h := Media().Health()
	// allow if no media yet (chat-only orch) OR something alive OR drops not extreme
	if h.Alive > 0 {
		return true
	}
	// no pipes — still ok for pattern/style-only takes
	return h.Total == 0
}

// OrchAutoGap minimum time between auto orchestrate passes.
func OrchAutoGap() time.Duration {
	return 20 * time.Second
}
