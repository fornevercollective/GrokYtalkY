package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// VideoPipe decodes media via ffmpeg → RGB24 for terminal half-blocks,
// with scrub: pause, seek, rate. Audio via ffplay (restarted on seek).
type VideoPipe struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	audio   *exec.Cmd
	stdout  io.ReadCloser
	cancel  chan struct{}
	running bool

	W, H     int
	Src      string // display label
	Input    string // original user path/URL
	VideoURL string // resolved stream for ffmpeg -i
	AudioURL string // optional separate audio
	Via      string // file | direct | yt-dlp | raw
	FPS      float64
	HasAudio bool
	Err      string
	withAudio bool

	// scrub state
	paused   bool
	rate     float64 // 0.5, 1, 1.5, 2, …
	duration time.Duration
	// position = baseSeek + wall elapsed * rate (when playing)
	baseSeek  time.Duration
	playStart time.Time // wall clock when current segment started playing

	// latest frame (RGB24)
	frame []byte
	seq   uint64

	// media supervisor ids (kill-on-exit / health)
	mediaID  string
	audioMID string
}

// OpenVideoPipe starts playback from a path/URL (auto yt-dlp resolve).
func OpenVideoPipe(src string, w, h int, withAudio bool) (*VideoPipe, error) {
	w, h = clampWH(w, h)
	resolved, err := ResolveMediaTimeout(src, 90*time.Second)
	if err != nil {
		return nil, err
	}
	return openVideoPipeResolved(resolved, w, h, withAudio, 0, 1.0)
}

// OpenVideoPipeResolved starts at offset 0, rate 1.
func OpenVideoPipeResolved(r *ResolvedStream, w, h int, withAudio bool) (*VideoPipe, error) {
	w, h = clampWH(w, h)
	return openVideoPipeResolved(r, w, h, withAudio, 0, 1.0)
}

func clampWH(w, h int) (int, int) {
	if w < 8 {
		w = 80
	}
	if h < 4 {
		h = 40
	}
	if h%2 != 0 {
		h++
	}
	return w, h
}

func openVideoPipeResolved(r *ResolvedStream, w, h int, withAudio bool, seek time.Duration, rate float64) (*VideoPipe, error) {
	if r == nil || r.Video == "" {
		return nil, fmt.Errorf("video: empty resolved stream")
	}
	if rate <= 0 {
		rate = 1
	}
	if seek < 0 {
		seek = 0
	}

	dur := probeDuration(r.Video)
	if r.Via == "file" && !isURL(r.Input) {
		if d := probeDuration(expandPath(r.Input)); d > dur {
			dur = d
		}
	}

	vp := &VideoPipe{
		cancel:    make(chan struct{}),
		running:   true,
		W:         w,
		H:         h,
		Src:       firstNonEmptyStr(r.Title, r.Input),
		Input:     r.Input,
		VideoURL:  r.Video,
		AudioURL:  r.Audio,
		Via:       r.Via,
		FPS:       15,
		withAudio: withAudio,
		rate:      rate,
		duration:  dur,
		baseSeek:  seek,
		playStart: time.Now(),
		frame:     make([]byte, w*h*3),
	}

	if err := vp.startSegment(seek, rate); err != nil {
		return nil, err
	}
	go vp.readLoop()
	return vp, nil
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (vp *VideoPipe) startSegment(seek time.Duration, rate float64) error {
	// kill previous processes if any
	vp.killProcs()

	args := []string{"-hide_banner", "-loglevel", "error"}
	// seek input (fast for files/http with index)
	if seek > 0 {
		args = append(args, "-ss", formatFFtime(seek))
	}
	// realtime only at 1× for files; speed uses setpts
	isFile := vp.Via == "file"
	if isFile && rate == 1.0 {
		args = append(args, "-re")
	}
	if !isFile && vp.Via != "raw" {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
		)
	}
	args = append(args, "-i", vp.VideoURL)

	vf := fmt.Sprintf("scale=%d:%d:flags=bicubic,format=rgb24", vp.W, vp.H)
	if rate != 1.0 {
		// setpts: higher rate → smaller pts → faster playback
		vf = fmt.Sprintf("setpts=PTS/%g,%s", rate, vf)
	}
	args = append(args,
		"-an",
		"-vf", vf,
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"pipe:1",
	)

	cmd := exec.Command("ffmpeg", args...)
	PrepMediaCmd(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}
	mid, err := Media().Register(MediaKindWatch, firstNonEmptyStr(vp.Src, "watch"), cmd)
	if err != nil {
		_ = killCmd(cmd)
		return err
	}

	vp.mu.Lock()
	vp.cmd = cmd
	vp.stdout = stdout
	vp.mediaID = mid
	vp.baseSeek = seek
	vp.rate = rate
	vp.playStart = time.Now()
	vp.paused = false
	vp.running = true
	// fresh cancel channel if previous was closed
	select {
	case <-vp.cancel:
		vp.cancel = make(chan struct{})
	default:
	}
	vp.mu.Unlock()

	if vp.withAudio {
		audioSrc := vp.VideoURL
		if vp.AudioURL != "" {
			audioSrc = vp.AudioURL
		}
		vp.startAudioAt(audioSrc, seek, rate)
		vp.HasAudio = true
	}
	return nil
}

func (vp *VideoPipe) killProcs() {
	vp.mu.Lock()
	mid := vp.mediaID
	amid := vp.audioMID
	cmd := vp.cmd
	audio := vp.audio
	stdout := vp.stdout
	vp.cmd = nil
	vp.audio = nil
	vp.stdout = nil
	vp.mediaID = ""
	vp.audioMID = ""
	vp.mu.Unlock()

	if mid != "" {
		Media().Kill(mid)
	} else if cmd != nil {
		_ = killCmd(cmd)
	}
	if amid != "" {
		Media().Kill(amid)
	} else if audio != nil {
		_ = killCmd(audio)
	}
	if stdout != nil {
		_ = stdout.Close()
	}
}

func (vp *VideoPipe) startAudioAt(src string, seek time.Duration, rate float64) {
	args := []string{"-hide_banner", "-loglevel", "error", "-nodisp", "-autoexit", "-vn", "-volume", "80"}
	if seek > 0 {
		args = append(args, "-ss", formatFFtime(seek))
	}
	if rate != 1.0 && rate > 0.5 && rate <= 2.0 {
		// atempo valid 0.5–2.0
		args = append(args, "-af", fmt.Sprintf("atempo=%g", rate))
	} else if rate > 2.0 {
		// chain atempo
		args = append(args, "-af", "atempo=2.0,atempo="+fmt.Sprintf("%g", rate/2))
	}
	args = append(args, src)
	cmd := exec.Command("ffplay", args...)
	PrepMediaCmd(cmd)
	if err := cmd.Start(); err != nil {
		return
	}
	aid, err := Media().Register(MediaKindAudio, "watch-a", cmd)
	if err != nil {
		_ = killCmd(cmd)
		return
	}
	vp.mu.Lock()
	vp.audio = cmd
	vp.audioMID = aid
	vp.mu.Unlock()
}

func formatFFtime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := d.Seconds()
	return strconv.FormatFloat(sec, 'f', 3, 64)
}

func (vp *VideoPipe) readLoop() {
	frameSize := vp.W * vp.H * 3
	buf := make([]byte, frameSize)
	defer vp.markStopped()

	for {
		vp.mu.Lock()
		stdout := vp.stdout
		cancel := vp.cancel
		paused := vp.paused
		vp.mu.Unlock()

		select {
		case <-cancel:
			return
		default:
		}
		if stdout == nil {
			time.Sleep(30 * time.Millisecond)
			continue
		}
		if paused {
			time.Sleep(40 * time.Millisecond)
			continue
		}
		r := bufio.NewReaderSize(stdout, frameSize*2)
		_, err := io.ReadFull(r, buf)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "file already closed") &&
				!strings.Contains(err.Error(), "closed pipe") {
				vp.mu.Lock()
				vp.Err = err.Error()
				vp.mu.Unlock()
			}
			// EOF: end of file — freeze last frame, mark not running
			return
		}
		cp := make([]byte, frameSize)
		copy(cp, buf)
		vp.mu.Lock()
		mid := ""
		if !vp.paused {
			vp.frame = cp
			vp.seq++
			mid = vp.mediaID
		}
		vp.mu.Unlock()
		if mid != "" {
			Media().Heartbeat(mid)
		}
	}
}

func (vp *VideoPipe) markStopped() {
	vp.mu.Lock()
	vp.running = false
	vp.mu.Unlock()
}

// Snapshot returns latest RGB frame.
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
	return vp.running || len(vp.frame) > 0 // keep scrubbing UI after EOF with last frame
}

func (vp *VideoPipe) Alive() bool {
	if vp == nil {
		return false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	return vp.running
}

func (vp *VideoPipe) Paused() bool {
	if vp == nil {
		return false
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	return vp.paused
}

// Position estimates current playhead.
func (vp *VideoPipe) Position() time.Duration {
	if vp == nil {
		return 0
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	pos := vp.baseSeek
	if !vp.paused && vp.running {
		elapsed := time.Since(vp.playStart)
		pos += time.Duration(float64(elapsed) * vp.rate)
	}
	if vp.duration > 0 && pos > vp.duration {
		pos = vp.duration
	}
	if pos < 0 {
		pos = 0
	}
	return pos
}

func (vp *VideoPipe) Duration() time.Duration {
	if vp == nil {
		return 0
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	return vp.duration
}

func (vp *VideoPipe) Rate() float64 {
	if vp == nil {
		return 1
	}
	vp.mu.Lock()
	defer vp.mu.Unlock()
	if vp.rate <= 0 {
		return 1
	}
	return vp.rate
}

// StatusLine e.g. "▶ 1:23 / 4:56  1×" or "⏸ 0:45 / 4:56  2×"
func (vp *VideoPipe) StatusLine() string {
	if vp == nil {
		return ""
	}
	pos := vp.Position()
	dur := vp.Duration()
	rate := vp.Rate()
	icon := "▶"
	if vp.Paused() {
		icon = "⏸"
	}
	if !vp.Alive() && !vp.Paused() {
		icon = "■"
	}
	s := fmt.Sprintf("%s %s", icon, formatClock(pos))
	if dur > 0 {
		s += " / " + formatClock(dur)
	}
	if rate != 1.0 {
		s += fmt.Sprintf("  %g×", rate)
	}
	return s
}

func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	sec := int(d.Seconds())
	h := sec / 3600
	m := (sec % 3600) / 60
	s := sec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// TogglePause freezes frame + kills audio; resume restarts from position.
func (vp *VideoPipe) TogglePause() {
	if vp == nil {
		return
	}
	if vp.Paused() {
		_ = vp.Resume()
	} else {
		vp.Pause()
	}
}

func (vp *VideoPipe) Pause() {
	if vp == nil {
		return
	}
	vp.mu.Lock()
	if vp.paused {
		vp.mu.Unlock()
		return
	}
	// freeze playhead
	elapsed := time.Since(vp.playStart)
	vp.baseSeek += time.Duration(float64(elapsed) * vp.rate)
	if vp.duration > 0 && vp.baseSeek > vp.duration {
		vp.baseSeek = vp.duration
	}
	vp.paused = true
	audio := vp.audio
	cmd := vp.cmd
	vp.mu.Unlock()

	// stop decode/audio; keep last frame
	if audio != nil && audio.Process != nil {
		_ = audio.Process.Kill()
	}
	if cmd != nil && cmd.Process != nil {
		// stop reading new frames — kill ffmpeg, keep frame buffer
		_ = cmd.Process.Kill()
	}
	vp.mu.Lock()
	vp.running = false
	vp.cmd = nil
	vp.audio = nil
	vp.stdout = nil
	vp.mu.Unlock()
}

func (vp *VideoPipe) Resume() error {
	if vp == nil {
		return fmt.Errorf("no video")
	}
	vp.mu.Lock()
	seek := vp.baseSeek
	rate := vp.rate
	vp.mu.Unlock()
	return vp.Seek(seek, rate)
}

// Seek restarts pipe at absolute position with optional rate (0 = keep).
func (vp *VideoPipe) Seek(at time.Duration, rate float64) error {
	if vp == nil {
		return fmt.Errorf("no video")
	}
	vp.mu.Lock()
	if rate <= 0 {
		rate = vp.rate
	}
	if rate <= 0 {
		rate = 1
	}
	dur := vp.duration
	vp.mu.Unlock()

	if at < 0 {
		at = 0
	}
	if dur > 0 && at > dur {
		at = dur - 100*time.Millisecond
		if at < 0 {
			at = 0
		}
	}

	// signal readLoop to exit old cycle
	vp.mu.Lock()
	select {
	case <-vp.cancel:
		vp.cancel = make(chan struct{})
	default:
		close(vp.cancel)
		vp.cancel = make(chan struct{})
	}
	vp.mu.Unlock()
	vp.killProcs()

	if err := vp.startSegment(at, rate); err != nil {
		return err
	}
	go vp.readLoop()
	return nil
}

// SeekRel moves playhead by delta (negative = back).
func (vp *VideoPipe) SeekRel(delta time.Duration) error {
	if vp == nil {
		return fmt.Errorf("no video")
	}
	pos := vp.Position()
	return vp.Seek(pos+delta, 0)
}

// Restart reopens the ffmpeg watch segment at the current position (vision control plane).
func (vp *VideoPipe) Restart() error {
	if vp == nil {
		return fmt.Errorf("no video")
	}
	if vp.VideoURL == "" {
		return fmt.Errorf("no video url")
	}
	return vp.Seek(vp.Position(), 0)
}

// SetRate changes playback speed (0.5, 1, 1.5, 2).
func (vp *VideoPipe) SetRate(rate float64) error {
	if rate < 0.25 {
		rate = 0.25
	}
	if rate > 4 {
		rate = 4
	}
	return vp.Seek(vp.Position(), rate)
}

// NudgeRate steps through common speeds.
func (vp *VideoPipe) NudgeRate(dir int) error {
	rates := []float64{0.5, 1.0, 1.5, 2.0, 3.0}
	cur := vp.Rate()
	idx := 1
	for i, r := range rates {
		if r >= cur-0.01 {
			idx = i
			break
		}
		idx = i
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(rates) {
		idx = len(rates) - 1
	}
	return vp.SetRate(rates[idx])
}

func (vp *VideoPipe) Stop() {
	if vp == nil {
		return
	}
	vp.mu.Lock()
	select {
	case <-vp.cancel:
	default:
		close(vp.cancel)
	}
	vp.running = false
	vp.paused = false
	vp.mu.Unlock()
	vp.killProcs()
}

// probeDuration via ffprobe (seconds).
func probeDuration(src string) time.Duration {
	if src == "" {
		return 0
	}
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		src,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return time.Duration(f * float64(time.Second))
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
	for _, ext := range []string{
		".mp4", ".mkv", ".mov", ".webm", ".avi", ".m4v", ".ts", ".m3u8",
		".flv", ".wmv", ".gif",
		// binary-level stream codecs
		".gyst", ".gyhex", ".gybin", ".pcap", ".hex",
	} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	return isURL(s)
}

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
