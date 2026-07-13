// Package midi is adapted from emprcl/signls and emprcl/sektron:
// buffered per-device MIDI out, virtual port, clock-friendly send.
//
//	https://github.com/emprcl/signls
//	https://github.com/emprcl/sektron
//
// Hardware I/O requires CGO + rtmidi (see midi_rtmidi.go).
// Pure-Go / release matrix builds (CGO_ENABLED=0) use a no-op backend.
package midi

import (
	"strings"

	gomidi "gitlab.com/gomidi/midi/v2"
)

// Midi is the device interface (signls-shaped).
type Midi interface {
	Devices() gomidi.OutPorts
	NoteOn(device int, channel, note, velocity uint8)
	NoteOff(device int, channel, note uint8)
	Silence(device int, channel uint8)
	SilenceAll()
	ControlChange(device int, channel, controller, value uint8)
	ProgramChange(device int, channel, value uint8)
	Pitchbend(device int, channel uint8, value int16)
	AfterTouch(device int, channel, value uint8)
	SendClock(device int)
	TransportStart(device int)
	TransportStop(device int)
	DeviceNames() []string
	Close()
}

// NoteName is the string form of a MIDI note (signls helper).
func NoteName(note uint8) string { return gomidi.Note(note).String() }

// FindDevice returns index by name substring, or 0.
func FindDevice(names []string, want string) int {
	if want == "" {
		for i, n := range names {
			if n == "GrokYtalkY" || strings.Contains(strings.ToLower(n), "grokytalky") {
				return i
			}
		}
		return 0
	}
	lw := strings.ToLower(want)
	for i, n := range names {
		if n == want || strings.Contains(strings.ToLower(n), lw) {
			return i
		}
	}
	return 0
}

// noop Midi when no devices / CGO off
type noop struct{}

func (*noop) Devices() gomidi.OutPorts               { return nil }
func (*noop) NoteOn(int, uint8, uint8, uint8)        {}
func (*noop) NoteOff(int, uint8, uint8)              {}
func (*noop) Silence(int, uint8)                     {}
func (*noop) SilenceAll()                            {}
func (*noop) ControlChange(int, uint8, uint8, uint8) {}
func (*noop) ProgramChange(int, uint8, uint8)        {}
func (*noop) Pitchbend(int, uint8, int16)            {}
func (*noop) AfterTouch(int, uint8, uint8)           {}
func (*noop) SendClock(int)                          {}
func (*noop) TransportStart(int)                     {}
func (*noop) TransportStop(int)                      {}
func (*noop) DeviceNames() []string                  { return nil }
func (*noop) Close()                                 {}
