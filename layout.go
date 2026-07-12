package main

import (
	"strings"
	"unicode/utf8"
)

// Visual width helpers — prevent ANSI truecolor half-block rows from wrapping
// (wrap spool was the multi-line glitch on resize).

// cellWidth counts terminal cells, skipping CSI sequences. CJK not needed for ▀.
func cellWidth(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			// CSI ... letter
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
				continue
			}
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			size = 1
		}
		i += size
		n++
	}
	return n
}

// clampCells truncates s to at most maxCells visible cells, then resets SGR.
func clampCells(s string, maxCells int) string {
	if maxCells <= 0 {
		return ""
	}
	if cellWidth(s) <= maxCells {
		// still ensure reset so colors don't bleed
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
			}
			b.WriteString(s[start:i])
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		if size <= 0 {
			size = 1
		}
		b.WriteString(s[i : i+size])
		i += size
		cells++
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

// fitFrame forces exactly cols × rows of half-block lines (rows = pixelH/2).
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
		if i < len(lines) {
			out[i] = clampCells(lines[i], cols)
			// pad short lines with spaces (no color) so panel stays stable
			cw := cellWidth(out[i])
			// strip trailing reset for padding
			base := strings.TrimSuffix(out[i], "\x1b[0m")
			if cw < cols {
				base += strings.Repeat(" ", cols-cw)
			}
			out[i] = base + "\x1b[0m"
		} else {
			out[i] = strings.Repeat(" ", cols)
		}
	}
	return strings.Join(out, "\n")
}

// stableView packs the full frame to exactly height lines of width cells.
// Prevents scrollback spool on resize/redraw.
func stableView(body string, width, height int) string {
	if width < 20 {
		width = 20
	}
	if height < 8 {
		height = 8
	}
	lines := strings.Split(body, "\n")
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out[i] = padOrTrim(lines[i], width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(out, "\n")
}

func padOrTrim(s string, width int) string {
	s = clampCells(s, width)
	// remove reset to measure, re-add
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
