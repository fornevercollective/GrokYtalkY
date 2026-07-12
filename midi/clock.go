// Clock adapted from emprcl/sektron sequencer/clock.go
// MIDI clock: 6 pulses per 16th note.
// http://midi.teragonaudio.com/tech/midispec/clock.htm
package midi

import "time"

const (
	pulsesPerStep       = 6
	stepsPerQuarterNote = 4
	tempoMin            = 1.0
	tempoMax            = 300.0
	updateBufferSize    = 128
)

// Clock ticks at MIDI pulse rate for a given tempo (BPM).
type Clock struct {
	ticker       *time.Ticker
	update       chan float64
	tempo        float64
	shouldUpdate bool
	stop         chan struct{}
}

// NewClock starts a clock; tick is called each MIDI pulse.
func NewClock(tempo float64, tick func()) *Clock {
	if tempo < tempoMin {
		tempo = 120
	}
	c := &Clock{
		ticker: time.NewTicker(clockInterval(tempo)),
		update: make(chan float64, updateBufferSize),
		tempo:  tempo,
		stop:   make(chan struct{}),
	}
	go func() {
		for {
			select {
			case <-c.stop:
				c.ticker.Stop()
				return
			case <-c.ticker.C:
				if tick != nil {
					tick()
				}
				if c.shouldUpdate {
					c.ticker.Reset(clockInterval(c.tempo))
					c.shouldUpdate = false
				}
			case newTempo := <-c.update:
				c.shouldUpdate = true
				c.tempo = newTempo
			}
		}
	}()
	return c
}

func (c *Clock) SetTempo(tempo float64) {
	if tempo > tempoMax || tempo < tempoMin {
		return
	}
	select {
	case c.update <- tempo:
	default:
	}
}

func (c *Clock) Tempo() float64 { return c.tempo }

func (c *Clock) Stop() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
}

func clockInterval(tempo float64) time.Duration {
	return time.Duration(1000000*60/(tempo*float64(pulsesPerStep*stepsPerQuarterNote))) * time.Microsecond
}
