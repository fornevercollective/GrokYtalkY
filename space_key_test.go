package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeStreamKey(t *testing.T) {
	if sanitizeStreamKey("  abcdefgh  \n") != "abcdefgh" {
		t.Fatal(sanitizeStreamKey("  abcdefgh  \n"))
	}
	if sanitizeStreamKey("# comment") != "" {
		t.Fatal("comment")
	}
	if sanitizeStreamKey("stream_key=xyz12345") != "xyz12345" {
		t.Fatal(sanitizeStreamKey("stream_key=xyz12345"))
	}
}

func TestPullStreamKeyEnv(t *testing.T) {
	t.Setenv("GY_X_STREAM_KEY", "envkey12345")
	// clear file override
	t.Setenv("GY_X_STREAM_KEY_FILE", filepath.Join(t.TempDir(), "none"))
	src, key, err := PullStreamKey(PullKeyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if key != "envkey12345" || !strings.Contains(src, "env") {
		t.Fatal(src, key)
	}
}

func TestPullStreamKeyFile(t *testing.T) {
	t.Setenv("GY_X_STREAM_KEY", "")
	dir := t.TempDir()
	p := filepath.Join(dir, "key.txt")
	_ = os.WriteFile(p, []byte("filekey99999\n"), 0o600)
	src, key, err := PullStreamKey(PullKeyOpts{File: p})
	if err != nil || key != "filekey99999" {
		t.Fatal(src, key, err)
	}
}

func TestPullStreamKeyJSON(t *testing.T) {
	t.Setenv("GY_X_STREAM_KEY", "")
	t.Setenv("GY_X_STREAM_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	dir := t.TempDir()
	// point default home-less via explicit file only — use opts.File for plain;
	// JSON path uses DefaultStreamKeyJSONPath — write to cwd relative via temp chdir
	jp := filepath.Join(dir, "x-rtmp.json")
	_ = os.WriteFile(jp, []byte(`{"stream_key":"jsonkey88888","secure":true}`), 0o600)
	// readStreamKeyJSON unit
	k, base, sec, err := readStreamKeyJSON(jp)
	if err != nil || k != "jsonkey88888" {
		t.Fatal(k, base, sec, err)
	}
	if sec == nil || !*sec {
		t.Fatal("secure")
	}
}

func TestWriteStreamKeyFile(t *testing.T) {
	// use temp home via GY_X_STREAM_KEY_FILE
	p := filepath.Join(t.TempDir(), "k")
	t.Setenv("GY_X_STREAM_KEY_FILE", p)
	path, err := WriteStreamKeyFile("writtenkey123")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "writtenkey123") {
		t.Fatal(string(b))
	}
}

func TestMuteAndListeners(t *testing.T) {
	s := NewSpaceState("1AJEmmANrPeJL")
	_ = s.Seat(SpaceRoleSpeaker, 0, "bob")
	if err := s.SetMute(SpaceRoleSpeaker, 0, true, "host"); err != nil {
		t.Fatal(err)
	}
	s.SetLevel(SpaceRoleSpeaker, 0, 0.9)
	if s.Snapshot().Speakers[0].Level != 0 || !s.Snapshot().Speakers[0].Muted {
		t.Fatal("muted should zero level")
	}
	s.SetMuteAll(true, "host")
	if !s.Snapshot().MuteAll || !s.Snapshot().Speakers[1].Muted {
		t.Fatal("mute all")
	}
	s.AddListener("alice", "1")
	s.AddListener("alice", "1") // idempotent
	s.AddListener("carol", "2")
	if s.Snapshot().Listeners != 2 {
		t.Fatal(s.Snapshot().Listeners)
	}
	s.RemoveListener("alice")
	if s.Snapshot().Listeners != 1 {
		t.Fatal(s.Snapshot().ListenerList)
	}
}

func TestPublicAPISnapshotNoKey(t *testing.T) {
	s := NewSpaceState("1AJEmmANrPeJL")
	s.SetStreamKey("supersecretkey99")
	api := s.PublicAPISnapshot()
	raw, _ := jsonMarshal(api)
	if strings.Contains(string(raw), "supersecretkey99") {
		t.Fatal("key leaked in public API")
	}
	rtmp, _ := api["rtmp"].(map[string]any)
	if ready, _ := rtmp["ready"].(bool); !ready {
		t.Fatal("ready")
	}
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
