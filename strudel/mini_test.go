package strudel

import "testing"

func TestParseDrums(t *testing.T) {
	p, err := Parse(`s("bd sd hh cp")`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Layers) != 1 {
		t.Fatalf("layers %d", len(p.Layers))
	}
	if len(p.Layers[0].Events) != 4 {
		t.Fatalf("events %d", len(p.Layers[0].Events))
	}
	if p.Layers[0].Events[0].MIDI != 36 {
		t.Fatalf("bd midi %d", p.Layers[0].Events[0].MIDI)
	}
}

func TestParseStack(t *testing.T) {
	p, err := Parse(`stack(s("bd*4"), note("c2 e2 g2"))`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Layers) < 2 {
		t.Fatalf("want 2 layers got %d", len(p.Layers))
	}
}

func TestParseCPS(t *testing.T) {
	p, err := Parse(`setcps(1)\ns("bd*4")`)
	if err != nil {
		t.Fatal(err)
	}
	if p.CPS != 1 {
		t.Fatalf("cps %v", p.CPS)
	}
}

func TestNoteToMIDI(t *testing.T) {
	if NoteToMIDI("c4") != 60 {
		t.Fatal(NoteToMIDI("c4"))
	}
	if NoteToMIDI("a4") != 69 {
		t.Fatal(NoteToMIDI("a4"))
	}
}

func TestMultiply(t *testing.T) {
	p, err := Parse(`s("bd*4")`)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Layers[0].Events) != 4 {
		t.Fatalf("bd*4 → %d events", len(p.Layers[0].Events))
	}
}
