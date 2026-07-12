package strudel

import (
	"time"

	hwmidi "github.com/fornevercollective/grokytalky/midi"
)

// MIDISink plays events through the signls/sektron-style MIDI bridge.
type MIDISink struct {
	MIDI    hwmidi.Midi
	Device  int
	DrumCh  uint8 // default 9 (GM drums)
	MelCh   uint8 // default 0
	onHit   func(Event)
}

func NewMIDISink(m hwmidi.Midi, device int) *MIDISink {
	return &MIDISink{MIDI: m, Device: device, DrumCh: 9, MelCh: 0}
}

func (s *MIDISink) OnHit(fn func(Event)) { s.onHit = fn }

func (s *MIDISink) Hit(ev Event, cycle int64) {
	if s == nil || s.MIDI == nil {
		return
	}
	if s.onHit != nil {
		s.onHit(ev)
	}
	vel := ev.Vel
	if vel == 0 {
		vel = 100
	}
	ch := s.MelCh
	note := uint8(ev.MIDI)
	if ev.Kind == "drum" {
		ch = s.DrumCh
	}
	if note > 127 {
		return
	}
	s.MIDI.NoteOn(s.Device, ch, note, vel)
	// schedule note-off
	dur := time.Duration(ev.Dur * 0.4 * float64(time.Second))
	if dur < 40*time.Millisecond {
		dur = 80 * time.Millisecond
	}
	go func() {
		time.Sleep(dur)
		s.MIDI.NoteOff(s.Device, ch, note)
	}()
}

// MultiSink fans out to multiple sinks.
type MultiSink struct {
	Sinks []Sink
}

func (m *MultiSink) Hit(ev Event, cycle int64) {
	for _, s := range m.Sinks {
		if s != nil {
			s.Hit(ev, cycle)
		}
	}
}

// FuncSink adapts a callback.
type FuncSink struct {
	Fn func(Event, int64)
}

func (f *FuncSink) Hit(ev Event, cycle int64) {
	if f.Fn != nil {
		f.Fn(ev, cycle)
	}
}
