//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PopOutMacPlayer opens media in macOS QuickTime Player (built-in, PiP-capable).
// Prefer resolved stream URL when available so yt-dlp/m3u8 paths work.
// After open, best-effort AppleScript enters Picture in Picture (macOS 12+).
func PopOutMacPlayer(src, videoURL, title string) (string, error) {
	target, how, err := resolvePopOutTarget(src, videoURL)
	if err != nil {
		return "", err
	}
	if err := openQuickTime(target); err != nil {
		// fallback: default app association
		if err2 := openDefault(target); err2 != nil {
			return "", fmt.Errorf("QuickTime: %v; open: %w", err, err2)
		}
		how += " · default app"
	} else {
		// give QT a moment to load, then request PiP
		go func() {
			time.Sleep(900 * time.Millisecond)
			_ = tryEnterQuickTimePiP()
		}()
		how += " · QuickTime"
	}
	label := title
	if label == "" {
		label = filepath.Base(target)
	}
	return fmt.Sprintf("PiP → %s (%s)", truncate(label, 36), how), nil
}

func resolvePopOutTarget(src, videoURL string) (target, via string, err error) {
	src = strings.TrimSpace(src)
	videoURL = strings.TrimSpace(videoURL)
	// Prefer live resolved URL (m3u8 / progressive) for streams
	if videoURL != "" && (isURL(videoURL) || isDirectMediaURL(videoURL) || isRawStreamURL(videoURL)) {
		return videoURL, "stream", nil
	}
	if src == "" {
		return "", "", fmt.Errorf("no media to pop out — /watch first")
	}
	// local file
	p := expandPath(src)
	if !isURL(p) {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			abs, _ := filepath.Abs(p)
			return abs, "file", nil
		}
	}
	// re-resolve share links / handles for a player-friendly URL
	r, e := ResolveMediaTimeout(src, 60*time.Second)
	if e != nil {
		// last resort: pass original if URL
		if isURL(src) {
			return src, "url", nil
		}
		return "", "", fmt.Errorf("resolve: %w", e)
	}
	if r.Video != "" {
		return r.Video, r.Via, nil
	}
	return r.Input, "input", nil
}

func openQuickTime(target string) error {
	// QuickTime Player.app — built-in, supports PiP on modern macOS
	cmd := exec.Command("open", "-a", "QuickTime Player", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncate(string(out), 120))
	}
	return nil
}

func openDefault(target string) error {
	cmd := exec.Command("open", target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncate(string(out), 120))
	}
	return nil
}

// tryEnterQuickTimePiP uses System Events / menu to enter Picture in Picture.
// Best-effort: fails quietly if UI not ready, permissions missing, or menu layout differs.
func tryEnterQuickTimePiP() error {
	// Prefer menu item (English locale). Accessibility may be required for System Events.
	script := `
tell application "QuickTime Player"
	activate
	if (count of documents) is 0 then return
	try
		set playing of front document to true
	end try
end tell
delay 0.35
tell application "System Events"
	tell process "QuickTime Player"
		set frontmost to true
		try
			click menu item "Enter Picture in Picture" of menu "View" of menu bar 1
			return "pip"
		end try
		try
			click menu item "Picture in Picture" of menu "View" of menu bar 1
			return "pip"
		end try
		-- some macOS builds nest under Window
		try
			click menu item "Enter Picture in Picture" of menu "Window" of menu bar 1
			return "pip"
		end try
	end tell
end tell
return "open-only"
`
	cmd := exec.Command("osascript", "-e", script)
	_, err := cmd.CombinedOutput()
	return err
}

// PopOutSupported is true on macOS.
func PopOutSupported() bool { return true }

// PopOutPlayerName for UI strings.
func PopOutPlayerName() string { return "QuickTime Player" }
