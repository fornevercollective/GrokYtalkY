package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Grok overlay lane — lightweight caption / effect / prompt on active glyph streams.
// Throttled so mid-lane / TUI stay inside performance budgets.

// OverlayMode selects Grok system prompt + how the reply is applied.
type OverlayMode string

const (
	OverlayCaption OverlayMode = "caption" // soft or program caption
	OverlayEffect  OverlayMode = "effect"  // effect prompt line (soft caption)
	OverlayPrompt  OverlayMode = "prompt"  // free jam assistant (chat only)
)

// GrokOverlayState holds throttle + last results for the TUI overlay strip.
type GrokOverlayState struct {
	mu       sync.Mutex
	Mode     OverlayMode
	Auto     bool // auto-caption on stream events (throttled)
	LastText string
	LastAt   time.Time
	LastFrom string // stream peer that triggered
	Busy     bool
	// min gap between auto calls
	MinGap time.Duration
}

func newGrokOverlayState() *GrokOverlayState {
	return &GrokOverlayState{
		Mode:   OverlayCaption,
		MinGap: 8 * time.Second,
	}
}

// CanAuto returns true if enough time has passed for another auto Grok call.
func (s *GrokOverlayState) CanAuto() bool {
	if s == nil || !s.Auto {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Busy {
		return false
	}
	gap := s.MinGap
	if gap < time.Second {
		gap = 8 * time.Second
	}
	return time.Since(s.LastAt) >= gap
}

// MarkBusy sets in-flight flag.
func (s *GrokOverlayState) MarkBusy(v bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.Busy = v
	s.mu.Unlock()
}

// Record stores the last overlay text.
func (s *GrokOverlayState) Record(text, from string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.LastText = strings.TrimSpace(text)
	s.LastFrom = from
	s.LastAt = time.Now()
	s.Busy = false
	s.mu.Unlock()
}

// StatusLine short chrome crumb.
func (s *GrokOverlayState) StatusLine() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.LastText == "" {
		if s.Auto {
			return "overlay auto·" + string(s.Mode)
		}
		return ""
	}
	t := s.LastText
	if len(t) > 48 {
		t = t[:45] + "…"
	}
	return "✦ " + t
}

// OverlaySystemPrompt is the specialized Grok system string for overlay modes.
func OverlaySystemPrompt(mode OverlayMode) string {
	switch mode {
	case OverlayEffect:
		return `You are Grok inside GrokYtalkY glyph streams. Reply with ONE short terminal effect prompt
(max 12 words) that could drive pixel style, depth, or Strudel texture. No quotes, no markdown.
Examples: neon edges on kick · poster freeze · scanline rain · hexlum bloom`
	case OverlayPrompt:
		return `You are Grok in a live terminal jam dock (hex video + walkie + Strudel).
Be concise (1–2 sentences). Prefer actionable gy commands: /watch, /social, s("bd*4"), m style, b burst.`
	default: // caption
		return `You write live terminal captions for a glyph/hex video mesh (GrokYtalkY / GrokGlyph).
Reply with ONE caption line only (max 80 chars). No quotes, no markdown, no preamble.
Style: tight, present-tense, broadcast chyron.`
	}
}

// BuildOverlayUserPrompt builds the user message from stream context.
func BuildOverlayUserPrompt(mode OverlayMode, hint, peer, kind string, w, h int) string {
	hint = strings.TrimSpace(hint)
	ctx := fmt.Sprintf("stream peer=%s kind=%s %dx%d", peer, kind, w, h)
	if hint != "" {
		return hint + "\n(" + ctx + ")"
	}
	switch mode {
	case OverlayEffect:
		return "Suggest one visual/audio effect for this live glyph stream. " + ctx
	case OverlayPrompt:
		return "What should the operator try next on this live mesh? " + ctx
	default:
		return "Caption this live glyph stream for the terminal chyron. " + ctx
	}
}

// AskGrokOverlay runs a throttled non-stream Grok call for overlay text.
func AskGrokOverlay(cfg GrokConfig, mode OverlayMode, user string) (string, error) {
	cfg.System = OverlaySystemPrompt(mode)
	// keep history empty for overlay — cheap and non-sticky
	return AskGrok(cfg, nil, user)
}

// NormalizeOverlayMode parses user mode string.
func NormalizeOverlayMode(s string) OverlayMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fx", "effect", "effects":
		return OverlayEffect
	case "prompt", "ask", "jam":
		return OverlayPrompt
	default:
		return OverlayCaption
	}
}

// OverlayReplyToCaption turns a Grok reply into a CaptionPayload for program/soft bus.
func OverlayReplyToCaption(text, speaker string) CaptionPayload {
	text = strings.TrimSpace(text)
	// single line
	if i := strings.IndexAny(text, "\n\r"); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	if len(text) > 120 {
		text = text[:117] + "…"
	}
	if speaker == "" {
		speaker = "grok"
	}
	return CaptionPayload{
		Text:    text,
		Lang:    "en",
		Role:    "lower",
		Speaker: speaker,
		Source:  "grok-overlay",
	}
}
