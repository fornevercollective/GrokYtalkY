//go:build !darwin

package main

import "fmt"

// PopOutMacPlayer is unavailable off macOS.
func PopOutMacPlayer(src, videoURL, title string) (string, error) {
	return "", fmt.Errorf("PiP pop-out is macOS-only (QuickTime Player) — this build is not darwin")
}

func PopOutSupported() bool { return false }

func PopOutPlayerName() string { return "QuickTime Player (macOS)" }
