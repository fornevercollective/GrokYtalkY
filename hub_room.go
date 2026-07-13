package main

import (
	"os"
	"strconv"
	"strings"
)

// Default mesh room when client omits room= (backward compatible flat hub).
const DefaultMeshRoom = "global"

// NormalizeMeshRoom cleans a room id for tenancy keys.
// Empty → global. Allows a-z 0-9 . _ - only; lowercased.
func NormalizeMeshRoom(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "*" || s == "default" {
		return DefaultMeshRoom
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '/':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return DefaultMeshRoom
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// RoomMaxPeers soft capacity per room (0 = unlimited).
// Env: GY_ROOM_MAX (default 48).
func RoomMaxPeers() int {
	v := strings.TrimSpace(os.Getenv("GY_ROOM_MAX"))
	if v == "" {
		return 48
	}
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "unlimited") {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 48
	}
	return n
}

// RoomListEntry is one row for GET /api/rooms.
type RoomListEntry struct {
	ID         string `json:"id"`
	Peers      int    `json:"peers"`
	ProgramSeq uint32 `json:"program_seq,omitempty"`
	Conductor  string `json:"conductor,omitempty"`
	Mode       string `json:"mode,omitempty"`
	HasProgram bool   `json:"has_program"`
}

// programSeqFromMesh extracts seq from stored type:program envelope.
func programMetaFromMesh(pgm map[string]any) (seq uint32, conductor, mode string, ok bool) {
	if pgm == nil {
		return 0, "", "", false
	}
	bus, okp := ParseProgramBus(pgm)
	if !okp {
		// flat seq
		if v, ok := pgm["seq"].(float64); ok {
			return uint32(v), "", "", true
		}
		return 0, "", "", false
	}
	return bus.Seq, bus.Conductor, bus.Mode, true
}
