package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VideoPipe decodes mp4/mkv/mov (or URL/RTSP/HTTP) via ffmpeg into RGB frames
// for terminal half-block rendering. Optional audio via ffplay.
type VideoPipe struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	audio   *exec.Cmd
	stdout  io.ReadCloser
	cancel  chan struct{}
	running bool

	W, H    int
	Src     string
	FPS     float64
	HasAudio bool
	Err     string

	// latest frame (RGB24)
	frame []byte
	seq   uint64
}

// OpenVideoPipe starts ffmpeg → raw RGB24 pipe (+ optional ffplay audio).
// src: local path, raw stream URL, or site link (auto yt-dlp resolve).
func OpenVideoPipe(src string, w, h int, withAudio bool) (*VideoPipe, error) {
	if w < 8 {
		w = 80
	}
	if h < 4 {
		h = 40
	}
	if h%2 != 0 {
		h++
	}

	resolved, err := ResolveMediaTimeout(src, 90*time.Second)
	if err != nil {
		return nil, err
	}
	return openVideoPipeResolved(resolved, w, h, withAudio)
}

// OpenVideoPipeResolved skips re-resolve (caller already ran ResolveMedia).
func OpenVideoPipeResolved(r *ResolvedStream, w, h int, withAudio bool) (*VideoPipe, error) {
	if w < 8 {
		w = 80
	}
	if h < 4 {
		h = 40
	}
	if h%2 != 0 {
		h++
	}
	return openVideoPipeResolved(r, w, h, withAudio)
}

func openVideoPipeResolved(r *ResolvedStream, w, h int, withAudio bool) (*VideoPipe, error) {
	if r == nil || r.Video == "" {
		return nil, fmt.Errorf("video: empty resolved stream")
	}

	// -re for files; live/network streams pace on arrival
	args := []string{"-hide_banner", "-loglevel", "error"}
	if r.Via == "file" {
		args = append(args, "-re")
	}
	// reconnect-ish for network
	if r.Via != "file" {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
		)
	}
	args = append(args, "-i", r.Video)
	// separate audio input when yt-dlp splits streams (video still -an on pixel pipe)
	if r.Audio != "" {
		// video pipe stays video-only; audio handled by startAudio
	}
	args = append(args,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic,format=rgb24", w, h),
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"pipe:1",
	)
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w (need ffmpeg on PATH)", err)
	}

	label := r.Title
	if label == "" {
		label = r.Input
	}
	vp := &VideoPipe{
		cmd:     cmd,
		stdout:  stdout,
		cancel:  make(chan struct{}),
		running: true,
		W:       w,
		H:       h,
		Src:     label,
		FPS:     15,
		frame:   make([]byte, w*h*3),
	}

	if withAudio {
		audioSrc := r.Video
		if r.Audio != "" {
			audioSrc = r.Audio
		}
		vp.startAudio(audioSrc)
		vp.HasAudio = true
	}

	go vp.readLoop()
	return vp, nil
}

func (vp *VideoPipe) startAudio(src string) {
	// ffplay audio only — same file/URL, best-effort sync
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-nodisp", "-autoexit",
		"-vn",
		"-volume", "80",
		src,
	}
	// for files use -autoexit; streams may need different flags
	cmd := exec.Command("ffplay", args...)
	if err := cmd.Start(); err != nil {
		// fallback: ffmpeg → ffplay pcm
		cmd2 := exec.Command("ffmpeg",
			"-hide_banner", "-loglevel", "error",
			"-re", "-i", src,
			"-vn", "-f", "s16le", "-ac", "2", "-ar", "44100", "pipe:1",
		)
		stdout, err2 := cmd2.StdoutPipe()
		if err2 != nil {
			return
		}
		play := exec.Command("ffplay",
			"-hide_banner", "-loglevel", "error",
			"-nodisp", "-autoexit",
			"-f", "s16le", "-ac", "2", "-ar", "44100", "-i", "pipe:0",
		)
		play.Stdin = stdout
		if err := cmd2.Start(); err != nil {
			return
		}
		if err := play.Start(); err != nil {
			_ = cmd2.Process.Kill()
			return
		}
		vp.audio = play
		vp.HasAudio = true
		go func() {
			_ = play.Wait()
			_ = cmd2.Process.Kill()
		}()
		return
	}
	vp.audio = cmd
	vp.HasAudio = true
	go func() { _ = cmd.Wait() }()
}

func (vp *VideoPipe) readLoop() {
	frameSize := vp.W * vp.H * 3
	buf := make([]byte, frameSize)
	r := bufio.NewReaderSize(vp.stdout, frameSize*2)
	defer vp.markStopped()

	for {
		select {
		case <-vp.cancel:
			return
		default:
		}
		_, err := io.ReadFull(r, buf)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "file already closed") {
				vp.mu.Lock()
				vp.Err = err.Error()
				vp.mu.Unlock()
			}
			return
		}
		cp := make([]byte, frameSize)
		copy(cp, buf)
		vp.mu.Lock()
		vp.frame = cp
		vp.seq++
		vp.mu.Unlock()
	}
}

func (vp *VideoPipe) markStopped() {
	vp.mu.Lock()
	vp.running = false
	vp.mu.Unlock()
}

// Snapshot returns a copy of the latest RGB frame + dimensions + seq.
func (vp *VideoPipe) Snapshot() (rgb []byte, w, h int, seq uint64, ok bool) {
	if vp == nil {
		return nil, 0, 0, 0, false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if len(vp.frame) == 0 || vp.seq == 0 {
		return nil, vp.W, vp.H, vp.seq, false
	}
	cp := make([]byte, len(vp.frame))
	copy(cp, vp.frame)
	return cp, vp.W, vp.H, vp.seq, true
}

func (vp *VideoPipe) Running() bool {
	if vp == nil {
		return false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	return vp.running
}

func (vp *VideoPipe) Stop() {
	if vp == nil {
		return
	}
	vp.mu.Lock()
	if !vp.running && vp.cmd == nil {
		vp.mu.Unlock()
		return
	}
	select {
	case <-vp.cancel:
	default:
		close(vp.cancel)
	}
	cmd := vp.cmd
	audio := vp.audio
	stdout := vp.stdout
	vp.running = false
	vp.mu.Unlock()

	if stdout != nil {
		_ = stdout.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	if audio != nil && audio.Process != nil {
		_ = audio.Process.Kill()
		_, _ = audio.Process.Wait()
	}
}

// RGBToFramePixels wraps raw RGB24 for the half-block renderer.
func RGBToFramePixels(rgb []byte, w, h int, src string) *FramePixels {
	if len(rgb) < w*h*3 {
		return nil
	}
	cp := make([]byte, w*h*3)
	copy(cp, rgb[:w*h*3])
	return &FramePixels{W: w, H: h, RGB: cp, Source: src}
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func isURL(s string) bool {
	low := strings.ToLower(s)
	return strings.HasPrefix(low, "http://") ||
		strings.HasPrefix(low, "https://") ||
		strings.HasPrefix(low, "rtsp://") ||
		strings.HasPrefix(low, "rtmp://") ||
		strings.HasPrefix(low, "srt://") ||
		strings.HasPrefix(low, "udp://") ||
		strings.HasPrefix(low, "tcp://") ||
		strings.HasPrefix(low, "file://")
}

func isVideoPath(s string) bool {
	low := strings.ToLower(s)
	for _, ext := range []string{".mp4", ".mkv", ".mov", ".webm", ".avi", ".m4v", ".ts", ".m3u8", ".flv", ".wmv", ".gif"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return isURL(s)
}

// probeSize asks ffmpeg for display size (best-effort).
func probeSize(src string) (w, h int) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		src,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "x")
	if len(parts) != 2 {
		return 0, 0
	}
	fmt.Sscanf(parts[0], "%d", &w)
	fmt.Sscanf(parts[1], "%d", &h)
	return w, h
}

