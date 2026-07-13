package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Reliability pack — panic recovery, process metrics, crash dumps, checksum helpers.
// Complements media_supervisor for production-shaped live sessions.

var (
	reliMu       sync.Mutex
	reliStarted  = time.Now()
	reliPanics   int64
	reliLastDump string
	// counters for observability (atomic)
	metricMediaDrops   atomic.Int64 // mirrored from supervisor when sampling
	metricOrchTakes    atomic.Int64
	metricWatchStarts  atomic.Int64
	metricNewsStarts   atomic.Int64
	metricRecoveries   atomic.Int64
	metricUpdatesOK    atomic.Int64
)

// MetricIncr bumps a named counter (best-effort observability).
func MetricIncr(name string) {
	switch name {
	case "orch_takes":
		metricOrchTakes.Add(1)
	case "watch_starts":
		metricWatchStarts.Add(1)
	case "news_starts":
		metricNewsStarts.Add(1)
	case "recoveries":
		metricRecoveries.Add(1)
	case "updates_ok":
		metricUpdatesOK.Add(1)
	case "media_drops":
		metricMediaDrops.Add(1)
	}
}

// ReliabilitySnapshot is a point-in-time process health report.
type ReliabilitySnapshot struct {
	Uptime      time.Duration
	GoRoutines  int
	MemAllocMB  float64
	Panics      int64
	LastDump    string
	Media       MediaHealth
	OrchTakes   int64
	WatchStarts int64
	NewsStarts  int64
	Recoveries  int64
	Version     string
	Commit      string
	GOOS        string
	GOARCH      string
}

// SampleReliability gathers live metrics (includes media supervisor).
func SampleReliability() ReliabilitySnapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	h := Media().Health()
	// sync drop counter from supervisor when higher
	if h.Drops > metricMediaDrops.Load() {
		metricMediaDrops.Store(h.Drops)
	}
	reliMu.Lock()
	dump := reliLastDump
	reliMu.Unlock()
	return ReliabilitySnapshot{
		Uptime:      time.Since(reliStarted).Round(time.Second),
		GoRoutines:  runtime.NumGoroutine(),
		MemAllocMB:  float64(ms.Alloc) / (1024 * 1024),
		Panics:      atomic.LoadInt64(&reliPanics),
		LastDump:    dump,
		Media:       h,
		OrchTakes:   metricOrchTakes.Load(),
		WatchStarts: metricWatchStarts.Load(),
		NewsStarts:  metricNewsStarts.Load(),
		Recoveries:  metricRecoveries.Load(),
		Version:     Version,
		Commit:      Commit,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
	}
}

// FormatReliabilityDoctor multi-line for gy doctor reliability /metrics.
func FormatReliabilityDoctor(s ReliabilitySnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "reliability · gy %s (%s) %s/%s\n", s.Version, s.Commit, s.GOOS, s.GOARCH)
	fmt.Fprintf(&b, "  uptime %s · goroutines %d · heap %.1f MiB\n", s.Uptime, s.GoRoutines, s.MemAllocMB)
	fmt.Fprintf(&b, "  panics %d · recoveries %d · orch %d · watch %d · news %d\n",
		s.Panics, s.Recoveries, s.OrchTakes, s.WatchStarts, s.NewsStarts)
	fmt.Fprintf(&b, "  media alive %d/%d · news %d/%d · drops %d · kills %d\n",
		s.Media.Alive, s.Media.Max, s.Media.NewsAlive, s.Media.NewsMax, s.Media.Drops, s.Media.Kills)
	if s.LastDump != "" {
		fmt.Fprintf(&b, "  last crash dump: %s\n", s.LastDump)
	}
	b.WriteString("  graceful: Media().Shutdown on exit · Setpgid kill · news soft-restart\n")
	b.WriteString("  env: GY_MEDIA_MAX · GY_NEWS_MAX · GY_NO_AUTO_UPDATE · GY_CRASH_DIR\n")
	return b.String()
}

// FormatMetricsProm is a minimal Prometheus text exposition (no server required — paste/log).
func FormatMetricsProm(s ReliabilitySnapshot) string {
	var b strings.Builder
	b.WriteString("# HELP gy_up 1 if process is running\n")
	b.WriteString("# TYPE gy_up gauge\n")
	b.WriteString("gy_up 1\n")
	fmt.Fprintf(&b, "# HELP gy_uptime_seconds process uptime\n# TYPE gy_uptime_seconds gauge\ngy_uptime_seconds %d\n", int(s.Uptime.Seconds()))
	fmt.Fprintf(&b, "# HELP gy_goroutines current goroutines\n# TYPE gy_goroutines gauge\ngy_goroutines %d\n", s.GoRoutines)
	fmt.Fprintf(&b, "# HELP gy_heap_alloc_bytes heap alloc\n# TYPE gy_heap_alloc_bytes gauge\ngy_heap_alloc_bytes %d\n", int(s.MemAllocMB*1024*1024))
	fmt.Fprintf(&b, "# HELP gy_media_alive supervised media processes\n# TYPE gy_media_alive gauge\ngy_media_alive %d\n", s.Media.Alive)
	fmt.Fprintf(&b, "# HELP gy_media_drops_total spawn rejections\n# TYPE gy_media_drops_total counter\ngy_media_drops_total %d\n", s.Media.Drops)
	fmt.Fprintf(&b, "# HELP gy_media_kills_total supervised kills\n# TYPE gy_media_kills_total counter\ngy_media_kills_total %d\n", s.Media.Kills)
	fmt.Fprintf(&b, "# HELP gy_panics_total recovered panics\n# TYPE gy_panics_total counter\ngy_panics_total %d\n", s.Panics)
	fmt.Fprintf(&b, "# HELP gy_orch_takes_total orchestrate applies\n# TYPE gy_orch_takes_total counter\ngy_orch_takes_total %d\n", s.OrchTakes)
	fmt.Fprintf(&b, "# HELP gy_news_starts_total news wall starts\n# TYPE gy_news_starts_total counter\ngy_news_starts_total %d\n", s.NewsStarts)
	fmt.Fprintf(&b, "# HELP gy_watch_starts_total watch starts\n# TYPE gy_watch_starts_total counter\ngy_watch_starts_total %d\n", s.WatchStarts)
	fmt.Fprintf(&b, "# HELP gy_recoveries_total soft media recoveries\n# TYPE gy_recoveries_total counter\ngy_recoveries_total %d\n", s.Recoveries)
	return b.String()
}

// crashDir returns directory for panic dumps.
func crashDir() string {
	if d := strings.TrimSpace(os.Getenv("GY_CRASH_DIR")); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "grokytalky-crashes")
}

// WriteCrashDump writes panic stack to disk; returns path.
func WriteCrashDump(recovered any, stack []byte) string {
	atomic.AddInt64(&reliPanics, 1)
	dir := crashDir()
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("panic-%s-%d.txt", time.Now().UTC().Format("20060102T150405"), os.Getpid())
	path := filepath.Join(dir, name)
	var b strings.Builder
	fmt.Fprintf(&b, "GrokYtalkY crash dump\n")
	fmt.Fprintf(&b, "version: %s commit: %s\n", Version, Commit)
	fmt.Fprintf(&b, "time: %s\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "go: %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "panic: %v\n\n", recovered)
	b.Write(stack)
	_ = os.WriteFile(path, []byte(b.String()), 0o600)
	reliMu.Lock()
	reliLastDump = path
	reliMu.Unlock()
	return path
}

// WithPanicRecovery runs fn; on panic dumps stack, shuts down media, re-panics or returns.
// If exitOnPanic is true, process exits 2 after cleanup (CLI main).
func WithPanicRecovery(exitOnPanic bool, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			path := WriteCrashDump(r, stack)
			// always try to reap ffmpeg children
			Media().Shutdown()
			fmt.Fprintf(os.Stderr, "gy: panic recovered — dump %s\n", path)
			fmt.Fprintf(os.Stderr, "gy: %v\n", r)
			if exitOnPanic {
				os.Exit(2)
			}
			err = fmt.Errorf("panic: %v (dump %s)", r, path)
		}
	}()
	return fn()
}

// SHA256File returns hex digest of a file (release integrity).
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// WriteSHA256SUMS writes GNU-style checksum lines for paths into outPath.
func WriteSHA256SUMS(outPath string, files []string) error {
	var b strings.Builder
	for _, f := range files {
		sum, err := SHA256File(f)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, filepath.Base(f))
	}
	return os.WriteFile(outPath, []byte(b.String()), 0o644)
}

// VerifySHA256SUMS checks a SUMS file against files in the same directory.
func VerifySHA256SUMS(sumsPath string) error {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return err
	}
	dir := filepath.Dir(sumsPath)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// format: <hex>  <filename> or <hex> *filename
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		want, name := fields[0], fields[len(fields)-1]
		name = strings.TrimPrefix(name, "*")
		got, err := SHA256File(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !strings.EqualFold(got, want) {
			return fmt.Errorf("%s: checksum mismatch", name)
		}
	}
	return nil
}
