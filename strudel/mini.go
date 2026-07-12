// Package strudel implements a live subset of Strudel / Tidal mini-notation
// for terminal performance — inspired by https://strudel.cc/ and Qbpm jam bridge.
package strudel

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Event is one hit inside a cycle (cycle time in [0,1)).
type Event struct {
	At     float64 // 0..1 within cycle
	Dur    float64 // fraction of cycle
	Kind   string  // "drum" | "note"
	Sound  string  // bd, sd, … or note name c4
	MIDI   int     // resolved midi note (or GM drum)
	Vel    uint8
	Source string  // which layer
}

// Pattern is a compiled multi-layer cycle pattern.
type Pattern struct {
	Raw    string
	CPS    float64 // cycles per second (Strudel setcps)
	BPM    float64 // convenience; BPM = CPS * 60 * 4 for 4/4 bar cycle
	Layers []Layer
}

// Layer is one sequence of events over a cycle.
type Layer struct {
	Name   string
	Events []Event
}

// Parse compiles Strudel-ish source into a Pattern.
// Supports:
//
//	s("bd sd hh cp")
//	s("bd*4, [~ sd]*2")
//	note("c4 e4 g4") / n("c3")
//	stack(s("bd*4"), note("c2"))
//	setcps(0.5)  bpm(120)
//	<bd sd>  [bd hh]  ~ rest  *N  , parallel inside s()
func Parse(src string) (*Pattern, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	p := &Pattern{Raw: src, CPS: 0.5, BPM: 120}

	// extract global setcps / bpm
	if m := reFind(src, `setcps\s*\(\s*([0-9.]+)\s*\)`); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil && v > 0 {
			p.CPS = v
			p.BPM = v * 60 * 4 // treat cycle as 4 beats
		}
	}
	if m := reFind(src, `bpm\s*\(\s*([0-9.]+)\s*\)`); m != "" {
		if v, err := strconv.ParseFloat(m, 64); err == nil && v > 0 {
			p.BPM = v
			p.CPS = v / 60 / 4
		}
	}

	// stack(...) → multiple roots
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(src)), "stack") {
		inner := extractParen(src, strings.Index(strings.ToLower(src), "stack"))
		parts := splitTopLevel(inner, ',')
		for i, part := range parts {
			layer, err := parseExpr(strings.TrimSpace(part), fmt.Sprintf("L%d", i))
			if err != nil {
				return nil, err
			}
			if layer != nil {
				p.Layers = append(p.Layers, *layer)
			}
		}
	} else {
		// single or multiple s()/note() calls
		found := false
		for _, call := range findCalls(src) {
			layer, err := parseExpr(call, call[:min(8, len(call))])
			if err != nil {
				return nil, err
			}
			if layer != nil {
				p.Layers = append(p.Layers, *layer)
				found = true
			}
		}
		if !found {
			// bare mini string: bd sd hh  or  c4 e4
			layer, err := parseBare(src, "main")
			if err != nil {
				return nil, err
			}
			p.Layers = append(p.Layers, *layer)
		}
	}

	if len(p.Layers) == 0 {
		return nil, fmt.Errorf("no events parsed")
	}
	return p, nil
}

func parseExpr(expr, name string) (*Layer, error) {
	expr = strings.TrimSpace(expr)
	low := strings.ToLower(expr)
	if strings.HasPrefix(low, "s(") || strings.HasPrefix(low, "sound(") {
		inner := unquote(extractParen(expr, 0))
		// comma = parallel sub-layers merged into one layer
		subs := splitTopLevel(inner, ',')
		var all []Event
		for _, sub := range subs {
			evs, err := expandMini(strings.TrimSpace(sub), "drum")
			if err != nil {
				return nil, err
			}
			all = append(all, evs...)
		}
		return &Layer{Name: name, Events: all}, nil
	}
	if strings.HasPrefix(low, "note(") || strings.HasPrefix(low, "n(") {
		inner := unquote(extractParen(expr, 0))
		evs, err := expandMini(inner, "note")
		if err != nil {
			return nil, err
		}
		return &Layer{Name: name, Events: evs}, nil
	}
	return parseBare(expr, name)
}

func parseBare(src, name string) (*Layer, error) {
	// strip setcps/bpm noise
	src = reReplace(src, `setcps\s*\([^)]*\)`, " ")
	src = reReplace(src, `bpm\s*\([^)]*\)`, " ")
	src = strings.TrimSpace(src)
	kind := "drum"
	if looksLikeNotes(src) {
		kind = "note"
	}
	evs, err := expandMini(src, kind)
	if err != nil {
		return nil, err
	}
	return &Layer{Name: name, Events: evs}, nil
}

func looksLikeNotes(s string) bool {
	low := strings.ToLower(s)
	for _, d := range []string{"bd", "sd", "hh", "oh", "cp", "rim", "rd", "kick", "snare"} {
		if strings.Contains(low, d) {
			return false
		}
	}
	return hasNoteToken(s)
}

func hasNoteToken(s string) bool {
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		if (c >= 'a' && c <= 'g') || (c >= 'A' && c <= 'G') {
			j := i + 1
			if j < len(s) && (s[j] == '#' || s[j] == 'b') {
				j++
			}
			if j < len(s) && s[j] >= '0' && s[j] <= '9' {
				return true
			}
		}
	}
	return false
}

// expandMini parses one mini-notation sequence into events spanning [0,1).
func expandMini(src string, kind string) ([]Event, error) {
	tokens, err := tokenizeMini(src)
	if err != nil {
		return nil, err
	}
	flat := flattenTokens(tokens, 1)
	if len(flat) == 0 {
		return nil, nil
	}
	// equal division of cycle among leaves (Tidal-like for simple seq)
	n := 0
	for _, t := range flat {
		n += t.reps
	}
	if n == 0 {
		return nil, nil
	}
	step := 1.0 / float64(n)
	var evs []Event
	i := 0
	for _, t := range flat {
		for r := 0; r < t.reps; r++ {
			at := float64(i) * step
			i++
			if t.rest || t.sound == "~" || t.sound == "-" {
				continue
			}
			ev := Event{
				At: at, Dur: step * 0.9, Kind: kind, Sound: t.sound, Vel: 100, Source: kind,
			}
			if kind == "note" {
				ev.MIDI = NoteToMIDI(t.sound)
			} else {
				ev.MIDI = DrumToMIDI(t.sound)
				ev.Kind = "drum"
			}
			if ev.MIDI > 0 {
				evs = append(evs, ev)
			}
		}
	}
	return evs, nil
}

type mtoken struct {
	sound string
	rest  bool
	reps  int
	// for groups we expand before flatten
	group []mtoken
	alt   [][]mtoken // <a b>
	altI  int
}

func tokenizeMini(src string) ([]mtoken, error) {
	src = strings.TrimSpace(src)
	var out []mtoken
	i := 0
	runes := []rune(src)
	for i < len(runes) {
		for i < len(runes) && unicode.IsSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			break
		}
		switch runes[i] {
		case '[':
			end := findMatching(runes, i, '[', ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed [")
			}
			inner, err := tokenizeMini(string(runes[i+1 : end]))
			if err != nil {
				return nil, err
			}
			tok := mtoken{group: inner, reps: 1}
			i = end + 1
			tok.reps, i = parseReps(runes, i)
			out = append(out, tok)
		case '<':
			end := findMatching(runes, i, '<', '>')
			if end < 0 {
				return nil, fmt.Errorf("unclosed <")
			}
			// split by space into alternatives (simple)
			parts := strings.Fields(string(runes[i+1 : end]))
			var alts [][]mtoken
			for _, p := range parts {
				alts = append(alts, []mtoken{{sound: p, reps: 1}})
			}
			tok := mtoken{alt: alts, reps: 1}
			i = end + 1
			tok.reps, i = parseReps(runes, i)
			out = append(out, tok)
		case '~', '-':
			tok := mtoken{rest: true, sound: "~", reps: 1}
			i++
			tok.reps, i = parseReps(runes, i)
			out = append(out, tok)
		case ',', '|':
			i++ // treated at higher level
		default:
			// read word
			j := i
			for j < len(runes) && !unicode.IsSpace(runes[j]) && runes[j] != '[' && runes[j] != ']' && runes[j] != '<' && runes[j] != '>' && runes[j] != ',' && runes[j] != '*' {
				j++
			}
			word := string(runes[i:j])
			i = j
			// strip trailing punctuation
			word = strings.Trim(word, `"'`)
			if word == "" {
				continue
			}
			tok := mtoken{sound: word, reps: 1}
			tok.reps, i = parseReps(runes, i)
			out = append(out, tok)
		}
	}
	return out, nil
}

func parseReps(runes []rune, i int) (int, int) {
	reps := 1
	if i < len(runes) && runes[i] == '*' {
		i++
		j := i
		for j < len(runes) && unicode.IsDigit(runes[j]) {
			j++
		}
		if j > i {
			if n, err := strconv.Atoi(string(runes[i:j])); err == nil && n > 0 {
				reps = n
			}
			i = j
		}
	}
	// !n = repeat n times (strudel) — treat like *
	if i < len(runes) && runes[i] == '!' {
		i++
		j := i
		for j < len(runes) && unicode.IsDigit(runes[j]) {
			j++
		}
		if j > i {
			if n, err := strconv.Atoi(string(runes[i:j])); err == nil && n > 0 {
				reps = n
			}
			i = j
		}
	}
	return reps, i
}

func flattenTokens(toks []mtoken, outerReps int) []mtoken {
	var out []mtoken
	for r := 0; r < outerReps; r++ {
		for _, t := range toks {
			if len(t.group) > 0 {
				out = append(out, flattenTokens(t.group, t.reps)...)
				continue
			}
			if len(t.alt) > 0 {
				// pick first alt for static compile; engine can rotate per cycle
				pick := t.alt[0]
				if len(pick) > 0 {
					for _, p := range pick {
						p.reps = t.reps
						out = append(out, p)
					}
				}
				continue
			}
			out = append(out, t)
		}
	}
	return out
}

// --- helpers ---

func findMatching(runes []rune, open int, l, r rune) int {
	depth := 0
	for i := open; i < len(runes); i++ {
		if runes[i] == l {
			depth++
		} else if runes[i] == r {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func extractParen(s string, from int) string {
	// find first ( after from
	i := strings.Index(s[from:], "(")
	if i < 0 {
		return s
	}
	i += from
	depth := 0
	for j := i; j < len(s); j++ {
		if s[j] == '(' {
			depth++
		} else if s[j] == ')' {
			depth--
			if depth == 0 {
				return s[i+1 : j]
			}
		}
	}
	return s[i+1:]
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '(', '[', '<':
			depth++
		case ')', ']', '>':
			if depth > 0 {
				depth--
			}
		}
		if c == sep && depth == 0 {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func findCalls(src string) []string {
	var out []string
	low := strings.ToLower(src)
	for _, name := range []string{"s(", "sound(", "note(", "n("} {
		idx := 0
		for {
			p := strings.Index(low[idx:], name)
			if p < 0 {
				break
			}
			p += idx
			// find matching )
			depth := 0
			end := -1
			for j := p; j < len(src); j++ {
				if src[j] == '(' {
					depth++
				} else if src[j] == ')' {
					depth--
					if depth == 0 {
						end = j
						break
					}
				}
			}
			if end > p {
				out = append(out, src[p:end+1])
				idx = end + 1
			} else {
				break
			}
		}
	}
	return out
}

func reFind(s, pat string) string {
	// minimal without regexp package for simple capture — use strings
	// setcps(0.5)
	if strings.Contains(pat, "setcps") {
		i := strings.Index(strings.ToLower(s), "setcps")
		if i < 0 {
			return ""
		}
		return numberInParen(s[i:])
	}
	if strings.Contains(pat, "bpm") {
		i := strings.Index(strings.ToLower(s), "bpm")
		if i < 0 {
			return ""
		}
		return numberInParen(s[i:])
	}
	return ""
}

func numberInParen(s string) string {
	a := strings.Index(s, "(")
	b := strings.Index(s, ")")
	if a < 0 || b <= a {
		return ""
	}
	return strings.TrimSpace(s[a+1 : b])
}

func reReplace(s, _ , repl string) string {
	// strip setcps(...) / bpm(...)
	for _, name := range []string{"setcps", "bpm"} {
		for {
			i := strings.Index(strings.ToLower(s), name)
			if i < 0 {
				break
			}
			a := strings.Index(s[i:], "(")
			if a < 0 {
				break
			}
			a += i
			b := strings.Index(s[a:], ")")
			if b < 0 {
				break
			}
			b += a
			s = s[:i] + repl + s[b+1:]
		}
	}
	return s
}

// DrumToMIDI GM percussion
func DrumToMIDI(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	// bank suffixes sd:2 → sd
	if i := strings.Index(n, ":"); i > 0 {
		n = n[:i]
	}
	switch n {
	case "bd", "kick", "k":
		return 36
	case "sd", "snare", "sn":
		return 38
	case "hh", "hat", "ch":
		return 42
	case "oh", "openhat":
		return 46
	case "cp", "clap":
		return 39
	case "rim":
		return 37
	case "rd", "ride":
		return 51
	case "crash", "cr":
		return 49
	case "lt":
		return 45
	case "mt":
		return 47
	case "ht":
		return 50
	case "cb", "cowbell":
		return 56
	default:
		return 0
	}
}

// NoteToMIDI parses c4, d#3, eb5, f#2
func NoteToMIDI(name string) int {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return 0
	}
	// already number?
	if v, err := strconv.Atoi(n); err == nil && v >= 0 && v <= 127 {
		return v
	}
	pc := map[byte]int{
		'c': 0, 'd': 2, 'e': 4, 'f': 5, 'g': 7, 'a': 9, 'b': 11,
	}
	if len(n) < 2 {
		return 0
	}
	base, ok := pc[n[0]]
	if !ok {
		return 0
	}
	i := 1
	if i < len(n) && n[i] == '#' {
		base++
		i++
	} else if i < len(n) && n[i] == 'b' {
		base--
		i++
	}
	oct := 4
	if i < len(n) {
		if o, err := strconv.Atoi(n[i:]); err == nil {
			oct = o
		}
	}
	m := (oct+1)*12 + base
	if m < 0 {
		m = 0
	}
	if m > 127 {
		m = 127
	}
	return m
}
