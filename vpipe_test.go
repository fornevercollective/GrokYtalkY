package main

import (
	"testing"
	"time"
)

func TestFormatClock(t *testing.T) {
	if formatClock(65*time.Second) != "1:05" {
		t.Fatal(formatClock(65 * time.Second))
	}
	if formatClock(3661*time.Second) != "1:01:01" {
		t.Fatal(formatClock(3661 * time.Second))
	}
}

func TestFormatFFtime(t *testing.T) {
	s := formatFFtime(90*time.Second + 500*time.Millisecond)
	if s != "90.500" {
		t.Fatal(s)
	}
}

func TestVideoPipeStatusPaused(t *testing.T) {
	vp := &VideoPipe{
		running:   false,
		paused:    true,
		baseSeek:  12 * time.Second,
		duration:  120 * time.Second,
		rate:      1,
		playStart: time.Now(),
		frame:     []byte{1, 2, 3},
		seq:       1,
	}
	if !vp.Paused() {
		t.Fatal("paused")
	}
	st := vp.StatusLine()
	if st == "" || st[0:1] != "⏸" && !containsRune(st, '⏸') {
		// may be multi-byte
		if !containsStr(st, "⏸") && !containsStr(st, "0:12") {
			t.Fatal(st)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
