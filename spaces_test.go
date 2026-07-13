package main

import (
	"strings"
	"testing"
)

func TestParseSpaceURL(t *testing.T) {
	id, canon, ok := ParseSpaceURL("https://x.com/i/spaces/1AJEmmANrPeJL?s=20")
	if !ok || id != "1AJEmmANrPeJL" {
		t.Fatalf("id %q ok=%v", id, ok)
	}
	if !strings.Contains(canon, "1AJEmmANrPeJL") {
		t.Fatal(canon)
	}
	id2, _, ok2 := ParseSpaceURL("1AJEmmANrPeJL")
	if !ok2 || id2 != "1AJEmmANrPeJL" {
		t.Fatal(id2)
	}
}

func TestNormalizeSpaceID(t *testing.T) {
	if NormalizeSpaceID("https://twitter.com/i/spaces/AbC123xyz") != "AbC123xyz" {
		t.Fatal(NormalizeSpaceID("https://twitter.com/i/spaces/AbC123xyz"))
	}
}

func TestRTMPPublishURL(t *testing.T) {
	cfg := SpaceRTMPConfig{Secure: true, StreamKey: "secretKEY"}
	pub, err := cfg.PublishURL()
	if err != nil {
		t.Fatal(err)
	}
	if pub != XRTMPSURL+"/secretKEY" {
		t.Fatal(pub)
	}
	cfg2 := SpaceRTMPConfig{Secure: false, StreamKey: "k"}
	pub2, _ := cfg2.PublishURL()
	if pub2 != XRTMPURL+"/k" {
		t.Fatal(pub2)
	}
	_, err = (SpaceRTMPConfig{Secure: true}).PublishURL()
	if err == nil {
		t.Fatal("expected not ready")
	}
}

func TestBuildSpaceRTMPArgs(t *testing.T) {
	cfg := SpaceRTMPConfig{Secure: true, StreamKey: "abc"}
	args, pub, err := BuildSpaceRTMPArgs("clip.mp4", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(pub, "/abc") {
		t.Fatal(pub)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "libx264") || !strings.Contains(joined, "flv") {
		t.Fatal(joined)
	}
	if _, _, err := BuildSpaceRTMPArgs("", cfg); err == nil {
		t.Fatal("empty input")
	}
}

func TestSpaceSeatAndLevel(t *testing.T) {
	s := NewSpaceState("1AJEmmANrPeJL")
	if err := s.Seat(SpaceRoleHost, 0, "qbit"); err != nil {
		t.Fatal(err)
	}
	if err := s.Seat(SpaceRoleCohost, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Seat(SpaceRoleSpeaker, 9, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := s.Seat(SpaceRoleSpeaker, 10, "nope"); err == nil {
		t.Fatal("expected range err")
	}
	s.SetLevel(SpaceRoleSpeaker, 9, 0.7)
	snap := s.Snapshot()
	if snap.Speakers[9].Nick != "bob" || snap.Speakers[9].Level < 0.69 {
		t.Fatalf("%+v", snap.Speakers[9])
	}
	if snap.Cohosts[1].Nick != "alice" {
		t.Fatal(snap.Cohosts)
	}
}

func TestFormatSpaceDoctor(t *testing.T) {
	s := NewSpaceState("1AJEmmANrPeJL")
	_ = s.Seat(SpaceRoleHost, 0, "hosty")
	doc := FormatSpaceDoctor(s)
	if !strings.Contains(doc, "1AJEmmANrPeJL") || !strings.Contains(doc, "rtmps://ca.pscp.tv") {
		t.Fatal(doc)
	}
	if !strings.Contains(doc, "available when ready") {
		t.Fatal(doc)
	}
	s.SetStreamKey("longstreamkey99")
	doc2 := FormatSpaceDoctor(s)
	if !strings.Contains(doc2, "ready") {
		t.Fatal(doc2)
	}
}

func TestApplySpaceMeshInbound(t *testing.T) {
	// use fresh via SetID on global
	Spaces().SetID("1AJEmmANrPeJL")
	ApplySpaceMeshInbound(map[string]any{
		"type": "space-roster",
		"space": "1AJEmmANrPeJL",
		"caption": "hello stage",
		"listeners": float64(12),
		"host": map[string]any{"nick": "h1", "level": 0.5},
		"speakers": []any{
			map[string]any{"index": float64(0), "nick": "s0", "level": 0.2},
		},
	})
	snap := Spaces().Snapshot()
	if snap.Caption != "hello stage" || snap.Listeners != 12 {
		t.Fatalf("%+v", snap)
	}
	if snap.Host.Nick != "h1" {
		t.Fatal(snap.Host)
	}
	ApplySpaceMeshInbound(map[string]any{
		"type": "space-chat", "from": "h1", "text": "hi chat", "role": "host",
	})
	if len(Spaces().Snapshot().Chat) < 1 {
		t.Fatal("chat")
	}
}

func TestValidateRTMPBase(t *testing.T) {
	if err := ValidateRTMPBase(XRTMPSURL); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRTMPBase("http://nope"); err == nil {
		t.Fatal("expected err")
	}
}

func TestParseSeatSpec(t *testing.T) {
	r, i, err := parseSeatSpec("cohost:1")
	if err != nil || r != SpaceRoleCohost || i != 1 {
		t.Fatal(r, i, err)
	}
	r, i, err = parseSeatSpec("speaker:7")
	if err != nil || r != SpaceRoleSpeaker || i != 7 {
		t.Fatal(r, i, err)
	}
}
