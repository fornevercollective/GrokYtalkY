package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// installProgress is a multi-step terminal progress UI for gy install / update.
type installProgress struct {
	total   int
	current int
	width   int
	mu      sync.Mutex
}

func newInstallProgress(total int) *installProgress {
	if total < 1 {
		total = 1
	}
	return &installProgress{total: total, width: 24}
}

// Step marks a completed step and prints a bar + label.
func (p *installProgress) Step(label string) {
	if p == nil {
		fmt.Println("→", label)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current++
	if p.current > p.total {
		p.current = p.total
	}
	fmt.Println(p.render(label))
}

// Note prints a dim side note without advancing the bar.
func (p *installProgress) Note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("    %s\n", msg)
}

// Fail prints a failed step.
func (p *installProgress) Fail(label string, err error) {
	if p == nil {
		fmt.Printf("✗ %s: %v\n", label, err)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Printf("  ✗ [%d/%d] %s\n      %v\n", p.current, p.total, label, err)
}

// Done completes remaining steps and prints success.
func (p *installProgress) Done(label string) {
	if p == nil {
		fmt.Println("✓", label)
		return
	}
	p.mu.Lock()
	p.current = p.total
	p.mu.Unlock()
	fmt.Println()
	fmt.Printf("  ✓  %s\n", label)
}

func (p *installProgress) render(label string) string {
	filled := p.width * p.current / p.total
	if filled > p.width {
		filled = p.width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", p.width-filled)
	return fmt.Sprintf("  [%d/%d] %s  %s", p.current, p.total, bar, label)
}

// runWithSpinner runs a command while showing an animated spinner on stderr.
// Stdout/stderr of the command are tee'd after the spinner line for errors.
func runWithSpinner(label string, cmd *exec.Cmd) error {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	done := make(chan error, 1)

	// capture combined output for failure detail
	var buf strings.Builder
	cmd.Stdout = io.MultiWriter(&buf, discardWriter{})
	cmd.Stderr = io.MultiWriter(&buf, discardWriter{})

	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { done <- cmd.Wait() }()

	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	// initial line
	fmt.Fprintf(os.Stderr, "  %s  %s", frames[0], label)

	for {
		select {
		case err := <-done:
			// clear spinner line
			fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 4+utf8.RuneCountInString(label)+8))
			if err != nil {
				out := strings.TrimSpace(buf.String())
				if out != "" {
					// print last few lines of go install noise
					lines := strings.Split(out, "\n")
					start := 0
					if len(lines) > 8 {
						start = len(lines) - 8
					}
					for _, ln := range lines[start:] {
						fmt.Fprintln(os.Stderr, "    ", ln)
					}
				}
				return err
			}
			fmt.Fprintf(os.Stderr, "  ✓  %s\n", label)
			return nil
		case <-ticker.C:
			i = (i + 1) % len(frames)
			fmt.Fprintf(os.Stderr, "\r  %s  %s", frames[i], label)
		}
	}
}

// discardWriter swallows bytes (we capture in MultiWriter buffer).
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// runCmdSpinner is a convenience for name+args with spinner.
func runCmdSpinner(label string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	return runWithSpinner(label, cmd)
}

// printInstallBanner for new-user / update sessions.
func printInstallBanner(title string) {
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────┐")
	fmt.Printf("  │  %-41s  │\n", title)
	fmt.Println("  └─────────────────────────────────────────────┘")
	fmt.Println()
}

// printNewUserGuide is the first-run path after a successful install.
func printNewUserGuide(binDir string) {
	fmt.Println()
	fmt.Println("  Getting started")
	fmt.Println("  ───────────────")
	fmt.Println("  gy                 companion dock (default)")
	fmt.Println("  gy serve           mesh hub for phone / peers")
	fmt.Println("  gy burst           dual Glyph Matrix walkie")
	fmt.Println("  gy lab             multi-feed video wall")
	fmt.Println("  gy /news           live news agency glyph wall (in TUI: /news)")
	fmt.Println("  gy doctor          check ffmpeg · yt-dlp · tools")
	fmt.Println("  gy update          check GitHub + upgrade")
	fmt.Println("  gy install deps -y go · ffmpeg · yt-dlp via brew")
	fmt.Println()
	if !pathHasDir(binDir) {
		fmt.Println("  Add to PATH (zsh):")
		fmt.Printf("    echo 'export PATH=\"%s:$PATH\"' >> ~/.zshrc && source ~/.zshrc\n", binDir)
		fmt.Println()
	}
	fmt.Println("  Docs  https://fornevercollective.github.io/GrokYtalkY/")
	fmt.Println("  Phone http://<LAN>:9876/phone.html  (after gy serve)")
	fmt.Println()
}
