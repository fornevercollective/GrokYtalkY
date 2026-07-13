package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHelpMentionsLifecycle(t *testing.T) {
	h := installHelp()
	for _, s := range []string{
		"gy install",
		"gy uninstall",
		"clean install",
		"dependencies",
		"gy update",
		"upgrade",
	} {
		if !strings.Contains(h, s) {
			t.Fatalf("installHelp missing %q", s)
		}
	}
}

func TestDefaultInstallBinDir(t *testing.T) {
	t.Setenv("PREFIX", "")
	d := defaultInstallBinDir()
	if !strings.Contains(d, ".local") && !strings.HasSuffix(d, "bin") {
		// still ok if home missing in sandbox — just must be non-empty
		if d == "" {
			t.Fatal("empty bin dir")
		}
	}
	t.Setenv("PREFIX", "/tmp/gy-prefix-test")
	d2 := defaultInstallBinDir()
	if d2 != filepath.Join("/tmp/gy-prefix-test", "bin") {
		t.Fatalf("PREFIX bin=%s", d2)
	}
}

func TestWalkForModFindsCheckout(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// test runs from module root
	if !fileExists(filepath.Join(wd, "go.mod")) {
		t.Skip("not in module root")
	}
	root := walkForMod(wd)
	if root == "" {
		t.Fatal("expected repo root from cwd")
	}
	if !fileExists(filepath.Join(root, "main.go")) {
		t.Fatalf("root %s missing main.go", root)
	}
}

func TestRunInstallCmdUnknown(t *testing.T) {
	err := runInstallCmd("not-a-verb", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
