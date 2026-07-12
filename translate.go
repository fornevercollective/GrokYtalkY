package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TranslateConfig drives whisper-cli live STT / translation (emprcl audio path pairing).
type TranslateConfig struct {
	Enabled  bool
	Model    string // path to ggml model
	Lang     string // source language code, or "auto"
	ToEN     bool   // whisper -tr translate to English
	Bin      string // whisper-cli path
	Threads  int
}

func defaultTranslateConfig() TranslateConfig {
	model := firstExisting(
		os.Getenv("GROKYTALKY_WHISPER_MODEL"),
		filepath.Join(os.Getenv("HOME"), "models/audio/whisper/ggml/ggml-small.en.bin"),
		filepath.Join(os.Getenv("HOME"), "models/whisper/ggml-base.en.bin"),
		filepath.Join(os.Getenv("HOME"), "models/audio/whisper/ggml/ggml-large-v3-turbo-q5_0.bin"),
	)
	bin := firstExisting(
		os.Getenv("GROKYTALKY_WHISPER"),
		"whisper-cli",
		"/usr/local/bin/whisper-cli",
		"/opt/homebrew/bin/whisper-cli",
	)
	return TranslateConfig{
		Enabled: model != "" && bin != "",
		Model:   model,
		Lang:    "auto",
		ToEN:    true,
		Bin:     bin,
		Threads: 4,
	}
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		// allow bare command name if in PATH
		if !strings.Contains(p, string(os.PathSeparator)) {
			if _, err := exec.LookPath(p); err == nil {
				return p
			}
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Transcript is the result of live STT/translate on a PTT clip.
type Transcript struct {
	Text       string `json:"text"`
	Translated string `json:"translated,omitempty"`
	Lang       string `json:"lang,omitempty"`
	Raw        string `json:"-"`
}

// TranscribeFile runs whisper-cli on a wav/mp3 path.
// When cfg.ToEN is true, uses -tr (translate to English) — live translation mode.
func TranscribeFile(cfg TranslateConfig, audioPath string) (Transcript, error) {
	if cfg.Bin == "" || cfg.Model == "" {
		return Transcript{}, fmt.Errorf("whisper not configured (set model path)")
	}
	if _, err := os.Stat(audioPath); err != nil {
		return Transcript{}, err
	}

	args := []string{
		"-m", cfg.Model,
		"-t", fmt.Sprintf("%d", max(1, cfg.Threads)),
	}
	// language
	if cfg.Lang != "" && cfg.Lang != "auto" {
		args = append(args, "-l", cfg.Lang)
	} else {
		args = append(args, "-l", "auto")
	}
	if cfg.ToEN {
		args = append(args, "-tr") // live translate → English
	}
	args = append(args, "-oj") // write JSON sidecar
	args = append(args, audioPath)

	cmd := exec.Command(cfg.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String() + "\n" + stderr.String()

	// Prefer JSON sidecar
	jsonPath := audioPath + ".json"
	// whisper-cli often writes basename.json without full path suffix patterns
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	candidates := []string{jsonPath, base + ".json", audioPath + ".json"}
	var tr Transcript
	for _, jp := range candidates {
		if b, e := os.ReadFile(jp); e == nil {
			tr = parseWhisperJSON(b)
			_ = os.Remove(jp)
			if tr.Text != "" {
				return tr, nil
			}
		}
	}

	// Fallback: parse stdout lines (last non-meta line)
	text := extractWhisperText(out)
	if text == "" && err != nil {
		return Transcript{}, fmt.Errorf("whisper: %v\n%s", err, truncate(stderr.String(), 400))
	}
	tr.Text = text
	tr.Raw = out
	if cfg.ToEN {
		tr.Translated = text
	}
	return tr, nil
}

func parseWhisperJSON(b []byte) Transcript {
	// whisper.cpp json: { "transcription": [ {"text":"..."} ] } or { "text": "..." }
	var root map[string]any
	if json.Unmarshal(b, &root) != nil {
		return Transcript{Raw: string(b)}
	}
	if t, ok := root["text"].(string); ok && strings.TrimSpace(t) != "" {
		return Transcript{Text: strings.TrimSpace(t), Translated: strings.TrimSpace(t)}
	}
	if arr, ok := root["transcription"].([]any); ok {
		var parts []string
		for _, seg := range arr {
			if m, ok := seg.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, strings.TrimSpace(t))
				}
			}
		}
		joined := strings.TrimSpace(strings.Join(parts, " "))
		return Transcript{Text: joined, Translated: joined}
	}
	// some versions: {"transcription":"..."}
	if t, ok := root["transcription"].(string); ok {
		return Transcript{Text: strings.TrimSpace(t), Translated: strings.TrimSpace(t)}
	}
	return Transcript{Raw: string(b)}
}

func extractWhisperText(out string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// skip common log noise
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "whisper_") ||
			strings.HasPrefix(low, "system_info") ||
			strings.HasPrefix(low, "main:") ||
			strings.HasPrefix(low, "ggml_") ||
			strings.HasPrefix(low, "load_") ||
			strings.Contains(low, "loading model") ||
			strings.HasPrefix(line, "[") && strings.Contains(line, "-->") {
			// timestamp lines like [00:00:00.000 --> 00:00:01.000] text
			if i := strings.Index(line, "]"); i >= 0 && i+1 < len(line) {
				t := strings.TrimSpace(line[i+1:])
				if t != "" {
					lines = append(lines, t)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "whisper_print") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, " "))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
