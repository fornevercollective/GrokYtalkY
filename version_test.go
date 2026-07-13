package main

import (
	"strings"
	"testing"
)

func TestNormalizeAndCompareSemver(t *testing.T) {
	if normalizeVersion("v1.9.0-burst") != "1.9.0" {
		t.Fatalf("normalize: %s", normalizeVersion("v1.9.0-burst"))
	}
	if compareSemver("1.8.0", "1.9.0") != -1 {
		t.Fatal("1.8 < 1.9")
	}
	if compareSemver("1.9.0", "v1.9.0") != 0 {
		t.Fatal("equal")
	}
	if compareSemver("2.0.0", "1.9.9") != 1 {
		t.Fatal("2 > 1.9")
	}
}

func TestInstallChannel(t *testing.T) {
	if installChannel("/opt/homebrew/Cellar/grokytalky/1.9.0/bin/gy") != "homebrew" {
		t.Fatal("homebrew")
	}
	if installChannel("/Users/x/go/bin/grokytalky") != "go-install" {
		t.Fatal("go")
	}
	if installChannel("/Users/x/.local/bin/gy") != "local" {
		t.Fatal("local")
	}
}

func TestAutoUpdateDisabledEnv(t *testing.T) {
	t.Setenv("GY_SKIP_AUTO_UPDATE", "")
	t.Setenv("GY_NO_AUTO_UPDATE", "")
	t.Setenv("GY_AUTO_UPDATE", "")
	if autoUpdateDisabled() {
		t.Fatal("default enabled")
	}
	t.Setenv("GY_NO_AUTO_UPDATE", "1")
	if !autoUpdateDisabled() {
		t.Fatal("GY_NO_AUTO_UPDATE")
	}
	t.Setenv("GY_NO_AUTO_UPDATE", "")
	t.Setenv("GY_AUTO_UPDATE", "off")
	if !autoUpdateDisabled() {
		t.Fatal("GY_AUTO_UPDATE=off")
	}
	t.Setenv("GY_AUTO_UPDATE", "check")
	if autoUpdateDisabled() {
		t.Fatal("check is not disabled")
	}
	if !autoUpdateCheckOnly() {
		t.Fatal("check only")
	}
	t.Setenv("GY_SKIP_AUTO_UPDATE", "1")
	if !autoUpdateDisabled() {
		t.Fatal("skip after re-exec")
	}
}

func TestFilterEnv(t *testing.T) {
	in := []string{"A=1", "GY_SKIP_AUTO_UPDATE=1", "B=2", "GY_JUST_UPDATED=1"}
	out := filterEnv(in, "GY_SKIP_AUTO_UPDATE", "GY_JUST_UPDATED")
	s := strings.Join(out, ",")
	if strings.Contains(s, "GY_SKIP") || strings.Contains(s, "GY_JUST") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "A=1") || !strings.Contains(s, "B=2") {
		t.Fatal(s)
	}
}
