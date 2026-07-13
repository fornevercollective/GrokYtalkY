package main

import (
	"fmt"
	"strings"
	"time"
)

// Program bus — conductor / director on-air control plane.
// Venue adapters (NDI / ST 2110 / LED walls) consume this contract later;
// lattice identity stays pass-through on whatever source is selected.

const (
	ProgramModeLive  = "live"
	ProgramModeHold  = "hold"  // freeze last program frame
	ProgramModeBlack = "black" // safe slate / black

	ProgramSourceForge = "forge"
	ProgramSourceGyst  = "gyst"
	ProgramSourceBurst = "burst"
	ProgramSourceSim   = "sim"
	ProgramSourceSlate = "slate"
)

// ProgramSource identifies what is on program (or preview).
type ProgramSource struct {
	Source string `json:"source"`          // forge|gyst|burst|sim|slate
	Nick   string `json:"nick,omitempty"`  // publisher nick
	Slot   int    `json:"slot,omitempty"`  // forge lab slot 1..N
	Mark   string `json:"mark,omitempty"`  // cgf:… lattice identity
	Lane   string `json:"lane,omitempty"`  // glyph|hex|gyst
	Label  string `json:"label,omitempty"` // human label
}

// ProgramBus is the room-wide on-air state (mesh type:program).
type ProgramBus struct {
	V         int            `json:"v"`
	Mode      string         `json:"mode"` // live|hold|black
	Program   ProgramSource  `json:"program"`
	Preview   *ProgramSource `json:"preview,omitempty"`
	Caption   string          `json:"caption,omitempty"` // on-air text (compat primary)
	CapMeta   *CaptionPayload `json:"caption_meta,omitempty"` // rich lang/role/speaker/source
	Conductor string          `json:"conductor,omitempty"`
	Seq       uint32          `json:"seq"`
	T         int64           `json:"t"`
}

// NewProgramBus empty live bus (no conductor yet).
func NewProgramBus() ProgramBus {
	return ProgramBus{
		V:    1,
		Mode: ProgramModeLive,
		Program: ProgramSource{
			Source: ProgramSourceSlate,
			Label:  "slate",
			Lane:   LaneGlyph,
		},
		T: time.Now().UnixMilli(),
	}
}

// MeshJSON for hub type:program fan-out.
func (b ProgramBus) MeshJSON(from string) map[string]any {
	msg := map[string]any{
		"type": "program",
		"from": from,
		"bus":  b,
		"t":    b.T,
		"seq":  b.Seq,
	}
	if b.Conductor != "" {
		msg["conductor"] = b.Conductor
	}
	return msg
}

// ParseProgramBus from mesh type:program (or nested bus).
func ParseProgramBus(msg map[string]any) (ProgramBus, bool) {
	if msg == nil {
		return ProgramBus{}, false
	}
	raw, ok := msg["bus"]
	if !ok {
		// allow flat fields for thin agents
		if typ, _ := msg["type"].(string); typ != "program" {
			return ProgramBus{}, false
		}
		return parseProgramBusMap(msg)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return ProgramBus{}, false
	}
	return parseProgramBusMap(m)
}

func parseProgramBusMap(m map[string]any) (ProgramBus, bool) {
	b := NewProgramBus()
	if v, ok := m["v"].(float64); ok {
		b.V = int(v)
	}
	if s, ok := m["mode"].(string); ok && s != "" {
		b.Mode = s
	}
	if s, ok := m["conductor"].(string); ok {
		b.Conductor = s
	}
	if v, ok := m["seq"].(float64); ok {
		b.Seq = uint32(v)
	}
	if v, ok := m["t"].(float64); ok {
		b.T = int64(v)
	}
	if prog, ok := m["program"].(map[string]any); ok {
		b.Program = parseProgramSource(prog)
	}
	if prev, ok := m["preview"].(map[string]any); ok {
		ps := parseProgramSource(prev)
		b.Preview = &ps
	}
	if s, ok := m["caption"].(string); ok {
		b.Caption = s
	}
	if raw, ok := m["caption_meta"].(map[string]any); ok {
		cp := captionFromMap(raw)
		if !cp.IsEmpty() {
			cp = cp.Normalize()
			b.CapMeta = &cp
			if b.Caption == "" {
				b.Caption = cp.Text
			}
		}
	}
	return b, true
}

func captionFromMap(m map[string]any) CaptionPayload {
	var c CaptionPayload
	c.Text, _ = m["text"].(string)
	c.Lang, _ = m["lang"].(string)
	c.Role, _ = m["role"].(string)
	c.Speaker, _ = m["speaker"].(string)
	c.Source, _ = m["source"].(string)
	return c
}

func parseProgramSource(m map[string]any) ProgramSource {
	var s ProgramSource
	s.Source, _ = m["source"].(string)
	s.Nick, _ = m["nick"].(string)
	s.Mark, _ = m["mark"].(string)
	s.Lane, _ = m["lane"].(string)
	s.Label, _ = m["label"].(string)
	if v, ok := m["slot"].(float64); ok {
		s.Slot = int(v)
	}
	return s
}

// FormatProgramLine TUI / agent status.
func FormatProgramLine(b ProgramBus) string {
	mode := b.Mode
	if mode == "" {
		mode = ProgramModeLive
	}
	cond := b.Conductor
	if cond == "" {
		cond = "—"
	}
	return fmt.Sprintf("◈ program %s · %s · cond %s · seq %d",
		mode, FormatProgramSource(b.Program), truncate(cond, 12), b.Seq)
}

// FormatProgramSource short source identity.
func FormatProgramSource(s ProgramSource) string {
	if s.Source == "" {
		return "—"
	}
	parts := []string{s.Source}
	if s.Slot > 0 {
		parts = append(parts, fmt.Sprintf("s%d", s.Slot))
	}
	if s.Mark != "" {
		parts = append(parts, ShortMarkID(s.Mark))
	} else if s.Label != "" {
		parts = append(parts, truncate(s.Label, 10))
	} else if s.Nick != "" {
		parts = append(parts, truncate(s.Nick, 10))
	}
	return strings.Join(parts, " ")
}

// SourceFromForge builds a program source from a forge mark + nick.
func SourceFromForge(nick string, mark *ForgeMark, lane string) ProgramSource {
	s := ProgramSource{
		Source: ProgramSourceForge,
		Nick:   nick,
		Lane:   lane,
	}
	if lane == "" {
		s.Lane = LaneGlyph
	}
	if mark != nil {
		s.Slot = mark.Slot
		s.Mark = mark.ID
		s.Label = mark.Source
	}
	return s
}

// Take applies preview→program (or sets program directly). Increments seq.
func (b *ProgramBus) Take(prog ProgramSource, conductor string) {
	if b.Preview != nil && prog.Source == "" {
		prog = *b.Preview
	}
	b.Program = prog
	b.Mode = ProgramModeLive
	b.Conductor = conductor
	b.Seq++
	b.T = time.Now().UnixMilli()
	b.V = 1
}

// SetPreview arms preview without changing program (bumps seq for ANC re-emit).
func (b *ProgramBus) SetPreview(prev ProgramSource, conductor string) {
	cp := prev
	b.Preview = &cp
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
	b.V = 1
}

// ClearPreview disarms PVW (tally flag bit1 clears).
func (b *ProgramBus) ClearPreview(conductor string) {
	b.Preview = nil
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
}

// SetCaption sets on-air caption text for ANC SDID caption (max 120 bytes).
// Empty string clears caption (no caption ANC packet). Clears CapMeta.
func (b *ProgramBus) SetCaption(text, conductor string) {
	text = strings.TrimSpace(text)
	if len(text) > ANCCaptionMax {
		text = truncateUTF8(text, ANCCaptionMax)
	}
	b.Caption = text
	if text == "" {
		b.CapMeta = nil
	} else {
		// keep plain meta so Encode path stays simple unless SetCaptionRich used
		b.CapMeta = nil
	}
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
	b.V = 1
}

// SetCaptionRich sets structured caption (lang/role/speaker/source) + primary text.
// Empty text clears. Bumps seq for ANC re-emit (preview-first style).
func (b *ProgramBus) SetCaptionRich(cap CaptionPayload, conductor string) {
	cap = cap.Normalize()
	if cap.IsEmpty() {
		b.Caption = ""
		b.CapMeta = nil
	} else {
		b.Caption = cap.Text
		if cap.IsRich() {
			cp := cap
			b.CapMeta = &cp
		} else {
			b.CapMeta = nil
		}
	}
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
	b.V = 1
}

// EffectiveCaption returns rich meta if present, else plain Caption string.
func (b ProgramBus) EffectiveCaption() CaptionPayload {
	if b.CapMeta != nil && !b.CapMeta.IsEmpty() {
		c := *b.CapMeta
		if c.Text == "" {
			c.Text = b.Caption
		}
		return c.Normalize()
	}
	if strings.TrimSpace(b.Caption) == "" {
		return CaptionPayload{}
	}
	return CaptionPayload{Text: b.Caption, Source: CaptionSourceManual}.Normalize()
}

// Hold freezes program (venue holds last frame).
func (b *ProgramBus) Hold(conductor string) {
	b.Mode = ProgramModeHold
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
}

// Black safe slate — venue adapters show black/logo, not panic.
func (b *ProgramBus) Black(conductor string) {
	b.Mode = ProgramModeBlack
	b.Program = ProgramSource{
		Source: ProgramSourceSlate,
		Label:  "black",
		Lane:   LaneGlyph,
	}
	if conductor != "" {
		b.Conductor = conductor
	}
	b.Seq++
	b.T = time.Now().UnixMilli()
}

// IsOnAir reports whether src matches current program (mark or nick+slot).
// Used by thin agents / venue sinks to filter preview traffic.
func (b ProgramBus) IsOnAir(nick string, markID string, slot int) bool {
	if b.Mode == ProgramModeBlack {
		return false
	}
	p := b.Program
	if markID != "" && p.Mark != "" && markID == p.Mark {
		return true
	}
	if nick != "" && p.Nick != "" && nick == p.Nick {
		if p.Slot == 0 || slot == 0 || p.Slot == slot {
			return true
		}
	}
	return false
}

// VenueAdapterHint documents the stable fields venue adapters should read.
// Not a runtime type — kept as comments + Summary for doctor.
func (b ProgramBus) VenueAdapterHint() string {
	return fmt.Sprintf(
		"venue sink: mode=%s program.source=%s mark=%s lane=%s seq=%d (NDI/2110 later)",
		b.Mode, b.Program.Source, ShortMarkID(b.Program.Mark), b.Program.Lane, b.Seq,
	)
}
