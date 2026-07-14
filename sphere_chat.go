package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
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

	// After xAI credit/403 failures, stick to local replies for a while so the
	// UI doesn't feel stuck waiting on a dead cloud key every turn.
	sphereAIFailMu   sync.Mutex
	sphereAIFailUntil time.Time
	sphereAIFailMsg   string
)

func sphereForceLocal() bool {
	v := strings.TrimSpace(os.Getenv("GY_CHAT_LOCAL"))
	if v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "on") {
		return true
	}
	if v := strings.TrimSpace(os.Getenv("GROK_OFFLINE")); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	sphereAIFailMu.Lock()
	defer sphereAIFailMu.Unlock()
	return time.Now().Before(sphereAIFailUntil)
}

func sphereMarkAIFail(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	// sticky for credit / auth style failures
	if strings.Contains(low, "credit") || strings.Contains(low, "spending") ||
		strings.Contains(low, "403") || strings.Contains(low, "401") ||
		strings.Contains(low, "permission-denied") {
		sphereAIFailMu.Lock()
		sphereAIFailUntil = time.Now().Add(30 * time.Minute)
		sphereAIFailMsg = msg
		sphereAIFailMu.Unlock()
	}
}

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

	// Local-first when forced, offline, or sticky after recent xAI credit failure
	if sphereForceLocal() || !cfg.Available() || cfg.Offline || cfg.APIKey == "" {
		reply := localSphereReply(msg, hist)
		if !req.Ephemeral {
			sess.appendTurn(msg, reply)
		}
		sphereAIFailMu.Lock()
		warn := sphereAIFailMsg
		sphereAIFailMu.Unlock()
		out := map[string]any{
			"ok":      true,
			"reply":   reply,
			"via":     "local",
			"model":   "local",
			"session": firstNonEmpty(req.Session, "default"),
			"hint":    "local Sphere brain · set GY_CHAT_LOCAL=0 and top up XAI_API_KEY for full Grok",
		}
		if warn != "" {
			out["warning"] = warn
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	start := time.Now()
	reply, err := AskGrok(cfg, hist, msg)
	if err != nil {
		sphereMarkAIFail(err)
		fb := localSphereReply(msg, hist)
		if strings.Contains(strings.ToLower(err.Error()), "credit") ||
			strings.Contains(strings.ToLower(err.Error()), "spending") ||
			strings.Contains(err.Error(), "403") {
			fb = "Switching to local Sphere mode (xAI credits). " + fb
		}
		if !req.Ephemeral {
			sess.appendTurn(msg, fb)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"reply":   fb,
			"model":   "local",
			"via":     "local",
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

// localSphereReply is a small rule-based brain so the sphere always chats
// without waiting on the network (used when xAI is down or GY_CHAT_LOCAL=1).
func localSphereReply(msg string, hist []GrokMessage) string {
	low := strings.ToLower(strings.TrimSpace(msg))
	// strip [name]: prefix from multi-person
	if i := strings.Index(low, "]:"); i > 0 && i < 40 {
		low = strings.TrimSpace(low[i+2:])
	}

	switch {
	case low == "" || low == "ping":
		return "I'm online — local Sphere brain. Tap the mic or type anytime."
	case strings.Contains(low, "hello") || strings.Contains(low, "hi ") || low == "hi" || strings.Contains(low, "hey"):
		return "Hey — I'm the Glyph Sphere. Local mode is up. Ask me about seats, lights, or who I am."
	case strings.Contains(low, "who are you") || strings.Contains(low, "what are you") || low == "who are you?":
		return "I'm the living LED dome in GrokYtalkY — venue assistant for seats, stage, lights, and glyph casts."
	case strings.Contains(low, "how are you"):
		return "Lit and listening. Blue when I hear you, amber when I think, green when I speak."
	case strings.Contains(low, "light") || strings.Contains(low, "color") || strings.Contains(low, "wave"):
		return "Watch the dome — wave modes are cascade, azimuth, spiral, latitude. Lights panel is bottom-right."
	case strings.Contains(low, "seat") || strings.Contains(low, "section") || strings.Contains(low, "venue"):
		return "About twenty thousand seats on this Bloch³ map. Click the dome to pick a seat or LED; bulk activate sections from the bar."
	case strings.Contains(low, "phone") || strings.Contains(low, "cast"):
		return "Open phone.html on the same Wi‑Fi, or scan /api/lan/qr. Phones cast glyphs onto seats and the screen."
	case strings.Contains(low, "news") || strings.Contains(low, "live"):
		return "Live News is at /livenews.html — Resolve live pulls YouTube through the hub play proxy."
	case strings.Contains(low, "help") || low == "?":
		return "Try: who are you · lights · seats · phone cast · or just chat. Mic listens; Continuous keeps going."
	case strings.Contains(low, "thank"):
		return "Anytime — the dome's got you."
	case strings.Contains(low, "bye") || strings.Contains(low, "goodnight"):
		return "Catch you later. I'll keep the venue warm."
	case strings.Contains(low, "credit") || strings.Contains(low, "grok") || strings.Contains(low, "api"):
		return "Full Grok needs XAI_API_KEY with credits. Local mode stays free and instant — top up console.x.ai when you want cloud Grok."
	default:
		snippet := strings.TrimSpace(msg)
		if len(snippet) > 80 {
			snippet = snippet[:77] + "…"
		}
		n := len(hist) / 2
		if n > 0 {
			return "Got it — \"" + snippet + "\". I'm on local brain (turn " + itoaLocal(n+1) + "). Ask about lights, seats, or say help."
		}
		return "Got it — \"" + snippet + "\". Local Sphere is listening. Say help for ideas."
	}
}

func itoaLocal(n int) string {
	if n <= 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
