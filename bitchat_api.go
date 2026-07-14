package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HandleBitChatAPI serves:
//
//	GET  /api/bitchat              status snapshot
//	POST /api/bitchat/ingress      native adapter → hub mesh
//	GET  /api/bitchat/egress       hub → native adapter (drain or peek)
//	POST /api/bitchat/send         convenience chat inject (site/CLI)
//	POST /api/bitchat/sim          simulate BLE peer (dev)
func HandleBitChatAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/bitchat" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(BitChat().Snapshot())
		return

	case strings.HasSuffix(path, "/ingress") && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		var env BitChatEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			// also accept {messages:[…]}
			var batch struct {
				Messages []BitChatEnvelope `json:"messages"`
			}
			if err2 := json.Unmarshal(body, &batch); err2 != nil || len(batch.Messages) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid JSON envelope"})
				return
			}
			var n int
			var last error
			for _, m := range batch.Messages {
				if err := BitChat().Ingress(m); err != nil {
					last = err
				} else {
					n++
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": n > 0, "ingested": n, "error": errStr(last)})
			return
		}
		if err := BitChat().Ingress(env); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": env.ID, "from": env.From})
		return

	case strings.HasSuffix(path, "/egress") && r.Method == http.MethodGet:
		n := 32
		if v := r.URL.Query().Get("n"); v != "" {
			if i, err := strconv.Atoi(v); err == nil && i > 0 && i <= 256 {
				n = i
			}
		}
		peek := r.URL.Query().Get("peek") == "1"
		var msgs []BitChatEnvelope
		if peek {
			msgs = BitChat().PeekEgress(n)
		} else {
			msgs = BitChat().DrainEgress(n)
		}
		if msgs == nil {
			msgs = []BitChatEnvelope{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"messages": msgs,
			"n":        len(msgs),
			"peek":     peek,
		})
		return

	case strings.HasSuffix(path, "/send") && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 32<<10))
		var req struct {
			Text      string `json:"text"`
			From      string `json:"from"`
			Room      string `json:"room"`
			Channel   string `json:"channel"`
			Transport string `json:"transport"`
			Dual      bool   `json:"dual"` // also enqueue egress to BLE
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		from := strings.TrimSpace(req.From)
		if from == "" {
			from = "hub"
		}
		env := BitChatEnvelope{
			Type:      "chat",
			Text:      strings.TrimSpace(req.Text),
			From:      from,
			Room:      req.Room,
			Channel:   req.Channel,
			Transport: firstNonEmpty(req.Transport, "wifi-hub"),
			T:         time.Now().UnixMilli(),
		}
		// Inject into mesh as hub-origin chat (wifi) with dual-path meta
		env.Meta = map[string]any{"via": "hub", "dual": req.Dual}
		// Fanout as normal chat + bitchat-chat
		if err := BitChat().Ingress(BitChatEnvelope{
			Type:      "chat",
			Text:      env.Text,
			From:      env.From,
			Room:      env.Room,
			Channel:   env.Channel,
			Transport: "bridge",
			T:         env.T,
			Meta:      map[string]any{"via": "bitchat", "origin": "api-send"},
		}); err != nil {
			// if bitchat disabled, still try raw — report error
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if req.Dual {
			BitChat().EnqueueEgress(env)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "from": env.From, "text": env.Text})
		return

	case strings.HasSuffix(path, "/sim") && r.Method == http.MethodPost:
		body, _ := io.ReadAll(io.LimitReader(r.Body, 16<<10))
		var req struct {
			Text string `json:"text"`
			From string `json:"from"`
			Room string `json:"room"`
			N    int    `json:"n"` // presence only count
		}
		_ = json.Unmarshal(body, &req)
		from := strings.TrimSpace(req.From)
		if from == "" {
			from = "alice"
		}
		text := strings.TrimSpace(req.Text)
		if text == "" {
			text = "hello from simulated BLE mesh"
		}
		env := BitChatEnvelope{
			Type:      "chat",
			Text:      text,
			From:      from,
			Room:      req.Room,
			Transport: "sim",
			Channel:   "mesh#bluetooth",
			T:         time.Now().UnixMilli(),
			Meta:      map[string]any{"via": "bitchat", "sim": true},
		}
		if err := BitChat().Ingress(env); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		// optional presence swarm
		if req.N > 0 {
			if req.N > 12 {
				req.N = 12
			}
			for i := 0; i < req.N; i++ {
				_ = BitChat().Ingress(BitChatEnvelope{
					Type:      "presence",
					From:      "sim-peer-" + strconv.Itoa(i+1),
					Room:      env.Room,
					Transport: "sim",
					Channel:   "mesh#bluetooth",
					T:         time.Now().UnixMilli(),
				})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "sim": true, "from": "bt:" + from})
		return
	}

	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    false,
		"error": "unknown bitchat path",
		"hint":  "GET /api/bitchat · POST /ingress · GET /egress · POST /send · POST /sim",
	})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
