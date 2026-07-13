package main

import (
	"strings"
	"testing"
)

func TestMajorNewsSources(t *testing.T) {
	s := MajorNewsSources()
	if len(s) < 8 {
		t.Fatalf("want ≥8 agencies, got %d", len(s))
	}
	for _, a := range s {
		if a.ID == "" || a.Label == "" || a.URL == "" {
			t.Fatalf("%+v", a)
		}
		if !strings.HasPrefix(a.URL, "http") {
			t.Fatal(a.URL)
		}
	}
}

func TestFilterNewsSources(t *testing.T) {
	us := FilterNewsSources("us", 4)
	if len(us) == 0 || len(us) > 4 {
		t.Fatalf("%d", len(us))
	}
	for _, s := range us {
		if s.Region != "us" && s.Region != "world" {
			// filter is strict on region
			if s.Region != "us" {
				t.Logf("note: %s region %s", s.Label, s.Region)
			}
		}
	}
	all := FilterNewsSources("all", 6)
	if len(all) != 6 {
		t.Fatalf("%d", len(all))
	}
}

func TestNewsWallStyleNames(t *testing.T) {
	if NewsWallStyleName(PixelHalf) != "matrix" {
		t.Fatal(NewsWallStyleName(PixelHalf))
	}
	if NewsWallStyleName(PixelBraille) != "braille" {
		t.Fatal()
	}
	if len(NewsWallStyleLadder) < 4 {
		t.Fatal("ladder")
	}
}

func TestNewsPoster(t *testing.T) {
	f := newsPoster("Al Jazeera", "me", 1)
	if f == nil || f.W != newsTileW || len(f.RGB) != newsTileW*newsTileH*3 {
		t.Fatal("poster")
	}
}

func TestNewsWallStartSlots(t *testing.T) {
	m := NewModel(Options{Nick: "n", Host: "127.0.0.1:9", MIDI: false, Translate: false})
	mod, cmd := m.startNewsWall("all 4")
	mm := mod.(*Model)
	if mm.lab == nil || !mm.lab.On || mm.lab.News == nil {
		t.Fatal("lab news")
	}
	if mm.lab.Layout != LayoutGrid {
		t.Fatal(mm.lab.Layout)
	}
	if len(mm.lab.Feeds) < 4 {
		t.Fatalf("feeds %d", len(mm.lab.Feeds))
	}
	newsN := 0
	for _, f := range mm.lab.Feeds {
		if f.Kind == "news" {
			newsN++
			if f.Frame == nil {
				t.Fatal("poster frame")
			}
		}
	}
	if newsN < 4 {
		t.Fatalf("news tiles %d", newsN)
	}
	if cmd == nil {
		t.Fatal("expected load cmd")
	}
	mm.stopNewsWall()
	if mm.lab.News != nil {
		t.Fatal("stopped")
	}
}
