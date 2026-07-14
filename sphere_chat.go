package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Sphere chat — Siri-like multi-turn Grok (SpaceXAI / xAI) for the glyph sphere.
// Key stays server-side (XAI_API_KEY). Browser uses POST /api/chat.

const sphereSystemPrompt = `You are the Glyph Sphere — a living LED dome assistant inside GrokYtalkY (think Siri, but visual and spatial).

You talk with people in the venue / browser / phone mesh. Be warm, clear, and concise — prefer 1–3 short spoken sentences so text-to-speech sounds natural. Avoid markdown lists and code fences unless the user asks for code.

You can reference the 16K addressable sphere venue: seats, stage, aisles, parking, screen, lights, phone flashlights, glyph casts. If someone asks you to change the room vibe, describe it vividly (color, pulse, wave) so the UI can react.

If multiple people talk, address them by name when you know it. Stay helpful and a little playful.`

// session store for short multi-turn continuity when client omits history
type sphereChatSession struct {
	mu       sync.Mutex
	history  []GrokMessage
	updated  time.Time
	maxTurns int
}

var (
	sphereSessionsMu sync.Mutex
	sphereSessions   = map[string]*sphereChatSession{}
)

func getSphereSession(id string) *sphereChatSession {
	if id == "" {
		id = "default"
	}
	sphereSessionsMu.Lock()
	defer sphereSessionsMu.Unlock()
	// prune stale
	now := time.Now()
	for k, s := range sphereSessions {
		if now.Sub(s.updated) > 2*time.Hour {
			delete(sphereSessions, k)
		}
	}
	s, ok := sphereSessions[id]
	if !ok {
		s = &sphereChatSession{maxTurns: 24, updated: now}
		sphereSessions[id] = s
	}
	s.updated = now
	return s
}

func (s *sphereChatSession) snapHistory() []GrokMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GrokMessage, len(s.history))
	copy(out, s.history)
	return out
}

func (s *sphereChatSession) appendTurn(user, assistant string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, GrokMessage{Role: "user", Content: user})
	s.history = append(s.history, GrokMessage{Role: "assistant", Content: assistant})
	// keep last N turns (2 messages each)
	maxMsg := s.maxTurns * 2
	if len(s.history) > maxMsg {
		s.history = s.history[len(s.history)-maxMsg:]
	}
	s.updated = time.Now()
}

func (s *sphereChatSession) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.updated = time.Now()
}

// HandleAPIChat serves GET/POST /api/chat and POST /api/chat/clear.
func HandleAPIChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cfg := loadGrokConfig()
	// Sphere-specific system prompt (keep model/key from env)
	cfg.System = sphereSystemPrompt

	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"available": cfg.APIKey != "" || cfg.BackendURL != "",
			"has_key":   cfg.APIKey != "",
			"model":     cfg.Model,
			"mode":      cfg.ModeLabel(),
			"offline":   cfg.Offline,
			"persona":   "glyph-sphere",
			"voice":     true,
			"hint":      "POST {\"message\":\"hello\"} · optional history[] · session",
		})
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "method not allowed"})
		return
	}

	// clear session
	if strings.HasSuffix(r.URL.Path, "/clear") {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		var j struct {
			Session string `json:"session"`
		}
		_ = json.Unmarshal(body, &j)
		getSphereSession(j.Session).clear()
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cleared": true})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 256<<10))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad body"})
		return
	}
	var req struct {
		Message string `json:"message"`
		Text    string `json:"text"` // alias
		Session string `json:"session"`
		From    string `json:"from"`
		// client-supplied history overrides session when non-empty
		History []GrokMessage `json:"history"`
		// if true, do not persist to server session
		Ephemeral bool `json:"ephemeral"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "invalid JSON"})
		return
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(req.Text)
	}
	if msg == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "missing message"})
		return
	}
	if len(msg) > 8000 {
		msg = msg[:8000]
	}

	// name hint for multi-person
	if from := strings.TrimSpace(req.From); from != "" && from != "sphere" {
		msg = "[" + from + "]: " + msg
	}

	var hist []GrokMessage
	sess := getSphereSession(req.Session)
	if len(req.History) > 0 {
		// take last 24 client messages
		hist = req.History
		if len(hist) > 48 {
			hist = hist[len(hist)-48:]
		}
	} else {
		hist = sess.snapHistory()
	}

	if !cfg.Available() || (cfg.APIKey == "" && cfg.Offline) {
		// graceful offline reply so UI still works
		reply := offlineSphereReply(msg)
		if !req.Ephemeral {
			sess.appendTurn(msg, reply)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"reply":  reply,
			"via":    "offline",
			"model":  "offline",
			"hint":   "set XAI_API_KEY for live Grok (SpaceXAI)",
		})
		return
	}

	start := time.Now()
	reply, err := AskGrok(cfg, hist, msg)
	if err != nil {
		// Credits / network / model errors: still keep the conversation alive
		fb := offlineSphereReply(msg)
		if strings.Contains(strings.ToLower(err.Error()), "credit") ||
			strings.Contains(strings.ToLower(err.Error()), "spending") ||
			strings.Contains(err.Error(), "403") {
			fb = "I'd love to keep talking with full Grok, but this hub's xAI credits are spent. Top up at console.x.ai — meanwhile I'm in local Sphere mode. " + shortLocalAck(msg)
		}
		if !req.Ephemeral {
			sess.appendTurn(msg, fb)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"reply":   fb,
			"model":   "fallback",
			"via":     "fallback",
			"warning": err.Error(),
			"ms":      time.Since(start).Milliseconds(),
			"session": firstNonEmpty(req.Session, "default"),
		})
		return
	}
	if !req.Ephemeral {
		sess.appendTurn(msg, reply)
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"reply":   reply,
		"model":   cfg.Model,
		"via":     cfg.ModeLabel(),
		"ms":      time.Since(start).Milliseconds(),
		"session": firstNonEmpty(req.Session, "default"),
	})
}

func shortLocalAck(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "who"):
		return "I'm still the Glyph Sphere LED dome."
	case strings.Contains(low, "light") || strings.Contains(low, "color"):
		return "Watch the dome — blue for listen, amber for think, green when I speak."
	case strings.Contains(low, "hello") || strings.Contains(low, "hi"):
		return "Hello from the venue."
	default:
		return "Say hello, ask who I am, or talk about lights and seats."
	}
}

func offlineSphereReply(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "hello") || strings.Contains(low, "hi ") || low == "hi":
		return "Hey — I'm the Glyph Sphere. Set XAI_API_KEY on the hub for full Grok chat; I can still greet you offline."
	case strings.Contains(low, "who are you") || strings.Contains(low, "what are you"):
		return "I'm the living LED dome in GrokYtalkY — your room's visual assistant. Connect XAI_API_KEY and we can really talk."
	case strings.Contains(low, "light") || strings.Contains(low, "color"):
		return "I'd pulse the dome for you — live replies need XAI_API_KEY on gy serve."
	default:
		return "I'm here, but running offline. Export XAI_API_KEY and restart gy serve for full back-and-forth chat."
	}
}
