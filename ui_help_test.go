package main

import (
	"strings"
	"testing"
)

func TestHelpPageTitles(t *testing.T) {
	if HelpPageCount != 6 {
		t.Fatal(HelpPageCount)
	}
	for i := 0; i < HelpPageCount; i++ {
		if HelpPageTitle(i) == "?" {
			t.Fatal(i)
		}
	}
}

func TestHelpOverlayPages(t *testing.T) {
	for i := 0; i < HelpPageCount; i++ {
		out := helpOverlay(72, 24, i)
		if out == "" || !strings.Contains(stripANSI(out), HelpPageTitle(i)) {
			t.Fatalf("page %d empty or missing title", i)
		}
	}
	// venue page mentions 2110
	v := stripANSI(helpOverlay(72, 28, HelpPageVenue))
	for _, want := range []string{"2110", "venue", "2022-7", "doctor"} {
		if !strings.Contains(v, want) {
			t.Fatalf("venue page missing %q", want)
		}
	}
	f := stripANSI(helpOverlay(72, 28, HelpPageForge))
	for _, want := range []string{"/forge", "/conductor", "/take", "cgf"} {
		if !strings.Contains(f, want) {
			t.Fatalf("forge page missing %q", want)
		}
	}
	d := stripANSI(helpOverlay(72, 28, HelpPageDocs))
	if !strings.Contains(d, "fornevercollective.github.io") {
		t.Fatal(d)
	}
}

func TestHelpTabCyclesModel(t *testing.T) {
	m := NewModel(Options{Nick: "h", Host: "127.0.0.1:0"})
	m.showHelp = true
	m.helpPage = 0
	// simulate tab
	m.helpPage = (m.helpPage + 1) % HelpPageCount
	if m.helpPage != 1 {
		t.Fatal(m.helpPage)
	}
}
