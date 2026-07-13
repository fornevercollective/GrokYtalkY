package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Hub is the mesh WebSocket relay with server-side room tenancy.
// Peers only receive traffic for their room; each room has its own program bus.
type Hub struct {
	mu       sync.Mutex
	peers    map[*websocket.Conn]*peerMeta
	programs map[string]map[string]any // room → last type:program
	server   *http.Server
	addr     string
	quiet    bool
	// same-WiFi phone discovery
	lan    LanInfo
	lanUDP *LanDiscoverer
}

type peerMeta struct {
	ID      string
	Nick    string
	Role    string
	Room    string // normalized mesh room
	Talking bool
	Cap     CapProfile
	HasCap  bool
}

func NewHub(addr string, quiet bool, staticDir string) *Hub {
	h := &Hub{
		peers:    make(map[*websocket.Conn]*peerMeta),
		programs: make(map[string]map[string]any),
		addr:     addr,
		quiet:    quiet,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			h.handleWS(w, r)
			return
		}
		if staticDir != "" {
			p := filepath.Join(staticDir, filepath.Clean("/"+r.URL.Path))
			if r.URL.Path == "/" {
				// prefer site index / grokglyph when present
				for _, cand := range []string{"index.html", "grokglyph.html", "walkie.html"} {
					try := filepath.Join(staticDir, cand)
					if st, err := os.Stat(try); err == nil && !st.IsDir() {
						p = try
						break
					}
				}
			}
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				http.ServeFile(w, r, p)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("GrokYtalkY hub — rooms + program bus\n  gy · ws://HOST/?nick=…&room=global\n  GET /api/rooms · /api/peers · /api/lan · /api/social · /api/space\n  phone cast: /phone.html (same Wi‑Fi)\n"))
	})
	// X Spaces stage + stream asset (public; never leaks stream key)
	mux.HandleFunc("/api/space", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(Spaces().PublicAPISnapshot())
	})
	// Stream key only with GY_SPACE_TOKEN (operator vault / auto-pull remote)
	mux.HandleFunc("/api/space/key", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		tok := SpaceToken()
		got := strings.TrimSpace(r.URL.Query().Get("token"))
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			got = strings.TrimSpace(got)
		}
		if tok == "" || got == "" || got != tok {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":  "unauthorized — set GY_SPACE_TOKEN and pass ?token= or Authorization: Bearer",
				"ready":  Spaces().Snapshot().RTMP.Ready,
				"status": Spaces().Snapshot().RTMP.Status,
			})
			return
		}
		if r.Method == http.MethodPost {
			// set key: POST body plain or JSON
			body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<10))
			k := strings.TrimSpace(string(body))
			var j map[string]any
			if json.Unmarshal(body, &j) == nil {
				if v, ok := j["stream_key"].(string); ok {
					k = v
				} else if v, ok := j["key"].(string); ok {
					k = v
				}
			}
			Spaces().SetStreamKeyFrom(k, "api")
			if k != "" {
				_, _ = WriteStreamKeyFile(k)
			}
		}
		// auto-pull attempt if empty
		snap := Spaces().Snapshot()
		if !snap.RTMP.Ready {
			if src, key, err := PullStreamKey(PullKeyOpts{}); err == nil {
				Spaces().SetStreamKeyFrom(key, src)
				snap = Spaces().Snapshot()
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ready":      snap.RTMP.Ready,
			"stream_key": snap.RTMP.StreamKey,
			"base_url":   snap.RTMP.BaseRTMPURL(),
			"secure":     snap.RTMP.Secure,
			"source":     snap.KeySource,
			"status":     snap.RTMP.Status,
		})
	})
	mux.HandleFunc("/api/peers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		room := NormalizeMeshRoom(r.URL.Query().Get("room"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"room":  room,
			"peers": h.peerList(room),
		})
	})
	mux.HandleFunc("/api/rooms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": h.roomList()})
	})
	// Social handle resolve: live/broadcast first, lazy secondary list for GrokGlyph/lab.
	// GET /api/social?q=@user | twitch:name | yt:@channel
	mux.HandleFunc("/api/social", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			q = strings.TrimSpace(r.URL.Query().Get("handle"))
		}
		if q == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "missing q — use ?q=@user or ?q=twitch:name",
			})
			return
		}
		// optional platform pin
		if plat := strings.TrimSpace(r.URL.Query().Get("platform")); plat != "" && ParseSocialQuery(q) == nil {
			q = plat + ":" + strings.TrimPrefix(q, "@")
		}
		src, err := ResolveMediaTimeout(q, 100*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "q": q})
			return
		}
		// mobile double-stack size for GrokGlyph clients
		gn := 25
		mw, mh := MobileGlyphStackSize(gn)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"q":        q,
			"input":    src.Input,
			"title":    src.Title,
			"video":    src.Video,
			"audio":    src.Audio,
			"via":      src.Via,
			"live":     src.Live,
			"platform": src.Platform,
			"handle":   src.Handle,
			"mobile":   src.Mobile,
			"lazy":     src.Lazy,
			"stack": map[string]any{
				"glyph_n": gn,
				"w":       mw,
				"h":       mh,
				"mode":    "double",
			},
		})
	})
	// Same-WiFi phone → terminal: join URLs + discovery metadata
	mux.HandleFunc("/api/lan", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		info := BuildLanInfo(ParseHubPort(h.addr), NormalizeMeshRoom(os.Getenv("GY_ROOM")))
		h.mu.Lock()
		h.lan = info
		h.mu.Unlock()
		_ = json.NewEncoder(w).Encode(info)
	})
	// lightweight phone cast landing (redirect if static missing)
	mux.HandleFunc("/phone", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/phone.html", http.StatusFound)
	})
	h.server = &http.Server{Addr: addr, Handler: mux}
	return h
}

func (h *Hub) ListenAndServe() error {
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return err
	}
	port := ParseHubPort(ln.Addr().String())
	info := BuildLanInfo(port, NormalizeMeshRoom(os.Getenv("GY_ROOM")))
	h.mu.Lock()
	h.lan = info
	h.mu.Unlock()
	// UDP LAN discovery for native phone apps (GYWHO1 → GYHUB1)
	if d, err := StartLanDiscoverer(info); err == nil {
		h.lanUDP = d
	} else if !h.quiet {
		log.Printf("hub · LAN UDP discover off: %v", err)
	}
	if !h.quiet {
		log.Printf("GrokYtalkY hub on %s (rooms · program-per-room · max/room=%d)", ln.Addr(), RoomMaxPeers())
		for _, line := range strings.Split(strings.TrimRight(FormatLanBanner(info), "\n"), "\n") {
			log.Print(line)
		}
	}
	return h.server.Serve(ln)
}

func (h *Hub) Close() error {
	if h.lanUDP != nil {
		_ = h.lanUDP.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.server.Shutdown(ctx)
}

// LanInfo returns the last advertised LAN join info (for TUI /lan).
func (h *Hub) LanInfo() LanInfo {
	if h == nil {
		return BuildLanInfo(9876, "")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lan.Port == 0 {
		return BuildLanInfo(ParseHubPort(h.addr), "")
	}
	return h.lan
}

// peerList returns peers in room (empty room = all peers).
func (h *Hub) peerList(room string) []map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]any, 0, len(h.peers))
	for _, m := range h.peers {
		if room != "" && m.Room != room {
			continue
		}
		row := map[string]any{
			"id": m.ID, "nick": m.Nick, "role": m.Role, "room": m.Room, "talking": m.Talking,
		}
		if m.HasCap {
			row["cap"] = m.Cap.MeshMap()
		}
		out = append(out, row)
	}
	return out
}

func (h *Hub) roomList() []RoomListEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	counts := map[string]int{}
	for _, m := range h.peers {
		counts[m.Room]++
	}
	// include rooms that only have stored program
	for room := range h.programs {
		if _, ok := counts[room]; !ok {
			counts[room] = 0
		}
	}
	if len(counts) == 0 {
		counts[DefaultMeshRoom] = 0
	}
	out := make([]RoomListEntry, 0, len(counts))
	for id, n := range counts {
		e := RoomListEntry{ID: id, Peers: n}
		if pgm := h.programs[id]; pgm != nil {
			e.HasProgram = true
			if seq, cond, mode, ok := programMetaFromMesh(pgm); ok {
				e.ProgramSeq = seq
				e.Conductor = cond
				e.Mode = mode
			}
		}
		out = append(out, e)
	}
	return out
}

func (h *Hub) roomPeerCount(room string) int {
	n := 0
	for _, m := range h.peers {
		if m.Room == room {
			n++
		}
	}
	return n
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
		Room: NormalizeMeshRoom(q.Get("room")),
	}
	if meta.Nick == "" {
		meta.Nick = "peer"
	}
	if meta.Role == "" {
		meta.Role = "peer"
	}

	// soft capacity — refuse join if room full (bridges exempt)
	max := RoomMaxPeers()
	h.mu.Lock()
	if max > 0 && meta.Role != "bridge" && h.roomPeerCount(meta.Room) >= max {
		h.mu.Unlock()
		_ = writeJSON(r.Context(), c, map[string]any{
			"type": "error", "code": "room_full",
			"room": meta.Room, "max": max,
			"text": "room at capacity",
		})
		_ = c.Close(websocket.StatusPolicyViolation, "room full")
		return
	}
	h.peers[c] = meta
	nRoom := h.roomPeerCount(meta.Room)
	nAll := len(h.peers)
	pgm := h.programs[meta.Room]
	h.mu.Unlock()

	if !h.quiet {
		log.Printf("+ %s room=%s (%s) room_n=%d total=%d", meta.Nick, meta.Room, meta.ID, nRoom, nAll)
	}

	ctx := r.Context()
	_ = writeJSON(ctx, c, map[string]any{
		"type": "hello", "id": meta.ID, "nick": meta.Nick, "room": meta.Room, "version": Version,
	})
	_ = writeJSON(ctx, c, map[string]any{"type": "roster", "room": meta.Room, "peers": h.peerList(meta.Room)})
	if pgm != nil {
		_ = writeJSON(ctx, c, pgm)
	}
	h.broadcastRoom(meta.Room, c, mustJSON(map[string]any{
		"type": "join", "id": meta.ID, "nick": meta.Nick, "role": meta.Role, "room": meta.Room,
	}))

	defer func() {
		room := meta.Room
		h.mu.Lock()
		delete(h.peers, c)
		h.mu.Unlock()
		h.broadcastRoom(room, c, mustJSON(map[string]any{
			"type": "leave", "id": meta.ID, "nick": meta.Nick, "room": room,
		}))
		_ = c.Close(websocket.StatusNormalClosure, "")
		if !h.quiet {
			log.Printf("- %s room=%s", meta.Nick, room)
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
				if _, ok := hdr["room"]; !ok {
					hdr["room"] = meta.Room
				}
				// re-encode header with room if we mutated — keep original body for simplicity
				h.broadcastRoom(meta.Room, from, data)
				return
			}
		}
	}
	var msg map[string]any
	if err := json.Unmarshal(data, &msg); err != nil {
		h.broadcastRoom(meta.Room, from, data)
		return
	}
	typ, _ := msg["type"].(string)
	// stamp room on all fan-out messages
	if _, ok := msg["room"]; !ok {
		msg["room"] = meta.Room
	}

	switch typ {
	case "join", "hello":
		if n, ok := msg["nick"].(string); ok && n != "" {
			meta.Nick = n
		}
		if r, ok := msg["role"].(string); ok && r != "" {
			meta.Role = r
		}
		// room switch
		if r, ok := msg["room"].(string); ok && r != "" {
			h.setPeerRoom(from, meta, NormalizeMeshRoom(r))
		}
		if cap, ok := ParseCapFromMesh(msg); ok {
			meta.Cap = cap
			meta.HasCap = true
			if meta.Role == "" || meta.Role == "peer" {
				meta.Role = cap.Role
			}
		}
		joinOut := map[string]any{
			"type": "join", "id": meta.ID, "nick": meta.Nick, "role": meta.Role, "room": meta.Room,
		}
		if meta.HasCap {
			joinOut["cap"] = meta.Cap.MeshMap()
		}
		h.broadcastRoom(meta.Room, from, mustJSON(joinOut))
		roster := map[string]any{"type": "roster", "room": meta.Room, "peers": h.peerList(meta.Room)}
		h.broadcastRoom(meta.Room, from, mustJSON(roster))
		_ = writeJSON(context.Background(), from, roster)
		// late program for (possibly new) room
		h.mu.Lock()
		pgm := h.programs[meta.Room]
		h.mu.Unlock()
		if pgm != nil {
			_ = writeJSON(context.Background(), from, pgm)
		}
	case "cap":
		if cap, ok := ParseCapFromMesh(msg); ok {
			meta.Cap = cap
			meta.HasCap = true
		}
		out := map[string]any{
			"type": "cap", "from": coalesce(msg["from"], meta.Nick), "id": meta.ID, "room": meta.Room,
		}
		if meta.HasCap {
			out["cap"] = meta.Cap.MeshMap()
		}
		h.broadcastRoom(meta.Room, from, mustJSON(out))
	case "chat":
		out := map[string]any{
			"type": "chat",
			"text": msg["text"],
			"from": coalesce(msg["from"], meta.Nick),
			"id":   meta.ID,
			"room": meta.Room,
			"t":    time.Now().UnixMilli(),
		}
		h.broadcastRoom(meta.Room, from, mustJSON(out))
	case "ptt":
		st, _ := msg["state"].(string)
		meta.Talking = st == "down"
		h.broadcastRoom(meta.Room, from, mustJSON(map[string]any{
			"type": "ptt", "state": st,
			"from": coalesce(msg["from"], meta.Nick), "id": meta.ID, "room": meta.Room,
		}))
	case "vburst-start", "vburst-end", "vburst-frame", "vburst-audio":
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["room"] = meta.Room
		if typ == "vburst-start" {
			meta.Talking = true
		}
		if typ == "vburst-end" {
			meta.Talking = false
		}
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
		if typ == "vburst-frame" {
			if hexMsg, ok := VburstGlyphToHexLumMesh(msg); ok {
				hexMsg["room"] = meta.Room
				h.broadcastRoom(meta.Room, from, mustJSON(hexMsg))
			}
		}
	case "gyst", "gyst-frame":
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "gyst"
		msg["room"] = meta.Room
		if k, _ := msg["kind"].(string); k == "hexlum" || k == "hex" {
			if _, has := msg["lane"]; !has {
				msg["lane"] = LaneHex
			}
		}
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
	case "program":
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "program"
		msg["room"] = meta.Room
		h.mu.Lock()
		h.programs[meta.Room] = msg
		h.mu.Unlock()
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
	case "program-caption", "caption-set":
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		cap, ok := ParseCaptionFromMesh(msg)
		if !ok {
			cap = CaptionPayload{}
		}
		h.mu.Lock()
		stored := h.programs[meta.Room]
		h.mu.Unlock()
		next := ApplyProgramCaption(stored, coalesce(msg["from"], meta.Nick), cap)
		next["room"] = meta.Room
		h.mu.Lock()
		h.programs[meta.Room] = next
		h.mu.Unlock()
		h.broadcastRoom(meta.Room, from, mustJSON(next))
	case "caption":
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["type"] = "caption"
		msg["room"] = meta.Room
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
	case "mid-lane", "edge-hook":
		// edge mid-lane telemetry — room scoped (publishers / mid-lane bridge)
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["room"] = meta.Room
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
	case "audio":
		h.broadcastRoom(meta.Room, from, data)
	default:
		if _, ok := msg["from"]; !ok {
			msg["from"] = meta.Nick
		}
		msg["room"] = meta.Room
		h.broadcastRoom(meta.Room, from, mustJSON(msg))
	}
}

// setPeerRoom moves a peer between rooms and notifies both rosters.
func (h *Hub) setPeerRoom(c *websocket.Conn, meta *peerMeta, newRoom string) {
	newRoom = NormalizeMeshRoom(newRoom)
	if newRoom == meta.Room {
		return
	}
	old := meta.Room
	max := RoomMaxPeers()
	h.mu.Lock()
	if max > 0 && meta.Role != "bridge" && h.roomPeerCount(newRoom) >= max {
		h.mu.Unlock()
		_ = writeJSON(context.Background(), c, map[string]any{
			"type": "error", "code": "room_full", "room": newRoom, "max": max,
		})
		return
	}
	meta.Room = newRoom
	h.mu.Unlock()

	// leave old room
	h.broadcastRoom(old, c, mustJSON(map[string]any{
		"type": "leave", "id": meta.ID, "nick": meta.Nick, "room": old, "reason": "room_switch",
	}))
	// join new
	h.broadcastRoom(newRoom, c, mustJSON(map[string]any{
		"type": "join", "id": meta.ID, "nick": meta.Nick, "role": meta.Role, "room": newRoom,
	}))
	_ = writeJSON(context.Background(), c, map[string]any{
		"type": "roster", "room": newRoom, "peers": h.peerList(newRoom),
	})
	h.mu.Lock()
	pgm := h.programs[newRoom]
	h.mu.Unlock()
	if pgm != nil {
		_ = writeJSON(context.Background(), c, pgm)
	}
	if !h.quiet {
		log.Printf("~ %s room %s → %s", meta.Nick, old, newRoom)
	}
}

// broadcastRoom sends to all peers in room except the sender.
func (h *Hub) broadcastRoom(room string, except *websocket.Conn, data []byte) {
	room = NormalizeMeshRoom(room)
	h.mu.Lock()
	defer h.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for c, m := range h.peers {
		if c == except {
			continue
		}
		if m.Room != room {
			continue
		}
		_ = c.Write(ctx, websocket.MessageText, data)
	}
}

// broadcast is legacy all-peers fan-out (tests / rare); prefer broadcastRoom.
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
