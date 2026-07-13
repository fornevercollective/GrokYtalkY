package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleReliability(t *testing.T) {
	s := SampleReliability()
	if s.Version == "" {
		t.Fatal("version")
	}
	if s.GoRoutines < 1 {
		t.Fatal("goroutines")
	}
	doc := FormatReliabilityDoctor(s)
	if !strings.Contains(doc, "reliability") || !strings.Contains(doc, "media") {
		t.Fatal(doc)
	}
	prom := FormatMetricsProm(s)
	if !strings.Contains(prom, "gy_up 1") || !strings.Contains(prom, "gy_media_alive") {
		t.Fatal(prom)
	}
}

func TestMetricIncr(t *testing.T) {
	before := metricOrchTakes.Load()
	MetricIncr("orch_takes")
	if metricOrchTakes.Load() != before+1 {
		t.Fatal("orch")
	}
	MetricIncr("recoveries")
	MetricIncr("watch_starts")
	MetricIncr("news_starts")
}

func TestSHA256SUMSRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	b := filepath.Join(dir, "b.bin")
	_ = os.WriteFile(a, []byte("hello"), 0o644)
	_ = os.WriteFile(b, []byte("world"), 0o644)
	sums := filepath.Join(dir, "SHA256SUMS")
	if err := WriteSHA256SUMS(sums, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256SUMS(sums); err != nil {
		t.Fatal(err)
	}
	// tamper
	_ = os.WriteFile(a, []byte("HELLO"), 0o644)
	if err := VerifySHA256SUMS(sums); err == nil {
		t.Fatal("expected mismatch")
	}
}

func TestWriteCrashDump(t *testing.T) {
	t.Setenv("GY_CRASH_DIR", t.TempDir())
	path := WriteCrashDump("test panic", []byte("stack here\n"))
	if path == "" {
		t.Fatal("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test panic") {
		t.Fatal(string(data))
	}
}

func TestWithPanicRecovery(t *testing.T) {
	t.Setenv("GY_CRASH_DIR", t.TempDir())
	err := WithPanicRecovery(false, func() error {
		panic("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
	// normal path
	if err := WithPanicRecovery(false, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestMediaMultiPipeSmoke(t *testing.T) {
	// supervisor backpressure + multi register without real ffmpeg
	s := &MediaSupervisor{procs: make(map[string]*MediaProc), max: 4, newsMax: 3}
	var ids []string
	for i := 0; i < 3; i++ {
		cmd := sleepCmd(t)
		id, err := s.Register(MediaKindNews, "t"+string(rune('a'+i)), cmd)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	h := s.Health()
	if h.NewsAlive != 3 {
		t.Fatalf("news %d", h.NewsAlive)
	}
	// 4th news should fail news max
	cmd := sleepCmd(t)
	_, err := s.Register(MediaKindNews, "overflow", cmd)
	if err == nil {
		t.Fatal("expected news cap")
	}
	s.Shutdown()
	if s.Health().Alive != 0 {
		t.Fatal("shutdown leak")
	}
	_ = ids
}
