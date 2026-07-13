//go:build cgo

package midi

import (
	"errors"
	"log"
	"runtime"
	"sync"

	gomidi "gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/drivers"
	_ "gitlab.com/gomidi/midi/v2/drivers/rtmididrv" // autoregisters driver
	rtmidi "gitlab.com/gomidi/midi/v2/drivers/rtmididrv"
)

const midiBufferSize = 1024

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
