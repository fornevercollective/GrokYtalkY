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
  gy install dependencies    check / install go · ffmpeg · yt-dlp (brew -y)
  gy install deps            alias for install dependencies
  gy clean install           uninstall then install (fresh local channel)
  gy clean-install           same as clean install
  gy uninstall               remove gy / grokytalky / gy-burst from local + /usr/local/bin
  gy update | upgrade        GitHub check + progress install (module @vX.Y.Z)
  gy update --check          report only; exit 2 if newer release exists

new user (from scratch)
  # need Go: https://go.dev/dl/  or  brew install go
  go install github.com/fornevercollective/grokytalky@latest
  mkdir -p ~/.local/bin
  ln -sfn "$(go env GOPATH)/bin/grokytalky" ~/.local/bin/gy
  # or clone + scripts/install.sh (progress bars, version ldflags)

  gy install deps -y         ffmpeg · yt-dlp
  gy serve                   hub · phone cast URL on LAN
  gy                         companion dock

env
  PREFIX=~/.local            install root (bin under PREFIX/bin)
  GOBIN=…                    used by go install channel
  GY_NO_AUTO_UPDATE=1         skip TUI launch auto-update

see also
  make install · scripts/install.sh · docs @ fornevercollective.github.io/GrokYtalkY`
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

// depSpec describes an optional/required runtime tool.
type depSpec struct {
	Name     string
	Required bool
	Brew     string // brew formula
	Hint     string
	Check    func() bool
}

func runInstallDependencies(args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println(installHelp())
			return nil
		}
	}
	auto := false
	for _, a := range args {
		if a == "--yes" || a == "-y" || a == "--install" {
			auto = true
		}
	}

	deps := []depSpec{
		{
			Name: "go", Required: true, Brew: "go",
			Hint: "https://go.dev/dl/",
			Check: func() bool {
				_, err := exec.LookPath("go")
				return err == nil
			},
		},
		{
			Name: "ffmpeg", Required: false, Brew: "ffmpeg",
			Hint: "needed for watch / cam encode",
			Check: func() bool {
				_, err := exec.LookPath("ffmpeg")
				return err == nil
			},
		},
		{
			Name: "yt-dlp", Required: false, Brew: "yt-dlp",
			Hint: "needed for gy watch <url>",
			Check: func() bool {
				_, err := exec.LookPath("yt-dlp")
				return err == nil
			},
		},
		{
			Name: "cargo", Required: false, Brew: "rust",
			Hint: "optional — gy-sfu (make sfu)",
			Check: func() bool {
				_, err := exec.LookPath("cargo")
				return err == nil
			},
		},
	}

	fmt.Println("GrokYtalkY dependencies")
	fmt.Println()
	var missing []depSpec
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
		fmt.Printf("  %s  %-8s  (%s)", mark, d.Name, req)
		if !ok && d.Hint != "" {
			fmt.Printf("  — %s", d.Hint)
		}
		fmt.Println()
	}
	fmt.Println()

	if len(missing) == 0 {
		fmt.Println("all checked tools present")
		fmt.Print(StreamDoctor())
		return nil
	}

	brew, brewErr := exec.LookPath("brew")
	if brewErr != nil {
		fmt.Println("Homebrew not found — install missing tools manually:")
		for _, d := range missing {
			if d.Brew != "" {
				fmt.Printf("  %s  (%s)\n", d.Name, d.Hint)
			}
		}
		return fmt.Errorf("%d dependency(ies) missing", len(missing))
	}

	if !auto {
		fmt.Println("to install missing via Homebrew:")
		var formulas []string
		for _, d := range missing {
			if d.Brew != "" {
				formulas = append(formulas, d.Brew)
			}
		}
		fmt.Printf("  brew install %s\n", strings.Join(formulas, " "))
		fmt.Println("or re-run:  gy install dependencies --yes")
		return fmt.Errorf("%d dependency(ies) missing (use --yes to brew install)", len(missing))
	}

	var formulas []string
	for _, d := range missing {
		if d.Brew != "" {
			formulas = append(formulas, d.Brew)
		}
	}
	fmt.Printf("→ brew install %s\n", strings.Join(formulas, " "))
	cmd := exec.Command(brew, append([]string{"install"}, formulas...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("brew install: %w", err)
	}
	fmt.Println("dependencies installed")
	return nil
}
