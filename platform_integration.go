package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Platform integration — FFmpeg + Grok vision + Aito + SFU readiness (v1.76).
// Partner / capital / streaming-platform handoff surface.

// PlatformCheck is one readiness row.
type PlatformCheck struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Level   string `json:"level"` // required | recommended | optional
	Detail  string `json:"detail"`
	Hint    string `json:"hint,omitempty"`
}

// PlatformReadiness is the full integration snapshot.
type PlatformReadiness struct {
	Version   string          `json:"version"`
	At        time.Time       `json:"at"`
	Status    string          `json:"status"` // ready | partial | blocked
	Score     int             `json:"score"`  // 0–100
	Checks    []PlatformCheck `json:"checks"`
	Planes    map[string]any  `json:"planes"`
	Wins      []string        `json:"wins"`
	OutOfScope []string       `json:"out_of_scope"`
	EnvHints  []string        `json:"env_hints"`
	Contract  string          `json:"contract"` // path to JSON contract
}

// SamplePlatformReadiness probes local tools + buses for partner handoff.
func SamplePlatformReadiness() PlatformReadiness {
	r := PlatformReadiness{
		Version: Version,
		At:      time.Now().UTC(),
		Planes:  map[string]any{},
		Contract: "integrations/grok-stream-platform.json",
		Wins: []string{
			"v1.70 vision FFmpeg control plane (MEDIA)",
			"v1.71 Aito SAM / pose / gsplat / depth",
			"v1.72 SAM bbox → crop+retune",
			"v1.73 theme-vision plugin",
			"v1.74 SFU token + glyph DC bridge",
			"v1.75 Live News segment_top + pose",
			"v1.75.1 docs site nav consistency",
			"v1.76 platform readiness export",
			"v1.77 Live News → glyph-cast full-res wire",
			"v1.78 phone/film camera + lighting (aito-aligned)",
			"v1.79 phone quick connect (QR + one-tap hub+cam)",
			"v1.79.1 QR client-side MIT encoder (no Go QR dep)",
			"v1.79.2 Sphere Vegas Bloch³ seating map → phone cast pos",
			"v1.80 Sphere Glyph viewer (live seats, no qpu bells)",
			"v1.81 16K addressable venue + bulk section/chunk/zone cast",
			"v1.82 venue camera views + lighting + phone flashlights",
		},
		OutOfScope: []string{
			"auto-cast every livenews feed (manual pin/shuffle only)",
			"in-process TensorFlow / full gsplat viewer",
			"1k+ peers on gy-sfu (use CF mid-lane)",
		},
	}

	// ── FFmpeg ──
	ff := lookPathOK("ffmpeg")
	fp := lookPathOK("ffplay")
	yd := lookPathOK("yt-dlp")
	r.Checks = append(r.Checks,
		PlatformCheck{ID: "ffmpeg", OK: ff, Level: "required",
			Detail: boolDetail(ff, "ffmpeg on PATH", "ffmpeg missing"),
			Hint:   "gy install dependencies -y"},
		PlatformCheck{ID: "ffplay", OK: fp, Level: "recommended",
			Detail: boolDetail(fp, "ffplay on PATH", "ffplay missing (audio/PiP)"),
			Hint:   "brew install ffmpeg"},
		PlatformCheck{ID: "yt-dlp", OK: yd, Level: "recommended",
			Detail: boolDetail(yd, "yt-dlp on PATH", "yt-dlp missing (URL resolve)"),
			Hint:   "brew install yt-dlp"},
	)
	mh := Media().Health()
	r.Planes["media"] = map[string]any{
		"alive": mh.Alive, "max": mh.Max, "news": mh.NewsAlive, "news_max": mh.NewsMax,
		"drops": mh.Drops, "kills": mh.Kills,
	}
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "media_supervisor", OK: true, Level: "required",
		Detail: fmt.Sprintf("Media() alive %d/%d news %d/%d", mh.Alive, mh.Max, mh.NewsAlive, mh.NewsMax),
	})

	// ── Grok / vision ──
	gcfg := loadGrokConfig()
	keyOK := gcfg.APIKey != "" && !gcfg.Offline
	vis := Vision()
	vs := vis.Snapshot()
	r.Checks = append(r.Checks,
		PlatformCheck{ID: "xai_api_key", OK: keyOK, Level: "required",
			Detail: boolDetail(keyOK, "XAI_API_KEY set (multimodal)", "set XAI_API_KEY for live Grok vision"),
			Hint:   "export XAI_API_KEY=…"},
		PlatformCheck{ID: "vision_enabled", OK: vs.Enabled || envTruthy("GY_VISION"), Level: "recommended",
			Detail: fmt.Sprintf("vision enabled=%v primary=%s takes=%d", vs.Enabled, emptyDash(vs.Primary), vs.Takes),
			Hint:   "export GY_VISION=1"},
	)
	provOK := false
	provNames := []string{}
	for _, p := range vis.Registry().ListProviders() {
		provNames = append(provNames, p.Name())
		if p.Available() {
			provOK = true
		}
	}
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "vision_providers", OK: provOK, Level: "required",
		Detail: "providers: " + strings.Join(provNames, ", "),
		Hint:   "GY_VISION_OFFLINE=1 or XAI_API_KEY",
	})
	r.Planes["vision"] = map[string]any{
		"enabled": vs.Enabled, "primary": vs.Primary, "takes": vs.Takes,
		"theme": vs.LastTheme, "segment_n": vs.LastSegN, "pose_hands": vs.LastHands,
		"depth": vs.LastDepthB, "providers": provNames,
	}

	// ── Vision media control plane ──
	vm := VisionMedia().Snapshot()
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "vision_media", OK: vm.Enabled, Level: "required",
		Detail: fmt.Sprintf("FFmpeg control plane enabled=%v applied=%d dropped=%d encoded=%d",
			vm.Enabled, vm.Applied, vm.Dropped, vm.Encoded),
		Hint: "GY_VISION_MEDIA=1",
	})
	rt := LoadRetargetConfig()
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "vision_retarget", OK: rt.Enabled, Level: "recommended",
		Detail: fmt.Sprintf("SAM retarget enabled=%v pad=%.2f", rt.Enabled, rt.Pad),
		Hint:   "GY_VISION_RETARGET=1",
	})
	r.Planes["vision_media"] = map[string]any{
		"enabled": vm.Enabled, "applied": vm.Applied, "encoded": vm.Encoded,
		"retarget": rt.Enabled,
	}

	// ── theme-vision plugin ──
	tv := ThemeVision().Snapshot()
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "theme_vision_plugin", OK: tv.Enabled, Level: "recommended",
		Detail: fmt.Sprintf("theme-vision enabled=%v theme=%s takes=%d", tv.Enabled, emptyDash(tv.Theme), tv.Takes),
		Hint:   "/plugin on theme-vision",
	})

	// ── Aito ──
	aitoMock := envTruthy("GY_VISION_AITO_MOCK")
	aitoURL := strings.TrimSpace(os.Getenv("GY_VISION_AITO_URL"))
	if aitoURL == "" {
		aitoURL = "http://127.0.0.1:8766"
	}
	aitoCtx, aitoCancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	aitoUp := aitoMock || aitoHealth(aitoCtx)
	aitoCancel()
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "aito_sidecar", OK: aitoUp, Level: "optional",
		Detail: boolDetail(aitoUp,
			fmt.Sprintf("Aito reachable mock=%v url=%s", aitoMock, aitoURL),
			"Aito down — mock or start zipdepth/SAM sidecars"),
		Hint: "GY_VISION_AITO_MOCK=1 or GY_VISION_AITO_URL=…",
	})
	r.Planes["aito"] = map[string]any{"url": aitoURL, "mock": aitoMock, "up": aitoUp}

	// ── SFU ──
	sfuHost := envOr("GY_SFU_HOST", "127.0.0.1:9880")
	tokenSet := strings.TrimSpace(os.Getenv("GY_SFU_TOKEN")) != ""
	sfuHealth, sfuErr := ProbeSfuHealth(sfuHost, 800*time.Millisecond)
	sfuOK := sfuErr == nil
	r.Checks = append(r.Checks,
		PlatformCheck{ID: "sfu_token", OK: tokenSet, Level: "optional",
			Detail: boolDetail(tokenSet, "GY_SFU_TOKEN set", "mint: gy sfu-token --export"),
			Hint:   "gy sfu-token --export"},
		PlatformCheck{ID: "sfu_health", OK: sfuOK, Level: "optional",
			Detail: boolDetail(sfuOK, "gy-sfu /health ok", fmt.Sprintf("sfu down: %v", sfuErr)),
			Hint:   "make sfu-media && gy-sfu --token $GY_SFU_TOKEN"},
	)
	r.Planes["sfu"] = map[string]any{
		"host": sfuHost, "token_set": tokenSet, "up": sfuOK, "health": sfuHealth,
		"bridge_joined": bridgeLiveStats.joined.Load(),
		"glyph_out":     bridgeLiveStats.glyphOut.Load(),
	}

	// ── X / stream-x ──
	xKey := strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY")) != "" ||
		strings.TrimSpace(os.Getenv("GY_X_STREAM_KEY_FILE")) != ""
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "x_stream_key", OK: xKey, Level: "optional",
		Detail: boolDetail(xKey, "X stream key env/file present", "no GY_X_STREAM_KEY — RTMP later"),
		Hint:   "gy stream-x init · GY_X_STREAM_KEY",
	})

	// ── contract file ──
	contractPath := findPlatformContract()
	contractOK := contractPath != ""
	if contractOK {
		r.Contract = contractPath
	}
	r.Checks = append(r.Checks, PlatformCheck{
		ID: "platform_contract", OK: contractOK, Level: "recommended",
		Detail: boolDetail(contractOK, "integrations/grok-stream-platform.json", "contract file missing"),
	})

	// score
	req, reqOK, rec, recOK := 0, 0, 0, 0
	for _, c := range r.Checks {
		switch c.Level {
		case "required":
			req++
			if c.OK {
				reqOK++
			}
		case "recommended":
			rec++
			if c.OK {
				recOK++
			}
		}
	}
	// 70% weight required, 30% recommended
	score := 0
	if req > 0 {
		score += (reqOK * 70) / req
	} else {
		score += 70
	}
	if rec > 0 {
		score += (recOK * 30) / rec
	} else {
		score += 30
	}
	r.Score = score
	switch {
	case reqOK < req:
		r.Status = "blocked"
	case score >= 85:
		r.Status = "ready"
	default:
		r.Status = "partial"
	}

	r.EnvHints = []string{
		"XAI_API_KEY · GY_VISION=1 · GY_VISION_MEDIA=1 · GY_VISION_RETARGET=1",
		"GY_VISION_AITO_MOCK=1  or  GY_VISION_AITO_URL=http://127.0.0.1:8766",
		"GY_SFU_TOKEN=$(gy sfu-token) · gy sfu-bridge --token …",
		"GY_MEDIA_MAX · GY_NEWS_MAX · GY_HUB · GY_ROOM",
	}
	return r
}

func lookPathOK(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func boolDetail(ok bool, good, bad string) string {
	if ok {
		return good
	}
	return bad
}

// findPlatformContract locates integrations/grok-stream-platform.json.
func findPlatformContract() string {
	cands := []string{
		"integrations/grok-stream-platform.json",
		filepath.Join("..", "integrations", "grok-stream-platform.json"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, "integrations", "grok-stream-platform.json"),
			filepath.Join(dir, "..", "integrations", "grok-stream-platform.json"),
			filepath.Join(dir, "..", "share", "grokytalky", "integrations", "grok-stream-platform.json"),
		)
	}
	// walk up from cwd
	wd, _ := os.Getwd()
	for i := 0; i < 6 && wd != ""; i++ {
		cands = append(cands, filepath.Join(wd, "integrations", "grok-stream-platform.json"))
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(p); err == nil {
				return abs
			}
			return p
		}
	}
	return ""
}

// FormatPlatformDoctor multi-line for gy doctor platform.
func FormatPlatformDoctor(r PlatformReadiness) string {
	var b strings.Builder
	fmt.Fprintf(&b, "platform · status=%s score=%d/100 · gy %s\n", r.Status, r.Score, r.Version)
	fmt.Fprintf(&b, "  contract  %s\n", emptyDash(r.Contract))
	fmt.Fprintf(&b, "  purpose   FFmpeg + Grok vision + Aito + SFU → streaming platform handoff\n")
	b.WriteString("  checks\n")
	for _, c := range r.Checks {
		mark := "·"
		if c.OK {
			mark = "✓"
		} else if c.Level == "required" {
			mark = "✗"
		} else {
			mark = "○"
		}
		fmt.Fprintf(&b, "    %s %-22s  [%s] %s\n", mark, c.ID, c.Level, c.Detail)
		if !c.OK && c.Hint != "" {
			fmt.Fprintf(&b, "      → %s\n", c.Hint)
		}
	}
	b.WriteString("  wins\n")
	for _, w := range r.Wins {
		fmt.Fprintf(&b, "    · %s\n", w)
	}
	b.WriteString("  out_of_scope\n")
	for _, w := range r.OutOfScope {
		fmt.Fprintf(&b, "    · %s\n", w)
	}
	b.WriteString("  env\n")
	for _, e := range r.EnvHints {
		fmt.Fprintf(&b, "    · %s\n", e)
	}
	b.WriteString("  cmds     gy doctor platform · gy platform export · gy doctor vision · gy doctor sfu\n")
	b.WriteString("  docs     docs/platform-integration.md · integrations/grok-stream-platform.json\n")
	return b.String()
}

// PlatformExportJSON returns pretty JSON readiness (+ optional contract embed).
func PlatformExportJSON(r PlatformReadiness, embedContract bool) ([]byte, error) {
	out := map[string]any{
		"readiness": r,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"product":     "GrokYtalkY",
		"module":      goModule,
	}
	if embedContract {
		if p := findPlatformContract(); p != "" {
			if raw, err := os.ReadFile(p); err == nil {
				var doc any
				if json.Unmarshal(raw, &doc) == nil {
					out["contract"] = doc
				} else {
					out["contract_raw"] = string(raw)
				}
			}
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

func runPlatformCmd(args []string) error {
	sub := "doctor"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "doctor", "status", "ready", "check":
		fmt.Print(FormatPlatformDoctor(SamplePlatformReadiness()))
		return nil
	case "export", "json":
		embed := true
		outPath := ""
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--no-contract":
				embed = false
			case "-o", "--out":
				if i+1 < len(args) {
					outPath = args[i+1]
					i++
				}
			case "-h", "--help":
				fmt.Print(platformUsage())
				return nil
			}
		}
		raw, err := PlatformExportJSON(SamplePlatformReadiness(), embed)
		if err != nil {
			return err
		}
		if outPath != "" {
			if err := os.WriteFile(outPath, raw, 0o644); err != nil {
				return err
			}
			fmt.Printf("wrote %s (%d B)\n", outPath, len(raw))
			return nil
		}
		fmt.Println(string(raw))
		return nil
	case "contract", "show-contract":
		p := findPlatformContract()
		if p == "" {
			return fmt.Errorf("integrations/grok-stream-platform.json not found")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		if !strings.HasSuffix(string(b), "\n") {
			fmt.Println()
		}
		return nil
	case "help", "-h", "--help":
		fmt.Print(platformUsage())
		return nil
	default:
		fmt.Fprintf(os.Stderr, "unknown platform subcommand %q\n\n", sub)
		fmt.Print(platformUsage())
		return fmt.Errorf("platform: bad subcommand")
	}
}

func platformUsage() string {
	return `gy platform — Grok streaming platform integration handoff

  gy platform              readiness doctor (default)
  gy platform doctor       same
  gy platform export       JSON readiness + contract (−o file · --no-contract)
  gy platform contract     print integrations/grok-stream-platform.json

Also: gy doctor platform · docs/platform-integration.md

Planes: FFmpeg Media() · Grok vision · Aito sides · SFU glyph DC · Live News mesh
`
}
