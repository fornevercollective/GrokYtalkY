package main

import (
	"fmt"
	"strings"
	"time"

	hwmidi "github.com/fornevercollective/grokytalky/midi"
)

// Mesh MIDI kinds for jam-session peer sync (walkie + Strudel hits).
const (
	MeshMIDINoteOn  = "noteon"
	MeshMIDINoteOff = "noteoff"
	MeshMIDICC      = "cc"
	MeshMIDIClock   = "clock"
	MeshMIDIStart   = "start"
	MeshMIDIStop    = "stop"
	MeshMIDITempo   = "tempo" // share CPS / BPM for jam phase
)

// MeshMIDIMsg is the wire shape for bidirectional MIDI over the hub.
// Hub default fan-out carries unknown types room-scoped.
type MeshMIDIMsg struct {
	Type   string  `json:"type"` // "midi"
	From   string  `json:"from"`
	Kind   string  `json:"kind"` // noteon|noteoff|cc|clock|start|stop|tempo
	Ch     int     `json:"ch,omitempty"`
	Note   int     `json:"note,omitempty"`
	Vel    int     `json:"vel,omitempty"`
	CC     int     `json:"cc,omitempty"`
	Val    int     `json:"val,omitempty"`
	CPS    float64 `json:"cps,omitempty"`
	BPM    float64 `json:"bpm,omitempty"`
	Beat   float64 `json:"beat,omitempty"`
	Cycle  int64   `json:"cycle,omitempty"`
	T      int64   `json:"t"`
	Origin string  `json:"origin,omitempty"` // walkie|strudel|controller
}

// MeshMIDIType is the hub message type string.
const MeshMIDIType = "midi"

// BuildMeshMIDINote constructs a note-on/off mesh MIDI message.
func BuildMeshMIDINote(from, kind string, ch, note, vel int, origin string) map[string]any {
	if vel < 0 {
		vel = 0
	}
	if vel > 127 {
		vel = 127
	}
	if ch < 0 {
		ch = 0
	}
	return map[string]any{
		"type":   MeshMIDIType,
		"from":   from,
		"kind":   kind,
		"ch":     ch,
		"note":   note,
		"vel":    vel,
		"t":      time.Now().UnixMilli(),
		"origin": origin,
	}
}

// BuildMeshMIDICC constructs a control-change mesh MIDI message.
func BuildMeshMIDICC(from string, ch, cc, val int, origin string) map[string]any {
	if val < 0 {
		val = 0
	}
	if val > 127 {
		val = 127
	}
	return map[string]any{
		"type":   MeshMIDIType,
		"from":   from,
		"kind":   MeshMIDICC,
		"ch":     ch,
		"cc":     cc,
		"val":    val,
		"t":      time.Now().UnixMilli(),
		"origin": origin,
	}
}

// BuildMeshMIDITempo shares jam tempo (CPS preferred).
func BuildMeshMIDITempo(from string, cps float64, cycle int64) map[string]any {
	bpm := 0.0
	if cps > 0 {
		bpm = cps * 60 * 4 // approximate: 4 beats/cycle common in mini-notation
	}
	return map[string]any{
		"type":  MeshMIDIType,
		"from":  from,
		"kind":  MeshMIDITempo,
		"cps":   cps,
		"bpm":   bpm,
		"cycle": cycle,
		"t":     time.Now().UnixMilli(),
	}
}

// ParseMeshMIDI extracts fields from hub JSON.
func ParseMeshMIDI(msg map[string]any) (MeshMIDIMsg, bool) {
	if msg == nil {
		return MeshMIDIMsg{}, false
	}
	if t, _ := msg["type"].(string); t != MeshMIDIType {
		return MeshMIDIMsg{}, false
	}
	m := MeshMIDIMsg{Type: MeshMIDIType}
	m.From, _ = msg["from"].(string)
	m.Kind, _ = msg["kind"].(string)
	m.Kind = strings.ToLower(m.Kind)
	if v, ok := msg["ch"].(float64); ok {
		m.Ch = int(v)
	}
	if v, ok := msg["note"].(float64); ok {
		m.Note = int(v)
	}
	if v, ok := msg["vel"].(float64); ok {
		m.Vel = int(v)
	}
	if v, ok := msg["cc"].(float64); ok {
		m.CC = int(v)
	}
	if v, ok := msg["val"].(float64); ok {
		m.Val = int(v)
	}
	if v, ok := msg["cps"].(float64); ok {
		m.CPS = v
	}
	if v, ok := msg["bpm"].(float64); ok {
		m.BPM = v
	}
	if v, ok := msg["beat"].(float64); ok {
		m.Beat = v
	}
	if v, ok := msg["cycle"].(float64); ok {
		m.Cycle = int64(v)
	}
	if v, ok := msg["t"].(float64); ok {
		m.T = int64(v)
	}
	m.Origin, _ = msg["origin"].(string)
	if m.Kind == "" {
		return m, false
	}
	return m, true
}

// ApplyMeshMIDI drives local MIDI hardware from a peer mesh message.
// Skips when m is nil / disabled. Returns short status string for TUI.
func ApplyMeshMIDI(b *hwmidi.Bridge, m MeshMIDIMsg) string {
	if b == nil || !b.Enabled || b.MIDI == nil {
		return ""
	}
	dev := b.Device
	ch := uint8(m.Ch)
	switch m.Kind {
	case MeshMIDINoteOn, "on", "note_on":
		vel := uint8(m.Vel)
		if vel < 1 {
			vel = 90
		}
		b.MIDI.NoteOn(dev, ch, uint8(m.Note&0x7f), vel)
		return fmt.Sprintf("midi←%s note %d", m.From, m.Note)
	case MeshMIDINoteOff, "off", "note_off":
		b.MIDI.NoteOff(dev, ch, uint8(m.Note&0x7f))
		return ""
	case MeshMIDICC, "control":
		b.MIDI.ControlChange(dev, ch, uint8(m.CC&0x7f), uint8(m.Val&0x7f))
		return ""
	case MeshMIDIStart, "transport_start":
		b.MIDI.TransportStart(dev)
		return "midi←start"
	case MeshMIDIStop, "transport_stop":
		b.MIDI.TransportStop(dev)
		return "midi←stop"
	case MeshMIDIClock:
		b.MIDI.SendClock(dev)
		return ""
	case MeshMIDITempo:
		// tempo is soft — Bridge clock restart when CPS known
		if m.BPM > 20 && m.BPM < 400 {
			b.StartClock(m.BPM)
			return fmt.Sprintf("midi←tempo %.0fbpm", m.BPM)
		}
		return ""
	default:
		return ""
	}
}

// (c *MeshClient) SendMIDI publishes a mesh MIDI event (non-blocking).
func (c *MeshClient) SendMIDI(msg map[string]any) {
	if c == nil || msg == nil {
		return
	}
	if _, ok := msg["from"]; !ok || msg["from"] == "" {
		msg["from"] = c.Nick
	}
	msg["type"] = MeshMIDIType
	if _, ok := msg["t"]; !ok {
		msg["t"] = time.Now().UnixMilli()
	}
	_ = c.SendJSON(msg)
}

// SendMIDINote is a convenience for walkie/strudel TX.
func (c *MeshClient) SendMIDINote(kind string, ch, note, vel int, origin string) {
	if c == nil {
		return
	}
	c.SendMIDI(BuildMeshMIDINote(c.Nick, kind, ch, note, vel, origin))
}
