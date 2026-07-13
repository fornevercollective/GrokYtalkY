package main

import (
	"os"
	"strconv"
	"strings"
)

// Capability profiles — one binary, same mesh semantics from 80×24 terms
// to thin Glyph/IoT agents. Lattice identity is never recomputed here;
// profiles only choose lanes, N, and backpressure.

// Cap class identifiers (stable wire values).
const (
	CapClassTermFull = "term-full" // truecolor dual Glyph capable
	CapClassTermLean = "term-lean" // small term / 256-color degrade
	CapClassTermMono = "term-mono" // no truecolor
	CapClassGlyphIoT = "glyph-iot" // thin agent: glyph+hex+mark only
	CapClassBridge   = "bridge"    // sfu/chat bridges
)

// Lane names (subset of streams-capacity.md).
const (
	LaneGlyph = "glyph"
	LaneHex   = "hex"
	LaneChat  = "chat"
	LaneGyst  = "gyst"
	LaneMid   = "mid"
)

// CapProfile is advertised on join / type:cap and used for degrade + backpressure.
type CapProfile struct {
	Class        string   `json:"class"`
	Role         string   `json:"role"` // term | agent | bridge
	GlyphN       int      `json:"glyph_n"`
	TrueColor    bool     `json:"truecolor"`
	Dual         bool     `json:"dual"`
	Lanes        []string `json:"lanes"`
	MaxFPS       int      `json:"max_fps"`
	Backpressure int      `json:"bp"` // inbound queue depth hint (agent/publishers)
	Cols         int      `json:"cols,omitempty"`
	Rows         int      `json:"rows,omitempty"`
	ColorTerm    string   `json:"colorterm,omitempty"`
	Term         string   `json:"term,omitempty"`
	Version      string   `json:"v"`
	Forge        bool     `json:"forge"` // understands forge-mark meta + lattice
}

// DetectCapProfile builds a profile from terminal geometry + environment.
// Override: GY_CAP=term-full|term-lean|term-mono|glyph-iot|bridge
// Role:     GY_ROLE=term|agent|bridge
func DetectCapProfile(cols, rows int) CapProfile {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	term := os.Getenv("TERM")
	colorTerm := os.Getenv("COLORTERM")
	trueColor := isTrueColorEnv(colorTerm, term)

	role := strings.TrimSpace(os.Getenv("GY_ROLE"))
	if role == "" {
		role = "term"
	}
	p := CapProfile{
		Role:         role,
		Cols:         cols,
		Rows:         rows,
		Term:         term,
		ColorTerm:    colorTerm,
		TrueColor:    trueColor,
		Version:      Version,
		Forge:        true,
		Backpressure: 8,
		MaxFPS:       12,
		Lanes:        []string{LaneGlyph, LaneHex, LaneChat, LaneGyst},
	}

	// explicit override
	if forced := strings.ToLower(strings.TrimSpace(os.Getenv("GY_CAP"))); forced != "" {
		applyCapClass(&p, forced)
		p.GlyphN = PreferGlyphNForGeom(p.GlyphN, cols, rows, p.Dual)
		return p
	}

	// auto class from env + geometry
	switch {
	case p.Role == "agent" || p.Role == "iot":
		applyCapClass(&p, CapClassGlyphIoT)
	case p.Role == "bridge":
		applyCapClass(&p, CapClassBridge)
	case !trueColor:
		applyCapClass(&p, CapClassTermMono)
	case !glyphFitsDual(cols, rows, GlyphPhone3, 1):
		// 80×24 and friends: lean dual 13
		applyCapClass(&p, CapClassTermLean)
	default:
		applyCapClass(&p, CapClassTermFull)
	}
	p.GlyphN = PreferGlyphNForGeom(p.GlyphN, cols, rows, p.Dual)
	return p
}

func applyCapClass(p *CapProfile, class string) {
	class = strings.ToLower(strings.TrimSpace(class))
	// aliases
	switch class {
	case "full", "term":
		class = CapClassTermFull
	case "lean", "small", "80x24":
		class = CapClassTermLean
	case "mono", "dumb":
		class = CapClassTermMono
	case "iot", "agent", "glyph", "thin":
		class = CapClassGlyphIoT
	case "bridge", "sfu", "relay":
		class = CapClassBridge
	}
	p.Class = class
	switch class {
	case CapClassTermFull:
		p.Role = "term"
		p.Dual = true
		p.TrueColor = true
		p.GlyphN = GlyphPhone3
		p.MaxFPS = 15
		p.Backpressure = 16
		p.Lanes = []string{LaneGlyph, LaneHex, LaneChat, LaneGyst, LaneMid}
	case CapClassTermLean:
		p.Role = "term"
		p.Dual = true
		p.GlyphN = GlyphPhone4a
		p.MaxFPS = 10
		p.Backpressure = 8
		p.Lanes = []string{LaneGlyph, LaneHex, LaneChat, LaneGyst}
	case CapClassTermMono:
		p.Role = "term"
		p.Dual = false
		p.TrueColor = false
		p.GlyphN = GlyphPhone4a
		p.MaxFPS = 8
		p.Backpressure = 4
		p.Lanes = []string{LaneHex, LaneChat, LaneGyst}
	case CapClassGlyphIoT:
		p.Role = "agent"
		p.Dual = false
		p.TrueColor = false
		p.GlyphN = GlyphPhone3
		p.MaxFPS = 12
		p.Backpressure = 4 // tight — drop under pressure
		p.Lanes = []string{LaneGlyph, LaneHex, LaneChat}
		p.Forge = true
	case CapClassBridge:
		p.Role = "bridge"
		p.Dual = false
		p.GlyphN = GlyphPhone3
		p.MaxFPS = 30
		p.Backpressure = 64
		p.Lanes = []string{LaneGlyph, LaneHex, LaneChat, LaneGyst}
	default:
		p.Class = CapClassTermLean
		p.Role = "term"
		p.Dual = true
		p.GlyphN = GlyphPhone4a
		p.Lanes = []string{LaneGlyph, LaneHex, LaneChat, LaneGyst}
	}
}

// PreferGlyphNForGeom clamps preferred N so dual (or single) full circles fit.
func PreferGlyphNForGeom(prefer, cols, rows int, dual bool) int {
	prefer = NormalizeGlyphN(prefer)
	if cols < 8 || rows < 4 {
		return GlyphPhone4a
	}
	// try prefer, then ladder down
	for _, n := range []int{prefer, GlyphPhone3, GlyphPhone4a} {
		n = NormalizeGlyphN(n)
		if dual {
			if glyphFitsDual(cols, rows, n, 1) {
				return n
			}
		} else if n <= cols && n <= rows-BurstChromeRows {
			return n
		}
	}
	return GlyphPhone4a
}

func isTrueColorEnv(colorTerm, term string) bool {
	ct := strings.ToLower(colorTerm)
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return true
	}
	// common modern defaults
	t := strings.ToLower(term)
	if strings.Contains(t, "truecolor") {
		return true
	}
	// COLORTERM empty but modern terminals often still support truecolor —
	// be optimistic unless TERM looks dumb.
	if term == "dumb" || term == "" && colorTerm == "" {
		// empty TERM in tests → assume truecolor capable host
		if term == "dumb" {
			return false
		}
	}
	if term == "dumb" {
		return false
	}
	// 256-color only TERMs without COLORTERM → not truecolor
	if colorTerm == "" && strings.Contains(t, "256color") && !strings.Contains(t, "direct") {
		return false
	}
	// default optimistic for interactive terms
	return term != "dumb"
}

// AcceptsLane reports whether this profile wants a lane.
func (p CapProfile) AcceptsLane(lane string) bool {
	lane = strings.ToLower(lane)
	for _, l := range p.Lanes {
		if l == lane {
			return true
		}
	}
	return false
}

// MeshMap is the join/cap payload fragment.
func (p CapProfile) MeshMap() map[string]any {
	return map[string]any{
		"class":     p.Class,
		"role":      p.Role,
		"glyph_n":   p.GlyphN,
		"truecolor": p.TrueColor,
		"dual":      p.Dual,
		"lanes":     p.Lanes,
		"max_fps":   p.MaxFPS,
		"bp":        p.Backpressure,
		"cols":      p.Cols,
		"rows":      p.Rows,
		"v":         p.Version,
		"forge":     p.Forge,
	}
}

// JoinFields merges cap into a join message.
func (p CapProfile) JoinFields(nick, role string) map[string]any {
	if role == "" {
		role = p.Role
	}
	if role == "" {
		role = "term"
	}
	return map[string]any{
		"type": "join",
		"nick": nick,
		"role": role,
		"cap":  p.MeshMap(),
	}
}

// CapAnnounce is type:cap for resize / late advertise.
func (p CapProfile) CapAnnounce(nick string) map[string]any {
	return map[string]any{
		"type": "cap",
		"from": nick,
		"cap":  p.MeshMap(),
	}
}

// ParseCapFromMesh extracts CapProfile from join/cap/roster peer objects.
func ParseCapFromMesh(msg map[string]any) (CapProfile, bool) {
	if msg == nil {
		return CapProfile{}, false
	}
	raw, ok := msg["cap"]
	if !ok {
		return CapProfile{}, false
	}
	return parseCapValue(raw)
}

func parseCapValue(raw any) (CapProfile, bool) {
	m, ok := raw.(map[string]any)
	if !ok {
		return CapProfile{}, false
	}
	p := CapProfile{Version: Version, Forge: true}
	if s, ok := m["class"].(string); ok {
		p.Class = s
	}
	if s, ok := m["role"].(string); ok {
		p.Role = s
	}
	if v, ok := m["glyph_n"].(float64); ok {
		p.GlyphN = int(v)
	}
	if v, ok := m["truecolor"].(bool); ok {
		p.TrueColor = v
	}
	if v, ok := m["dual"].(bool); ok {
		p.Dual = v
	}
	if v, ok := m["max_fps"].(float64); ok {
		p.MaxFPS = int(v)
	}
	if v, ok := m["bp"].(float64); ok {
		p.Backpressure = int(v)
	}
	if v, ok := m["cols"].(float64); ok {
		p.Cols = int(v)
	}
	if v, ok := m["rows"].(float64); ok {
		p.Rows = int(v)
	}
	if v, ok := m["forge"].(bool); ok {
		p.Forge = v
	}
	if v, ok := m["v"].(string); ok {
		p.Version = v
	}
	if arr, ok := m["lanes"].([]any); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				p.Lanes = append(p.Lanes, s)
			}
		}
	}
	if p.Class == "" {
		return p, false
	}
	if p.GlyphN < 1 {
		p.GlyphN = GlyphPhone4a
	}
	return p, true
}

// RoomGlyphN picks a glyph side that all forge-capable glyph peers can take.
// Empty peers → local prefer. Lattice is not resized here — callers downsample
// only when publishing; pass-through consumers keep wire N when possible.
func RoomGlyphN(local int, peers []CapProfile) int {
	n := NormalizeGlyphN(local)
	for _, p := range peers {
		if !p.AcceptsLane(LaneGlyph) && !p.AcceptsLane(LaneHex) {
			continue
		}
		if p.GlyphN > 0 && p.GlyphN < n {
			n = NormalizeGlyphN(p.GlyphN)
		}
	}
	return n
}

// CapSummaryLine for doctor / sys.
func (p CapProfile) SummaryLine() string {
	lanes := strings.Join(p.Lanes, ",")
	return "cap " + p.Class + " · " + p.Role +
		" · ◎" + strconv.Itoa(p.GlyphN) +
		" · lanes[" + lanes + "]" +
		" · bp=" + strconv.Itoa(p.Backpressure) +
		" · fps≤" + strconv.Itoa(p.MaxFPS)
}
