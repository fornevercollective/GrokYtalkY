package main

import (
	"fmt"
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
	cTitle   = lipgloss.ANSIColor(14)
	cDim     = lipgloss.ANSIColor(8)
	cText    = lipgloss.ANSIColor(15)
	cAccent  = lipgloss.ANSIColor(11)
	cLive    = lipgloss.ANSIColor(10)
	cGrok    = lipgloss.ANSIColor(13)
	cErr     = lipgloss.ANSIColor(9)
	cBorder  = lipgloss.ANSIColor(8)
	cKeyBG   = lipgloss.ANSIColor(8)
	cKeyFG   = lipgloss.ANSIColor(15)
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

func panel(title, body string, width int) string {
	// No heavy borders in companion path — borders + ANSI video caused wrap glitches.
	// Keep a simple title line only.
	if width < 20 {
		width = 20
	}
	head := styDim().Render(title)
	if body == "" {
		return clampCells(head, width)
	}
	return clampCells(head, width) + "\n" + clampBlock(body, width, 0)
}

func modeTabs(active PromptMode, width int) string {
	modes := []PromptMode{ModeChat, ModeLive, ModeGrok, ModeWatch}
	var parts []string
	for _, m := range modes {
		label := fmt.Sprintf("%s %s", m.Glyph(), m.String())
		if m == active {
			parts = append(parts, lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.ANSIColor(0)).
				Background(cTitle).
				Padding(0, 1).
				Render(label))
		} else {
			parts = append(parts, styDim().Padding(0, 1).Render(label))
		}
	}
	tabs := strings.Join(parts, styDim().Render("│"))
	help := styDim().Render(" tab modes · ? help · ctrl+c quit")
	line := tabs + "  " + help
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
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
	// strip rough ansi length estimate
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
	g := mode.Glyph()
	var prefix string
	switch mode {
	case ModeGrok:
		prefix = styGrok().Render("✦ grok") + styDim().Render(" › ")
	case ModeLive:
		prefix = styLive().Render("◎ live") + styDim().Render(" › ")
	case ModeWatch:
		prefix = styAccent().Render("▶ watch") + styDim().Render(" › ")
	default:
		prefix = styTitle().Render(nick) + styDim().Render(" › ")
	}
	_ = g
	cursor := styDim().Render("█")
	if thinking {
		cursor = styGrok().Render("…")
	}
	line := prefix + styText().Render(input) + cursor
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

func helpOverlay(width, height int) string {
	body := `GrokYtalkY — Charm / Grok terminal

  tab          cycle chat · live · grok · watch
  enter        send (mode-dependent)
  space        PTT walkie (chat mode, empty line)
  p            play/stop strudel
  c            camera
  m            pixel mode (Half = real video)
  1-7          pattern presets
  ?            this help
  ctrl+c / q   quit

  /watch file.mp4   ffmpeg → terminal pixels + ffplay audio
  /vstop            stop video pipe
  s("bd*4")         live mini-notation (strudel.cc)
  grok mode:        ✦ ask Grok (XAI_API_KEY or local backend)

  env: XAI_API_KEY / GROK_API_KEY · GROK_MODEL · GROK_CLI_URL`
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
			who := styGrok().Render(c.From)
			row = who + styDim().Render(": ") + styAccent().Render(c.Text)
		case strings.HasPrefix(c.From, "grok") || c.From == "grok":
			row = styGrok().Render("✦ grok") + styDim().Render(": ") + styText().Render(c.Text)
		case c.From == nick:
			row = styTitle().Render(c.From) + styDim().Render(": ") + styText().Render(c.Text)
		default:
			row = styAccent().Render(c.From) + styDim().Render(": ") + styText().Render(c.Text)
		}
		// hard wrap rough
		plain := stripANSI(row)
		if len(plain) > width-2 {
			// keep as-is; terminal will wrap
		}
		out = append(out, row)
	}
	for len(out) < height {
		out = append([]string{""}, out...)
	}
	return strings.Join(out, "\n")
}
