//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePopOutTargetFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(p, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	target, via, err := resolvePopOutTarget(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if via != "file" {
		t.Fatal(via)
	}
	if target == "" {
		t.Fatal("empty target")
	}
}

func TestResolvePopOutTargetStreamURL(t *testing.T) {
	u := "https://example.com/live/playlist.m3u8"
	target, via, err := resolvePopOutTarget("ignored", u)
	if err != nil {
		t.Fatal(err)
	}
	if target != u || via != "stream" {
		t.Fatalf("%s %s", target, via)
	}
}

func TestResolvePopOutEmpty(t *testing.T) {
	_, _, err := resolvePopOutTarget("", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPopOutSupportedDarwin(t *testing.T) {
	if !PopOutSupported() {
		t.Fatal("darwin should support PiP")
	}
	if PopOutPlayerName() == "" {
		t.Fatal("name")
	}
}
