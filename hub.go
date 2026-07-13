package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Hub is the mesh WebSocket relay (hexcast-compatible frames + walkie msgs).
type Hub struct {
	mu      sync.Mutex
	peers   map[*websocket.Conn]*peerMeta
	server  *http.Server
	addr    string
	quiet   bool
	program map[string]any // last type:program bus for late joiners
}

type peerMeta struct {
	ID      string
	Nick    string
	Role    string
	Talking bool
	Cap     CapProfile // capability handshake (lanes, glyph N, bp)
	HasCap  bool
}

func NewHub(addr string, quiet bool, staticDir string) *Hub {
	h := &Hub{
		peers: make(map[*websocket.Conn]*peerMeta),
		addr:  addr,
		quiet: quiet,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			h.handleWS(w, r)
			return
		}
		// static optional (browser walkie still served if present)
		if staticDir != "" {
			p := filepath.Join(staticDir, filepath.Clean("/"+r.URL.Path))
			if r.URL.Path == "/" {
				p = filepath.Join(staticDir, "walkie.html")
			}
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				http.ServeFile(w, r, p)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("GrokYtalkY hub — connect with: grokytalky\n"))
	})
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"peers": h.peerList()})
	})
	h.server = &http.Server{Addr: addr, Handler: mux}
	return h
}

func (h *Hub) ListenAndServe() error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return err
	}
	if !h.quiet {
		log.Printf("GrokYtalkY hub on %s", ln.Addr())
	}
	return h.server.Serve(ln)
}

func (h *Hub) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.server.Shutdown(ctx)
}

func (h *Hub) peerList() []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, 0, len(h.peers))
	for _, m := range h.peers {
		row := map[string]any{
			"id": m.ID, "nick": m.Nick, "role": m.Role, "talking": m.Talking,
		}
		if m.HasCap {
			row["cap"] = m.Cap.MeshMap()
		}
		out = append(out, row)
	}
	return out
}

func (h *Hub) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		OriginPatterns:     []string{"*"},
	})
	if err != nil {
		return
	}
	q := r.URL.Query()
	meta := &peerMeta{
		ID:   randID(3),
		Nick: q.Get("nick"),
		Role: q.Get("role"),
	}
	if meta.Nick == "" {
		meta.Nick = "peer"
	}
	if meta.Role == "" {
		meta.Role = "peer"
	}
	h.mu.Lock()
	h.peers[c] = meta
	n := len(h.peers)
	h.mu.Unlock()
	if !h.quiet {
		log.Printf("+ %s (%s) n=%d", meta.Nick, meta.ID, n)
	}

	ctx := r.Context()
	_ = writeJSON(ctx, c, map[string]any{"type": "hello", "id": meta.ID, "nick": meta.Nick, "version": Version})
	_ = writeJSON(ctx, c, map[string]any{"type": "roster", "peers": h.peerList()})
	// late join: push last program bus so venue/agents sync on-air state
	h.mu.Lock()
	pgm := h.program
	h.mu.Unlock()
	if pgm != nil {
		_ = writeJSON(ctx, c, pgm)
	}
	h.broadcast(c, mustJSON(map[string]any{"type": "join", "id": meta.ID, "nick": meta.Nick, "role": meta.Role}))

	defer func() {
		h.mu.Lock()
		delete(h.peers, c)
		h.mu.Unlock()
		h.broadcast(c, mustJSON(map[string]any{"type": "leave", "id": meta.ID, "nick": meta.Nick}))
		_ = c.Close(websocket.StatusNormalClosure, "")
		if !h.quiet {
			log.Printf("- %s", meta.Nick)
		}
	}()

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		h.route(c, meta, data)
	}
}

func (h *Hub) route(from *websocket.Conn, meta *peerMeta, data []byte) {
	// hexcast frame: JSON\nbase64
	if i := indexByte(data, '\n'); i > 0 && data[0] == '{' {
		var hdr map[string]any
		if json.Unmarshal(data[:i], &hdr) == nil {
			if t, _ := hdr["type"].(string); t == "frame" {
				h.broadcast(from, data)
				return
			}
		}
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		h.broadcast(from, data)
		return
	}
	typ, _ := msg["type"].(string)
	switch typ {
	case "join", "hello":
		if n, ok := msg["nick"].(string); ok && n != "" {
			meta.Nick = n
		}
		if r, ok := msg["role"].(string); ok && r != "" {
			meta.Role = r
		}
		if cap, ok := ParseCapFromMesh(msg); ok {
			meta.Cap = cap
			meta.HasCap = true
			if meta.Role == "" || meta.Role == "peer" {
				meta.Role = cap.Role
			}
		}
		// announce peer join with cap so others can adapt glyph N / lanes
		joinOut := map[string]any{
			"type": "join", "id": meta.ID, "nick": meta.Nick, "role": meta.Role,
		}
		if meta.HasCap {
			joinOut["cap"] = meta.Cap.MeshMap()
		}
		h.broadcast(from, mustJSON(joinOut))
		h.broadcast(from, mustJSON(map[string]any{"type": "roster", "peers": h.peerList()}))
		_ = writeJSON(context.Background(), from, map[string]any{"type": "roster", "peers": h.peerList()})
	case "cap":
		// capability update (resize / late advertise)
		if cap, ok := ParseCapFromMesh(msg); ok {
			meta.Cap = cap
			meta.HasCap = true
		}
		out := map[string]any{
			"type": "cap", "from": coalesce(msg["from"], meta.Nick), "id": meta.ID,
		}
		if meta.HasCap {
			out["cap"] = meta.Cap.MeshMap()
		}
		h.broadcast(from, mustJSON(out))
	case "chat":
		out := map[string]any{
			"type": "chat",
			"text": msg["text"],
			"from": coalesce(msg["from"], meta.Nick),
			"id":   meta.ID,
			"t":    time.Now().UnixMilli(),
		}
		h.broadcast(from, mustJSON(out))
	case "ptt":
		st, _ := msg["state"].(string)
		meta.Talking = st == "down"
		h.broadcast(from, mustJSON(map[string]any{
			"type": "ptt", "state": st,
			"from": coalesce(msg["from"], meta.Nick), "id": meta.ID,
		}))
	case "vburst-start", "vburst-end", "vburst-frame", "vburst-audio":
		// Siri-sized video burst walkie — relay as-is (glyph grid + jpeg)
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		if typ == "vburst-start" {
			meta.Talking = true
		}
		if typ == "vburst-end" {
			meta.Talking = false
		}
		h.broadcast(from, mustJSON(msg))
		// live hexlum lane: additive promote glyph[] → type:gyst kind:hexlum
		// (SFU · agent · venue · GrokGlyph). Skip when client sets hex_lane (dual-pub).
		if typ == "vburst-frame" {
			if hexMsg, ok := VburstGlyphToHexLumMesh(msg); ok {
				h.broadcast(from, mustJSON(hexMsg))
			}
		}
	case "gyst", "gyst-frame":
		// live headless binary/hex stream packets (rgb24|hexlum|jpeg)
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "gyst"
		// tag formal hex lane when kind is hexlum (telemetry only)
		if k, _ := msg["kind"].(string); k == "hexlum" || k == "hex" {
			if _, has := msg["lane"]; !has {
				msg["lane"] = LaneHex
			}
		}
		h.broadcast(from, mustJSON(msg))
	case "program":
		// conductor program bus — store for late joiners, fan-out
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "program"
		h.mu.Lock()
		h.program = msg
		h.mu.Unlock()
		h.broadcast(from, mustJSON(msg))
	case "program-caption", "caption-set":
		// caption-only merge — does not change PGM/PVW/mode (conductor take authority)
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		cap, ok := ParseCaptionFromMesh(msg)
		if !ok {
			// empty = clear caption on bus
			cap = CaptionPayload{}
		}
		h.mu.Lock()
		stored := h.program
		h.mu.Unlock()
		next := ApplyProgramCaption(stored, coalesce(msg["from"], meta.Nick), cap)
		h.mu.Lock()
		h.program = next
		h.mu.Unlock()
		h.broadcast(from, mustJSON(next))
	case "caption":
		// informational caption event (UI / GrokGlyph) — no program authority
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "caption"
		h.broadcast(from, mustJSON(msg))
	case "audio":
		h.broadcast(from, data)
	default:
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		h.broadcast(from, mustJSON(msg))
	}
}

func (h *Hub) broadcast(except *websocket.Conn, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for c := range h.peers {
		if c == except {
			continue
		}
		_ = c.Write(ctx, websocket.MessageText, data)
	}
}

func writeJSON(ctx context.Context, c *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(ctx, websocket.MessageText, b)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func coalesce(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func randID(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n*2)
	// cheap: time-based
	t := time.Now().UnixNano()
	for i := range b {
		b[i] = hex[(int(t)+i*17)%16]
		t >>= 3
	}
	return string(b)
}

// DecodeFrameB64 helper for clients.
func DecodeFrameB64(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}
