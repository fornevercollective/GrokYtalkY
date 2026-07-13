package main

import (
	"os/exec"
	"testing"
	"time"
)

func TestMediaSupervisorRegisterKill(t *testing.T) {
	s := &MediaSupervisor{
		procs:   make(map[string]*MediaProc),
		max:     4,
		newsMax: 2,
	}
	// sleep long enough to register
	cmd := exec.Command("sleep", "30")
	PrepMediaCmd(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	id, err := s.Register(MediaKindWatch, "test", cmd)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	h := s.Health()
	if h.Alive != 1 {
		t.Fatalf("alive %d", h.Alive)
	}
	s.Kill(id)
	time.Sleep(50 * time.Millisecond)
	h = s.Health()
	if h.Alive != 0 {
		t.Fatalf("after kill alive %d", h.Alive)
	}
}

func TestMediaSupervisorBackpressure(t *testing.T) {
	s := &MediaSupervisor{
		procs:   make(map[string]*MediaProc),
		max:     1,
		newsMax: 1,
	}
	cmd1 := exec.Command("sleep", "30")
	PrepMediaCmd(cmd1)
	if err := cmd1.Start(); err != nil {
		t.Skip(err)
	}
	id1, err := s.Register(MediaKindNews, "a", cmd1)
	if err != nil {
		t.Fatal(err)
	}
	cmd2 := exec.Command("sleep", "30")
	PrepMediaCmd(cmd2)
	if err := cmd2.Start(); err != nil {
		t.Fatal(err)
	}
	_, err = s.Register(MediaKindNews, "b", cmd2)
	if err == nil {
		t.Fatal("expected backpressure")
	}
	// cmd2 should be killed by Register
	s.Kill(id1)
	s.Shutdown()
}

func TestFormatMediaHealth(t *testing.T) {
	h := MediaHealth{Alive: 2, Max: 16, NewsAlive: 2, NewsMax: 8, Drops: 1}
	s := FormatMediaHealthChrome(h)
	if s == "" {
		t.Fatal("empty chrome")
	}
	d := FormatMediaHealthDetail(h)
	if d == "" {
		t.Fatal("empty detail")
	}
}

func TestCanSpawnNewsMax(t *testing.T) {
	s := &MediaSupervisor{procs: make(map[string]*MediaProc), max: 10, newsMax: 1}
	cmd := exec.Command("sleep", "5")
	PrepMediaCmd(cmd)
	_ = cmd.Start()
	_, _ = s.Register(MediaKindNews, "n1", cmd)
	if s.CanSpawn(MediaKindNews) {
		t.Fatal("news max")
	}
	if !s.CanSpawn(MediaKindWatch) {
		t.Fatal("watch should still be ok under global max")
	}
	s.Shutdown()
}
