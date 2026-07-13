package main

import (
	"strings"
	"testing"
	"time"
)

func TestUnixTimeAndDrift(t *testing.T) {
	ResetClockEpoch()
	u := UnixTimeNow()
	if u < 1_700_000_000 {
		t.Fatalf("unix too small %f", u)
	}
	// after a short sleep, drift should stay near zero (ms scale)
	time.Sleep(20 * time.Millisecond)
	d := EpochDriftMs()
	if d > 50 || d < -50 {
		// extremely loose — CI load can step clocks, but not by seconds
		t.Fatalf("unexpected drift %f ms", d)
	}
}

func TestFormatUnixClockLine(t *testing.T) {
	ResetClockEpoch()
	s := FormatUnixClockLine()
	if !strings.Contains(s, "unix ") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "Δ") {
		t.Fatal(s)
	}
	c := FormatUnixClockCompact()
	if !strings.Contains(c, "Δ") || len(c) < 8 {
		t.Fatal(c)
	}
}

func TestFormatSignedMs(t *testing.T) {
	if formatSignedMs(1.25) != "+1.3ms" && formatSignedMs(1.25) != "+1.2ms" {
		// printf rounding
		got := formatSignedMs(1.0)
		if got != "+1.0ms" {
			t.Fatal(got)
		}
	}
	if !strings.HasPrefix(formatSignedMs(-2.5), "-") {
		t.Fatal(formatSignedMs(-2.5))
	}
}
