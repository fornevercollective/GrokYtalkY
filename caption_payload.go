package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Richer caption payload for program bus + ANC SDID 0x05.
// Backward compatible: plain UTF-8 UDW when only text is set;
// magic 0xC1 + compact JSON when lang/role/speaker/source present.

const (
	// CaptionUDWMagic prefixes structured caption UDW (not valid UTF-8 start for most text).
	CaptionUDWMagic = 0xC1

	CaptionRoleLower   = "lower"
	CaptionRoleUpper   = "upper"
	CaptionRoleCrawl   = "crawl"
	CaptionRoleChyron  = "chyron"
	CaptionRoleSpeaker = "speaker"

	CaptionSourceManual  = "manual"
	CaptionSourceChat    = "chat"
	CaptionSourceXL8     = "xl8"
	CaptionSourceBridge  = "bridge"
	CaptionSourceProgram = "program"
)

// CaptionPayload is on-air text plus optional presentation metadata.
// Text is required for emission; other fields are optional telemetry for
// venue / Space / CEA-708 mappers (not full 708 themselves).
type CaptionPayload struct {
	Text    string `json:"text"`
	Lang    string `json:"lang,omitempty"`    // BCP-47 short (en, es, ja…)
	Role    string `json:"role,omitempty"`    // lower|upper|crawl|chyron|speaker
	Speaker string `json:"speaker,omitempty"` // attribution
	Source  string `json:"source,omitempty"`  // manual|chat|xl8|bridge|program
}

// IsEmpty reports no on-air text.
func (c CaptionPayload) IsEmpty() bool {
	return strings.TrimSpace(c.Text) == ""
}

// IsRich is true when any meta beyond plain text is set.
func (c CaptionPayload) IsRich() bool {
	return c.Lang != "" || c.Role != "" || c.Speaker != "" ||
		(c.Source != "" && c.Source != CaptionSourceManual && c.Source != "plain")
}

// Display returns speaker-prefixed text for TUI one-liners.
func (c CaptionPayload) Display() string {
	t := strings.TrimSpace(c.Text)
	if c.Speaker != "" {
		return c.Speaker + ": " + t
	}
	return t
}

// Normalize trims and clamps fields for wire/ANC.
func (c CaptionPayload) Normalize() CaptionPayload {
	c.Text = strings.TrimSpace(c.Text)
	c.Lang = strings.ToLower(strings.TrimSpace(c.Lang))
	if len(c.Lang) > 8 {
		c.Lang = c.Lang[:8]
	}
	c.Role = strings.ToLower(strings.TrimSpace(c.Role))
	c.Speaker = strings.TrimSpace(c.Speaker)
	if len(c.Speaker) > 24 {
		c.Speaker = c.Speaker[:24]
	}
	c.Source = strings.ToLower(strings.TrimSpace(c.Source))
	if len(c.Text) > ANCCaptionMax {
		c.Text = truncateUTF8(c.Text, ANCCaptionMax)
	}
	// validate role
	switch c.Role {
	case "", CaptionRoleLower, CaptionRoleUpper, CaptionRoleCrawl,
		CaptionRoleChyron, CaptionRoleSpeaker:
	default:
		// keep unknown role as opaque short tag
		if len(c.Role) > 12 {
			c.Role = c.Role[:12]
		}
	}
	return c
}

// EncodeCaptionUDW packs payload for ANC SDID caption.
// Plain text only → raw UTF-8 (legacy). Rich → 0xC1 + JSON (fits ANCCaptionMax).
func EncodeCaptionUDW(c CaptionPayload) []byte {
	c = c.Normalize()
	if c.IsEmpty() {
		return nil
	}
	if !c.IsRich() {
		return []byte(c.Text)
	}
	// compact JSON — omit empty via omitempty tags
	b, err := json.Marshal(c)
	if err != nil {
		return []byte(c.Text)
	}
	if 1+len(b) > ANCCaptionMax {
		// prefer structured shrink: drop speaker then source then role
		for _, drop := range []func(*CaptionPayload){
			func(p *CaptionPayload) { p.Speaker = "" },
			func(p *CaptionPayload) { p.Source = "" },
			func(p *CaptionPayload) { p.Role = "" },
			func(p *CaptionPayload) { p.Lang = "" },
		} {
			drop(&c)
			b, err = json.Marshal(c)
			if err == nil && 1+len(b) <= ANCCaptionMax {
				out := make([]byte, 1+len(b))
				out[0] = CaptionUDWMagic
				copy(out[1:], b)
				return out
			}
		}
		// last resort: plain text
		return []byte(truncateUTF8(c.Text, ANCCaptionMax))
	}
	out := make([]byte, 1+len(b))
	out[0] = CaptionUDWMagic
	copy(out[1:], b)
	return out
}

// ParseCaptionUDW decodes plain or structured caption UDW.
func ParseCaptionUDW(udw []byte) CaptionPayload {
	if len(udw) == 0 {
		return CaptionPayload{}
	}
	if udw[0] == CaptionUDWMagic && len(udw) > 1 {
		var c CaptionPayload
		if err := json.Unmarshal(udw[1:], &c); err == nil && strings.TrimSpace(c.Text) != "" {
			return c.Normalize()
		}
		// fall through: treat rest as text if JSON fails
		return CaptionPayload{Text: string(udw[1:]), Source: "plain"}.Normalize()
	}
	return CaptionPayload{Text: string(udw), Source: "plain"}.Normalize()
}

// ParseCaptionArg parses /caption CLI argument into a payload.
//
//	clear|off|none
//	plain text…
//	lang=en role=lower speaker=alice source=chat Hello world
//	en: Hello          (lang shorthand before colon)
func ParseCaptionArg(arg string) (CaptionPayload, bool /*clear*/, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return CaptionPayload{}, false, fmt.Errorf("empty")
	}
	low := strings.ToLower(arg)
	if low == "clear" || low == "off" || low == "none" {
		return CaptionPayload{}, true, nil
	}

	var c CaptionPayload
	c.Source = CaptionSourceManual
	rest := arg

	// lang shorthand: "en: text" or "es:texto"
	if i := strings.Index(rest, ":"); i > 0 && i <= 8 {
		maybe := strings.ToLower(strings.TrimSpace(rest[:i]))
		if isLangTag(maybe) && !strings.Contains(maybe, "=") {
			c.Lang = maybe
			rest = strings.TrimSpace(rest[i+1:])
		}
	}

	// key=value tokens then free text
	parts := strings.Fields(rest)
	var textParts []string
	for _, p := range parts {
		if kv := strings.SplitN(p, "=", 2); len(kv) == 2 {
			k := strings.ToLower(kv[0])
			v := strings.TrimSpace(kv[1])
			switch k {
			case "lang", "l", "locale":
				c.Lang = strings.ToLower(v)
			case "role", "r":
				c.Role = strings.ToLower(v)
			case "speaker", "who", "from", "spk":
				c.Speaker = v
			case "source", "src":
				c.Source = strings.ToLower(v)
			default:
				textParts = append(textParts, p)
			}
			continue
		}
		textParts = append(textParts, p)
	}
	c.Text = strings.TrimSpace(strings.Join(textParts, " "))
	if c.Text == "" {
		return CaptionPayload{}, false, fmt.Errorf("caption needs text")
	}
	return c.Normalize(), false, nil
}

func isLangTag(s string) bool {
	if len(s) < 2 || len(s) > 8 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	if maxBytes <= 0 {
		return ""
	}
	return s[:maxBytes]
}

// FormatCaptionLine TUI / doctor.
func FormatCaptionLine(c CaptionPayload) string {
	c = c.Normalize()
	if c.IsEmpty() {
		return "caption off"
	}
	var bits []string
	if c.Lang != "" {
		bits = append(bits, c.Lang)
	}
	if c.Role != "" {
		bits = append(bits, c.Role)
	}
	if c.Speaker != "" {
		bits = append(bits, "@"+c.Speaker)
	}
	if c.Source != "" && c.Source != CaptionSourceManual && c.Source != "plain" {
		bits = append(bits, c.Source)
	}
	meta := ""
	if len(bits) > 0 {
		meta = " · " + strings.Join(bits, " ")
	}
	return "caption " + truncate(c.Display(), 48) + meta
}
