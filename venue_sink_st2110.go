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

// ST2110VenueSink — ST 2110-oriented egress for venue/soundstage pipelines.
//
// Lab profile (shippable without full NMOS/PTP stack):
//   - Uncompressed-ish low-latency RTP video via FFmpeg (RTP/MPEG-TS or raw RTP)
//   - SMPTE-style SDP written for receivers (media + fmtp notes)
//   - Program bus sidecar JSON (mark/slot/mode) for tally / automation
//
// Full ST 2110-20 uncompressed + PTP genlock is site-specific; this sink is the
// GrokYtalkY-side contract fulfillment — swap ffmpeg args / NMOS registration
// at the facility without changing VenueSink.
//
//	gy venue --sink st2110 --rtp rtp://239.100.1.10:5004 --sdp /tmp/gy-2110.sdp

// ST2110Opts configures ST 2110-style RTP egress.
type ST2110Opts struct {
	RTP     string // e.g. rtp://239.100.1.10:5004
	SDPPath string // where to write SDP
	Width   int
	Height  int
	FPS     int
	Quiet   bool
	MetaDir string
	// Payload: "mpegts" (default, robust) or "rtp" (ffmpeg rtp muxer)
	Payload string
}

// NewST2110VenueSink builds an ST 2110-oriented VenueSink.
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

	// Write SDP before ffmpeg starts so receivers can join
	if err := writeST2110SDP(opts.SDPPath, host, port, opts.Width, opts.Height, opts.FPS); err != nil {
		return nil, fmt.Errorf("sdp: %w", err)
	}
	if !opts.Quiet {
		log.Printf("venue · st2110 · SDP %s", opts.SDPPath)
		log.Printf("venue · st2110 · RTP %s (%dx%d@%d)", opts.RTP, opts.Width, opts.Height, opts.FPS)
	}

	args := ffmpegRawInputArgs(opts.Width, opts.Height, opts.FPS)
	payload := strings.ToLower(opts.Payload)
	if payload == "" {
		payload = "mpegts"
	}
	switch payload {
	case "rtp":
		// ffmpeg native rtp mux — companion SDP describes the session
		args = append(args,
			"-an",
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-g", strconv.Itoa(opts.FPS),
			"-f", "rtp",
			opts.RTP,
		)
	default:
		// RTP-encapsulated MPEG-TS — common lab / venue gateway input
		args = append(args,
			"-an",
			"-c:v", "libx264",
			"-preset", "ultrafast",
			"-tune", "zerolatency",
			"-pix_fmt", "yuv420p",
			"-g", strconv.Itoa(opts.FPS),
			"-f", "rtp_mpegts",
			opts.RTP,
		)
	}

	s := newFFmpegPipeSink("st2110", "st2110", opts.Width, opts.Height, opts.FPS, args, opts.Quiet)
	s.metaPath = metaPath
	return s, nil
}

// writeST2110SDP writes a session description receivers can use.
// Notes ST 2110 intent; media line matches lab RTP/H.264 profile.
func writeST2110SDP(path, host string, port, w, h, fps int) error {
	// Clock rate 90000 for video; fmtp carries resolution for adapters
	now := time.Now().Unix()
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-lab PGM
i=GrokYtalkY venue sink — glyph lattice nearest-neighbor to %dx%d @ %d fps; program bus mark in sidecar JSON. Full ST 2110-20 uncompressed+PTP is facility-side.
c=IN IP4 %s
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-program-meta:st2110-program.json
a=x-gy-lattice:pass-through
m=video %d RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 packetization-mode=1;profile-level-id=42e01f
a=framesize:96 %d-%d
a=framerate:%d
a=recvonly
`, now, now, host, w, h, fps, host, Version, port, w, h, fps)
	return os.WriteFile(path, []byte(body), 0o644)
}

func parseRTPURL(u string) (host string, port int, err error) {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "rtp://")
	u = strings.TrimPrefix(u, "udp://")
	// strip query
	if i := strings.IndexByte(u, '?'); i >= 0 {
		u = u[:i]
	}
	h, p, err := net.SplitHostPort(u)
	if err != nil {
		// host only
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
