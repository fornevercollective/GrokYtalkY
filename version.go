package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Set at link time via -ldflags, e.g.:
//
//	-X main.Version=1.9.1 -X main.Commit=abc1234 -X main.Date=2026-07-12
//
// Defaults are used for plain `go build` / `go run`.
var (
	// Default when not set by ldflags. make install uses git describe.
	Version = "1.19.0"
	Commit  = "dev"
	Date    = "unknown"
)

const (
	githubOwner = "fornevercollective"
	githubRepo  = "GrokYtalkY"
	// module path for `go install` updates
	goModule = "github.com/fornevercollective/grokytalky"
)

// versionLine is the canonical one-liner for --version / version cmd.
func versionLine() string {
	return fmt.Sprintf("GrokYtalkY %s", Version)
}

// versionDetail multi-line for `gy version`.
func versionDetail() string {
	var b strings.Builder
	fmt.Fprintf(&b, "GrokYtalkY %s\n", Version)
	fmt.Fprintf(&b, "  commit:  %s\n", Commit)
	fmt.Fprintf(&b, "  built:   %s\n", Date)
	fmt.Fprintf(&b, "  go:      %s\n", runtime.Version())
	fmt.Fprintf(&b, "  os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if p, err := os.Executable(); err == nil {
		if r, err2 := filepath.EvalSymlinks(p); err2 == nil {
			p = r
		}
		fmt.Fprintf(&b, "  binary:  %s\n", p)
		fmt.Fprintf(&b, "  channel: %s\n", installChannel(p))
	}
	return b.String()
}

func installChannel(binPath string) string {
	p := strings.ToLower(binPath)
	switch {
	case strings.Contains(p, "/cellar/") || strings.Contains(p, "homebrew"):
		return "homebrew"
	case strings.Contains(p, "/go/bin/") || strings.Contains(p, filepath.Join("go", "bin")):
		return "go-install"
	case strings.Contains(p, "/.local/bin/"):
		return "local" // make install / scripts/install.sh
	default:
		return "unknown"
	}
}

// ── GitHub latest release ────────────────────────────────────

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Published  string `json:"published_at"`
}

func fetchLatestRelease(timeout time.Duration) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GrokYtalkY/"+Version)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusNotFound {
		// no releases yet — try tags
		return fetchLatestTag(timeout)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API %s: %s", res.Status, truncate(string(body), 120))
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("empty release tag")
	}
	return &rel, nil
}

func fetchLatestTag(timeout time.Duration) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/tags?per_page=1", githubOwner, githubRepo)
	client := &http.Client{Timeout: timeout}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "GrokYtalkY/"+Version)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("no releases/tags yet (HTTP %s)", res.Status)
	}
	var tags []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tags); err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no tags on %s/%s", githubOwner, githubRepo)
	}
	return &ghRelease{
		TagName: tags[0].Name,
		HTMLURL: fmt.Sprintf("https://github.com/%s/%s/releases", githubOwner, githubRepo),
	}, nil
}

// normalizeVersion strips leading v and build suffixes like -burst, -dock.
func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	// drop +metadata and -prerelease for coarse compare (keep numeric core)
	if i := strings.IndexAny(s, "+"); i >= 0 {
		s = s[:i]
	}
	// keep prerelease for semver compare but strip known app suffixes after first -
	// e.g. 1.9.0-burst → 1.9.0 for "is latest" UX
	parts := strings.SplitN(s, "-", 2)
	return parts[0]
}

// compareSemver returns -1 if a<b, 0 if equal, 1 if a>b (numeric dotted only).
func compareSemver(a, b string) int {
	a, b = normalizeVersion(a), normalizeVersion(b)
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(as) {
			fmt.Sscanf(as[i], "%d", &ai)
		}
		if i < len(bs) {
			fmt.Sscanf(bs[i], "%d", &bi)
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// ── update command ───────────────────────────────────────────

type updateResult struct {
	Current string
	Latest  string
	URL     string
	Channel string
	Status  string // up-to-date | update-available | unknown | error
	Err     error
}

func checkUpdate() updateResult {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	ch := installChannel(exe)
	res := updateResult{Current: Version, Channel: ch}
	rel, err := fetchLatestRelease(8 * time.Second)
	if err != nil {
		res.Status = "error"
		res.Err = err
		return res
	}
	res.Latest = strings.TrimPrefix(rel.TagName, "v")
	res.URL = rel.HTMLURL
	switch compareSemver(Version, rel.TagName) {
	case 0:
		res.Status = "up-to-date"
	case -1:
		res.Status = "update-available"
	default:
		// local newer than remote (dev / dirty)
		res.Status = "ahead"
	}
	return res
}

func printUpdateCheck(res updateResult) {
	fmt.Printf("current:  %s\n", res.Current)
	if res.Err != nil {
		fmt.Printf("latest:   (unavailable: %v)\n", res.Err)
		fmt.Printf("channel:  %s\n", res.Channel)
		fmt.Println("status:   unknown — publish a GitHub release/tag to enable checks")
		return
	}
	fmt.Printf("latest:   %s\n", res.Latest)
	fmt.Printf("channel:  %s\n", res.Channel)
	switch res.Status {
	case "up-to-date":
		fmt.Println("status:   up to date")
	case "ahead":
		fmt.Println("status:   local is newer than GitHub (dev build)")
	case "update-available":
		fmt.Println("status:   update available")
		if res.URL != "" {
			fmt.Printf("release:  %s\n", res.URL)
		}
	default:
		fmt.Printf("status:   %s\n", res.Status)
	}
}

// runUpdate checks GitHub and optionally installs via the same channel.
func runUpdate(checkOnly bool) error {
	res := checkUpdate()
	printUpdateCheck(res)
	if checkOnly {
		if res.Status == "update-available" {
			return errUpdateAvailable // exit 2 for scripts
		}
		// no tags / network: not a hard failure for --check
		return nil
	}
	if res.Status == "up-to-date" || res.Status == "ahead" {
		return nil
	}
	if res.Err != nil {
		// still try go install @latest (works without tags via module proxy)
		fmt.Println()
		fmt.Println("no GitHub release metadata — trying go install @latest anyway")
		if _, err := exec.LookPath("go"); err == nil {
			return goInstallLatestToPreferred()
		}
		return res.Err
	}
	if res.Status != "update-available" {
		return nil
	}

	fmt.Println()
	return goInstallLatestToPreferred()
}

// goInstallLatestToPreferred picks GOBIN from install channel.
func goInstallLatestToPreferred() error {
	exe := mustExe()
	ch := installChannel(exe)
	switch ch {
	case "homebrew":
		fmt.Println("→ Homebrew install detected")
		fmt.Println("  brew update && brew upgrade grokytalky")
		if err := runCmdHint("brew", "upgrade", "grokytalky"); err != nil {
			fmt.Println("  (formula may be local-only — use: brew install --build-from-source ./Formula/grokytalky.rb)")
			return err
		}
		return nil
	case "local":
		fmt.Println("→ local (~/.local/bin) install")
		if _, err := exec.LookPath("go"); err == nil {
			return goInstallLatestTo(filepath.Dir(exe))
		}
		fmt.Println("  re-run from checkout: git pull && make install")
		return fmt.Errorf("auto-update needs go on PATH, or pull + make install")
	case "go-install":
		fmt.Println("→ go install channel")
		return goInstallLatest()
	default:
		fmt.Println("→ trying go install @latest")
		if _, err := exec.LookPath("go"); err == nil {
			return goInstallLatest()
		}
		return fmt.Errorf("cannot auto-update; install go or use brew/make")
	}
}

type updateAvail struct{}

func (updateAvail) Error() string { return "update available" }

// errUpdateAvailable signals exit code 2 for --check when outdated.
var errUpdateAvailable error = updateAvail{}

func mustExe() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func goInstallLatest() error {
	mod := goModule + "@latest"
	fmt.Printf("  go install %s\n", mod)
	cmd := exec.Command("go", "install", mod)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install: %w", err)
	}
	fmt.Println("done — re-open shell or hash -r if the binary path is cached")
	return nil
}

func goInstallLatestTo(dir string) error {
	mod := goModule + "@latest"
	fmt.Printf("  GOBIN=%s go install %s\n", dir, mod)
	cmd := exec.Command("go", "install", mod)
	cmd.Env = append(os.Environ(), "GOBIN="+dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install: %w", err)
	}
	// ensure short alias
	full := filepath.Join(dir, "grokytalky")
	short := filepath.Join(dir, "gy")
	if _, err := os.Stat(full); err == nil {
		_ = os.Remove(short)
		_ = os.Symlink("grokytalky", short)
	}
	fmt.Printf("updated %s (+ gy symlink)\n", full)
	return nil
}

func runCmdHint(name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not on PATH", name)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
