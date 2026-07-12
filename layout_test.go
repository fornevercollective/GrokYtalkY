package main

import (
	"strings"
	"testing"
)

func TestClampAndStable(t *testing.T) {
	line := "\x1b[38;2;255;0;0m\x1b[48;2;0;0;0m" + strings.Repeat("▀", 100) + "\x1b[0m"
	out := clampCells(line, 40)
	base := strings.TrimSuffix(out, "\x1b[0m")
	if cellWidth(base) > 40 {
		t.Fatalf("width %d", cellWidth(base))
	}
	block := line + "\n" + line + "\n" + line
	fit := fitHalfBlock(block, 20, 2)
	lines := strings.Split(fit, "\n")
	if len(lines) != 2 {
		t.Fatalf("rows %d", len(lines))
	}
	for _, ln := range lines {
		b := strings.TrimSuffix(ln, "\x1b[0m")
		if cellWidth(b) > 20 {
			t.Fatalf("line too wide %d", cellWidth(b))
		}
	}
	view := stableView(strings.Repeat("hello\n", 50), 30, 12)
	n := strings.Count(view, "\n") + 1
	if n != 12 {
		t.Fatalf("lines %d want 12", n)
	}
}
