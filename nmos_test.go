package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBuildNMOSResources(t *testing.T) {
	b := BuildNMOSResources()
	if b.Node.ID == "" || len(b.Senders) < 4 {
		t.Fatalf("%+v", b)
	}
	labels := map[string]bool{}
	for _, s := range b.Senders {
		labels[s.Label] = true
	}
	for _, want := range []string{"2110-20", "2110-30", "2110-40", "mid-lane"} {
		if !labels[want] {
			t.Fatalf("missing sender %s", want)
		}
	}
	report := FormatNMOSReport(b)
	if !strings.Contains(report, "NMOS") || !strings.Contains(report, "IS-04") {
		t.Fatal(report)
	}
}

func TestSyncClockFromEnvLocked(t *testing.T) {
	t.Setenv("GY_PTP_LOCKED", "1")
	t.Setenv("GY_PTP_DOMAIN", "42")
	t.Setenv("GY_PTP_OFFSET_NS", "100")
	t.Setenv("GY_PTP_IFACE", "en0")
	r := SyncClockFromEnv()
	if r.PTP.Mode != PTPLocked || r.PTP.Domain != 42 || r.PTP.OffsetNs != 100 {
		t.Fatalf("%+v", r.PTP)
	}
	if !r.Compliant {
		// 100 ns is under 1µs budget
		t.Fatal("expected compliant")
	}
}

func TestSyncClockFromEnvFreeRun(t *testing.T) {
	os.Unsetenv("GY_PTP_LOCKED")
	os.Unsetenv("GY_PTP_MODE")
	r := SyncClockFromEnv()
	if r.PTP.Mode != PTPFreeRun {
		t.Fatal(r.PTP.Mode)
	}
	if r.Compliant {
		t.Fatal("free-run must not be compliant")
	}
}

func TestPostNMOSRegistrationRequiresRegistry(t *testing.T) {
	t.Setenv("GY_NMOS_REGISTRY", "")
	b := BuildNMOSResources()
	b.Registry = ""
	if err := PostNMOSRegistration(b); err == nil {
		t.Fatal("expected error")
	}
}

func TestMapHubToMidLaneFullLadder(t *testing.T) {
	bus := NewProgramBus()
	bus.Take(ProgramSource{Source: ProgramSourceGyst, Nick: "c"}, "d")
	msg := bus.MeshJSON("d")
	raw := mustJSON(msg)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["type"] = "program"
	env, ok := MapHubToMidLane(m, "dojo", MidLaneOpts{
		Program: true,
		WhipURL: "https://calls.example/whip",
		PlayURL: "https://cdn.example/play.m3u8",
	})
	if !ok || env.Ladder != "full" || env.WhipURL == "" {
		t.Fatalf("%+v", env)
	}
}
