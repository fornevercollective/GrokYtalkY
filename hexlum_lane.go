package main

import (
	"encoding/base64"
	"fmt"
	"math"
	"time"
)

// Live hexlum lane — promote walkie/cast glyph grids onto the formal GYST hexlum path
// (forge · SFU · agent · venue · GrokGlyph) without changing program-bus authority.

// VburstGlyphToHexLumMesh maps a vburst-frame (with glyph[]) onto type:gyst kind:hexlum.
// Returns ok=false when no usable lattice is present (no authority side effects).
//
// Additive fan-out only: callers broadcast the original vburst unchanged, then this
// mesh envelope for hex-lane consumers. Preview-first / zero authority risk.
func VburstGlyphToHexLumMesh(msg map[string]any) (map[string]any, bool) {
	if msg == nil {
		return nil, false
	}
	// client already dual-published hexlum — don't double-fan
	if skip, _ := msg["hex_lane"].(bool); skip {
		return nil, false
	}
	if v, _ := msg["hex_lane"].(string); v == "1" || v == "true" || v == "skip" {
		return nil, false
	}

	data, n, ok := extractGlyphLattice(msg)
	if !ok || n < 4 || len(data) < n*n {
		return nil, false
	}
	// trim / pad to exact n×n
	if len(data) > n*n {
		data = data[:n*n]
	}
	if len(data) < n*n {
		pad := make([]byte, n*n)
		copy(pad, data)
		data = pad
	}

	from, _ := msg["from"].(string)
	var seq uint32
	switch v := msg["seq"].(type) {
	case float64:
		seq = uint32(v)
	case int:
		seq = uint32(v)
	case uint32:
		seq = v
	default:
		// derive from t when present
		if t, ok := msg["t"].(float64); ok && t > 0 {
			seq = uint32(int64(t) & 0xffffffff)
		} else {
			seq = uint32(time.Now().UnixMilli() & 0xffffffff)
		}
	}

	p := PacketFromHexLum(data, n, seq)
	if t, ok := msg["t"].(float64); ok && t > 0 {
		p.TimeMS = uint64(t)
	} else {
		p.TimeMS = uint64(time.Now().UnixMilli())
	}
	out := PacketToMesh(p, from)
	out["lane"] = LaneHex
	out["via"] = "vburst-promote" // telemetry; not authority
	if id, ok := msg["id"]; ok {
		out["id"] = id
	}
	// keep mark if forge-stamped walkie ever carries it
	if mk, ok := msg["mark"]; ok {
		out["mark"] = mk
	}
	return out, true
}

// extractGlyphLattice pulls N×N luminance bytes from vburst-frame / web cast shapes.
func extractGlyphLattice(msg map[string]any) (data []byte, n int, ok bool) {
	// preferred: glyph: number[]
	if arr, okA := msg["glyph"].([]any); okA && len(arr) > 0 {
		data = intsAnyToBytes(arr)
		n = glyphNFromMsg(msg, len(data))
		return data, n, len(data) > 0
	}
	// already int slice (non-JSON path)
	if arr, okA := msg["glyph"].([]int); okA && len(arr) > 0 {
		data = make([]byte, len(arr))
		for i, v := range arr {
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			data[i] = byte(v)
		}
		n = glyphNFromMsg(msg, len(data))
		return data, n, true
	}
	// alternate: data[] with kind hint
	if kind, _ := msg["kind"].(string); kind == "hexlum" || kind == "hex" {
		if arr, okA := msg["data"].([]any); okA && len(arr) > 0 {
			data = intsAnyToBytes(arr)
			n = glyphNFromMsg(msg, len(data))
			return data, n, len(data) > 0
		}
	}
	// raw b64 lattice without jpeg (rare)
	if fmt, _ := msg["fmt"].(string); fmt == "hexlum" || fmt == "raw" {
		if b64, _ := msg["b64"].(string); b64 != "" {
			raw, err := base64.StdEncoding.DecodeString(b64)
			if err == nil && len(raw) >= 16 {
				n = glyphNFromMsg(msg, len(raw))
				return raw, n, true
			}
		}
	}
	return nil, 0, false
}

func intsAnyToBytes(arr []any) []byte {
	out := make([]byte, len(arr))
	for i, x := range arr {
		switch v := x.(type) {
		case float64:
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			out[i] = byte(v)
		case int:
			if v < 0 {
				v = 0
			}
			if v > 255 {
				v = 255
			}
			out[i] = byte(v)
		}
	}
	return out
}

func glyphNFromMsg(msg map[string]any, dataLen int) int {
	if v, ok := msg["glyphN"].(float64); ok && v >= 4 {
		return int(v)
	}
	if v, ok := msg["glyphN"].(int); ok && v >= 4 {
		return v
	}
	if v, ok := msg["n"].(float64); ok && v >= 4 {
		return int(v)
	}
	if w, ok := msg["w"].(float64); ok {
		if h, ok2 := msg["h"].(float64); ok2 && int(w) == int(h) && w >= 4 && w <= 49 {
			// only if w*h matches lattice (not jpeg upscale)
			if dataLen == int(w)*int(h) {
				return int(w)
			}
		}
	}
	// nearest square root
	n := int(math.Sqrt(float64(dataLen)))
	if n < 4 {
		n = 25
	}
	if n*n > dataLen {
		// step down
		for n > 4 && n*n > dataLen {
			n--
		}
	}
	return n
}

// FormatHexLumLaneLine is a one-liner for doctor / logs.
func FormatHexLumLaneLine(n int, from string) string {
	if from == "" {
		from = "—"
	}
	return fmt.Sprintf("hexlum lane · %d×%d · from %s · gyst kind=hexlum", n, n, from)
}
