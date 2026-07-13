package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ST 2110 venue sink profiles.
const (
	// ST2110Profile211020 — uncompressed YCbCr-4:2:2 8-bit over RTP (RFC 4175-style)
	// with SMPTE ST 2110-20 SDP fmtp. Default for --sink st2110.
	// PTP/NMOS registration remain facility-side (ts-refclk marked local).
	ST2110Profile211020 = "2110-20"
	// ST2110ProfileLab — low-latency H.264 RTP/MPEG-TS for gateways without raw 2110.
	ST2110ProfileLab = "lab"
)

// ST2110VenueSink — ST 2110 egress for venue/soundstage pipelines.
//
//	gy venue --sink st2110                          # 2110-20 default
//	gy venue --sink st2110 --profile 2110-20
//	gy venue --sink st2110 --profile lab
//	gy venue --sink st2110 --rtp rtp://239.100.1.10:5004 --sdp /tmp/gy-2110.sdp

// ST2110Opts configures ST 2110 egress.
type ST2110Opts struct {
	RTP     string // e.g. rtp://239.100.1.10:5004
	SDPPath string
	Width   int
	Height  int
	FPS     int
	Quiet   bool
	MetaDir string
	// Profile: 2110-20 (default) | lab
	Profile string
	// Payload only applies to lab: mpegts | rtp
	Payload string
	// Sampling for 2110-20: YCbCr-4:2:2 (default) | YCbCr-4:2:0
	Sampling string
	// Depth bits (8 default; 10 reserved for v210 paths later)
	Depth int
	// AudioRTP enables ST 2110-30 companion (AES67-constrained PCM).
	AudioRTP string
	// MultiSDP path for combined 20+30 session (default metaDir/gy-st2110-bundle.sdp)
	MultiSDP string
	// Sync clock report embedded in sidecars
	Sync SyncClockReport
}

// NewST2110VenueSink builds an ST 2110 VenueSink.
func NewST2110VenueSink(opts ST2110Opts) (VenueSink, error) {
	if opts.RTP == "" {
		opts.RTP = "rtp://239.100.1.10:5004"
	}
	if opts.Width < 16 {
		opts.Width = VenueDefaultW
	}
	if opts.Height < 16 {
		opts.Height = VenueDefaultH
	}
	if opts.FPS < 1 {
		opts.FPS = VenueDefaultFPS
	}
	if opts.Depth < 8 {
		opts.Depth = 8
	}
	profile := normalizeST2110Profile(opts.Profile)
	sampling := opts.Sampling
	if sampling == "" {
		sampling = "YCbCr-4:2:2"
	}
	if opts.MetaDir == "" {
		opts.MetaDir = filepath.Join(os.TempDir(), "gy-venue")
	}
	_ = os.MkdirAll(opts.MetaDir, 0o755)
	if opts.SDPPath == "" {
		opts.SDPPath = filepath.Join(opts.MetaDir, "gy-st2110.sdp")
	}
	metaPath := filepath.Join(opts.MetaDir, "st2110-program.json")

	host, port, err := parseRTPURL(opts.RTP)
	if err != nil {
		return nil, err
	}

	var args []string
	var sinkName string
	switch profile {
	case ST2110Profile211020:
		if err := writeST211020SDP(opts.SDPPath, host, port, opts.Width, opts.Height, opts.FPS, sampling, opts.Depth); err != nil {
			return nil, fmt.Errorf("sdp 2110-20: %w", err)
		}
		args = buildST211020FFmpegArgs(opts.Width, opts.Height, opts.FPS, opts.RTP, sampling)
		sinkName = "st2110-20"
		if !opts.Quiet {
			log.Printf("venue · st2110-20 · uncompressed %s depth=%d → %s", sampling, opts.Depth, opts.RTP)
			log.Printf("venue · st2110-20 · SDP %s (PTP not locked — facility ts-refclk)", opts.SDPPath)
		}
	default: // lab
		if err := writeST2110LabSDP(opts.SDPPath, host, port, opts.Width, opts.Height, opts.FPS); err != nil {
			return nil, fmt.Errorf("sdp lab: %w", err)
		}
		args = buildST2110LabFFmpegArgs(opts.Width, opts.Height, opts.FPS, opts.RTP, opts.Payload)
		sinkName = "st2110-lab"
		if !opts.Quiet {
			log.Printf("venue · st2110-lab · H.264 RTP → %s", opts.RTP)
			log.Printf("venue · st2110-lab · SDP %s", opts.SDPPath)
		}
	}

	video := newFFmpegPipeSink(sinkName, "st2110", opts.Width, opts.Height, opts.FPS, args, opts.Quiet)
	video.metaPath = metaPath

	// Optional ST 2110-30 audio essence + multi-essence SDP
	if opts.AudioRTP != "" && profile == ST2110Profile211020 {
		aOpts := ST211030Opts{
			RTP: opts.AudioRTP, Quiet: opts.Quiet, MetaDir: opts.MetaDir,
			Rate: 48000, Channels: 2, Depth: 24, PtimeMs: 1, Level: ST211030LevelA,
		}
		audio, err := NewST211030Sink(aOpts)
		if err != nil {
			return nil, fmt.Errorf("2110-30: %w", err)
		}
		multiPath := opts.MultiSDP
		if multiPath == "" {
			multiPath = filepath.Join(opts.MetaDir, "gy-st2110-bundle.sdp")
		}
		aHost, aPort, _ := parseRTPURL(opts.AudioRTP)
		sync := opts.Sync
		if sync.MediaClockHz == 0 {
			sync = DefaultSyncClockReport()
		}
		if err := writeST2110MultiEssenceSDP(multiPath, host, port, aHost, aPort,
			opts.Width, opts.Height, opts.FPS, aOpts, sync); err != nil {
			return nil, err
		}
		if !opts.Quiet {
			log.Printf("venue · st2110 · multi-essence SDP %s (20+30)", multiPath)
			log.Print(FormatSyncClockReport(sync))
		}
		return &multiVenueSink{sinks: []VenueSink{video, audio}}, nil
	}
	return video, nil
}

func normalizeST2110Profile(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "2110-20", "2110_20", "20", "st2110-20", "raw", "uncompressed":
		return ST2110Profile211020
	case "lab", "h264", "mpegts", "gateway":
		return ST2110ProfileLab
	default:
		return ST2110Profile211020
	}
}

// buildST211020FFmpegArgs: RGB24 pipe → UYVY/I420 raw → RTP.
// Pixel format matches SDP sampling; lattice is only nearest-neighbor scaled upstream.
func buildST211020FFmpegArgs(w, h, fps int, rtp, sampling string) []string {
	args := ffmpegRawInputArgs(w, h, fps)
	pix := "uyvy422"
	if strings.Contains(sampling, "4:2:0") {
		pix = "yuv420p"
	}
	// Convert rgb24 → 4:2:2 (or 4:2:0), emit rawvideo over RTP.
	// payload_type 96 matches SDP a=rtpmap:96 raw/90000
	args = append(args,
		"-an",
		"-vf", fmt.Sprintf("format=%s", pix),
		"-c:v", "rawvideo",
		"-pix_fmt", pix,
		"-f", "rtp",
		"-payload_type", "96",
		rtp,
	)
	return args
}

func buildST2110LabFFmpegArgs(w, h, fps int, rtp, payload string) []string {
	args := ffmpegRawInputArgs(w, h, fps)
	payload = strings.ToLower(payload)
	if payload == "" {
		payload = "mpegts"
	}
	common := []string{
		"-an",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(fps),
	}
	args = append(args, common...)
	if payload == "rtp" {
		args = append(args, "-f", "rtp", rtp)
	} else {
		args = append(args, "-f", "rtp_mpegts", rtp)
	}
	return args
}

// writeST211020SDP writes SMPTE ST 2110-20 style SDP (RFC 4566 + 2110-20 fmtp).
// Reference: SMPTE ST 2110-20 / ST 2110-10 media description conventions.
func writeST211020SDP(path, host string, port, w, h, fps int, sampling string, depth int) error {
	if sampling == "" {
		sampling = "YCbCr-4:2:2"
	}
	if depth < 8 {
		depth = 8
	}
	// exactframerate as integer or 30000/1001 style
	fr := fmt.Sprintf("%d", fps)
	now := time.Now().Unix()
	// PM=2110GPM general packetization; TP=2110TPN narrow sender (software)
	// SSN=ST2110-20:2017 signals essence type to receivers
	fmtp := fmt.Sprintf(
		"sampling=%s; width=%d; height=%d; exactframerate=%s; depth=%d; "+
			"colorimetry=BT709; PM=2110GPM; SSN=ST2110-20:2017; TP=2110TPN; "+
			"interlace=0; segment=0",
		sampling, w, h, fr, depth,
	)
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-20 PGM
i=GrokYtalkY venue ST 2110-20 essence — glyph lattice nearest-neighbor to %dx%d@%s; program bus mark in st2110-program.json. PTP/NMOS not provided by this sender (ts-refclk=local).
c=IN IP4 %s/32
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-profile:2110-20
a=x-gy-program-meta:st2110-program.json
a=x-gy-lattice:pass-through
a=ts-refclk:localmac=00-00-00-00-00-00
a=mediaclk:direct=0
a=source-filter: incl IN IP4 * %s
m=video %d RTP/AVP 96
a=rtpmap:96 raw/90000
a=fmtp:96 %s
a=framesize:96 %d-%d
a=framerate:%s
a=recvonly
a=ptime:%.3f
`, now, now, host, w, h, fr, host, Version, host, port, fmtp, w, h, fr, 1000.0/float64(fps))
	return os.WriteFile(path, []byte(body), 0o644)
}

// writeST2110LabSDP — compressed lab path (not full 2110-20).
func writeST2110LabSDP(path, host string, port, w, h, fps int) error {
	now := time.Now().Unix()
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-lab PGM
i=Lab gateway profile (H.264 over RTP) — not ST 2110-20 uncompressed. Use --profile 2110-20 for raw essence.
c=IN IP4 %s
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-profile:lab
a=x-gy-program-meta:st2110-program.json
a=x-gy-lattice:pass-through
m=video %d RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1;profile-level-id=42e01f
a=framesize:96 %d-%d
a=framerate:%d
a=recvonly
`, now, now, host, host, Version, port, w, h, fps)
	return os.WriteFile(path, []byte(body), 0o644)
}

// Deprecated name kept for tests that called writeST2110SDP — routes to 2110-20.
func writeST2110SDP(path, host string, port, w, h, fps int) error {
	return writeST211020SDP(path, host, port, w, h, fps, "YCbCr-4:2:2", 8)
}

func parseRTPURL(u string) (host string, port int, err error) {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "rtp://")
	u = strings.TrimPrefix(u, "udp://")
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	h, p, err := net.SplitHostPort(u)
	if err != nil {
		if ip := net.ParseIP(u); ip != nil {
			return u, 5004, nil
		}
		return "", 0, fmt.Errorf("rtp url: %w", err)
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	return h, port, nil
}
