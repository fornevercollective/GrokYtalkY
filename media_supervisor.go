package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Media supervisor — single registry for ffmpeg/ffplay child processes.
// Fixes: orphaned pipes, unbounded concurrency, thin recovery, no health surface.

const (
	// MediaKind classifies supervised processes.
	MediaKindWatch = "watch" // terminal vpipe
	MediaKindNews  = "news"  // news wall tile
	MediaKindCam   = "cam"   // camera snap / capture
	MediaKindAudio = "audio" // ffplay / PTT capture
	MediaKindPub   = "pub"   // stream-pub ffmpeg
	MediaKindOther = "other"
)

// Default concurrency budgets (override with GY_MEDIA_MAX / GY_NEWS_MAX).
const (
	defaultMediaMaxProcs = 16
	defaultNewsMaxTiles  = 8
)

// MediaProc is one tracked child (ffmpeg/ffplay/…).
type MediaProc struct {
	ID        string
	Kind      string
	Label     string
	Cmd       *exec.Cmd
	Started   time.Time
	LastFrame time.Time // optional heartbeats from consumers
	Restarts  int
	Err       string
	// Soft stop flag — consumers check this
	Stopped atomic.Bool
}

// MediaHealth is a snapshot for TUI / doctor.
type MediaHealth struct {
	Alive     int
	Total     int
	Max       int
	NewsAlive int
	NewsMax   int
	Watching  bool
	Lines     []string // per-proc short status
	Drops     int64    // spawn rejections / backpressure
	Kills     int64
}

// MediaSupervisor owns all media children for the process.
type MediaSupervisor struct {
	mu      sync.Mutex
	procs   map[string]*MediaProc
	max     int
	newsMax int
	seq     uint64
	drops   int64
	kills   int64
	// optional hook when a proc dies unexpectedly (label, kind, err)
	OnDeath func(id, kind, label, err string)
}

var (
	mediaSuperOnce sync.Once
	mediaSuper     *MediaSupervisor
)

// Media returns the process-wide media supervisor.
func Media() *MediaSupervisor {
	mediaSuperOnce.Do(func() {
		max := defaultMediaMaxProcs
		if v := strings.TrimSpace(os.Getenv("GY_MEDIA_MAX")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				max = n
			}
		}
		nMax := defaultNewsMaxTiles
		if v := strings.TrimSpace(os.Getenv("GY_NEWS_MAX")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				nMax = n
			}
		}
		mediaSuper = &MediaSupervisor{
			procs:   make(map[string]*MediaProc),
			max:     max,
			newsMax: nMax,
		}
		// best-effort: kill children if we die hard (Unix process group)
		// Register is enough for graceful Shutdown; OS reaps if we setpgrp carefully.
	})
	return mediaSuper
}

// MaxProcs / NewsMax expose budgets.
func (s *MediaSupervisor) MaxProcs() int {
	if s == nil {
		return defaultMediaMaxProcs
	}
	return s.max
}
func (s *MediaSupervisor) NewsMax() int {
	if s == nil {
		return defaultNewsMaxTiles
	}
	return s.newsMax
}

// CanSpawn reports whether another process of kind is allowed.
func (s *MediaSupervisor) CanSpawn(kind string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.procs) >= s.max {
		return false
	}
	if kind == MediaKindNews {
		n := 0
		for _, p := range s.procs {
			if p.Kind == MediaKindNews && !p.Stopped.Load() {
				n++
			}
		}
		if n >= s.newsMax {
			return false
		}
	}
	return true
}

// Register tracks an already-started Cmd. Returns assigned id.
// If over budget, kills cmd and returns error (caller must not use it).
func (s *MediaSupervisor) Register(kind, label string, cmd *exec.Cmd) (string, error) {
	if s == nil {
		s = Media()
	}
	if cmd == nil || cmd.Process == nil {
		return "", fmt.Errorf("media: nil process")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// count live
	alive := 0
	newsN := 0
	for _, p := range s.procs {
		if p.Stopped.Load() {
			continue
		}
		alive++
		if p.Kind == MediaKindNews {
			newsN++
		}
	}
	if alive >= s.max || (kind == MediaKindNews && newsN >= s.newsMax) {
		s.drops++
		_ = killCmd(cmd)
		return "", fmt.Errorf("media: backpressure (alive=%d max=%d news=%d/%d) — drop %s",
			alive, s.max, newsN, s.newsMax, label)
	}

	s.seq++
	id := fmt.Sprintf("%s-%d-%d", kind, s.seq, cmd.Process.Pid)
	s.procs[id] = &MediaProc{
		ID:      id,
		Kind:    kind,
		Label:   label,
		Cmd:     cmd,
		Started: time.Now(),
	}
	// reap when process exits
	go s.watchExit(id, cmd)
	return id, nil
}

func (s *MediaSupervisor) watchExit(id string, cmd *exec.Cmd) {
	err := cmd.Wait()
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	s.mu.Lock()
	p, ok := s.procs[id]
	var kind, label string
	if ok && p != nil {
		kind, label = p.Kind, p.Label
		if !p.Stopped.Load() {
			// unexpected death
			p.Err = errStr
			if errStr == "" {
				p.Err = "exit"
			}
		}
		p.Stopped.Store(true)
		// keep entry briefly for health, or delete
		delete(s.procs, id)
	}
	onDeath := s.OnDeath
	s.mu.Unlock()
	if ok && onDeath != nil && errStr != "" {
		onDeath(id, kind, label, errStr)
	}
}

// Heartbeat marks a proc as producing frames (for stale detection).
func (s *MediaSupervisor) Heartbeat(id string) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.procs[id]; ok && p != nil {
		p.LastFrame = time.Now()
	}
}

// Unregister + kill a process by id.
func (s *MediaSupervisor) Kill(id string) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	p, ok := s.procs[id]
	if ok {
		p.Stopped.Store(true)
		delete(s.procs, id)
		s.kills++
	}
	s.mu.Unlock()
	if ok && p != nil && p.Cmd != nil {
		_ = killCmd(p.Cmd)
	}
}

// KillKind stops all processes of a kind (e.g. all news tiles).
func (s *MediaSupervisor) KillKind(kind string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	var ids []string
	for id, p := range s.procs {
		if p.Kind == kind {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Kill(id)
	}
	return len(ids)
}

// KillLabel stops procs matching label (news tile restart).
func (s *MediaSupervisor) KillLabel(kind, label string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	var ids []string
	for id, p := range s.procs {
		if p.Kind == kind && p.Label == label {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Kill(id)
	}
}

// Shutdown kills every supervised media process (call on TUI exit).
func (s *MediaSupervisor) Shutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	ids := make([]string, 0, len(s.procs))
	for id := range s.procs {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Kill(id)
	}
}

// Health snapshot for chrome / doctor.
func (s *MediaSupervisor) Health() MediaHealth {
	h := MediaHealth{Max: defaultMediaMaxProcs, NewsMax: defaultNewsMaxTiles}
	if s == nil {
		return h
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h.Max = s.max
	h.NewsMax = s.newsMax
	h.Drops = s.drops
	h.Kills = s.kills
	h.Total = len(s.procs)
	now := time.Now()
	for _, p := range s.procs {
		if p.Stopped.Load() {
			continue
		}
		h.Alive++
		if p.Kind == MediaKindNews {
			h.NewsAlive++
		}
		if p.Kind == MediaKindWatch {
			h.Watching = true
		}
		age := now.Sub(p.Started).Round(time.Second)
		stale := ""
		if !p.LastFrame.IsZero() && now.Sub(p.LastFrame) > 5*time.Second {
			stale = " stale"
		}
		err := ""
		if p.Err != "" {
			err = " !" + truncate(p.Err, 20)
		}
		h.Lines = append(h.Lines, fmt.Sprintf("%s %s %s %s%s%s",
			p.Kind, truncate(p.Label, 12), age, pidOf(p.Cmd), stale, err))
	}
	return h
}

// FormatMediaHealthChrome one-line lab status.
func FormatMediaHealthChrome(h MediaHealth) string {
	return fmt.Sprintf("media %d/%d · news %d/%d · drop %d · kill %d",
		h.Alive, h.Max, h.NewsAlive, h.NewsMax, h.Drops, h.Kills)
}

// FormatMediaHealthDetail multi-line for /media or doctor.
func FormatMediaHealthDetail(h MediaHealth) string {
	var b strings.Builder
	fmt.Fprintf(&b, "media supervisor · alive %d/%d · news %d/%d · drops %d · kills %d\n",
		h.Alive, h.Max, h.NewsAlive, h.NewsMax, h.Drops, h.Kills)
	if h.Watching {
		b.WriteString("  watch: active\n")
	}
	if len(h.Lines) == 0 {
		b.WriteString("  (no supervised ffmpeg/ffplay)\n")
		return b.String()
	}
	for _, ln := range h.Lines {
		fmt.Fprintf(&b, "  · %s\n", ln)
	}
	return b.String()
}

func pidOf(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil {
		return "—"
	}
	return fmt.Sprintf("pid%d", cmd.Process.Pid)
}

// killCmd terminates a process group when possible (avoids orphaned ffmpeg).
func killCmd(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	// Try process group first (if Setpgid was set at start)
	if runtime.GOOS != "windows" {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	return nil
}

// PrepMediaCmd configures a command for supervised lifecycle:
// - new process group (Unix) so Kill kills ffmpeg children too
// - hide window noise
func PrepMediaCmd(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}

// StartRegistered builds ffmpeg/ffplay-style cmd, starts it, registers under supervisor.
// On backpressure or start failure returns error and does not leak.
func StartRegistered(kind, label string, name string, args []string) (*exec.Cmd, string, error) {
	s := Media()
	if !s.CanSpawn(kind) {
		s.mu.Lock()
		s.drops++
		s.mu.Unlock()
		return nil, "", fmt.Errorf("media: at capacity for %s (%s)", kind, label)
	}
	cmd := exec.Command(name, args...)
	PrepMediaCmd(cmd)
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	id, err := s.Register(kind, label, cmd)
	if err != nil {
		return nil, "", err
	}
	return cmd, id, nil
}
