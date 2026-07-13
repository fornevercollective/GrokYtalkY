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

  gy install                 install gy → ~/.local/bin (go install or make)
  gy install dependencies    check / install go · ffmpeg · yt-dlp (brew when available)
  gy install deps            alias for install dependencies
  gy clean install           uninstall then install (fresh local channel)
  gy clean-install           same as clean install
  gy uninstall               remove gy / grokytalky / gy-burst from local + /usr/local/bin
  gy update | upgrade        channel update (GitHub → go install / brew / make)
  gy update --check          report only; exit 2 if newer release exists

env
  PREFIX=~/.local            install root (bin under PREFIX/bin)
  GOBIN=…                    used by go install channel

see also
  make install · make uninstall · scripts/install.sh · scripts/install-system.sh`
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
	binDir := defaultInstallBinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", binDir, err)
	}
	fmt.Printf("→ install target %s\n", binDir)

	// Prefer source checkout (Makefile) when we are running from a git worktree with go.mod.
	if root := findRepoRoot(); root != "" {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			if _, err := exec.LookPath("make"); err == nil {
				fmt.Printf("→ make install (PREFIX=%s) from %s\n", filepath.Dir(binDir), root)
				cmd := exec.Command("make", "install")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PREFIX="+filepath.Dir(binDir))
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("make install: %w", err)
				}
				printInstallDone(binDir)
				return nil
			}
			// make missing — go build from checkout
			if _, err := exec.LookPath("go"); err == nil {
				return installFromSource(root, binDir)
			}
		}
	}

	// Module install (published or proxy)
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go not on PATH — install Go, or clone the repo and run: make install\n  https://go.dev/dl/")
	}
	if force {
		fmt.Println("→ go install " + goModule + "@latest (clean)")
	} else {
		fmt.Println("→ go install " + goModule + "@latest")
	}
	if err := goInstallLatestTo(binDir); err != nil {
		// goInstallLatestTo already prints; try GOBIN default then symlink
		if err2 := goInstallLatest(); err2 != nil {
			return err
		}
		// copy/symlink from GOPATH/bin if different
		return ensureGySymlinks(binDir)
	}
	printInstallDone(binDir)
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
	fmt.Println("Installed:")
	fmt.Printf("  %s/gy\n", binDir)
	fmt.Printf("  %s/grokytalky\n", binDir)
	if !pathHasDir(binDir) {
		fmt.Println()
		fmt.Println("Add to PATH (zsh):")
		fmt.Printf("  echo 'export PATH=\"%s:$PATH\"' >> ~/.zshrc && source ~/.zshrc\n", binDir)
	}
	fmt.Println()
	fmt.Println("Try:  gy version · gy update · gy doctor · gy --help")
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
