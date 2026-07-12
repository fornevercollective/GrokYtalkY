package main

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Visual width helpers — prevent ANSI truecolor half-block rows from wrapping
// (wrap spool was the multi-line glitch on resize).

// safeCols returns a clamp width strictly within the terminal (margin for
// last-column wrap quirks on some emulators).
func safeCols(termW int) int {
	if termW < 1 {
		return 1
	}
	// leave 0 only when tiny; otherwise reserve 1 cell so we never wrap
	if termW <= 2 {
		return termW
	}
	return termW - 1
}

// cellWidth counts terminal cells, skipping CSI / OSC, using runewidth for glyphs.
func cellWidth(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			i++
			if i >= len(s) {
				break
			}
			switch s[i] {
			case '[': // CSI
				i++
				for i < len(s) {
					c := s[i]
					i++
					if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
						break
					}
				}
			case ']': // OSC … BEL or ST
				i++
				for i < len(s) {
					if s[i] == 0x07 { // BEL
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			case '(':
				// charset designate — skip next char
				i++
				if i < len(s) {
					i++
				}
			default:
				// other ESC — skip one
				i++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			size = 1
		}
		i += size
		if r == utf8.RuneError {
			n++
			continue
		}
		w := runewidth.RuneWidth(r)
		if w < 0 {
			w = 0
		}
		n += w
	}
	return n
}

// clampCells truncates s to at most maxCells visible cells, then resets SGR.
func clampCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return "\x1b[0m"
	}
	if cellWidth(s) <= maxCells {
		if !strings.HasSuffix(s, "\x1b[0m") && strings.Contains(s, "\x1b[") {
			return s + "\x1b[0m"
		}
		return s
	}
	var b strings.Builder
	cells := 0
	i := 0
	for i < len(s) && cells < maxCells {
		if s[i] == 0x1b {
			start := i
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) {
					c := s[i]
					i++
					if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
						break
					}
				}
			} else if i < len(s) && s[i] == ']' {
				i++
				for i < len(s) {
					if s[i] == 0x07 {
						i++
						break
					}
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else if i < len(s) {
				i++
			}
			b.WriteString(s[start:i])
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			size = 1
		}
		rw := runewidth.RuneWidth(r)
		if rw < 0 {
			rw = 0
		}
		if rw == 0 {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		if cells+rw > maxCells {
			break
		}
		b.WriteString(s[i : i+size])
		i += size
		cells += rw
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// clampBlock ensures every line ≤ width cells and at most maxLines lines.
func clampBlock(s string, width, maxLines int) string {
	if width < 1 {
		width = 1
	}
	lines := strings.Split(s, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for i := range lines {
		lines[i] = clampCells(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

// fitHalfBlock forces exactly cols × halfRows of half-block lines.
// Each line is exactly `cols` cells — never wider.
func fitHalfBlock(body string, cols, halfRows int) string {
	if cols < 4 {
		cols = 4
	}
	if halfRows < 1 {
		halfRows = 1
	}
	lines := strings.Split(body, "\n")
	out := make([]string, halfRows)
	for i := 0; i < halfRows; i++ {
		if i < len(lines) && strings.TrimSpace(stripANSI(lines[i])) != "" {
			out[i] = clampCells(lines[i], cols)
			base := strings.TrimSuffix(out[i], "\x1b[0m")
			cw := cellWidth(base)
			if cw < cols {
				// pad with blank cells (not extra ▀) so geometry is stable
				base += strings.Repeat(" ", cols-cw)
			}
			out[i] = base + "\x1b[0m"
		} else {
			out[i] = strings.Repeat(" ", cols)
		}
		// hard assert
		if cellWidth(strings.TrimSuffix(out[i], "\x1b[0m")) > cols {
			out[i] = clampCells(out[i], cols)
		}
	}
	return strings.Join(out, "\n")
}

// stableView packs the full frame to exactly height lines of width cells.
// Prevents scrollback spool on resize/redraw.
func stableView(body string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	// never invent a larger canvas than the real terminal
	w := safeCols(width)
	lines := strings.Split(body, "\n")
	// if body accidentally has more lines (lipgloss wrap), hard-cap
	if len(lines) > height {
		lines = lines[:height]
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out[i] = padOrTrim(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

func padOrTrim(s string, width int) string {
	// strip any internal newlines first (lipgloss can inject them)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = clampCells(s, width)
	base := strings.TrimSuffix(s, "\x1b[0m")
	cw := cellWidth(base)
	if cw < width {
		base += strings.Repeat(" ", width-cw)
	}
	if strings.Contains(s, "\x1b[") {
		return base + "\x1b[0m"
	}
	return base
}
