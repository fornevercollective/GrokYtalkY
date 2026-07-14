package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HDRI quick probe store — filmmaker hurdle hop.
// Browser stitches slot wedges (L2·L1·C·R1·R2); hub holds last probe + fans mesh.

type hdriProbe struct {
	Slots []string `json:"slots"`
	W     int      `json:"w"`
	H     int      `json:"h"`
	JPEG  string   `json:"jpeg,omitempty"`  // data URL or raw b64
	Strip string   `json:"strip,omitempty"` // subject contact strip
	T     int64    `json:"t"`
	From  string   `json:"from,omitempty"`
}

var (
	hdriMu   sync.Mutex
	hdriLast *hdriProbe
	hdriN    int64
)

// HandleHDRIAPI:
//
//	GET  /api/hdri           status + last probe meta (no huge jpeg by default)
//	GET  /api/hdri/probe     last probe including jpeg/strip
//	POST /api/hdri/probe     store + optional mesh fan-out
func HandleHDRIAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/hdri" && r.Method == http.MethodGet:
		hdriMu.Lock()
		n := hdriN
		last := hdriLast
		hdriMu.Unlock()
		meta := map[string]any{
			"ok":      true,
			"probes":  n,
			"has":     last != nil,
			"slots":   []string{"L2", "L1", "C", "R1", "R2"},
			"note":    "quick slot-wedge equirect + subject strip — not multi-bracket Debevec",
			"cli":     "gy hdri doctor",
			"site":    "GrokGlyph · HDRI button after cam",
			"export":  "PNG equirect + contact strip from browser",
		}
		if last != nil {
			meta["last"] = map[string]any{
				"t":     last.T,
				"w":     last.W,
				"h":     last.H,
				"slots": last.Slots,
				"from":  last.From,
			}
		}
		_ = json.NewEncoder(w).Encode(meta)
		return

	case strings.HasSuffix(path, "/probe") && r.Method == http.MethodGet:
		hdriMu.Lock()
		last := hdriLast
		hdriMu.Unlock()
		if last == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "no probe yet"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "probe": last})
		return

	case strings.HasSuffix(path, "/probe") && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 12<<20))
		var req hdriProbe
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		if req.T == 0 {
			req.T = time.Now().UnixMilli()
		}
		if req.From == "" {
			req.From = "hdri"
		}
		hdriMu.Lock()
		hdriLast = &req
		hdriN++
		hdriMu.Unlock()

		// Mesh fan-out for sphere / peers
		if bus := BitChat(); bus != nil {
			bus.mu.Lock()
			h := bus.hub
			bus.mu.Unlock()
			if h != nil {
				msg := map[string]any{
					"type":  "hdri-probe",
					"from":  req.From,
					"slots": req.Slots,
					"w":     req.W,
					"h":     req.H,
					"t":     req.T,
					"fmt":   "jpeg",
				}
				jpeg := req.JPEG
				if i := strings.Index(jpeg, ","); i >= 0 {
					msg["b64"] = jpeg[i+1:]
				} else if jpeg != "" {
					msg["b64"] = jpeg
				}
				h.broadcastRoom("global", nil, mustJSON(msg))
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    true,
			"t":     req.T,
			"slots": req.Slots,
			"w":     req.W,
			"h":     req.H,
		})
		return
	}

	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "use GET /api/hdri or POST /api/hdri/probe"})
}

// FormatHDRIDoctor for gy hdri / doctor.
func FormatHDRIDoctor() string {
	hdriMu.Lock()
	n := hdriN
	last := hdriLast
	hdriMu.Unlock()
	var b strings.Builder
	b.WriteString("hdri (filmmaker hurdle hop · quick probe)\n")
	b.WriteString("  mode      slot-wedge equirect + subject strip (single-EV tonemap)\n")
	b.WriteString("  slots     L2 · L1 · C(laptop) · R1 · R2\n")
	b.WriteString(fmt.Sprintf("  probes    %d stored on hub\n", n))
	if last != nil {
		b.WriteString(fmt.Sprintf("  last      %dx%d · slots %v · t %d\n", last.W, last.H, last.Slots, last.T))
	} else {
		b.WriteString("  last      none — open GrokGlyph · cam · HDRI\n")
	}
	b.WriteString("  site      GrokGlyph HDRI button · sphere maps probe on dome\n")
	b.WriteString("  api       GET /api/hdri · POST /api/hdri/probe\n")
	b.WriteString("  note      not multi-bracket Debevec / Hugin — lighting vibe + multi-angle board\n")
	return b.String()
}
