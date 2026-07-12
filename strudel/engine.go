package strudel

import (
	"sync"
	"time"
)

// Sink receives live events (MIDI / mesh / viz).
type Sink interface {
	Hit(ev Event, cycle int64)
}

// Engine is a live-coding pattern player (Strudel REPL-like hot swap).
type Engine struct {
	mu       sync.Mutex
	pat      *Pattern
	code     string
	playing  bool
	cycle    int64
	started  time.Time
	stopCh   chan struct{}
	sink     Sink
	onCycle  func(cycle int64, cps float64, code string)
	swing    float64 // 0..1 delay even steps
}

func NewEngine(sink Sink) *Engine {
	return &Engine{sink: sink, stopCh: make(chan struct{})}
}

func (e *Engine) SetOnCycle(fn func(cycle int64, cps float64, code string)) {
	e.mu.Lock()
	e.onCycle = fn
	e.mu.Unlock()
}

// Eval hot-swaps the pattern (like Strudel update while playing).
func (e *Engine) Eval(code string) error {
	p, err := Parse(code)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.pat = p
	e.code = code
	e.mu.Unlock()
	return nil
}

func (e *Engine) Code() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.code
}

func (e *Engine) Pattern() *Pattern {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pat
}

func (e *Engine) Playing() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.playing
}

func (e *Engine) Cycle() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cycle
}

func (e *Engine) CPS() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pat == nil {
		return 0.5
	}
	return e.pat.CPS
}

// Play starts the cycle loop if not running.
func (e *Engine) Play() {
	e.mu.Lock()
	if e.playing {
		e.mu.Unlock()
		return
	}
	if e.pat == nil {
		e.mu.Unlock()
		return
	}
	e.playing = true
	e.started = time.Now()
	e.cycle = 0
	// fresh stop channel
	e.stopCh = make(chan struct{})
	stop := e.stopCh
	e.mu.Unlock()
	go e.loop(stop)
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.playing {
		e.mu.Unlock()
		return
	}
	e.playing = false
	close(e.stopCh)
	e.mu.Unlock()
}

func (e *Engine) Toggle() {
	if e.Playing() {
		e.Stop()
	} else {
		e.Play()
	}
}

func (e *Engine) loop(stop <-chan struct{}) {
	for {
		e.mu.Lock()
		p := e.pat
		cyc := e.cycle
		onC := e.onCycle
		code := e.code
		e.mu.Unlock()
		if p == nil {
			return
		}
		cps := p.CPS
		if cps <= 0 {
			cps = 0.5
		}
		cycleDur := time.Duration(float64(time.Second) / cps)

		// schedule events in this cycle
		type timed struct {
			at time.Duration
			ev Event
		}
		var schedule []timed
		for _, layer := range p.Layers {
			for _, ev := range layer.Events {
				at := time.Duration(ev.At * float64(cycleDur))
				// simple swing on even 16ths
				schedule = append(schedule, timed{at: at, ev: ev})
			}
		}
		// sort by time
		for i := 0; i < len(schedule); i++ {
			for j := i + 1; j < len(schedule); j++ {
				if schedule[j].at < schedule[i].at {
					schedule[i], schedule[j] = schedule[j], schedule[i]
				}
			}
		}

		if onC != nil {
			onC(cyc, cps, code)
		}

		cycleStart := time.Now()
		idx := 0
		for idx < len(schedule) {
			select {
			case <-stop:
				return
			default:
			}
			wait := schedule[idx].at - time.Since(cycleStart)
			if wait > 0 {
				t := time.NewTimer(wait)
				select {
				case <-stop:
					t.Stop()
					return
				case <-t.C:
				}
			}
			ev := schedule[idx].ev
			if e.sink != nil {
				e.sink.Hit(ev, cyc)
			}
			idx++
		}
		// wait remainder of cycle
		rem := cycleDur - time.Since(cycleStart)
		if rem > 0 {
			t := time.NewTimer(rem)
			select {
			case <-stop:
				t.Stop()
				return
			case <-t.C:
			}
		}

		e.mu.Lock()
		if !e.playing {
			e.mu.Unlock()
			return
		}
		e.cycle++
		e.mu.Unlock()
	}
}

// QueryEvents returns events for visualization of current pattern.
func (e *Engine) QueryEvents() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pat == nil {
		return nil
	}
	var all []Event
	for _, l := range e.pat.Layers {
		all = append(all, l.Events...)
	}
	return all
}
