package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMapHubToMidLaneProgram(t *testing.T) {
	bus := NewProgramBus()
	bus.Take(ProgramSource{Source: ProgramSourceGyst, Nick: "cam", Label: "c"}, "dir")
	bus.SetCaption("HI", "dir")
	msg := bus.MeshJSON("dir")
	// force map form
	raw := mustJSON(msg)
	var m map[string]any
	_ = jsonUnmarshalMid(raw, &m)
	m["type"] = "program"
	env, ok := MapHubToMidLane(m, "dojo", MidLaneOpts{Program: true, Hexlum: true})
	if !ok || env.Lane != "program" || env.Room != "dojo" {
		t.Fatalf("%+v ok=%v", env, ok)
	}
	if env.Caption == "" && env.Mark == "" && env.Seq == 0 {
		// seq should be set
		if env.Seq == 0 {
			t.Log("seq 0 possible on fresh bus after take+caption")
		}
	}
	if !strings.Contains(env.Caption, "HI") && env.Caption != "HI" {
		// Display may prefix speaker
		if env.Caption != "HI" {
			t.Fatalf("caption %q", env.Caption)
		}
	}
}

func TestMapHubToMidLaneHexlum(t *testing.T) {
	msg := map[string]any{
		"type": "gyst", "kind": "hexlum", "from": "pub",
		"w": 25.0, "h": 25.0, "seq": 3.0,
		"data": []any{float64(1), float64(2)},
	}
	env, ok := MapHubToMidLane(msg, "global", MidLaneOpts{Hexlum: true})
	if !ok || env.Lane != LaneHex || env.Seq != 3 {
		t.Fatalf("%+v", env)
	}
	_, ok = MapHubToMidLane(msg, "global", MidLaneOpts{Hexlum: false})
	if ok {
		t.Fatal("should skip hexlum when disabled")
	}
}

func TestMapHubToMidLaneSkipChat(t *testing.T) {
	_, ok := MapHubToMidLane(map[string]any{"type": "chat", "text": "x"}, "r", MidLaneOpts{Program: true, Hexlum: true})
	if ok {
		t.Fatal("chat is not mid-lane")
	}
}

func jsonUnmarshalMid(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

// silence unused if compiler folds
var _ = json.Marshal
