package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Preferred user install location (matches Makefile PREFIX / scripts/install.sh).
func defaultInstallBinDir() string {
	if p := strings.TrimSpace(os.Getenv("PREFIX")); p != "" {
		return filepath.Join(p, "bin")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".local", "bin")
	}
	return filepath.Join(home, ".local", "bin")
}

func installHelp() string {
	return `gy install | uninstall | clean-install | install-dependencies

  gy install                 install gy → ~/.local/bin (progress + spinner)
  gy install dependencies    multi-manager deps (brew · uv · npm · pipx · apt…)
  gy install deps            alias for install dependencies
  gy install deps -y         install missing (auto-pick best manager)
  gy install deps --pm uv    force package manager (brew|uv|pipx|npm|cargo|apt|…)
  gy install deps --list     show catalog + available managers
  gy clean install           uninstall then install (fresh local channel)
  gy clean-install           same as clean install
  gy uninstall               remove gy / grokytalky / gy-burst from local + /usr/local/bin
  gy update | upgrade        GitHub check + progress install (module @vX.Y.Z)
  gy update --check          report only; exit 2 if newer release exists

package managers (auto-detected)
  brew    macOS/Linux system packages (go ffmpeg yt-dlp node rust)
  uv      Python CLIs: uv tool install yt-dlp  (recommended for yt-dlp)
  pipx    pipx install yt-dlp
  pip     pip install --user …  (last resort)
  npm     npm i -g wrangler  (mid-lane edge)
  cargo   rust tooling / optional crates
  go      go install …  (gy binary)
  apt/dnf/pacman · winget · choco

recommended order for tool deps
  brew → uv → pipx → apt/dnf → npm → cargo → go

new user (from scratch)
  # need Go: https://go.dev/dl/  or  brew install go
  go install github.com/fornevercollective/grokytalky@latest
  mkdir -p ~/.local/bin
  ln -sfn "$(go env GOPATH)/bin/grokytalky" ~/.local/bin/gy
  # or: clone + ./scripts/install.sh

  gy install deps -y         ffmpeg · yt-dlp · optional node/uv
  gy serve · gy · gy doctor

env
  PREFIX=~/.local            install root (bin under PREFIX/bin)
  GOBIN=…                    used by go install channel
  GY_NO_AUTO_UPDATE=1         skip TUI launch auto-update
  GY_PKG_MANAGER=brew|uv|…   default --pm for deps

see also
  make install · scripts/install.sh · gy doctor`
}

// runInstallCmd dispatches install / clean-install / deps from CLI words.
func runInstallCmd(verb string, args []string) error {
	verb = strings.ToLower(strings.TrimSpace(verb))
	// multi-word: install dependencies | clean install
	if verb == "install" && len(args) > 0 {
		sub := strings.ToLower(args[0])
		switch sub {
		case "dependencies", "dependency", "deps", "dep":
			return runInstallDependencies(args[1:])
		case "clean":
			return runCleanInstall(args[1:])
		case "-h", "--help", "help":
			fmt.Println(installHelp())
			return nil
		}
	}
	if verb == "clean" && len(args) > 0 && strings.ToLower(args[0]) == "install" {
		return runCleanInstall(args[1:])
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(installHelp())
			return nil
		}
	}
	switch verb {
	case "install":
		return runLocalInstall(false)
	case "clean-install", "reinstall":
		return runCleanInstall(args)
	case "uninstall":
		return runUninstall(args)
	case "install-dependencies", "install-deps", "deps", "dependencies":
		return runInstallDependencies(args)
	default:
		return fmt.Errorf("unknown install verb %q\n\n%s", verb, installHelp())
	}
}

func runCleanInstall(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(installHelp())
			return nil
		}
	}
	fmt.Println("→ clean install: uninstall then install")
	_ = runUninstall(nil) // best-effort remove old bins
	return runLocalInstall(true)
}

// runLocalInstall puts gy + grokytalky on PATH via go install @latest or make install from checkout.
func runLocalInstall(force bool) error {
	printInstallBanner("GrokYtalkY install")
	prog := newInstallProgress(6)
	binDir := defaultInstallBinDir()
	prog.Step("prepare install dir")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		prog.Fail("mkdir", err)
		return fmt.Errorf("mkdir %s: %w", binDir, err)
	}
	prog.Note("target %s", binDir)

	// Prefer source checkout (Makefile) when we are running from a git worktree with go.mod.
	if root := findRepoRoot(); root != "" {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			if _, err := exec.LookPath("make"); err == nil {
				prog.Step("build from checkout (make install)")
				prog.Note("PREFIX=%s · %s", filepath.Dir(binDir), root)
				cmd := exec.Command("make", "install")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PREFIX="+filepath.Dir(binDir))
				if err := runWithSpinner("make install", cmd); err != nil {
					prog.Fail("make install", err)
					return fmt.Errorf("make install: %w", err)
				}
				prog.Step("verify binary")
				_ = verifyInstalledBinary()
				prog.Done("installed from source")
				printInstallDone(binDir)
				printNewUserGuide(binDir)
				return nil
			}
			// make missing — go build from checkout
			if _, err := exec.LookPath("go"); err == nil {
				prog.Step("build from checkout (go build)")
				if err := installFromSource(root, binDir); err != nil {
					prog.Fail("go build", err)
					return err
				}
				prog.Done("installed from source")
				printNewUserGuide(binDir)
				return nil
			}
		}
	}

	// Module install (published or proxy)
	prog.Step("locate Go toolchain")
	if _, err := exec.LookPath("go"); err != nil {
		prog.Fail("go missing", err)
		return fmt.Errorf("go not on PATH — install Go, then re-run: gy install\n  https://go.dev/dl/\n  or: brew install go")
	}

	// Prefer versioned tag when GitHub has one
	prog.Step("resolve latest release")
	res := checkUpdate()
	ref := moduleInstallRef(res)
	if res.Latest != "" {
		prog.Note("GitHub latest v%s", normalizeVersion(res.Latest))
	} else {
		prog.Note("using %s", ref)
	}
	if force {
		prog.Note("clean install forced")
	}

	prog.Step("download + compile " + ref)
	if err := goInstallRefTo(binDir, ref); err != nil {
		// try default GOBIN then symlink
		prog.Note("retry default GOBIN…")
		if err2 := goInstallRef(ref); err2 != nil {
			prog.Fail("go install", err)
			return err
		}
		_ = ensureGySymlinks(binDir)
	}
	prog.Step("verify install")
	_ = verifyInstalledBinary()
	prog.Done("ready")
	printInstallDone(binDir)
	printNewUserGuide(binDir)
	return nil
}

func installFromSource(root, binDir string) error {
	fmt.Printf("→ go build from %s → %s\n", root, binDir)
	out := filepath.Join(binDir, "grokytalky")
	ld := fmt.Sprintf("-s -w -X main.Version=%s -X main.Commit=%s -X main.Date=%s",
		Version, Commit, Date)
	cmd := exec.Command("go", "build", "-ldflags", ld, "-o", out, ".")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	short := filepath.Join(binDir, "gy")
	_ = os.Remove(short)
	if err := os.Symlink("grokytalky", short); err != nil {
		// windows or no symlink: hard copy name
		_ = copyFile(out, short)
	}
	// optional burst alias
	_ = os.Remove(filepath.Join(binDir, "gy-burst"))
	_ = os.Symlink("grokytalky", filepath.Join(binDir, "gy-burst"))
	printInstallDone(binDir)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o755)
}

func ensureGySymlinks(binDir string) error {
	full := filepath.Join(binDir, "grokytalky")
	if _, err := os.Stat(full); err != nil {
		// try GOPATH/bin
		if gopath := goBinDir(); gopath != "" {
			src := filepath.Join(gopath, "grokytalky")
			if _, err := os.Stat(src); err == nil {
				_ = os.MkdirAll(binDir, 0o755)
				data, err := os.ReadFile(src)
				if err == nil {
					_ = os.WriteFile(full, data, 0o755)
				}
			}
		}
	}
	if _, err := os.Stat(full); err == nil {
		short := filepath.Join(binDir, "gy")
		_ = os.Remove(short)
		_ = os.Symlink("grokytalky", short)
	}
	printInstallDone(binDir)
	return nil
}

func goBinDir() string {
	if g := os.Getenv("GOBIN"); g != "" {
		return g
	}
	if g := os.Getenv("GOPATH"); g != "" {
		return filepath.Join(strings.Split(g, string(os.PathListSeparator))[0], "bin")
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		return filepath.Join(home, "go", "bin")
	}
	return ""
}

func printInstallDone(binDir string) {
	fmt.Println()
	fmt.Println("  Installed")
	fmt.Println("  ─────────")
	fmt.Printf("  %s/gy\n", binDir)
	fmt.Printf("  %s/grokytalky\n", binDir)
	if !pathHasDir(binDir) {
		fmt.Println()
		fmt.Println("  Add to PATH (zsh):")
		fmt.Printf("    echo 'export PATH=\"%s:$PATH\"' >> ~/.zshrc && source ~/.zshrc\n", binDir)
	}
	fmt.Println()
	if p := filepath.Join(binDir, "gy"); fileExists(p) {
		cmd := exec.Command(p, "--version")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

func pathHasDir(dir string) bool {
	path := os.Getenv("PATH")
	for _, p := range strings.Split(path, string(os.PathListSeparator)) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// findRepoRoot walks up from the executable or cwd for go.mod + Makefile.
func findRepoRoot() string {
	// 1) cwd
	if wd, err := os.Getwd(); err == nil {
		if r := walkForMod(wd); r != "" {
			return r
		}
	}
	// 2) next to binary (dev builds in bin/)
	if exe, err := os.Executable(); err == nil {
		if r, err2 := filepath.EvalSymlinks(exe); err2 == nil {
			exe = r
		}
		dir := filepath.Dir(exe)
		// bin/grokytalky → parent
		if filepath.Base(dir) == "bin" {
			dir = filepath.Dir(dir)
		}
		if r := walkForMod(dir); r != "" {
			return r
		}
	}
	return ""
}

func walkForMod(start string) string {
	dir := start
	for i := 0; i < 8; i++ {
		if fileExists(filepath.Join(dir, "go.mod")) {
			// prefer grokytalky module
			data, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
			if strings.Contains(string(data), "grokytalky") || fileExists(filepath.Join(dir, "main.go")) {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func runUninstall(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(installHelp())
			return nil
		}
	}
	names := []string{"gy", "grokytalky", "gy-burst"}
	dirs := []string{defaultInstallBinDir()}
	// also common system paths (no sudo — best effort)
	dirs = append(dirs, "/usr/local/bin")
	if runtime.GOOS == "darwin" {
		dirs = append(dirs, "/opt/homebrew/bin")
	}
	// current binary dir if not already listed
	if exe := mustExe(); exe != "" {
		dirs = append(dirs, filepath.Dir(exe))
	}

	seen := map[string]bool{}
	removed := 0
	for _, d := range dirs {
		d = filepath.Clean(d)
		if d == "" || d == "." || seen[d] {
			continue
		}
		seen[d] = true
		// skip cellar — brew should manage those
		low := strings.ToLower(d)
		if strings.Contains(low, "/cellar/") {
			fmt.Printf("skip brew cellar %s (use: brew uninstall grokytalky)\n", d)
			continue
		}
		for _, n := range names {
			p := filepath.Join(d, n)
			if !fileExists(p) {
				continue
			}
			if err := os.Remove(p); err != nil {
				fmt.Printf("  ! could not remove %s: %v\n", p, err)
				continue
			}
			fmt.Printf("  − %s\n", p)
			removed++
		}
	}
	if removed == 0 {
		fmt.Println("nothing to uninstall under local /usr/local (already clean)")
	} else {
		fmt.Printf("uninstalled %d path(s)\n", removed)
	}
	// hint brew
	if ch := installChannel(mustExe()); ch == "homebrew" {
		fmt.Println("homebrew channel: brew uninstall grokytalky")
	}
	return nil
}

func runInstallDependencies(args []string) error {
	auto := false
	listOnly := false
	var preferred PkgManager
	// env default
	if v := strings.TrimSpace(os.Getenv("GY_PKG_MANAGER")); v != "" {
		preferred = PkgManager(strings.ToLower(v))
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "-h", "--help", "help":
			fmt.Println(installHelp())
			return nil
		case "--yes", "-y", "--install":
			auto = true
		case "--list", "-l", "list":
			listOnly = true
		case "--pm", "--manager", "-m":
			if i+1 < len(args) {
				i++
				preferred = PkgManager(strings.ToLower(args[i]))
			}
		default:
			if strings.HasPrefix(a, "--pm=") {
				preferred = PkgManager(strings.ToLower(strings.TrimPrefix(a, "--pm=")))
			}
		}
	}

	printInstallBanner("GrokYtalkY dependencies")
	fmt.Print(FormatPackageManagersDoctor())
	fmt.Println()

	deps := toolDepsCatalog()
	if listOnly {
		fmt.Println("dependency catalog (install recipes)")
		fmt.Println()
		for _, d := range deps {
			req := "optional"
			if d.Required {
				req = "required"
			}
			ok := d.Check()
			mark := "✓"
			if !ok {
				mark = "✗"
			}
			fmt.Printf("  %s  %-10s  (%s)  %s\n", mark, d.Name, req, d.Hint)
			for _, r := range d.Recipes {
				avail := " "
				if HasPkgManager(r.Manager) {
					avail = "·"
				}
				fmt.Printf("      %s %s\n", avail, FormatRecipe(r))
			}
		}
		fmt.Println()
		fmt.Println(PreferredUpdateChannels())
		return nil
	}

	prog := newInstallProgress(2 + len(deps))
	prog.Step("scan tools")

	var missing []ToolDep
	for _, d := range deps {
		ok := d.Check()
		mark := "✓"
		if !ok {
			mark = "✗"
			missing = append(missing, d)
		}
		req := "optional"
		if d.Required {
			req = "required"
		}
		line := fmt.Sprintf("%s  %-10s  (%s)", mark, d.Name, req)
		if !ok {
			if r, ok := PickRecipe(d, preferred); ok {
				line += "  → " + FormatRecipe(r)
			} else if d.Hint != "" {
				line += "  — " + d.Hint
			}
		}
		fmt.Println("   ", line)
	}
	fmt.Println()

	if len(missing) == 0 {
		prog.Step("all present")
		prog.Done("dependencies ok")
		fmt.Print(StreamDoctor())
		return nil
	}

	// dry-run plan
	prog.Step(fmt.Sprintf("plan install (%d missing)", len(missing)))
	type planItem struct {
		dep    ToolDep
		recipe InstallRecipe
	}
	var plan []planItem
	var unplanned []ToolDep
	for _, d := range missing {
		r, ok := PickRecipe(d, preferred)
		if !ok {
			unplanned = append(unplanned, d)
			continue
		}
		plan = append(plan, planItem{d, r})
		fmt.Printf("    · %-10s  %s\n", d.Name, FormatRecipe(r))
	}
	for _, d := range unplanned {
		fmt.Printf("    ? %-10s  no manager for recipes — %s\n", d.Name, d.Hint)
	}
	fmt.Println()

	if !auto {
		fmt.Println("  re-run with --yes to install:")
		fmt.Println("    gy install deps -y")
		if preferred != "" {
			fmt.Printf("    gy install deps -y --pm %s\n", preferred)
		} else {
			fmt.Println("    gy install deps -y --pm uv    # force uv for Python tools")
			fmt.Println("    gy install deps -y --pm brew")
		}
		return fmt.Errorf("%d dependency(ies) missing (use --yes to install)", len(missing))
	}

	if len(plan) == 0 {
		return fmt.Errorf("no installable recipes (install a package manager: brew / uv / npm)")
	}

	// execute with progress
	ip := newInstallProgress(len(plan) + 1)
	var failed []string
	for _, p := range plan {
		ip.Step(fmt.Sprintf("install %s via %s", p.dep.Name, p.recipe.Manager))
		if err := RunRecipe(p.recipe); err != nil {
			ip.Fail(p.dep.Name, err)
			failed = append(failed, p.dep.Name)
			continue
		}
	}
	ip.Step("re-check tools")
	still := 0
	for _, d := range toolDepsCatalog() {
		if !d.Check() {
			// only count previously missing required/planned
			for _, m := range missing {
				if m.Name == d.Name {
					still++
					fmt.Printf("    still missing: %s\n", d.Name)
				}
			}
		}
	}
	if len(failed) > 0 || still > 0 {
		ip.Fail("deps", fmt.Errorf("%d failed · %d still missing", len(failed), still))
		return fmt.Errorf("dependency install incomplete")
	}
	ip.Done("dependencies installed")
	fmt.Print(StreamDoctor())
	fmt.Print(FormatPackageManagersDoctor())
	return nil
}
