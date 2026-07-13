package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Vision → FFmpeg control plane (v1.70).
//
// Vision is a first-class media controller: take lines (MEDIA …) and derived
// actions spawn / restart / kill / retune / encode supervised FFmpeg pipelines
// through Media() — not observe-only apply of STYLE/CAPTION.
//
// Env:
//
//	GY_VISION_MEDIA=1|0     enable control plane (default ON when GY_VISION=1, else ON for explicit MEDIA)
//	GY_VISION_MEDIA_MAX=4   max media ops per minute (budget)
//	GY_VISION_MEDIA_AUTO=1  auto-recover unhealthy focus tile after vision take

const (
	VisionMediaRestart = "restart"
	VisionMediaKill    = "kill"
	VisionMediaSpawn   = "spawn"
	VisionMediaRetune  = "retune"
	VisionMediaEncode  = "encode"
	VisionMediaRecover = "recover"
)

// MediaKindEncode one-shot encode jobs under the supervisor.
const MediaKindEncode = "encode"

// VisionMediaAction is one FFmpeg control-plane op from a vision/orch take.
type VisionMediaAction struct {
	Op     string // restart|kill|spawn|retune|encode|recover
	Target string // focus|all|news|watch|<label>
	// retune encode geometry
	ScaleW int
	ScaleH int
	FPS    int
	// spawn / retarget
	Source string // news source id or URL
	// encode
	Format string // jpeg|png|gyst|raw
	Out    string // optional output path
	Raw    string // original MEDIA line
}

// VisionMediaConfig budgets for the control plane.
type VisionMediaConfig struct {
	Enabled bool // GY_VISION_MEDIA (default true when vision on)
	Auto    bool // auto recover dead focus
	MaxPerM int  // ops per rolling minute
}

// LoadVisionMediaConfig from env.
func LoadVisionMediaConfig() VisionMediaConfig {
	c := VisionMediaConfig{
		Enabled: true, // full control plane by default
		Auto:    true,
		MaxPerM: 4,
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MEDIA")); v != "" {
		c.Enabled = envTruthy("GY_VISION_MEDIA")
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MEDIA_AUTO")); v != "" {
		c.Auto = envTruthy("GY_VISION_MEDIA_AUTO")
	}
	if v := strings.TrimSpace(os.Getenv("GY_VISION_MEDIA_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 60 {
			c.MaxPerM = n
		}
	}
	return c
}

// VisionMediaBus rate-limits and records control-plane ops.
type VisionMediaBus struct {
	mu      sync.Mutex
	cfg     VisionMediaConfig
	ops     []time.Time // rolling window
	lastOp  string
	lastAt  time.Time
	lastErr string
	applied int64
	dropped int64
	encoded int64
	// last encode path
	lastOut string
}

var (
	visionMediaOnce sync.Once
	visionMediaBus  *VisionMediaBus
	metricVisionMedia atomic.Int64
)

// VisionMedia returns the process-wide media control plane bus.
func VisionMedia() *VisionMediaBus {
	visionMediaOnce.Do(func() {
		visionMediaBus = &VisionMediaBus{cfg: LoadVisionMediaConfig()}
	})
	return visionMediaBus
}

// Config snapshot.
func (b *VisionMediaBus) Config() VisionMediaConfig {
	if b == nil {
		return LoadVisionMediaConfig()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cfg
}

// SetEnabled toggles control plane at runtime.
func (b *VisionMediaBus) SetEnabled(on bool) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.cfg.Enabled = on
	b.mu.Unlock()
}

// TryBudget acquires one op slot under MaxPerM. false = drop.
func (b *VisionMediaBus) TryBudget() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.cfg.Enabled {
		b.dropped++
		return false
	}
	now := time.Now()
	// prune older than 1 minute
	cut := now.Add(-time.Minute)
	kept := b.ops[:0]
	for _, t := range b.ops {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	b.ops = kept
	if b.cfg.MaxPerM > 0 && len(b.ops) >= b.cfg.MaxPerM {
		b.dropped++
		return false
	}
	b.ops = append(b.ops, now)
	return true
}

// Record records a successful/failed media op.
func (b *VisionMediaBus) Record(op, errStr, out string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastOp = op
	b.lastAt = time.Now()
	b.lastErr = errStr
	if out != "" {
		b.lastOut = out
	}
	if errStr == "" {
		b.applied++
		metricVisionMedia.Add(1)
		if strings.HasPrefix(op, "encode") {
			b.encoded++
		}
	}
}

// Snapshot for doctor.
type VisionMediaSnapshot struct {
	Enabled bool
	Auto    bool
	MaxPerM int
	Applied int64
	Dropped int64
	Encoded int64
	LastOp  string
	LastErr string
	LastOut string
	LastAt  time.Time
	WindowN int
}

// Snapshot copies control-plane state.
func (b *VisionMediaBus) Snapshot() VisionMediaSnapshot {
	if b == nil {
		return VisionMediaSnapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return VisionMediaSnapshot{
		Enabled: b.cfg.Enabled, Auto: b.cfg.Auto, MaxPerM: b.cfg.MaxPerM,
		Applied: b.applied, Dropped: b.dropped, Encoded: b.encoded,
		LastOp: b.lastOp, LastErr: b.lastErr, LastOut: b.lastOut, LastAt: b.lastAt,
		WindowN: len(b.ops),
	}
}

// ParseMediaLine parses one MEDIA take line into an action.
//
// Forms:
//
//	MEDIA restart [focus|all|news|watch|label]
//	MEDIA kill [focus|all|news|watch|label]
//	MEDIA recover [focus|all]
//	MEDIA retune [focus] [WxH@fps | scale=WxH fps=N]
//	MEDIA spawn <source-id|url>
//	MEDIA encode [focus] [jpeg|png|gyst] [path]
func ParseMediaLine(line string) (VisionMediaAction, bool) {
	line = strings.TrimSpace(line)
	up := strings.ToUpper(line)
	if !strings.HasPrefix(up, "MEDIA ") && up != "MEDIA" {
		return VisionMediaAction{}, false
	}
	rest := strings.TrimSpace(line[5:])
	a := VisionMediaAction{Raw: line, Target: "focus"}
	if rest == "" {
		a.Op = VisionMediaRestart
		return a, true
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		a.Op = VisionMediaRestart
		return a, true
	}
	a.Op = strings.ToLower(fields[0])
	// normalize synonyms
	switch a.Op {
	case "restart", "rs", "reopen":
		a.Op = VisionMediaRestart
	case "kill", "stop", "drop":
		a.Op = VisionMediaKill
	case "spawn", "start", "open":
		a.Op = VisionMediaSpawn
	case "retune", "tune", "scale", "fps":
		a.Op = VisionMediaRetune
	case "encode", "snap", "snapshot", "export":
		a.Op = VisionMediaEncode
	case "recover", "heal", "fix":
		a.Op = VisionMediaRecover
	default:
		// "MEDIA focus" → restart focus
		a.Target = a.Op
		a.Op = VisionMediaRestart
		return a, true
	}
	// remaining tokens
	for i := 1; i < len(fields); i++ {
		tok := fields[i]
		low := strings.ToLower(tok)
		// geometry WxH@fps or WxH
		if w, h, fps, ok := parseGeomToken(tok); ok {
			if w > 0 {
				a.ScaleW = w
			}
			if h > 0 {
				a.ScaleH = h
			}
			if fps > 0 {
				a.FPS = fps
			}
			continue
		}
		if strings.HasPrefix(low, "scale=") {
			if w, h, _, ok := parseGeomToken(strings.TrimPrefix(low, "scale=")); ok {
				a.ScaleW, a.ScaleH = w, h
			}
			continue
		}
		if strings.HasPrefix(low, "fps=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(low, "fps=")); err == nil && n > 0 {
				a.FPS = n
			}
			continue
		}
		if strings.HasPrefix(low, "out=") || strings.HasPrefix(low, "path=") {
			a.Out = strings.TrimPrefix(strings.TrimPrefix(tok, "out="), "path=")
			// handle Out= form case
			if i2 := strings.Index(tok, "="); i2 >= 0 {
				a.Out = tok[i2+1:]
			}
			continue
		}
		switch low {
		case "focus", "active", "all", "news", "watch", "tile":
			if low == "tile" {
				a.Target = "focus"
			} else {
				a.Target = low
			}
			continue
		case "jpeg", "jpg", "png", "gyst", "raw", "mp4":
			a.Format = low
			if a.Format == "jpg" {
				a.Format = "jpeg"
			}
			continue
		}
		// path-like or source id
		if strings.Contains(tok, "/") || strings.Contains(tok, ".") || strings.HasPrefix(tok, "http") {
			if a.Op == VisionMediaEncode && a.Out == "" && (strings.HasSuffix(low, ".jpg") ||
				strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".png") ||
				strings.HasSuffix(low, ".gyst") || strings.HasSuffix(low, ".mp4")) {
				a.Out = tok
			} else if a.Source == "" {
				a.Source = tok
			} else if a.Out == "" {
				a.Out = tok
			}
			continue
		}
		// label or source slug
		if a.Op == VisionMediaSpawn && a.Source == "" {
			a.Source = tok
			continue
		}
		if a.Target == "focus" || a.Target == "" {
			a.Target = tok
		} else if a.Source == "" {
			a.Source = tok
		}
	}
	if a.Format == "" && a.Op == VisionMediaEncode {
		a.Format = "jpeg"
	}
	return a, true
}

// parseGeomToken parses 96x54, 96x54@5, 80×45.
func parseGeomToken(s string) (w, h, fps int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "×", "x")
	if i := strings.IndexByte(s, '@'); i >= 0 {
		fmt.Sscanf(s[i+1:], "%d", &fps)
		s = s[:i]
	}
	if !strings.Contains(s, "x") {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0, 0, false
	}
	wi, e1 := strconv.Atoi(parts[0])
	hi, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || wi < 16 || hi < 9 {
		return 0, 0, 0, false
	}
	return wi, hi, fps, true
}

// ParseGrokTakeMedia extracts MEDIA lines into actions (called from ParseGrokTake).
func ParseGrokTakeMedia(text string) []VisionMediaAction {
	var out []VisionMediaAction
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		if a, ok := ParseMediaLine(line); ok {
			out = append(out, a)
		}
	}
	return out
}

// DeriveVisionMediaActions adds auto recover when focus pipe is dead.
func DeriveVisionMediaActions(m *Model, take GrokTake) []VisionMediaAction {
	cfg := VisionMedia().Config()
	if !cfg.Enabled || !cfg.Auto {
		return nil
	}
	if !take.Vision {
		return nil
	}
	// already has explicit media ops
	if len(take.Media) > 0 {
		return nil
	}
	if m == nil {
		return nil
	}
	// unhealthy focus news tile → recover
	if m.lab != nil && m.lab.News != nil && m.lab.News.On {
		i := m.lab.Active
		if i >= 0 && i < len(m.lab.News.Pipes) {
			tp := m.lab.News.Pipes[i]
			if tp != nil && !tp.Healthy() {
				return []VisionMediaAction{{Op: VisionMediaRecover, Target: "focus", Raw: "MEDIA recover focus (auto)"}}
			}
		}
	}
	// dead watch pipe
	if m.vpipe != nil && !m.vpipe.Alive() && m.vpipe.VideoURL != "" {
		return []VisionMediaAction{{Op: VisionMediaRecover, Target: "watch", Raw: "MEDIA recover watch (auto)"}}
	}
	return nil
}

// ApplyVisionMediaControl executes media actions from a take (FFmpeg control plane).
// Returns short applied tokens for orch status line.
func ApplyVisionMediaControl(m *Model, take GrokTake) []string {
	bus := VisionMedia()
	cfg := bus.Config()
	actions := append([]VisionMediaAction(nil), take.Media...)
	if len(actions) == 0 {
		actions = DeriveVisionMediaActions(m, take)
	}
	// explicit MEDIA always preferred; if disabled and only auto — skip
	if !cfg.Enabled {
		if len(take.Media) == 0 {
			return nil
		}
		// allow explicit MEDIA even when "disabled" only if env not hard-off?
		// honor Enabled strictly
		bus.Record("disabled", "GY_VISION_MEDIA=0", "")
		return nil
	}
	if len(actions) == 0 {
		return nil
	}
	var applied []string
	for _, a := range actions {
		if !bus.TryBudget() {
			bus.Record(a.Op, "budget", "")
			applied = append(applied, "media=budget")
			break
		}
		msg, err := execVisionMediaAction(m, a)
		if err != nil {
			bus.Record(a.Op+":"+a.Target, err.Error(), "")
			applied = append(applied, "media!"+a.Op)
			if m != nil {
				m.pushSys("vision·media " + a.Op + " fail: " + truncate(err.Error(), 48))
			}
			continue
		}
		bus.Record(a.Op+":"+a.Target, "", msg)
		MetricIncr("recoveries")
		MetricIncr("vision_media")
		token := "media=" + a.Op
		if a.Target != "" && a.Target != "focus" {
			token += ":" + truncate(a.Target, 12)
		}
		applied = append(applied, token)
		if m != nil && msg != "" {
			m.pushSys("vision·ffmpeg " + msg)
		}
	}
	return applied
}

// execVisionMediaAction runs one op against Model + Media supervisor.
func execVisionMediaAction(m *Model, a VisionMediaAction) (string, error) {
	if m == nil {
		return "", fmt.Errorf("no model")
	}
	target := strings.ToLower(strings.TrimSpace(a.Target))
	if target == "" {
		target = "focus"
	}
	switch a.Op {
	case VisionMediaRestart, VisionMediaRecover:
		return visionMediaRestart(m, target)
	case VisionMediaKill:
		return visionMediaKill(m, target)
	case VisionMediaRetune:
		return visionMediaRetune(m, target, a)
	case VisionMediaSpawn:
		return visionMediaSpawn(m, a)
	case VisionMediaEncode:
		return visionMediaEncode(m, a)
	default:
		return "", fmt.Errorf("unknown media op %q", a.Op)
	}
}

func visionMediaRestart(m *Model, target string) (string, error) {
	switch target {
	case "all", "news":
		if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
			return "", fmt.Errorf("no news wall")
		}
		n := 0
		for i, tp := range m.lab.News.Pipes {
			if tp == nil {
				continue
			}
			nt, err := RestartNewsTile(tp)
			if err != nil {
				continue
			}
			m.lab.News.Pipes[i] = nt
			if i < len(m.lab.Feeds) {
				if fr := nt.Snapshot(); fr != nil {
					m.lab.Feeds[i].Frame = fr
				}
			}
			n++
		}
		if n == 0 {
			return "", fmt.Errorf("no tiles restarted")
		}
		return fmt.Sprintf("restart %d news tiles", n), nil
	case "watch":
		return visionMediaRestartWatch(m)
	default:
		// focus or label
		if target == "focus" || target == "active" {
			if m.lab != nil && m.lab.News != nil && m.lab.News.On {
				i := m.lab.Active
				if i >= 0 && i < len(m.lab.News.Pipes) && m.lab.News.Pipes[i] != nil {
					return visionMediaRestartNewsIndex(m, i)
				}
			}
			if m.vpipe != nil {
				return visionMediaRestartWatch(m)
			}
			return "", fmt.Errorf("no focus media")
		}
		// match label
		if m.lab != nil && m.lab.News != nil {
			for i, tp := range m.lab.News.Pipes {
				if tp == nil {
					continue
				}
				if strings.EqualFold(tp.Label, target) || strings.Contains(strings.ToLower(tp.Label), target) {
					return visionMediaRestartNewsIndex(m, i)
				}
			}
		}
		// Media supervisor kill-label + can't restart without src — try news
		return "", fmt.Errorf("target %q not found", target)
	}
}

func visionMediaRestartNewsIndex(m *Model, i int) (string, error) {
	tp := m.lab.News.Pipes[i]
	if tp == nil {
		return "", fmt.Errorf("empty tile")
	}
	label := tp.Label
	nt, err := RestartNewsTile(tp)
	if err != nil {
		return "", err
	}
	m.lab.News.Pipes[i] = nt
	if i < len(m.lab.Feeds) {
		if fr := nt.Snapshot(); fr != nil {
			m.lab.Feeds[i].Frame = fr
		}
	}
	return "restart news:" + label, nil
}

func visionMediaRestartWatch(m *Model) (string, error) {
	if m.vpipe == nil {
		return "", fmt.Errorf("no watch pipe")
	}
	if err := m.vpipe.Restart(); err != nil {
		return "", err
	}
	MetricIncr("watch_starts")
	return "restart watch", nil
}

func visionMediaKill(m *Model, target string) (string, error) {
	switch target {
	case "all":
		n := Media().KillKind(MediaKindNews)
		n += Media().KillKind(MediaKindWatch)
		n += Media().KillKind(MediaKindEncode)
		// clear news pipe refs
		if m.lab != nil && m.lab.News != nil {
			for i, tp := range m.lab.News.Pipes {
				if tp != nil {
					tp.Stop()
					m.lab.News.Pipes[i] = nil
				}
			}
		}
		if m.vpipe != nil {
			m.vpipe.Stop()
			m.vpipe = nil
		}
		return fmt.Sprintf("kill media · %d", n), nil
	case "news":
		n := 0
		if m.lab != nil && m.lab.News != nil {
			for i, tp := range m.lab.News.Pipes {
				if tp != nil {
					tp.Stop()
					m.lab.News.Pipes[i] = nil
					n++
				}
			}
		}
		Media().KillKind(MediaKindNews)
		return fmt.Sprintf("kill %d news", n), nil
	case "watch":
		if m.vpipe != nil {
			m.vpipe.Stop()
			m.vpipe = nil
		}
		Media().KillKind(MediaKindWatch)
		return "kill watch", nil
	case "focus", "active":
		if m.lab != nil && m.lab.News != nil && m.lab.News.On {
			i := m.lab.Active
			if i >= 0 && i < len(m.lab.News.Pipes) && m.lab.News.Pipes[i] != nil {
				label := m.lab.News.Pipes[i].Label
				m.lab.News.Pipes[i].Stop()
				m.lab.News.Pipes[i] = nil
				return "kill news:" + label, nil
			}
		}
		if m.vpipe != nil {
			m.vpipe.Stop()
			m.vpipe = nil
			return "kill watch", nil
		}
		return "", fmt.Errorf("nothing to kill")
	default:
		// label match
		if m.lab != nil && m.lab.News != nil {
			for i, tp := range m.lab.News.Pipes {
				if tp == nil {
					continue
				}
				if strings.EqualFold(tp.Label, target) || strings.Contains(strings.ToLower(tp.Label), strings.ToLower(target)) {
					label := tp.Label
					tp.Stop()
					m.lab.News.Pipes[i] = nil
					return "kill news:" + label, nil
				}
			}
		}
		Media().KillLabel(MediaKindNews, target)
		Media().KillLabel(MediaKindWatch, target)
		return "kill label:" + target, nil
	}
}

func visionMediaRetune(m *Model, target string, a VisionMediaAction) (string, error) {
	w, h, fps := a.ScaleW, a.ScaleH, a.FPS
	if w == 0 {
		w = newsTileW
	}
	if h == 0 {
		h = newsTileH
	}
	if fps == 0 {
		fps = newsTileFPS
	}
	// clamp
	if w < 32 {
		w = 32
	}
	if w > 320 {
		w = 320
	}
	if h < 18 {
		h = 18
	}
	if h > 180 {
		h = 180
	}
	if h%2 != 0 {
		h++
	}
	if fps < 1 {
		fps = 1
	}
	if fps > 15 {
		fps = 15
	}
	opts := NewsTileOpts{W: w, H: h, FPS: fps}

	if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
		return "", fmt.Errorf("retune needs news wall")
	}
	indices := []int{}
	switch target {
	case "all", "news":
		for i, tp := range m.lab.News.Pipes {
			if tp != nil {
				indices = append(indices, i)
			}
		}
	default:
		i := m.lab.Active
		if target != "focus" && target != "active" {
			found := -1
			for j, tp := range m.lab.News.Pipes {
				if tp != nil && (strings.EqualFold(tp.Label, target) || strings.Contains(strings.ToLower(tp.Label), strings.ToLower(target))) {
					found = j
					break
				}
			}
			if found < 0 {
				return "", fmt.Errorf("retune target %q not found", target)
			}
			i = found
		}
		if i < 0 || i >= len(m.lab.News.Pipes) || m.lab.News.Pipes[i] == nil {
			return "", fmt.Errorf("no focus tile")
		}
		indices = []int{i}
	}
	n := 0
	for _, i := range indices {
		tp := m.lab.News.Pipes[i]
		nt, err := RetuneNewsTile(tp, opts)
		if err != nil {
			continue
		}
		m.lab.News.Pipes[i] = nt
		if i < len(m.lab.Feeds) {
			if fr := nt.Snapshot(); fr != nil {
				m.lab.Feeds[i].Frame = fr
			}
		}
		n++
	}
	if n == 0 {
		return "", fmt.Errorf("retune failed")
	}
	return fmt.Sprintf("retune %d tile(s) %dx%d@%d", n, w, h, fps), nil
}

func visionMediaSpawn(m *Model, a VisionMediaAction) (string, error) {
	srcKey := strings.TrimSpace(a.Source)
	if srcKey == "" {
		srcKey = strings.TrimSpace(a.Target)
	}
	if srcKey == "" || srcKey == "focus" {
		return "", fmt.Errorf("spawn needs source id or url")
	}
	if !Media().CanSpawn(MediaKindNews) {
		return "", fmt.Errorf("news at capacity")
	}
	var page, label string
	// URL direct
	if strings.HasPrefix(srcKey, "http://") || strings.HasPrefix(srcKey, "https://") {
		page = srcKey
		label = truncate(srcKey, 18)
	} else {
		// catalog lookup
		for _, s := range ExtendedNewsSources() {
			if strings.EqualFold(s.ID, srcKey) || strings.EqualFold(s.Label, srcKey) ||
				strings.Contains(strings.ToLower(s.Label), strings.ToLower(srcKey)) {
				page = s.URL
				label = s.Label
				break
			}
		}
	}
	if page == "" {
		return "", fmt.Errorf("unknown source %q", srcKey)
	}
	// ensure lab/news wall exists
	if m.lab == nil {
		return "", fmt.Errorf("open lab first")
	}
	style := m.pixelMode
	if m.lab.News != nil && m.lab.News.On {
		style = NewsWallStyleLadder[len(m.lab.News.Pipes)%len(NewsWallStyleLadder)]
	}
	// resolve + start (blocking — caller is apply path; keep timeout modest)
	r, err := ResolveMediaTimeout(page, 45*time.Second)
	if err != nil {
		return "", err
	}
	vid := r.Video
	if vid == "" {
		return "", fmt.Errorf("no video for %s", label)
	}
	tp, err := StartNewsTile(label, vid, style)
	if err != nil {
		return "", err
	}
	// attach to wall if on, else create soft slot
	if m.lab.News != nil && m.lab.News.On {
		// find empty pipe slot or append
		slot := -1
		for i, p := range m.lab.News.Pipes {
			if p == nil {
				slot = i
				break
			}
		}
		if slot < 0 {
			m.lab.News.Pipes = append(m.lab.News.Pipes, tp)
			slot = len(m.lab.News.Pipes) - 1
			m.lab.News.Sources = append(m.lab.News.Sources, NewsSource{ID: srcKey, Label: label, URL: page})
		} else {
			m.lab.News.Pipes[slot] = tp
		}
		if slot < len(m.lab.Feeds) {
			m.lab.Feeds[slot].Kind = "news"
			m.lab.Feeds[slot].Label = label
			if fr := tp.Snapshot(); fr != nil {
				m.lab.Feeds[slot].Frame = fr
			}
		}
	}
	MetricIncr("news_starts")
	return "spawn news:" + label, nil
}

func visionMediaEncode(m *Model, a VisionMediaAction) (string, error) {
	format := strings.ToLower(a.Format)
	if format == "" {
		format = "jpeg"
	}
	// prefer encoding from live source URL when available; else frame dump
	srcURL := ""
	label := "focus"
	if m.lab != nil && m.lab.News != nil && m.lab.News.On {
		i := m.lab.Active
		if i >= 0 && i < len(m.lab.News.Pipes) && m.lab.News.Pipes[i] != nil {
			srcURL = m.lab.News.Pipes[i].Src
			label = m.lab.News.Pipes[i].Label
		}
	}
	if srcURL == "" && m.vpipe != nil {
		srcURL = m.vpipe.VideoURL
		label = "watch"
	}
	out := strings.TrimSpace(a.Out)
	if out == "" {
		dir := os.TempDir()
		stamp := time.Now().Format("20060102-150405")
		safe := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '_'
		}, label)
		switch format {
		case "png":
			out = filepath.Join(dir, fmt.Sprintf("gy-vision-%s-%s.png", safe, stamp))
		case "gyst":
			out = filepath.Join(dir, fmt.Sprintf("gy-vision-%s-%s.gyst", safe, stamp))
		case "mp4":
			out = filepath.Join(dir, fmt.Sprintf("gy-vision-%s-%s.mp4", safe, stamp))
		case "raw":
			out = filepath.Join(dir, fmt.Sprintf("gy-vision-%s-%s.rgb", safe, stamp))
		default:
			out = filepath.Join(dir, fmt.Sprintf("gy-vision-%s-%s.jpg", safe, stamp))
			format = "jpeg"
		}
	}

	// Frame dump path (no network) when we have RGB and format is jpeg/png
	frame, _, _ := FocusFrameFromModel(m)
	if frame != nil && (format == "jpeg" || format == "png" || format == "jpg") {
		if err := writeFrameImage(frame, out, format); err != nil {
			return "", err
		}
		VisionMedia().Record("encode:frame", "", out)
		return "encode frame → " + out, nil
	}

	if srcURL == "" {
		return "", fmt.Errorf("no source for encode")
	}
	// FFmpeg one-shot: vision control plane spawns the pipeline itself.
	// Prefer StartRegistered for doctor visibility; watchExit owns Wait so we
	// poll Health + output file (never double-Wait).
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", srcURL}
	switch format {
	case "mp4":
		args = append(args, "-t", "3", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-vf", "scale=640:360", out)
	case "raw":
		args = append(args, "-an", "-vframes", "1", "-vf", "scale=320:180,format=rgb24", "-f", "rawvideo", out)
	case "gyst":
		jpg := strings.TrimSuffix(out, filepath.Ext(out)) + ".jpg"
		args = append(args, "-an", "-vframes", "1", "-q:v", "3", jpg)
		out = jpg
	default:
		args = append(args, "-an", "-vframes", "1", "-q:v", "3", out)
	}
	cmd, id, err := StartRegistered(MediaKindEncode, "vision-enc:"+label, "ffmpeg", args)
	if err != nil {
		// capacity / missing ffmpeg — direct one-shot still counts as control plane spawn
		c2 := exec.Command("ffmpeg", args...)
		PrepMediaCmd(c2)
		done := make(chan error, 1)
		go func() { done <- c2.Run() }()
		select {
		case e := <-done:
			if e != nil {
				return "", fmt.Errorf("ffmpeg encode: %w", e)
			}
		case <-time.After(45 * time.Second):
			_ = killCmd(c2)
			return "", fmt.Errorf("encode timeout")
		}
		return "encode ffmpeg → " + out, nil
	}
	_ = cmd
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if st, e := os.Stat(out); e == nil && st.Size() > 32 {
			// give encoder a beat to finish, then drop registry entry
			time.Sleep(80 * time.Millisecond)
			Media().Kill(id)
			return "encode ffmpeg → " + out, nil
		}
		// process reaped by supervisor?
		if !mediaProcAlive(id) {
			if st, e := os.Stat(out); e == nil && st.Size() > 0 {
				return "encode ffmpeg → " + out, nil
			}
			return "", fmt.Errorf("encode exited without output")
		}
		time.Sleep(40 * time.Millisecond)
	}
	Media().Kill(id)
	if st, e := os.Stat(out); e == nil && st.Size() > 0 {
		return "encode ffmpeg → " + out, nil
	}
	return "", fmt.Errorf("encode timeout")
}

// mediaProcAlive reports whether id is still in the supervisor map.
func mediaProcAlive(id string) bool {
	if id == "" {
		return false
	}
	s := Media()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[id]
	return ok && p != nil && !p.Stopped.Load()
}

// writeFrameImage dumps FramePixels to a JPEG file (encode budget via FrameToJPEGBase64).
func writeFrameImage(f *FramePixels, path, format string) error {
	if f == nil || f.W < 1 || f.H < 1 {
		return fmt.Errorf("empty frame")
	}
	dataURL, _, err := FrameToJPEGBase64(f, f.W, f.H, 85)
	if err != nil {
		return err
	}
	const pfx = "data:image/jpeg;base64,"
	if !strings.HasPrefix(dataURL, pfx) {
		return fmt.Errorf("bad jpeg data")
	}
	b, err := base64.StdEncoding.DecodeString(dataURL[len(pfx):])
	if err != nil {
		return err
	}
	// always write JPEG bytes; normalize extension if caller asked png without converter
	if format == "png" && strings.HasSuffix(strings.ToLower(path), ".png") {
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ".jpg"
	}
	return os.WriteFile(path, b, 0o644)
}

// FormatVisionMediaDoctor multi-line control plane status.
func FormatVisionMediaDoctor() string {
	s := VisionMedia().Snapshot()
	mh := Media().Health()
	var b strings.Builder
	fmt.Fprintf(&b, "vision·ffmpeg control plane · enabled=%v auto=%v\n", s.Enabled, s.Auto)
	fmt.Fprintf(&b, "  budget    %d ops/min · window %d · applied %d · dropped %d · encoded %d\n",
		s.MaxPerM, s.WindowN, s.Applied, s.Dropped, s.Encoded)
	fmt.Fprintf(&b, "  media     %s\n", FormatMediaHealthChrome(mh))
	if s.LastOp != "" {
		fmt.Fprintf(&b, "  last_op   %s", s.LastOp)
		if !s.LastAt.IsZero() {
			fmt.Fprintf(&b, " · %s", s.LastAt.Format(time.RFC3339))
		}
		b.WriteByte('\n')
	}
	if s.LastErr != "" {
		fmt.Fprintf(&b, "  last_err  %s\n", s.LastErr)
	}
	if s.LastOut != "" {
		fmt.Fprintf(&b, "  last_out  %s\n", s.LastOut)
	}
	b.WriteString("  ops       MEDIA restart|kill|spawn|retune|encode|recover [target]\n")
	b.WriteString("  env       GY_VISION_MEDIA=1 · GY_VISION_MEDIA_MAX=4 · GY_VISION_MEDIA_AUTO=1\n")
	b.WriteString("  stage     capture→encode→infer→apply(+ffmpeg)→emit\n")
	return b.String()
}
