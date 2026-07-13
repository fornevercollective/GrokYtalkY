package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSamplePlatformReadiness(t *testing.T) {
	r := SamplePlatformReadiness()
	if r.Version == "" {
		t.Fatal("version")
	}
	if len(r.Checks) < 5 {
		t.Fatalf("checks %d", len(r.Checks))
	}
	if r.Score < 0 || r.Score > 100 {
		t.Fatal(r.Score)
	}
	if r.Status != "ready" && r.Status != "partial" && r.Status != "blocked" {
		t.Fatal(r.Status)
	}
	doc := FormatPlatformDoctor(r)
	if !strings.Contains(doc, "platform") || !strings.Contains(doc, "ffmpeg") {
		t.Fatal(doc)
	}
}

func TestPlatformExportJSON(t *testing.T) {
	raw, err := PlatformExportJSON(SamplePlatformReadiness(), true)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["product"] != "GrokYtalkY" {
		t.Fatal(m["product"])
	}
	if _, ok := m["readiness"]; !ok {
		t.Fatal("readiness")
	}
}

func TestPlatformContractFile(t *testing.T) {
	// from repo root or via find
	p := findPlatformContract()
	if p == "" {
		// try relative to this file's package cwd
		if _, err := os.Stat("integrations/grok-stream-platform.json"); err != nil {
			t.Skip("contract not in cwd")
		}
		p = "integrations/grok-stream-platform.json"
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] == nil || doc["version"] == nil {
		t.Fatal(doc)
	}
	if planes, ok := doc["planes"].(map[string]any); !ok || planes["ffmpeg"] == nil {
		t.Fatal("planes.ffmpeg")
	}
}

func TestPlatformUsage(t *testing.T) {
	u := platformUsage()
	if !strings.Contains(u, "export") || !strings.Contains(u, "doctor") {
		t.Fatal(u)
	}
}

func TestFindPlatformContractWalk(t *testing.T) {
	// create temp contract under nested dir and chdir
	root := t.TempDir()
	dir := filepath.Join(root, "integrations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "grok-stream-platform.json")
	if err := os.WriteFile(path, []byte(`{"name":"t","version":"0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	got := findPlatformContract()
	if got == "" {
		t.Fatal("expected find")
	}
}
