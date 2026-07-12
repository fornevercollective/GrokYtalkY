package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GrokConfig drives prompt-style Grok integration (xAI API or local notes backend).
type GrokConfig struct {
	APIKey     string
	BaseURL    string // default https://api.x.ai/v1
	Model      string
	BackendURL string // optional grok-notes backend http://127.0.0.1:3000
	System     string
	Offline    bool
}

func loadGrokConfig() GrokConfig {
	loadDotEnv(filepath.Join(os.Getenv("HOME"), ".grok", "env"))
	loadDotEnv(filepath.Join(os.Getenv("HOME"), ".grok", "grok-env.sh"))

	key := firstNonEmpty(
		os.Getenv("XAI_API_KEY"),
		os.Getenv("GROK_API_KEY"),
		os.Getenv("XAI_KEY"),
	)
	model := firstNonEmpty(os.Getenv("GROK_MODEL"), os.Getenv("XAI_MODEL"), "grok-3-mini")
	backend := firstNonEmpty(os.Getenv("GROK_CLI_URL"), "http://127.0.0.1:3000")
	base := firstNonEmpty(os.Getenv("XAI_BASE_URL"), "https://api.x.ai/v1")

	return GrokConfig{
		APIKey:     key,
		BaseURL:    strings.TrimRight(base, "/"),
		Model:      model,
		BackendURL: strings.TrimRight(backend, "/"),
		System: `You are Grok inside GrokYtalkY — a terminal live-coding walkie (Strudel/Qbpm patterns, hex video, MIDI).
Be concise, useful, and terminal-friendly. Prefer short code blocks for mini-notation like s("bd*4").
If the user shares a pattern or stream issue, give concrete GrokYtalkY commands (/watch, s(...), p, c).`,
		Offline: os.Getenv("GROK_OFFLINE") == "1",
	}
}

func (c GrokConfig) Available() bool {
	return c.APIKey != "" || c.BackendURL != ""
}

func (c GrokConfig) ModeLabel() string {
	if c.APIKey != "" {
		return "xAI " + c.Model
	}
	return "backend " + c.BackendURL
}

// GrokMessage is one chat turn.
type GrokMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AskGrok sends a prompt and returns the assistant reply (non-stream).
func AskGrok(cfg GrokConfig, history []GrokMessage, user string) (string, error) {
	if cfg.APIKey != "" && !cfg.Offline {
		return askXAI(cfg, history, user)
	}
	// local notes backend (launch-cli style)
	return askBackend(cfg, user)
}

func askXAI(cfg GrokConfig, history []GrokMessage, user string) (string, error) {
	msgs := make([]GrokMessage, 0, len(history)+2)
	if cfg.System != "" {
		msgs = append(msgs, GrokMessage{Role: "system", Content: cfg.System})
	}
	msgs = append(msgs, history...)
	msgs = append(msgs, GrokMessage{Role: "user", Content: user})

	body := map[string]any{
		"model":       cfg.Model,
		"messages":    msgs,
		"temperature": 0.7,
		"stream":      false,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("xAI %s: %s", res.Status, truncate(string(b), 300))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

func askBackend(cfg GrokConfig, user string) (string, error) {
	body := map[string]any{
		"message": user,
		"offline": cfg.Offline,
	}
	raw, _ := json.Marshal(body)
	url := cfg.BackendURL + "/api/ai/chat"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("backend %s: %w (start grok-notes-backend or set XAI_API_KEY)", url, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return "", fmt.Errorf("backend %s: %s", res.Status, truncate(string(b), 300))
	}
	// flexible parse
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		for _, k := range []string{"reply", "response", "content", "message", "text"} {
			if s, ok := m[k].(string); ok && s != "" {
				return strings.TrimSpace(s), nil
			}
		}
		// nested choices
		if ch, ok := m["choices"].([]any); ok && len(ch) > 0 {
			if c0, ok := ch[0].(map[string]any); ok {
				if msg, ok := c0["message"].(map[string]any); ok {
					if s, ok := msg["content"].(string); ok {
						return strings.TrimSpace(s), nil
					}
				}
			}
		}
	}
	return strings.TrimSpace(string(b)), nil
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// export FOO=bar or FOO=bar
		line = strings.TrimPrefix(line, "export ")
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		v = strings.Trim(v, `"'`)
		if os.Getenv(k) == "" && v != "" && !strings.Contains(v, "your_") {
			_ = os.Setenv(k, v)
		}
	}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
