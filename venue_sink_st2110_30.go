package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ST 2110-30 PCM audio egress (AES67-constrained).
//
// Spec basis:
//   - Transport aligns with AES67 L16/L24 over RTP
//   - PTP profile MUST be ST 2059-2 (not AES67 media profile alone)
//   - media clock ↔ RTP timestamp offset = 0
//   - channel-order fmtp: channel-order=SMPTE2110.(ST) etc.
//
//	gy venue --sink st2110 --audio-rtp rtp://239.100.1.10:5006

// ST211030Opts configures 2110-30 audio sender.
type ST211030Opts struct {
	RTP       string // audio RTP URL (separate from video)
	SDPPath   string // if set, write audio-only SDP (or use multi-essence)
	Rate      int    // 48000
	Channels  int    // 2 default
	Depth     int    // 16 or 24 (L16/L24)
	PtimeMs   int    // 1 = Level A typical
	Level     string // A|B|C
	Quiet     bool
	MetaDir   string
	ChannelOrder string // SMPTE2110.(ST) default stereo
}

// st211030Sink pushes silence or PCM to FFmpeg L24/L16 RTP (AES67-style).
type st211030Sink struct {
	opts   ST211030Opts
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	started bool
	held   bool
	black  bool
	frames uint64
	lastBus ProgramBus
	sync   SyncClockReport
}

// NewST211030Sink builds a 2110-30 audio VenueSink (can pair via multiVenueSink).
func NewST211030Sink(opts ST211030Opts) (VenueSink, error) {
	if opts.RTP == "" {
		opts.RTP = "rtp://239.100.1.10:5006"
	}
	if opts.Rate < 1 {
		opts.Rate = 48000
	}
	if opts.Channels < 1 {
		opts.Channels = 2
	}
	if opts.Depth != 24 {
		opts.Depth = 24 // L24 preferred in 2110-30
	}
	if opts.PtimeMs < 1 {
		opts.PtimeMs = 1
	}
	if opts.Level == "" {
		opts.Level = ST211030LevelA
	}
	if opts.ChannelOrder == "" {
		opts.ChannelOrder = "SMPTE2110.(ST)"
	}
	if opts.MetaDir == "" {
		opts.MetaDir = filepath.Join(os.TempDir(), "gy-venue")
	}
	_ = os.MkdirAll(opts.MetaDir, 0o755)
	if opts.SDPPath == "" {
		opts.SDPPath = filepath.Join(opts.MetaDir, "gy-st2110-30.sdp")
	}
	host, port, err := parseRTPURL(opts.RTP)
	if err != nil {
		return nil, err
	}
	if err := writeST211030SDP(opts.SDPPath, host, port, opts); err != nil {
		return nil, err
	}
	if !opts.Quiet {
		log.Printf("venue · st2110-30 · L%d/%d/%d ch ptime=%dms → %s",
			opts.Depth, opts.Rate, opts.Channels, opts.PtimeMs, opts.RTP)
		log.Printf("venue · st2110-30 · SDP %s · PTP profile %s required",
			opts.SDPPath, PTPProfileST2059)
	}
	return &st211030Sink{
		opts: opts,
		sync: DefaultSyncClockReport(),
	}, nil
}

func (s *st211030Sink) Name() string { return "st2110-30" }

func (s *st211030Sink) OnProgram(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.held = bus.Mode == ProgramModeHold
	s.black = bus.Mode == ProgramModeBlack
	s.mu.Unlock()
	// sidecar
	meta := filepath.Join(s.opts.MetaDir, "st2110-30-program.json")
	b, _ := jsonMarshalProgramAudio(bus, s.sync)
	_ = os.WriteFile(meta, b, 0o644)
}

func (s *st211030Sink) OnGlyph(frame VenueGlyphFrame) {
	// audio sink: keep stream alive with silence while video frames flow (hold clock)
	s.mu.Lock()
	if s.black || s.held {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	_ = s.ensureStarted()
	s.writeSilencePacket()
}

func (s *st211030Sink) OnBlack(bus ProgramBus) {
	s.mu.Lock()
	s.black = true
	s.lastBus = bus
	s.mu.Unlock()
	// digital silence continues if running (or stop)
	_ = s.ensureStarted()
	s.writeSilencePacket()
}

func (s *st211030Sink) OnHold(bus ProgramBus) {
	s.mu.Lock()
	s.held = true
	s.lastBus = bus
	s.mu.Unlock()
}

func (s *st211030Sink) ensureStarted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("st2110-30: ffmpeg required")
	}
	// s16le pipe @ rate/ch → L24 or s16be RTP
	codec := "pcm_s24be"
	if s.opts.Depth == 16 {
		codec = "pcm_s16be"
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "s16le",
		"-ar", strconv.Itoa(s.opts.Rate),
		"-ac", strconv.Itoa(s.opts.Channels),
		"-i", "pipe:0",
		"-c:a", codec,
		"-f", "rtp",
		"-payload_type", "97",
		s.opts.RTP,
	}
	cmd := exec.Command(bin, args...)
	if s.opts.Quiet {
		cmd.Stderr = nil
	} else {
		cmd.Stderr = os.Stderr
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	s.cmd = cmd
	s.stdin = stdin
	s.started = true
	// background silence pump so AES67 receivers see continuous ptime
	go s.silenceLoop()
	return nil
}

func (s *st211030Sink) silenceLoop() {
	// 1 ms of s16le stereo @ 48k = 48 * 2 * 2 = 192 bytes
	nSamp := s.opts.Rate * s.opts.PtimeMs / 1000
	if nSamp < 1 {
		nSamp = 48
	}
	packet := make([]byte, nSamp*s.opts.Channels*2) // zeros = silence
	t := time.NewTicker(time.Duration(s.opts.PtimeMs) * time.Millisecond)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		if !s.started || s.stdin == nil {
			s.mu.Unlock()
			return
		}
		if s.black {
			// still send silence
		}
		stdin := s.stdin
		s.mu.Unlock()
		if _, err := stdin.Write(packet); err != nil {
			return
		}
		s.mu.Lock()
		s.frames++
		s.mu.Unlock()
	}
}

func (s *st211030Sink) writeSilencePacket() {
	// silenceLoop owns continuous send; this is a no-op kick
	_ = s.ensureStarted()
}

func (s *st211030Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		_ = s.stdin.Close()
		s.stdin = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
		s.cmd = nil
	}
	s.started = false
	return nil
}

// writeST211030SDP — AES67-aligned ST 2110-30 audio SDP.
func writeST211030SDP(path, host string, port int, opts ST211030Opts) error {
	depth := opts.Depth
	if depth != 16 {
		depth = 24
	}
	enc := "L24"
	if depth == 16 {
		enc = "L16"
	}
	now := time.Now().Unix()
	ptime := float64(opts.PtimeMs)
	if ptime < 0.125 {
		ptime = 1
	}
	fmtp := fmt.Sprintf("channel-order=%s", opts.ChannelOrder)
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-30 PGM
i=ST 2110-30 PCM audio (AES67 constrained). PTP profile MUST be %s. media clock to RTP offset 0. Level %s ptime=%.3f ms. Program bus in st2110-30-program.json.
c=IN IP4 %s/32
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-profile:2110-30
a=x-gy-level:%s
a=ts-refclk:localmac=00-00-00-00-00-00
a=mediaclk:direct=0
a=source-filter: incl IN IP4 * %s
m=audio %d RTP/AVP 97
a=rtpmap:97 %s/%d/%d
a=fmtp:97 %s
a=ptime:%.3f
a=maxptime:%.3f
a=recvonly
`, now, now, host, PTPProfileST2059, opts.Level, ptime, host, Version, opts.Level, host, port, enc, opts.Rate, opts.Channels, fmtp, ptime, ptime)
	return os.WriteFile(path, []byte(body), 0o644)
}

func jsonMarshalProgramAudio(bus ProgramBus, sync SyncClockReport) ([]byte, error) {
	return json.MarshalIndent(map[string]any{
		"type": "program", "essence": ST2110_30,
		"mode": bus.Mode, "seq": bus.Seq, "program": bus.Program,
		"sync": sync, "t": time.Now().UnixMilli(),
	}, "", "  ")
}
