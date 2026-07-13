package main

import (
	"encoding/json"
	"strings"
	"time"
)

// program-caption — caption-only program bus merge (chat-bridge / thin tools).
// Does not change PGM source, preview, or mode — take/preview stay conductor-owned.
// Zero authority risk relative to /take: only caption fields + seq bump.

// ApplyProgramCaption merges a caption payload into the last stored hub program
// envelope (type:program map). If stored is nil/invalid, starts from NewProgramBus.
// Returns a full mesh type:program message ready to store and broadcast.
func ApplyProgramCaption(stored map[string]any, from string, cap CaptionPayload) map[string]any {
	cap = cap.Normalize()
	// normalize in-memory MeshJSON (struct bus) via JSON so ParseProgramBus works
	if stored != nil {
		if raw, err := json.Marshal(stored); err == nil {
			var norm map[string]any
			if json.Unmarshal(raw, &norm) == nil {
				stored = norm
			}
		}
	}
	var bus ProgramBus
	if stored != nil {
		if b, ok := ParseProgramBus(stored); ok {
			bus = b
		} else {
			bus = NewProgramBus()
		}
	} else {
		bus = NewProgramBus()
	}
	// empty conductor arg — do not steal /conductor claim label
	bus.SetCaptionRich(cap, "")
	if from == "" {
		from = "caption-bridge"
	}
	// mesh from = bridge/tool nick; bus.Conductor unchanged unless previously set
	msg := bus.MeshJSON(from)
	msg["type"] = "program"
	msg["via"] = "program-caption"
	msg["t"] = time.Now().UnixMilli()
	// JSON round-trip so "bus" is map[string]any (hub store / ParseProgramBus)
	raw, err := json.Marshal(msg)
	if err != nil {
		return msg
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return msg
	}
	return out
}

// ParseCaptionFromMesh extracts CaptionPayload from program-caption / caption messages.
func ParseCaptionFromMesh(msg map[string]any) (CaptionPayload, bool) {
	if msg == nil {
		return CaptionPayload{}, false
	}
	// nested caption_meta object
	if raw, ok := msg["caption_meta"].(map[string]any); ok {
		c := captionFromMap(raw)
		if !c.IsEmpty() {
			return c.Normalize(), true
		}
	}
	// flat fields
	var c CaptionPayload
	if s, ok := msg["text"].(string); ok {
		c.Text = s
	}
	if s, ok := msg["caption"].(string); ok && c.Text == "" {
		c.Text = s
	}
	if s, ok := msg["lang"].(string); ok {
		c.Lang = s
	}
	if s, ok := msg["role"].(string); ok {
		c.Role = s
	}
	if s, ok := msg["speaker"].(string); ok {
		c.Speaker = s
	}
	if s, ok := msg["source"].(string); ok {
		c.Source = s
	}
	// speaker default from from
	if c.Speaker == "" {
		if s, ok := msg["from"].(string); ok {
			c.Speaker = s
		}
	}
	if c.Source == "" {
		c.Source = CaptionSourceChat
	}
	c = c.Normalize()
	if c.IsEmpty() {
		return CaptionPayload{}, false
	}
	return c, true
}

// CaptionFromChatLine builds a rich caption from a host chat line.
// Optional leading "en: " / "lang=es " prefixes are parsed like /caption.
func CaptionFromChatLine(from, text string) CaptionPayload {
	text = strings.TrimSpace(text)
	if text == "" {
		return CaptionPayload{}
	}
	// try /caption-style parse; if only clear-ish, use plain
	if cap, clear, err := ParseCaptionArg(text); err == nil && !clear && !cap.IsEmpty() {
		if cap.Speaker == "" {
			cap.Speaker = from
		}
		if cap.Source == "" || cap.Source == CaptionSourceManual {
			cap.Source = CaptionSourceChat
		}
		if cap.Role == "" {
			cap.Role = CaptionRoleLower
		}
		return cap.Normalize()
	}
	return CaptionPayload{
		Text:    text,
		Speaker: from,
		Source:  CaptionSourceChat,
		Role:    CaptionRoleLower,
	}.Normalize()
}
