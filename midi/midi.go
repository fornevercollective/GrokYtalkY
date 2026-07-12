// Package midi is adapted from emprcl/signls and emprcl/sektron:
// buffered per-device MIDI out, virtual port, clock-friendly send.
//
//	https://github.com/emprcl/signls
//	https://github.com/emprcl/sektron
package midi

import (
	"errors"
	"log"
	"runtime"
	"strings"
	"sync"

	gomidi "gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv" // autoregisters driver
	rtmidi "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

const midiBufferSize = 1024

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

type midi struct {
	devices   gomidi.OutPorts
	waitGroup *sync.WaitGroup
	done      chan struct{}
	outputs   []chan gomidi.Message
}

// New opens hardware outs + a virtual "GrokYtalkY" port (signls pattern).
func New() (Midi, error) {
	devices := gomidi.GetOutPorts()
	if runtime.GOOS != "windows" {
		if drv, ok := drivers.Get().(*rtmidi.Driver); ok {
			if virtual, err := drv.OpenVirtualOut("GrokYtalkY"); err == nil {
				devices = append(devices, virtual)
			}
		}
	}
	if len(devices) == 0 {
		return nil, errors.New("no midi devices available")
	}
	m := &midi{devices: devices}
	m.start()
	return m, nil
}

// NewOptional returns a no-op Midi when no devices are present.
func NewOptional() Midi {
	m, err := New()
	if err != nil {
		return &noop{}
	}
	return m
}

func (m *midi) start() {
	var wg sync.WaitGroup
	wg.Add(len(m.devices))
	m.done = make(chan struct{}, len(m.devices))
	for i, device := range m.devices {
		m.outputs = append(m.outputs, make(chan gomidi.Message, midiBufferSize))
		go func(device drivers.Out, done <-chan struct{}, output <-chan gomidi.Message) {
			defer wg.Done()
			send, err := gomidi.SendTo(device)
			if err != nil {
				log.Printf("midi open %s: %v", device, err)
				// drain so senders don't block forever
				for {
					select {
					case <-done:
						return
					case <-output:
					}
				}
			}
			for {
				select {
				case <-done:
					for len(output) > 0 {
						_ = send(<-output)
					}
					return
				case msg := <-output:
					if err := send(msg); err != nil {
						log.Printf("midi send: %v", err)
					}
				}
			}
		}(device, m.done, m.outputs[i])
	}
	m.waitGroup = &wg
}

func (m *midi) Devices() gomidi.OutPorts { return m.devices }

func (m *midi) DeviceNames() []string {
	out := make([]string, len(m.devices))
	for i, d := range m.devices {
		out[i] = d.String()
	}
	return out
}

func (m *midi) safeSend(device int, msg gomidi.Message) {
	if device < 0 || device >= len(m.outputs) {
		return
	}
	select {
	case m.outputs[device] <- msg:
	default:
		// drop on backpressure rather than block UI
	}
}

func (m *midi) NoteOn(device int, channel, note, velocity uint8) {
	m.safeSend(device, gomidi.NoteOn(channel, note, velocity))
}
func (m *midi) NoteOff(device int, channel, note uint8) {
	m.safeSend(device, gomidi.NoteOff(channel, note))
}
func (m *midi) Silence(device int, channel uint8) {
	for _, msg := range gomidi.SilenceChannel(int8(channel)) {
		m.safeSend(device, msg)
	}
}
func (m *midi) SilenceAll() {
	for d := range m.devices {
		for c := 0; c < 16; c++ {
			m.Silence(d, uint8(c))
		}
	}
}
func (m *midi) ControlChange(device int, channel, controller, value uint8) {
	m.safeSend(device, gomidi.ControlChange(channel, controller, value))
}
func (m *midi) ProgramChange(device int, channel, value uint8) {
	m.safeSend(device, gomidi.ProgramChange(channel, value))
}
func (m *midi) Pitchbend(device int, channel uint8, value int16) {
	m.safeSend(device, gomidi.Pitchbend(channel, value))
}
func (m *midi) AfterTouch(device int, channel, value uint8) {
	m.safeSend(device, gomidi.AfterTouch(channel, value))
}
func (m *midi) SendClock(device int) {
	m.safeSend(device, gomidi.TimingClock())
}
func (m *midi) TransportStart(device int) {
	m.safeSend(device, gomidi.Start())
}
func (m *midi) TransportStop(device int) {
	m.safeSend(device, gomidi.Stop())
}

func (m *midi) Close() {
	defer gomidi.CloseDriver()
	if m.waitGroup == nil {
		return
	}
	for range m.devices {
		m.done <- struct{}{}
	}
	m.waitGroup.Wait()
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

// noop Midi when no devices
type noop struct{}

func (*noop) Devices() gomidi.OutPorts                                  { return nil }
func (*noop) NoteOn(int, uint8, uint8, uint8)                           {}
func (*noop) NoteOff(int, uint8, uint8)                                 {}
func (*noop) Silence(int, uint8)                                        {}
func (*noop) SilenceAll()                                               {}
func (*noop) ControlChange(int, uint8, uint8, uint8)                    {}
func (*noop) ProgramChange(int, uint8, uint8)                           {}
func (*noop) Pitchbend(int, uint8, int16)                               {}
func (*noop) AfterTouch(int, uint8, uint8)                              {}
func (*noop) SendClock(int)                                             {}
func (*noop) TransportStart(int)                                        {}
func (*noop) TransportStop(int)                                         {}
func (*noop) DeviceNames() []string                                     { return nil }
func (*noop) Close()                                                    {}
