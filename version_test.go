package main

import "testing"

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
