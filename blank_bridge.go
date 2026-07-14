package main

import (
	"bytes"
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

// blank_bridge — resolve social/live URLs via blank's ingest API
// (TikTok live, watch pages) when blank is running.
//
// blank: ~/dev/blank · POST /api/ingest/resolve {"url":"…"}
// install: gy install blank  ·  ~/dev/blank/scripts/install-all.sh

const defaultBlankURL = "http://127.0.0.1:5173"

// BlankBaseURL returns blank origin. Empty when GY_BLANK=0.
// Default: http://127.0.0.1:5173 (used only when reachable).
func BlankBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("GY_BLANK")); v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
		return ""
	}
	if u := strings.TrimSpace(os.Getenv("GY_BLANK_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	return defaultBlankURL
}

// BlankRoot returns filesystem path to blank install.
func BlankRoot() string {
	if p := strings.TrimSpace(os.Getenv("GY_BLANK_PATH")); p != "" {
		return expandUserHome(p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, rel := range []string{"dev/blank", "Projects/blank"} {
			cand := filepath.Join(home, rel)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand
			}
		}
	}
	return expandUserHome("~/dev/blank")
}

func expandUserHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// BlankReachable pings blank with a short timeout.
func BlankReachable(base string) bool {
	if base == "" {
		return false
	}
	client := &http.Client{Timeout: 450 * time.Millisecond}
	resp, err := client.Get(base + "/")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode > 0 && resp.StatusCode < 500
}

type blankResolveResp struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
	PlayID     string `json:"playId"`
	PageURL    string `json:"pageUrl"`
	StreamURL  string `json:"streamUrl"`
	PlayPath   string `json:"playPath"`
	Title      string `json:"title"`
	StreamKind string `json:"streamKind"`
}

// ResolveViaBlank posts page URL to blank /api/ingest/resolve.
func ResolveViaBlank(pageURL string) (*ResolvedStream, error) {
	base := BlankBaseURL()
	if base == "" {
		return nil, fmt.Errorf("blank disabled (GY_BLANK=0)")
	}
	if !BlankReachable(base) {
		return nil, fmt.Errorf("blank not reachable at %s — cd %s && ./start.sh", base, BlankRoot())
	}
	pageURL = strings.TrimSpace(pageURL)
	if !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
		return nil, fmt.Errorf("blank resolve needs http(s) url")
	}
	body, _ := json.Marshal(map[string]any{"url": pageURL})
	client := &http.Client{Timeout: 95 * time.Second}
	req, err := http.NewRequest(http.MethodPost, base+"/api/ingest/resolve", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out blankResolveResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("blank resolve: bad json: %w", err)
	}
	if !out.OK || out.StreamURL == "" {
		msg := out.Error
		if msg == "" {
			msg = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("blank resolve: %s", msg)
	}
	r := &ResolvedStream{
		Input: pageURL,
		Title: out.Title,
		Video: out.StreamURL,
		Via:   "blank",
		Live:  strings.Contains(strings.ToLower(pageURL), "/live") || out.StreamKind == "hls",
	}
	// Prefer blank HLS proxy for browser/watch when stream is m3u8
	if out.PlayPath != "" && (out.StreamKind == "hls" || strings.Contains(out.StreamURL, ".m3u8")) {
		r.Video = base + out.PlayPath
		r.Via = "blank-proxy"
	}
	return r, nil
}

// FormatBlankDoctor multi-line status for gy doctor / install blank.
func FormatBlankDoctor() string {
	var b strings.Builder
	root := BlankRoot()
	base := BlankBaseURL()
	b.WriteString("blank (social · TikTok live resolve)\n")
	b.WriteString(fmt.Sprintf("  path      %s\n", root))
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		b.WriteString("  install   missing — gy install blank\n")
	} else {
		b.WriteString("  install   present\n")
		status := filepath.Join(root, "support", ".blank-install", "status.json")
		if _, err := os.Stat(status); err == nil {
			b.WriteString("  status    " + status + "\n")
		}
	}
	b.WriteString(fmt.Sprintf("  url       %s\n", base))
	if base != "" && BlankReachable(base) {
		b.WriteString("  server    up · POST /api/ingest/resolve\n")
	} else if base == "" {
		b.WriteString("  server    disabled (GY_BLANK=0)\n")
	} else {
		b.WriteString("  server    down · cd ~/dev/blank && ./start.sh\n")
	}
	b.WriteString("  social    tiktok:@user · /live first · yt-dlp · YTDLP_COOKIES optional\n")
	return b.String()
}

// InstallBlank runs blank scripts/install-all.sh.
func InstallBlank() error {
	root := BlankRoot()
	script := filepath.Join(root, "scripts", "install-all.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("blank install script missing at %s (expected blank at %s)", script, root)
	}
	cmd := exec.Command("bash", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = root
	cmd.Env = os.Environ()
	return cmd.Run()
}
