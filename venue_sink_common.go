package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Shared venue video geometry for NDI / ST 2110 egress.
// Glyph lattices are nearest-neighbor scaled so 40/200 corner stamps stay sharp.
const (
	VenueDefaultW   = 1280
	VenueDefaultH   = 720
	VenueDefaultFPS = 30
)

// ffmpegPipeSink feeds raw RGB24 frames into an ffmpeg process.
// Used by NDI and ST 2110 sinks — lattice is never re-stamped, only scaled.
type ffmpegPipeSink struct {
	name   string
	kind   string // ndi | st2110
	quiet  bool
	outW   int
	outH   int
	fps    int
	args   []string // full ffmpeg argv after binary
	metaPath string // optional sidecar (SDP / program JSON)

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	started bool
	held    bool
	black   bool
	lastRGB []byte
	frames  uint64
	lastBus ProgramBus
	err     error
}

func newFFmpegPipeSink(name, kind string, outW, outH, fps int, args []string, quiet bool) *ffmpegPipeSink {
	if outW < 16 {
		outW = VenueDefaultW
	}
	if outH < 16 {
		outH = VenueDefaultH
	}
	if fps < 1 {
		fps = VenueDefaultFPS
	}
	return &ffmpegPipeSink{
		name: name, kind: kind, quiet: quiet,
		outW: outW, outH: outH, fps: fps, args: args,
	}
}

func (s *ffmpegPipeSink) Name() string { return s.name }

func (s *ffmpegPipeSink) ensureStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return s.err
	}
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		s.err = fmt.Errorf("%s: ffmpeg not on PATH", s.kind)
		return s.err
	}
	cmd := exec.Command(bin, s.args...)
	cmd.Stdout = nil
	if s.quiet {
		cmd.Stderr = nil
	} else {
		cmd.Stderr = os.Stderr
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.err = err
		return err
	}
	if err := cmd.Start(); err != nil {
		s.err = fmt.Errorf("%s ffmpeg start: %w", s.kind, err)
		return s.err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.started = true
	if !s.quiet {
		log.Printf("venue · %s · ffmpeg pid=%d %dx%d@%d", s.kind, cmd.Process.Pid, s.outW, s.outH, s.fps)
	}
	return nil
}

func (s *ffmpegPipeSink) OnProgram(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.held = bus.Mode == ProgramModeHold
	s.black = bus.Mode == ProgramModeBlack
	s.mu.Unlock()
	_ = s.writeProgramMeta(bus)
	if !s.quiet {
		log.Printf("venue · %s · PGM %s", s.kind, FormatProgramLine(bus))
	}
	if bus.Mode == ProgramModeBlack {
		s.OnBlack(bus)
	}
}

func (s *ffmpegPipeSink) OnGlyph(frame VenueGlyphFrame) {
	if err := s.ensureStarted(); err != nil {
		if !s.quiet {
			log.Printf("venue · %s · %v", s.kind, err)
		}
		return
	}
	s.mu.Lock()
	if s.black {
		s.mu.Unlock()
		return
	}
	if s.held && s.lastRGB != nil {
		// hold: re-push last raster, ignore new lattice
		rgb := s.lastRGB
		stdin := s.stdin
		s.mu.Unlock()
		s.writeRGB(stdin, rgb)
		return
	}
	rgb := glyphLumToRGB24(frame.Data, frame.N, s.outW, s.outH)
	s.lastRGB = rgb
	s.frames++
	stdin := s.stdin
	n := s.frames
	s.mu.Unlock()
	if err := s.writeRGB(stdin, rgb); err != nil && !s.quiet {
		log.Printf("venue · %s · write: %v", s.kind, err)
	}
	if !s.quiet && (n == 1 || n%60 == 0) {
		log.Printf("venue · %s · frame %d n=%d→%dx%d mark=%s",
			s.kind, n, frame.N, s.outW, s.outH, ShortMarkID(frame.Mark))
	}
}

func (s *ffmpegPipeSink) OnBlack(bus ProgramBus) {
	s.mu.Lock()
	s.black = true
	s.held = false
	s.lastBus = bus
	s.mu.Unlock()
	if err := s.ensureStarted(); err != nil {
		return
	}
	rgb := make([]byte, s.outW*s.outH*3) // zeros
	s.mu.Lock()
	s.lastRGB = rgb
	stdin := s.stdin
	s.mu.Unlock()
	_ = s.writeRGB(stdin, rgb)
	if !s.quiet {
		log.Printf("venue · %s · BLACK", s.kind)
	}
}

func (s *ffmpegPipeSink) OnHold(bus ProgramBus) {
	s.mu.Lock()
	s.held = true
	s.black = false
	s.lastBus = bus
	rgb := s.lastRGB
	stdin := s.stdin
	started := s.started
	s.mu.Unlock()
	if started && rgb != nil {
		_ = s.writeRGB(stdin, rgb)
	}
	if !s.quiet {
		log.Printf("venue · %s · HOLD", s.kind)
	}
}

func (s *ffmpegPipeSink) writeRGB(w io.Writer, rgb []byte) error {
	if w == nil || len(rgb) == 0 {
		return fmt.Errorf("no writer")
	}
	_, err := w.Write(rgb)
	return err
}

func (s *ffmpegPipeSink) writeProgramMeta(bus ProgramBus) error {
	if s.metaPath == "" {
		return nil
	}
	// sidecar JSON for venue controllers (tally, NMOS-style labels later)
	b, _ := json.MarshalIndent(map[string]any{
		"type": "program", "sink": s.kind, "t": time.Now().UnixMilli(),
		"mode": bus.Mode, "seq": bus.Seq, "conductor": bus.Conductor,
		"program": bus.Program, "preview": bus.Preview,
		"mark": bus.Program.Mark, "slot": bus.Program.Slot,
		"venue": bus.VenueAdapterHint(),
	}, "", "  ")
	return os.WriteFile(s.metaPath, b, 0o644)
}

func (s *ffmpegPipeSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		_ = s.stdin.Close()
		s.stdin = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = s.cmd.Process.Kill()
		}
		s.cmd = nil
	}
	s.started = false
	return nil
}

// glyphLumToRGB24 nearest-neighbor upscales hexlum lattice to outW×outH RGB24.
// Corner stamps (40/200) remain blocky/visible — no re-stamp.
func glyphLumToRGB24(lum []byte, n, outW, outH int) []byte {
	if n < 1 {
		n = 1
	}
	need := n * n
	if len(lum) < need {
		// pad
		p := make([]byte, need)
		copy(p, lum)
		lum = p
	}
	out := make([]byte, outW*outH*3)
	for y := 0; y < outH; y++ {
		sy := y * n / outH
		if sy >= n {
			sy = n - 1
		}
		for x := 0; x < outW; x++ {
			sx := x * n / outW
			if sx >= n {
				sx = n - 1
			}
			v := lum[sy*n+sx]
			i := (y*outW + x) * 3
			out[i], out[i+1], out[i+2] = v, v, v
		}
	}
	return out
}

// ffmpegHasFormat reports whether `ffmpeg -formats` lists name (e.g. libndi_newtek).
func ffmpegHasFormat(name string) bool {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return false
	}
	cmd := exec.Command(bin, "-hide_banner", "-formats")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

// ffmpegRawInputArgs common raw RGB24 pipe input for venue sinks.
func ffmpegRawInputArgs(w, h, fps int) []string {
	return []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-s:v", fmt.Sprintf("%dx%d", w, h),
		"-r", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}
}

// multiVenueSink fans out to several sinks (e.g. log + ndi + st2110).
type multiVenueSink struct {
	sinks []VenueSink
}

func (m *multiVenueSink) Name() string {
	parts := make([]string, 0, len(m.sinks))
	for _, s := range m.sinks {
		parts = append(parts, s.Name())
	}
	return strings.Join(parts, "+")
}

func (m *multiVenueSink) OnProgram(bus ProgramBus) {
	for _, s := range m.sinks {
		s.OnProgram(bus)
	}
}
func (m *multiVenueSink) OnGlyph(f VenueGlyphFrame) {
	for _, s := range m.sinks {
		s.OnGlyph(f)
	}
}
func (m *multiVenueSink) OnBlack(bus ProgramBus) {
	for _, s := range m.sinks {
		s.OnBlack(bus)
	}
}
func (m *multiVenueSink) OnHold(bus ProgramBus) {
	for _, s := range m.sinks {
		s.OnHold(bus)
	}
}
func (m *multiVenueSink) Close() error {
	var first error
	for _, s := range m.sinks {
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

