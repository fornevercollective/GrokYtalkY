package main

import "testing"

func TestVideoScaleFillsTerminal(t *testing.T) {
	m := NewModel(Options{Nick: "t", Host: "127.0.0.1:9", MIDI: false, Translate: false})
	m.width, m.height = 100, 40
	m.videoOn = true
	m.frame = &FramePixels{W: 80, H: 40, RGB: make([]byte, 80*40*3)}

	sc := m.computeVideoScale(100, 40)
	if !sc.Active {
		t.Fatal("expected active video")
	}
	// should use nearly full width
	if sc.Cols < 90 {
		t.Fatalf("cols too small: %d", sc.Cols)
	}
	// should use substantial height (not 2-row strip)
	if sc.HalfRows < 8 {
		t.Fatalf("half-rows too small for 40-line term: %d", sc.HalfRows)
	}
	// chrome + video + chat + prompt ≤ height
	above, below := m.chromeLines()
	total := above + sc.HalfRows + sc.ChatLines + below
	if m.showPatternLine() {
		// already in chromeLines
	}
	if total > 40 {
		t.Fatalf("overflow: above=%d half=%d chat=%d below=%d total=%d",
			above, sc.HalfRows, sc.ChatLines, below, total)
	}
}

func TestVideoScaleNoVideoGivesChat(t *testing.T) {
	m := NewModel(Options{Nick: "t", Host: "127.0.0.1:9", MIDI: false, Translate: false})
	m.width, m.height = 80, 24
	m.videoOn = false
	sc := m.computeVideoScale(80, 24)
	if sc.Active {
		t.Fatal("no video")
	}
	if sc.ChatLines < 2 {
		t.Fatal("chat")
	}
}

func TestVideoColsTrackScale(t *testing.T) {
	m := NewModel(Options{Nick: "t", Host: "127.0.0.1:9", MIDI: false, Translate: false})
	m.width, m.height = 120, 50
	m.videoOn = true
	m.frame = &FramePixels{W: 10, H: 10, RGB: make([]byte, 300)}
	if m.videoCols() < 100 {
		t.Fatalf("videoCols %d", m.videoCols())
	}
	if m.videoPxH() < 8 {
		t.Fatalf("videoPxH %d", m.videoPxH())
	}
}
