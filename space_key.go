package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Stream key auto-pull for X Media Studio / Periscope RTMP.
// Keys are never logged in full; never sent on public mesh.

// PullKeyOpts controls optional sources (clipboard is opt-in).
type PullKeyOpts struct {
	// Clipboard try pbpaste / xclip / wl-paste
	Clipboard bool
	// File explicit path override
	File string
	// URL explicit key URL override
	URL string
	// Timeout for HTTP pull
	Timeout time.Duration
}

// DefaultStreamKeyPath ~/.config/grokytalky/x-stream-key
func DefaultStreamKeyPath() string {
	if p := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_FILE")); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return "x-stream-key"
	}
	return filepath.Join(home, ".config", "grokytalky", "x-stream-key")
}

// DefaultStreamKeyJSONPath ~/.config/grokytalky/x-rtmp.json
func DefaultStreamKeyJSONPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return "x-rtmp.json"
	}
	return filepath.Join(home, ".config", "grokytalky", "x-rtmp.json")
}

// PullStreamKey tries sources in order until a non-empty key is found.
// Order: flag file → env → env file → default file → JSON → URL → clipboard.
func PullStreamKey(opts PullKeyOpts) (source, key string, err error) {
	try := func(src, v string) bool {
		v = sanitizeStreamKey(v)
		if v != "" {
			source, key = src, v
			return true
		}
		return false
	}

	if opts.File != "" {
		if b, e := os.ReadFile(opts.File); e == nil && try("file:"+opts.File, string(b)) {
			return source, key, nil
		}
	}
	if try("env:GY_X_STREAM_KEY", os.Getenv("GY_X_STREAM_KEY")) {
		return source, key, nil
	}
	if p := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_FILE")); p != "" {
		if b, e := os.ReadFile(p); e == nil && try("file:"+p, string(b)) {
			return source, key, nil
		}
	}
	if b, e := os.ReadFile(DefaultStreamKeyPath()); e == nil && try("file:"+DefaultStreamKeyPath(), string(b)) {
		return source, key, nil
	}
	// JSON config
	for _, jp := range []string{DefaultStreamKeyJSONPath(), "x-rtmp.json"} {
		if k, base, sec, e := readStreamKeyJSON(jp); e == nil && k != "" {
			if base != "" {
				Spaces().mu.Lock()
				Spaces().RTMP.BaseURL = base
				if sec != nil {
					Spaces().RTMP.Secure = *sec
				}
				Spaces().mu.Unlock()
			}
			return "json:" + jp, sanitizeStreamKey(k), nil
		}
	}
	url := strings.TrimSpace(opts.URL)
	if url == "" {
		url = strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_URL"))
	}
	if url != "" {
		if k, e := pullKeyHTTP(url, opts.Timeout); e == nil && try("url", k) {
			return source, key, nil
		}
	}
	if opts.Clipboard {
		if k, e := readClipboardKey(); e == nil && try("clipboard", k) {
			return source, key, nil
		}
	}
	return "", "", fmt.Errorf("stream key available when ready — set GY_X_STREAM_KEY, write %s, or gy space key --pull", DefaultStreamKeyPath())
}

func sanitizeStreamKey(s string) string {
	s = strings.TrimSpace(s)
	// first line only (file may have comments)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// strip common prefixes
	s = strings.TrimPrefix(s, "stream_key=")
	s = strings.TrimPrefix(s, "STREAM_KEY=")
	s = strings.TrimSpace(s)
	// reject obvious non-keys
	if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") {
		return ""
	}
	if len(s) < 4 {
		return ""
	}
	return s
}

type streamKeyJSON struct {
	StreamKey string `json:"stream_key"`
	Key       string `json:"key"`
	BaseURL   string `json:"base_url"`
	URL       string `json:"url"`
	Secure    *bool  `json:"secure"`
	RTMPS     *bool  `json:"rtmps"`
}

func readStreamKeyJSON(path string) (key, base string, secure *bool, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", nil, err
	}
	var j streamKeyJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return "", "", nil, err
	}
	key = j.StreamKey
	if key == "" {
		key = j.Key
	}
	base = j.BaseURL
	if base == "" {
		base = j.URL
	}
	secure = j.Secure
	if secure == nil {
		secure = j.RTMPS
	}
	return key, base, secure, nil
}

func pullKeyHTTP(rawURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	// optional bearer for private vaults
	if t := strings.TrimSpace(os.Getenv("GY_SPACE_TOKEN")); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("key url status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	// try JSON first
	var j streamKeyJSON
	if json.Unmarshal(body, &j) == nil {
		k := j.StreamKey
		if k == "" {
			k = j.Key
		}
		if k != "" {
			return k, nil
		}
	}
	return string(body), nil
}

func readClipboardKey() (string, error) {
	// macOS
	if p, err := exec.LookPath("pbpaste"); err == nil {
		out, err := exec.Command(p).Output()
		if err == nil {
			return string(out), nil
		}
	}
	// wayland
	if p, err := exec.LookPath("wl-paste"); err == nil {
		out, err := exec.Command(p, "-n").Output()
		if err == nil {
			return string(out), nil
		}
	}
	// x11
	if p, err := exec.LookPath("xclip"); err == nil {
		out, err := exec.Command(p, "-selection", "clipboard", "-o").Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("no clipboard tool")
}

// WriteStreamKeyFile writes key to default path (0600).
func WriteStreamKeyFile(key string) (string, error) {
	key = sanitizeStreamKey(key)
	if key == "" {
		return "", fmt.Errorf("empty key")
	}
	path := DefaultStreamKeyPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// WatchStreamKeyFile polls path until a key appears, then calls onReady once
// (and again if key changes). Blocks until ctx cancelled if using long-lived —
// here: until interrupt via empty loop with sleep; caller can kill process.
func WatchStreamKeyFile(path string, every time.Duration, onReady func(src, key string)) error {
	if every <= 0 {
		every = 2 * time.Second
	}
	if path == "" {
		path = DefaultStreamKeyPath()
	}
	last := ""
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			k := sanitizeStreamKey(string(b))
			if k != "" && k != last {
				last = k
				if onReady != nil {
					onReady("file:"+path, k)
				}
			}
		}
		// also re-check env
		if k := sanitizeStreamKey(os.Getenv("GY_X_STREAM_KEY")); k != "" && k != last {
			last = k
			if onReady != nil {
				onReady("env:GY_X_STREAM_KEY", k)
			}
		}
		time.Sleep(every)
	}
}

// SpaceToken returns operator token for /api/space/key (GY_SPACE_TOKEN).
func SpaceToken() string {
	return strings.TrimSpace(os.Getenv("GY_SPACE_TOKEN"))
}
