package main

import (
	"os/exec"
	"testing"
	"time"
)

// sleepCmd starts a long-lived process for supervisor smoke tests.
func sleepCmd(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	PrepMediaCmd(cmd)
	if err := cmd.Start(); err != nil {
		t.Skip("sleep:", err)
	}
	return cmd
}

func TestMediaSupervisorKillKindSmoke(t *testing.T) {
	s := &MediaSupervisor{procs: make(map[string]*MediaProc), max: 8, newsMax: 4}
	for i := 0; i < 3; i++ {
		cmd := sleepCmd(t)
		_, err := s.Register(MediaKindNews, "n", cmd)
		if err != nil {
			t.Fatal(err)
		}
	}
	wcmd := sleepCmd(t)
	_, err := s.Register(MediaKindWatch, "w", wcmd)
	if err != nil {
		t.Fatal(err)
	}
	n := s.KillKind(MediaKindNews)
	if n != 3 {
		t.Fatalf("killed %d", n)
	}
	time.Sleep(30 * time.Millisecond)
	h := s.Health()
	if h.NewsAlive != 0 {
		t.Fatal("news remain")
	}
	if h.Alive != 1 {
		t.Fatalf("watch should remain alive=%d", h.Alive)
	}
	s.Shutdown()
}

func TestMediaGlobalMaxSmoke(t *testing.T) {
	s := &MediaSupervisor{procs: make(map[string]*MediaProc), max: 2, newsMax: 10}
	c1 := sleepCmd(t)
	c2 := sleepCmd(t)
	if _, err := s.Register(MediaKindWatch, "a", c1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(MediaKindAudio, "b", c2); err != nil {
		t.Fatal(err)
	}
	c3 := sleepCmd(t)
	if _, err := s.Register(MediaKindOther, "c", c3); err == nil {
		t.Fatal("global max")
	}
	if s.Health().Drops < 1 {
		t.Fatal("drops")
	}
	s.Shutdown()
}
