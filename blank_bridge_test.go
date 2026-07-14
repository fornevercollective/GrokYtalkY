package main

import (
	"strings"
	"testing"
)

func TestBlankBaseURL(t *testing.T) {
	t.Setenv("GY_BLANK", "0")
	if BlankBaseURL() != "" {
		t.Fatal("disabled")
	}
	t.Setenv("GY_BLANK", "")
	t.Setenv("GY_BLANK_URL", "http://example:5173/")
	if BlankBaseURL() != "http://example:5173" {
		t.Fatal(BlankBaseURL())
	}
}

func TestFormatBlankDoctor(t *testing.T) {
	s := FormatBlankDoctor()
	if !strings.Contains(s, "blank") || !strings.Contains(s, "tiktok") {
		t.Fatal(s)
	}
}

func TestBlankRoot(t *testing.T) {
	p := BlankRoot()
	if p == "" {
		t.Fatal("empty root")
	}
}

func TestBlankURLForPage(t *testing.T) {
	// without blank server, should be false even for tiktok
	t.Setenv("GY_BLANK", "0")
	if blankURLForPage("https://www.tiktok.com/@x/live") {
		t.Fatal("disabled still true")
	}
}
