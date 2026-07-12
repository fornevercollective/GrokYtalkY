package midi

// Bridge maps walkie events → MIDI (drive signls / sektron / soft synths).
//
// Mapping (channel 0 by default):
//
//	PTT down     → NoteOn  C3 (48) vel from level
//	PTT up       → NoteOff C3
//	RX audio VU  → CC 1  (mod wheel) 0–127
//	TX audio VU  → CC 11 (expression)
//	chat send    → NoteOn  D3 (50) short
//	translate    → NoteOn  E3 (52)
//	frame        → NoteOn  F3 (53) throttled
//	clock while TX → TimingClock pulses
type Bridge struct {
	MIDI     Midi
	Device   int
	Channel  uint8
	Clock    *Clock
	Enabled  bool
	clocking bool
}

// Default notes
const (
	NotePTT       uint8 = 48 // C3
	NoteChat      uint8 = 50 // D3
	NoteTranslate uint8 = 52 // E3
	NoteFrame     uint8 = 53 // F3
	CCMod         uint8 = 1
	CCExpression  uint8 = 11
)

func NewBridge(m Midi, device int) *Bridge {
	b := &Bridge{
		MIDI:    m,
		Device:  device,
		Channel: 0,
		Enabled: m != nil && len(m.DeviceNames()) > 0,
	}
	return b
}

func (b *Bridge) StartClock(tempo float64) {
	if b == nil || !b.Enabled {
		return
	}
	if b.Clock != nil {
		b.Clock.Stop()
	}
	b.clocking = true
	b.MIDI.TransportStart(b.Device)
	b.Clock = NewClock(tempo, func() {
		if b.clocking {
			b.MIDI.SendClock(b.Device)
		}
	})
}

func (b *Bridge) StopClock() {
	if b == nil {
		return
	}
	b.clocking = false
	if b.Clock != nil {
		b.Clock.Stop()
		b.Clock = nil
	}
	if b.Enabled {
		b.MIDI.TransportStop(b.Device)
	}
}

func (b *Bridge) PTT(down bool, velocity uint8) {
	if b == nil || !b.Enabled {
		return
	}
	if velocity < 1 {
		velocity = 64
	}
	if down {
		b.MIDI.NoteOn(b.Device, b.Channel, NotePTT, velocity)
		b.StartClock(120)
	} else {
		b.MIDI.NoteOff(b.Device, b.Channel, NotePTT)
		b.StopClock()
	}
}

func (b *Bridge) LevelTX(level float64) {
	if b == nil || !b.Enabled {
		return
	}
	v := uint8(level * 127)
	if v > 127 {
		v = 127
	}
	b.MIDI.ControlChange(b.Device, b.Channel, CCExpression, v)
}

func (b *Bridge) LevelRX(level float64) {
	if b == nil || !b.Enabled {
		return
	}
	v := uint8(level * 127)
	if v > 127 {
		v = 127
	}
	b.MIDI.ControlChange(b.Device, b.Channel, CCMod, v)
}

func (b *Bridge) Chat() {
	if b == nil || !b.Enabled {
		return
	}
	b.MIDI.NoteOn(b.Device, b.Channel, NoteChat, 90)
	// note-off deferred lightly via Silence is heavy; fire off after short note
	go func() {
		b.MIDI.NoteOff(b.Device, b.Channel, NoteChat)
	}()
}

func (b *Bridge) Translate() {
	if b == nil || !b.Enabled {
		return
	}
	b.MIDI.NoteOn(b.Device, b.Channel, NoteTranslate, 100)
	go func() {
		b.MIDI.NoteOff(b.Device, b.Channel, NoteTranslate)
	}()
}

func (b *Bridge) Frame() {
	if b == nil || !b.Enabled {
		return
	}
	b.MIDI.NoteOn(b.Device, b.Channel, NoteFrame, 40)
	go func() {
		b.MIDI.NoteOff(b.Device, b.Channel, NoteFrame)
	}()
}

func (b *Bridge) Close() {
	if b == nil {
		return
	}
	b.StopClock()
	if b.MIDI != nil {
		b.MIDI.SilenceAll()
		b.MIDI.Close()
	}
}
