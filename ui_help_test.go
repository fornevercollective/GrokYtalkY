package main

import (
	"strings"
	"testing"
)

func TestPromptModesAndKeys(t *testing.T) {
	if ModeCount != 7 {
		t.Fatalf("modes %d", ModeCount)
	}
	for _, m := range AllPromptModes() {
		if m.ModeFastKey() == "" || m.String() == "?" {
			t.Fatalf("%v", m)
		}
	}
	if mode, ok := ModeFromFastKey("5"); !ok || mode != ModeLab {
		t.Fatal("5 lab")
	}
	if mode, ok := ModeFromFastKey("7"); !ok || mode != ModePhone {
		t.Fatal("7 phone")
	}
	w, h := GlyphVertSize(25, GlyphAspectPhoneV)
	if w != 25 || h != 50 {
		t.Fatalf("phone-v %dx%d", w, h)
	}
	w, h = GlyphVertSize(13, GlyphAspectPhoneV)
	if w != 13 || h != 26 {
		t.Fatalf("4a-v %dx%d", w, h)
	}
	s := FormatModeHelp()
	if !strings.Contains(s, "lab") || !strings.Contains(s, "phone") {
		t.Fatal(s)
	}
}

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
