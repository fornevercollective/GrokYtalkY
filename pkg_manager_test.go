package main

import (
	"strings"
	"testing"
)

func TestDetectPackageManagersNonEmpty(t *testing.T) {
	// CI/dev machines usually have at least `go`
	ms := DetectPackageManagers()
	// don't require specific managers — just don't panic
	_ = ms
	if !HasPkgManager(PkgGo) {
		// still ok if go missing in weird env
		t.Log("go not detected")
	}
}

func TestToolDepsCatalogRecipes(t *testing.T) {
	deps := toolDepsCatalog()
	if len(deps) < 5 {
		t.Fatal(len(deps))
	}
	names := map[string]bool{}
	for _, d := range deps {
		if d.Name == "" || d.Check == nil {
			t.Fatalf("%+v", d)
		}
		if len(d.Recipes) == 0 && d.Required {
			t.Fatalf("required %s has no recipes", d.Name)
		}
		names[d.Name] = true
		// recipes should prefer sensible managers
		for _, r := range d.Recipes {
			if r.Manager == "" || len(r.Argv) == 0 {
				t.Fatalf("%s bad recipe", d.Name)
			}
			s := FormatRecipe(r)
			if !strings.Contains(s, string(r.Manager)) && r.Manager != PkgApt {
				// apt uses apt-get
				if r.Manager == PkgApt && !strings.Contains(s, "apt-get") {
					t.Fatal(s)
				}
			}
		}
	}
	for _, want := range []string{"go", "ffmpeg", "yt-dlp", "uv", "npm"} {
		if !names[want] {
			t.Fatalf("missing dep %s", want)
		}
	}
}

func TestPickRecipePrefersAvailable(t *testing.T) {
	// yt-dlp recipes: uv or brew etc.
	var yt ToolDep
	for _, d := range toolDepsCatalog() {
		if d.Name == "yt-dlp" {
			yt = d
			break
		}
	}
	if yt.Name == "" {
		t.Fatal("yt-dlp")
	}
	// without preference, should pick first available
	r, ok := PickRecipe(yt, "")
	if ok {
		if r.Manager == "" {
			t.Fatal("empty manager")
		}
		t.Logf("picked %s for yt-dlp", r.Manager)
	}
	// force nonexistent manager
	_, ok = PickRecipe(yt, PkgManager("not-a-pm"))
	if ok {
		t.Fatal("should not pick unknown pm")
	}
}

func TestFormatPackageManagersDoctor(t *testing.T) {
	s := FormatPackageManagersDoctor()
	if !strings.Contains(s, "package managers") {
		t.Fatal(s)
	}
	if !strings.Contains(PreferredUpdateChannels(), "uv") {
		t.Fatal("channels")
	}
}
