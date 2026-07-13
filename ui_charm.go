package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// PromptMode is the Grok-style input / view mode (TAB strip).
type PromptMode int

const (
	ModeChat  PromptMode = iota // mesh chat / walkie
	ModeLive                    // strudel eval
	ModeGrok                    // Grok prompt
	ModeWatch                   // path paste → video
	ModeLab                     // multi-feed video lab
	ModeBurst                   // dual Glyph Matrix walkie
	ModePhone                   // same-WiFi phone cast focus
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
	case ModeLab:
		return "lab"
	case ModeBurst:
		return "burst"
	case ModePhone:
		return "phone"
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
	case ModeLab:
		return "▦"
	case ModeBurst:
		return "◉"
	case ModePhone:
		return "▣"
	default:
		return ">"
	}
}

// ModeFastKey is the digit (and optional letter) for empty-input jump.
func (m PromptMode) ModeFastKey() string {
	switch m {
	case ModeChat:
		return "1"
	case ModeLive:
		return "2"
	case ModeGrok:
		return "3"
	case ModeWatch:
		return "4"
	case ModeLab:
		return "5" // also V
	case ModeBurst:
		return "6" // also b
	case ModePhone:
		return "7" // also /lan
	default:
		return ""
	}
}

// ModeFastKeyAlt secondary letter key shown in help (empty-input).
func (m PromptMode) ModeFastKeyAlt() string {
	switch m {
	case ModeLab:
		return "V"
	case ModeBurst:
		return "b"
	case ModePhone:
		return "P"
	case ModeGrok:
		return "g"
	default:
		return ""
	}
}

// AllPromptModes ordered for the TAB strip.
func AllPromptModes() []PromptMode {
	return []PromptMode{
		ModeChat, ModeLive, ModeGrok, ModeWatch,
		ModeLab, ModeBurst, ModePhone,
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

// modePills — compact tab strip.
func modePills(active PromptMode) string {
	modes := AllPromptModes()
	var parts []string
	for _, m := range modes {
		if m == active {
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
	modes := AllPromptModes()
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

// modeKeyHintsLine — fast keys aligned under tab names (shown under brand row when wide enough).
// Example:  1     2     3     4     5/V   6/b   7/P
func modeKeyHintsLine(width int) string {
	if width < 48 {
		// compact: single line of digits
		return styDim().Render("tab  1 chat · 2 live · 3 grok · 4 watch · 5 lab · 6 burst · 7 phone")
	}
	// mirror pill order with keys under
	var keys []string
	for _, m := range AllPromptModes() {
		k := m.ModeFastKey()
		if alt := m.ModeFastKeyAlt(); alt != "" {
			k = k + "/" + alt
		}
		// pad roughly to mode name width for visual under-align
		name := m.String()
		pad := len(name) - len(k)
		if pad < 0 {
			pad = 0
		}
		// center key under name
		left := pad / 2
		right := pad - left
		cell := strings.Repeat(" ", left) + k + strings.Repeat(" ", right)
		keys = append(keys, cell)
	}
	line := styDim().Render(strings.Join(keys, " · "))
	// prefix to roughly sit under mode pills (after "◈ gy ●  ")
	prefix := styDim().Render("       ")
	out := prefix + line
	if cellWidth(stripANSI(out)) > width {
		return clampCells(styDim().Render("1·2·3·4·5·6·7  tab cycle · empty-input"), width)
	}
	return clampCells(out, width)
}

// modeTabs kept for callers.
func modeTabs(active PromptMode, width int) string {
	return clampCells(modePills(active), width)
}

// ModeFromFastKey maps digit / letter to a prompt mode (empty-input).
func ModeFromFastKey(k string) (PromptMode, bool) {
	switch k {
	case "1":
		return ModeChat, true
	case "2":
		return ModeLive, true
	case "3":
		return ModeGrok, true
	case "4":
		return ModeWatch, true
	case "5", "V":
		return ModeLab, true
	case "6", "b":
		return ModeBurst, true
	case "7", "P":
		return ModePhone, true
	default:
		return 0, false
	}
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
	case ModeLab:
		prefix = styAccent().Render("▦") + styDim().Render(" lab › ")
	case ModeBurst:
		prefix = styLive().Render("◉") + styDim().Render(" burst › ")
	case ModePhone:
		prefix = styTitle().Render("▣") + styDim().Render(" phone › ")
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
	return clampCells(line, width)
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
	for len(out) < height {
		out = append([]string{""}, out...)
	}
	return strings.Join(out, "\n")
}

// FormatModeHelp one-liner for help / status.
func FormatModeHelp() string {
	var b strings.Builder
	b.WriteString("tabs  ")
	for i, m := range AllPromptModes() {
		if i > 0 {
			b.WriteString(" · ")
		}
		fmt.Fprintf(&b, "%s=%s", m.ModeFastKey(), m.String())
		if alt := m.ModeFastKeyAlt(); alt != "" {
			fmt.Fprintf(&b, "/%s", alt)
		}
	}
	b.WriteString("  (tab cycle)")
	return b.String()
}
