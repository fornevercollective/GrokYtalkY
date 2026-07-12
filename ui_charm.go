package main

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// PromptMode is the Grok-style input mode (like modality bar).
type PromptMode int

const (
	ModeChat PromptMode = iota // mesh chat / walkie
	ModeLive                   // strudel eval
	ModeGrok                   // Grok prompt
	ModeWatch                  // path paste → video
	ModeCount
)

func (m PromptMode) String() string {
	switch m {
	case ModeChat:
		return "chat"
	case ModeLive:
		return "live"
	case ModeGrok:
		return "grok"
	case ModeWatch:
		return "watch"
	default:
		return "?"
	}
}

func (m PromptMode) Glyph() string {
	switch m {
	case ModeChat:
		return "›"
	case ModeLive:
		return "◎"
	case ModeGrok:
		return "✦"
	case ModeWatch:
		return "▶"
	default:
		return ">"
	}
}

// Charm palette — cliamp-adjacent ANSI that adapts to terminal themes.
var (
	cTitle  = lipgloss.ANSIColor(14)
	cDim    = lipgloss.ANSIColor(8)
	cText   = lipgloss.ANSIColor(15)
	cAccent = lipgloss.ANSIColor(11)
	cLive   = lipgloss.ANSIColor(10)
	cGrok   = lipgloss.ANSIColor(13)
	cErr    = lipgloss.ANSIColor(9)
	cKeyBG  = lipgloss.ANSIColor(8)
	cKeyFG  = lipgloss.ANSIColor(15)
)

func styTitle() lipgloss.Style  { return lipgloss.NewStyle().Bold(true).Foreground(cTitle) }
func styDim() lipgloss.Style    { return lipgloss.NewStyle().Foreground(cDim) }
func styText() lipgloss.Style   { return lipgloss.NewStyle().Foreground(cText) }
func styAccent() lipgloss.Style { return lipgloss.NewStyle().Foreground(cAccent) }
func styLive() lipgloss.Style   { return lipgloss.NewStyle().Bold(true).Foreground(cLive) }
func styGrok() lipgloss.Style   { return lipgloss.NewStyle().Bold(true).Foreground(cGrok) }
func styErr() lipgloss.Style    { return lipgloss.NewStyle().Foreground(cErr) }
func styKey() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(cKeyFG).Background(cKeyBG).Padding(0, 1)
}

// modePills — compact tab strip (no help text, no lipgloss Width wrap).
func modePills(active PromptMode) string {
	modes := []PromptMode{ModeChat, ModeLive, ModeGrok, ModeWatch}
	var parts []string
	for _, m := range modes {
		if m == active {
			// no Padding — padding + background can mis-measure on narrow ttys
			parts = append(parts, lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.ANSIColor(0)).
				Background(cTitle).
				Render(" "+m.String()+" "))
		} else {
			parts = append(parts, styDim().Render(m.String()))
		}
	}
	return strings.Join(parts, styDim().Render(" · "))
}

// modePillsCompact — glyph-only for narrow terminals.
func modePillsCompact(active PromptMode) string {
	modes := []PromptMode{ModeChat, ModeLive, ModeGrok, ModeWatch}
	var parts []string
	for _, m := range modes {
		g := m.Glyph()
		if m == active {
			parts = append(parts, lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.ANSIColor(0)).
				Background(cTitle).
				Render(g))
		} else {
			parts = append(parts, styDim().Render(g))
		}
	}
	return strings.Join(parts, styDim().Render(" "))
}

// modeTabs kept for callers.
func modeTabs(active PromptMode, width int) string {
	return clampCells(modePills(active), width)
}

func panel(title, body string, width int) string {
	if width < 20 {
		width = 20
	}
	head := styDim().Render(title)
	if body == "" {
		return clampCells(head, width)
	}
	return clampCells(head, width) + "\n" + clampBlock(body, width, 0)
}

func statusBar(items []string, width int) string {
	left := strings.Join(items, styDim().Render(" · "))
	return lipgloss.NewStyle().
		Width(width).
		Background(lipgloss.ANSIColor(0)).
		Foreground(cDim).
		Render(padRight(left, width))
}

func padRight(s string, w int) string {
	plain := stripANSI(s)
	if len(plain) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(plain))
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			in = true
			continue
		}
		if in {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				in = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func promptLine(mode PromptMode, nick, input string, thinking bool, width int) string {
	var prefix string
	switch mode {
	case ModeGrok:
		prefix = styGrok().Render("✦") + styDim().Render(" grok › ")
	case ModeLive:
		prefix = styLive().Render("◎") + styDim().Render(" live › ")
	case ModeWatch:
		prefix = styAccent().Render("▶") + styDim().Render(" watch › ")
	default:
		prefix = styTitle().Render(truncate(nick, 12)) + styDim().Render(" › ")
	}
	cursor := styDim().Render("█")
	if thinking {
		cursor = styGrok().Render("…")
	}
	// room left for input after prefix
	pw := cellWidth(stripANSI(prefix)) + 1
	room := width - pw
	if room < 4 {
		room = 4
	}
	in := input
	if len([]rune(in)) > room-1 {
		r := []rune(in)
		in = string(r[len(r)-(room-1):])
	}
	line := prefix + styText().Render(in) + cursor
	// no lipgloss Width/MaxWidth — those can wrap into extra lines
	return clampCells(line, width)
}

func helpOverlay(width, height int) string {
	body := `keys
  tab        chat · live · grok · watch
  enter      send / eval / watch path
  space      PTT (chat, empty line)
  p          pattern play/stop
  c          camera strip
  m          pixel style
  1-7        pattern presets
  F          full ↔ companion
  ?          help ·  q / ctrl+c quit

  /watch url|file   ffmpeg (auto yt-dlp for YT/…)
  /resolve url      show resolved stream URLs
  /depth [off|lite|zipdepth|gsplat]
  d                 cycle live depth modes
  V / gy lab        multi-feed lab next to chat
  [ ]  scale        , .  fps        L  layout
  m    style        a  +sim         o  list
  /vstop            stop pipe
  /doctor           ffmpeg · yt-dlp · zipdepth
  s("bd*4")         live mini-notation

  styles: half hex braille ascii blocks points
          halftone depth gsplat
  layouts: side | stack | grid | focus

  depth: ZipDepth sidecar :8766 (zipdepth.github.io)

env  XAI_API_KEY · GROK_MODEL · ZIPDEPTH_URL`
	return panel("help", styText().Render(body), width)
}

func spinnerFrame(n int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[n%len(frames)]
}

func renderChatViewport(lines []chatLine, nick string, height, width int) string {
	if height < 1 {
		height = 1
	}
	// take last N real lines (skip nothing — sys already filtered at source)
	start := 0
	if len(lines) > height {
		start = len(lines) - height
	}
	var out []string
	for _, c := range lines[start:] {
		var row string
		switch {
		case c.Sys:
			row = styDim().Render("· " + c.Text)
		case c.XL8:
			row = styGrok().Render(c.From) + styDim().Render(": ") + styAccent().Render(c.Text)
		case strings.HasPrefix(c.From, "grok") || c.From == "grok":
			row = styGrok().Render("✦") + styDim().Render(" ") + styText().Render(c.Text)
		case c.From == nick:
			row = styTitle().Render(c.From) + styDim().Render(": ") + styText().Render(c.Text)
		default:
			row = styAccent().Render(c.From) + styDim().Render(": ") + styText().Render(c.Text)
		}
		out = append(out, clampCells(row, width))
	}
	// bottom-align: pad empty lines above
	for len(out) < height {
		out = append([]string{""}, out...)
	}
	return strings.Join(out, "\n")
}
