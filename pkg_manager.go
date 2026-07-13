package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Package managers we can detect and use for deps / tooling.
// Recommended install order is per-tool (see toolDepsCatalog), not global.
type PkgManager string

const (
	PkgBrew   PkgManager = "brew"
	PkgUV     PkgManager = "uv"   // Astral uv — preferred for Python tools (yt-dlp)
	PkgPipx   PkgManager = "pipx" // isolated Python CLIs
	PkgPip    PkgManager = "pip"  // last-resort Python
	PkgNpm    PkgManager = "npm"  // edge mid-lane / wrangler / site tooling
	PkgCargo  PkgManager = "cargo"
	PkgGo     PkgManager = "go" // gy itself + go install tools
	PkgApt    PkgManager = "apt"
	PkgDnf    PkgManager = "dnf"
	PkgPacman PkgManager = "pacman"
	PkgWinget PkgManager = "winget"
	PkgChoco  PkgManager = "choco"
)

// PkgManagerInfo is a detected manager on this machine.
type PkgManagerInfo struct {
	ID      PkgManager
	Path    string
	Version string
	Role    string // system | language | optional
}

// DetectPackageManagers returns available package managers (present on PATH).
func DetectPackageManagers() []PkgManagerInfo {
	type cand struct {
		id   PkgManager
		bin  string
		role string
		ver  []string // version args
	}
	cands := []cand{
		{PkgBrew, "brew", "system", []string{"--version"}},
		{PkgUV, "uv", "language", []string{"--version"}},
		{PkgPipx, "pipx", "language", []string{"--version"}},
		{PkgPip, "pip3", "language", []string{"--version"}},
		{PkgPip, "pip", "language", []string{"--version"}},
		{PkgNpm, "npm", "language", []string{"--version"}},
		{PkgCargo, "cargo", "language", []string{"--version"}},
		{PkgGo, "go", "language", []string{"version"}},
		{PkgApt, "apt-get", "system", []string{"--version"}},
		{PkgDnf, "dnf", "system", []string{"--version"}},
		{PkgPacman, "pacman", "system", []string{"--version"}},
		{PkgWinget, "winget", "system", []string{"--version"}},
		{PkgChoco, "choco", "system", []string{"--version"}},
	}
	seen := map[PkgManager]bool{}
	var out []PkgManagerInfo
	for _, c := range cands {
		if seen[c.id] && c.id != PkgPip {
			continue
		}
		p, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		// skip duplicate pip if pip3 already found
		if c.id == PkgPip && seen[PkgPip] {
			continue
		}
		ver := toolVersionShort(p, c.ver...)
		out = append(out, PkgManagerInfo{ID: c.id, Path: p, Version: ver, Role: c.role})
		seen[c.id] = true
	}
	return out
}

func toolVersionShort(bin string, args ...string) string {
	if len(args) == 0 {
		args = []string{"--version"}
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 48 {
		line = line[:48]
	}
	return line
}

// HasPkgManager reports whether id is on PATH.
func HasPkgManager(id PkgManager) bool {
	for _, m := range DetectPackageManagers() {
		if m.ID == id {
			return true
		}
	}
	// special: apt is apt-get binary
	if id == PkgApt {
		_, err := exec.LookPath("apt-get")
		return err == nil
	}
	_, err := exec.LookPath(string(id))
	return err == nil
}

// InstallRecipe is one way to install a tool via a package manager.
type InstallRecipe struct {
	Manager PkgManager
	// Argv is the full command after the manager binary name.
	// e.g. Manager=brew, Argv=["install","ffmpeg"]
	// Manager=uv, Argv=["tool","install","yt-dlp"]
	Argv []string
	// Sudo prefixes with sudo when true (linux system managers).
	Sudo bool
	// Note shown in dry-run / doctor.
	Note string
}

// ToolDep is a GrokYtalkY runtime/build dependency with multi-manager install paths.
type ToolDep struct {
	Name     string
	Required bool
	Hint     string
	// Check reports whether the tool is usable.
	Check func() bool
	// Recipes ordered by recommendation (first available manager wins).
	Recipes []InstallRecipe
}

// toolDepsCatalog is the canonical dependency list for gy install deps / doctor.
func toolDepsCatalog() []ToolDep {
	return []ToolDep{
		{
			Name: "go", Required: true,
			Hint: "build/install gy · https://go.dev/dl/",
			Check: func() bool { _, e := exec.LookPath("go"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "go"}},
				{Manager: PkgApt, Argv: []string{"install", "-y", "golang-go"}, Sudo: true},
				{Manager: PkgDnf, Argv: []string{"install", "-y", "golang"}, Sudo: true},
				{Manager: PkgPacman, Argv: []string{"-S", "--noconfirm", "go"}, Sudo: true},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "GoLang.Go"}},
				{Manager: PkgChoco, Argv: []string{"install", "golang", "-y"}},
			},
		},
		{
			Name: "ffmpeg", Required: false,
			Hint: "watch / cam / phone cast / news wall",
			Check: func() bool { _, e := exec.LookPath("ffmpeg"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "ffmpeg"}},
				{Manager: PkgApt, Argv: []string{"install", "-y", "ffmpeg"}, Sudo: true},
				{Manager: PkgDnf, Argv: []string{"install", "-y", "ffmpeg"}, Sudo: true},
				{Manager: PkgPacman, Argv: []string{"-S", "--noconfirm", "ffmpeg"}, Sudo: true},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "Gyan.FFmpeg"}},
				{Manager: PkgChoco, Argv: []string{"install", "ffmpeg", "-y"}},
			},
		},
		{
			Name: "yt-dlp", Required: false,
			Hint: "gy watch URLs · /news · /social",
			Check: func() bool {
				for _, n := range []string{"yt-dlp", "yt_dlp", "youtube-dl"} {
					if _, e := exec.LookPath(n); e == nil {
						return true
					}
				}
				return false
			},
			Recipes: []InstallRecipe{
				// uv first when present — fast, isolated, no brew required
				{Manager: PkgUV, Argv: []string{"tool", "install", "yt-dlp"}, Note: "isolated CLI via uv"},
				{Manager: PkgBrew, Argv: []string{"install", "yt-dlp"}},
				{Manager: PkgPipx, Argv: []string{"install", "yt-dlp"}},
				{Manager: PkgPip, Argv: []string{"install", "--user", "yt-dlp"}},
				{Manager: PkgApt, Argv: []string{"install", "-y", "yt-dlp"}, Sudo: true},
				{Manager: PkgPacman, Argv: []string{"-S", "--noconfirm", "yt-dlp"}, Sudo: true},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "yt-dlp.yt-dlp"}},
				{Manager: PkgChoco, Argv: []string{"install", "yt-dlp", "-y"}},
			},
		},
		{
			Name: "cargo", Required: false,
			Hint: "optional — make sfu / gy-sfu",
			Check: func() bool { _, e := exec.LookPath("cargo"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "rust"}},
				// rustup is preferred but not a "package manager" we detect as cargo install
				{Manager: PkgApt, Argv: []string{"install", "-y", "cargo"}, Sudo: true},
				{Manager: PkgDnf, Argv: []string{"install", "-y", "cargo"}, Sudo: true},
				{Manager: PkgPacman, Argv: []string{"-S", "--noconfirm", "rust"}, Sudo: true},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "Rustlang.Rustup"}},
			},
		},
		{
			Name: "node", Required: false,
			Hint: "optional — edge mid-lane (npm) · GrokGlyph static is plain JS",
			Check: func() bool { _, e := exec.LookPath("node"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "node"}},
				{Manager: PkgApt, Argv: []string{"install", "-y", "nodejs", "npm"}, Sudo: true},
				{Manager: PkgDnf, Argv: []string{"install", "-y", "nodejs", "npm"}, Sudo: true},
				{Manager: PkgPacman, Argv: []string{"-S", "--noconfirm", "nodejs", "npm"}, Sudo: true},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "OpenJS.NodeJS.LTS"}},
				{Manager: PkgChoco, Argv: []string{"install", "nodejs", "-y"}},
			},
		},
		{
			Name: "npm", Required: false,
			Hint: "optional — wrangler / mid-lane worker install",
			Check: func() bool { _, e := exec.LookPath("npm"); return e == nil },
			Recipes: []InstallRecipe{
				// usually comes with node
				{Manager: PkgBrew, Argv: []string{"install", "node"}, Note: "npm ships with node"},
				{Manager: PkgApt, Argv: []string{"install", "-y", "npm"}, Sudo: true},
			},
		},
		{
			Name: "wrangler", Required: false,
			Hint: "optional — deploy edge/mid-lane Cloudflare worker",
			Check: func() bool { _, e := exec.LookPath("wrangler"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgNpm, Argv: []string{"install", "-g", "wrangler"}},
				{Manager: PkgBrew, Argv: []string{"install", "cloudflare-wrangler2"}, Note: "formula name may vary"},
			},
		},
		{
			Name: "uv", Required: false,
			Hint: "optional — fast Python tools (yt-dlp via uv tool install)",
			Check: func() bool { _, e := exec.LookPath("uv"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "uv"}},
				{Manager: PkgPipx, Argv: []string{"install", "uv"}},
				{Manager: PkgCargo, Argv: []string{"install", "--locked", "uv"}, Note: "from source crate"},
				{Manager: PkgWinget, Argv: []string{"install", "-e", "--id", "astral-sh.uv"}},
			},
		},
		{
			Name: "pipx", Required: false,
			Hint: "optional — isolated Python CLIs",
			Check: func() bool { _, e := exec.LookPath("pipx"); return e == nil },
			Recipes: []InstallRecipe{
				{Manager: PkgBrew, Argv: []string{"install", "pipx"}},
				{Manager: PkgPip, Argv: []string{"install", "--user", "pipx"}},
				{Manager: PkgApt, Argv: []string{"install", "-y", "pipx"}, Sudo: true},
			},
		},
	}
}

// PickRecipe selects the first recipe whose package manager is available.
// preferred, if non-empty, restricts to that manager only.
func PickRecipe(d ToolDep, preferred PkgManager) (InstallRecipe, bool) {
	for _, r := range d.Recipes {
		if preferred != "" && r.Manager != preferred {
			continue
		}
		if HasPkgManager(r.Manager) {
			return r, true
		}
	}
	return InstallRecipe{}, false
}

// RunRecipe executes an install recipe with optional spinner.
func RunRecipe(r InstallRecipe) error {
	bin := string(r.Manager)
	if r.Manager == PkgApt {
		bin = "apt-get"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		// pip3 alias
		if r.Manager == PkgPip {
			path, err = exec.LookPath("pip3")
		}
		if err != nil {
			return fmt.Errorf("%s not on PATH", r.Manager)
		}
	}
	args := append([]string{}, r.Argv...)
	var cmd *exec.Cmd
	if r.Sudo && runtime.GOOS != "windows" {
		if _, e := exec.LookPath("sudo"); e == nil {
			cmd = exec.Command("sudo", append([]string{path}, args...)...)
		} else {
			cmd = exec.Command(path, args...)
		}
	} else {
		cmd = exec.Command(path, args...)
	}
	cmd.Env = os.Environ()
	label := fmt.Sprintf("%s %s", r.Manager, strings.Join(args, " "))
	if r.Note != "" {
		label += "  (" + r.Note + ")"
	}
	return runWithSpinner(label, cmd)
}

// FormatRecipe is a dry-run command line.
func FormatRecipe(r InstallRecipe) string {
	bin := string(r.Manager)
	if r.Manager == PkgApt {
		bin = "apt-get"
	}
	s := bin + " " + strings.Join(r.Argv, " ")
	if r.Sudo {
		s = "sudo " + s
	}
	if r.Note != "" {
		s += "  # " + r.Note
	}
	return s
}

// FormatPackageManagersDoctor lists detected managers for gy doctor / deps.
func FormatPackageManagersDoctor() string {
	ms := DetectPackageManagers()
	var b strings.Builder
	b.WriteString("package managers:\n")
	if len(ms) == 0 {
		b.WriteString("  ✗ none detected on PATH\n")
		return b.String()
	}
	// recommended order blurb
	b.WriteString("  recommended for deps: brew → uv → pipx → apt/dnf → npm → cargo → go\n")
	for _, m := range ms {
		ver := m.Version
		if ver == "" {
			ver = m.Path
		}
		fmt.Fprintf(&b, "  ✓ %-7s  %s\n", m.ID, truncate(ver, 50))
	}
	return b.String()
}

// PreferredUpdateChannels documents how to update gy itself per manager.
func PreferredUpdateChannels() string {
	return `update channels for gy binary
  go      go install github.com/fornevercollective/grokytalky@latest   (default)
  brew    brew upgrade grokytalky   (when formula published / local Formula/)
  make    git pull && make install  (from clone · local channel)

tool deps (not gy binary)
  brew    system packages (go ffmpeg yt-dlp node rust)
  uv      Python CLIs: uv tool install yt-dlp
  pipx    pipx install yt-dlp
  npm     npm i -g wrangler  (mid-lane edge)
  cargo   cargo install … / rustup  (gy-sfu)
  apt/dnf pacman · winget · choco   (Linux/Windows system)`
}
