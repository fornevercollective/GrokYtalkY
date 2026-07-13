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
	// ST2110Profile211020 — uncompressed YCbCr over RTP + full ST 2110-20 fmtp.
	// PTP/NMOS remain facility-side (ts-refclk local until GM attached).
	ST2110Profile211020 = "2110-20"
	// ST2110ProfileLab — low-latency H.264 RTP/MPEG-TS for gateways.
	ST2110ProfileLab = "lab"
)

// ST2110VenueSink — ST 2110 egress for venue/soundstage pipelines.
//
//	gy venue --sink st2110 --profile 2110-20
//	gy venue --sink st2110 --tp 2110TPN --fps 30000/1001
//	gy venue --sink st2110 --profile lab

// ST2110Opts configures ST 2110 egress.
type ST2110Opts struct {
	RTP     string
	SDPPath string
	Width   int
	Height  int
	FPS     int    // integer fps when FPSExact empty
	FPSExact string // "30" | "30000/1001" | "29.97"
	Quiet   bool
	MetaDir string
	Profile string
	Payload string // lab: mpegts | rtp
	Sampling string
	Depth   int
	// ST 2110-21 sender type
	TP string // 2110TPN | 2110TPNL | 2110TPW
	// Color
	Colorimetry string
	TCS         string
	RANGE       string
	// AudioRTP enables ST 2110-30 companion
	AudioRTP string
	MultiSDP string
	Sync     SyncClockReport
	// ST 2022-7 hitless dual destination (secondary video RTP)
	RTPB string // e.g. rtp://239.100.1.11:5004
	// ST 2110-40 ANC companion
	AncRTP string
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

	sync := opts.Sync
	if sync.MediaClockHz == 0 {
		sync = DefaultSyncClockReport()
	}

	var args []string
	var sinkName string
	var vp ST211020Params

	hitless := ST20227FromURLs(opts.RTP, opts.RTPB)

	switch profile {
	case ST2110Profile211020:
		vp = buildST211020Params(opts)
		if err := WriteST211020SDPTightEx(opts.SDPPath, host, port, vp, sync, hitless); err != nil {
			return nil, fmt.Errorf("sdp 2110-20: %w", err)
		}
		args = buildST211020FFmpegArgsFromParams(vp, opts.RTP, opts.RTPB)
		sinkName = "st2110-20"
		if hitless.Enabled {
			sinkName = "st2110-20+2022-7"
		}
		if !opts.Quiet {
			log.Printf("venue · st2110-20 · %dx%d@%s %s depth=%d TP=%s → %s",
				vp.Width, vp.Height, vp.ExactFramerate(), vp.Sampling, vp.Depth, vp.TP, opts.RTP)
			log.Printf("venue · st2110-20 · pix_fmt=%s SDP %s", vp.PixFmt(), opts.SDPPath)
			if hitless.Enabled {
				log.Printf("venue · %s", FormatST20227Line(hitless))
			}
			if !sync.Compliant {
				log.Printf("venue · st2110-20 · PTP free-run (ST 2059-2 lock required for production)")
			}
		}
		_ = WriteST20227Sidecar(filepath.Join(opts.MetaDir, "st2022-7.json"), hitless, ST2110_20)
	default:
		if err := writeST2110LabSDP(opts.SDPPath, host, port, opts.Width, opts.Height, opts.FPS); err != nil {
			return nil, fmt.Errorf("sdp lab: %w", err)
		}
		args = buildST2110LabFFmpegArgs(opts.Width, opts.Height, opts.FPS, opts.RTP, opts.Payload, opts.RTPB)
		sinkName = "st2110-lab"
		if hitless.Enabled {
			sinkName = "st2110-lab+2022-7"
		}
		if !opts.Quiet {
			log.Printf("venue · st2110-lab · H.264 RTP → %s", opts.RTP)
			if hitless.Enabled {
				log.Printf("venue · %s", FormatST20227Line(hitless))
			}
		}
	}

	// integer fps for pipe geometry (ffmpeg -r accepts fraction in args)
	pipeFPS := opts.FPS
	if profile == ST2110Profile211020 {
		pipeFPS = int(vp.FPSFloat() + 0.5)
		if pipeFPS < 1 {
			pipeFPS = 30
		}
	}
	video := newFFmpegPipeSink(sinkName, "st2110", opts.Width, opts.Height, pipeFPS, args, opts.Quiet)
	video.metaPath = metaPath

	var sinks []VenueSink
	sinks = append(sinks, video)

	if opts.AudioRTP != "" && profile == ST2110Profile211020 {
		aOpts := ST211030Opts{
			RTP: opts.AudioRTP, Quiet: opts.Quiet, MetaDir: opts.MetaDir,
			Rate: 48000, Channels: 2, Depth: 24, PtimeMs: 1, Level: ST211030LevelA,
		}
		audio, err := NewST211030Sink(aOpts)
		if err != nil {
			return nil, fmt.Errorf("2110-30: %w", err)
		}
		sinks = append(sinks, audio)
		multiPath := opts.MultiSDP
		if multiPath == "" {
			multiPath = filepath.Join(opts.MetaDir, "gy-st2110-bundle.sdp")
		}
		aHost, aPort, _ := parseRTPURL(opts.AudioRTP)
		if err := writeST2110MultiEssenceSDPTight(multiPath, host, port, aHost, aPort, vp, aOpts, sync); err != nil {
			return nil, err
		}
		// optional ANC media line on bundle
		if opts.AncRTP != "" {
			if _, ancPort, err := parseRTPURL(opts.AncRTP); err == nil {
				if b, err := os.ReadFile(multiPath); err == nil {
					ah, _, _ := parseRTPURL(opts.AncRTP)
					_ = ah
					nb := AppendANCToMultiEssence(string(b), host, ancPort)
					_ = os.WriteFile(multiPath, []byte(nb), 0o644)
				}
			}
		}
		if !opts.Quiet {
			log.Printf("venue · st2110 · multi-essence SDP %s", multiPath)
		}
	}

	if opts.AncRTP != "" {
		anc, err := NewST211040Sink(ST211040Opts{
			RTP: opts.AncRTP, Quiet: opts.Quiet, MetaDir: opts.MetaDir, Sync: sync,
		})
		if err != nil {
			return nil, fmt.Errorf("2110-40: %w", err)
		}
		sinks = append(sinks, anc)
	}

	if len(sinks) == 1 {
		return sinks[0], nil
	}
	return &multiVenueSink{sinks: sinks}, nil
}

func buildST211020Params(opts ST2110Opts) ST211020Params {
	p := DefaultST211020Params(opts.Width, opts.Height, opts.FPS)
	if opts.Sampling != "" {
		p.Sampling = opts.Sampling
	}
	if opts.Depth >= 8 {
		p.Depth = opts.Depth
	}
	p.TP = NormalizeTP(opts.TP)
	if opts.Colorimetry != "" {
		p.Colorimetry = opts.Colorimetry
	}
	if opts.TCS != "" {
		p.TCS = opts.TCS
	}
	if opts.RANGE != "" {
		p.RANGE = opts.RANGE
	}
	if opts.FPSExact != "" {
		if n, d, err := ParseExactFPS(opts.FPSExact); err == nil {
			p.FPSNum, p.FPSDen = n, d
		}
	}
	return p
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

// buildST211020FFmpegArgsFromParams: RGB24 pipe → essence pix_fmt → RTP raw.
// If rtpB set, use ffmpeg tee for ST 2022-7 dual destination.
func buildST211020FFmpegArgsFromParams(p ST211020Params, rtp, rtpB string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-s:v", fmt.Sprintf("%dx%d", p.Width, p.Height),
		"-r", p.FFmpegRateArg(),
		"-i", "pipe:0",
		"-an",
		"-vf", fmt.Sprintf("format=%s", p.PixFmt()),
		"-c:v", "rawvideo",
		"-pix_fmt", p.PixFmt(),
	}
	if strings.TrimSpace(rtpB) != "" {
		// single encode → dual path (2022-7 diversity)
		tee := ffmpegTeeRTPPayload(rtp, rtpB, 96)
		args = append(args, "-f", "tee", tee)
	} else {
		args = append(args, "-f", "rtp", "-payload_type", "96", rtp)
	}
	return args
}

// legacy helper used by older tests
func buildST211020FFmpegArgs(w, h, fps int, rtp, sampling string) []string {
	p := DefaultST211020Params(w, h, fps)
	if sampling != "" {
		p.Sampling = sampling
	}
	return buildST211020FFmpegArgsFromParams(p, rtp, "")
}

func buildST2110LabFFmpegArgs(w, h, fps int, rtp, payload, rtpB string) []string {
	args := ffmpegRawInputArgs(w, h, fps)
	payload = strings.ToLower(payload)
	if payload == "" {
		payload = "mpegts"
	}
	args = append(args,
		"-an",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", strconv.Itoa(fps),
	)
	if strings.TrimSpace(rtpB) != "" && payload == "rtp" {
		args = append(args, "-f", "tee", ffmpegTeeRTPPayload(rtp, rtpB, 96))
		return args
	}
	if payload == "rtp" {
		args = append(args, "-f", "rtp", rtp)
	} else {
		// mpegts dual: tee two rtp_mpegts is awkward; dual-process note — single path for mpegts
		args = append(args, "-f", "rtp_mpegts", rtp)
	}
	return args
}

// writeST211020SDP — thin wrapper for tests; uses tightened writer.
func writeST211020SDP(path, host string, port, w, h, fps int, sampling string, depth int) error {
	p := DefaultST211020Params(w, h, fps)
	if sampling != "" {
		p.Sampling = sampling
	}
	if depth >= 8 {
		p.Depth = depth
	}
	return WriteST211020SDPTight(path, host, port, p, DefaultSyncClockReport())
}

func writeST2110LabSDP(path, host string, port, w, h, fps int) error {
	now := time.Now().Unix()
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-lab PGM
i=Lab gateway profile (H.264 over RTP) — not ST 2110-20. Use --profile 2110-20 for raw essence.
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

func writeST2110SDP(path, host string, port, w, h, fps int) error {
	return writeST211020SDP(path, host, port, w, h, fps, "YCbCr-4:2:2", 8)
}

// writeST2110MultiEssenceSDPTight uses tightened 20 fmtp + 30 audio.
func writeST2110MultiEssenceSDPTight(path, vHost string, vPort int, aHost string, aPort int, vp ST211020Params, audio ST211030Opts, sync SyncClockReport) error {
	if audio.Rate < 1 {
		audio.Rate = 48000
	}
	if audio.Channels < 1 {
		audio.Channels = 2
	}
	if audio.Depth != 16 {
		audio.Depth = 24
	}
	if audio.ChannelOrder == "" {
		audio.ChannelOrder = "SMPTE2110.(ST)"
	}
	if audio.PtimeMs < 1 {
		audio.PtimeMs = 1
	}
	if audio.Level == "" {
		audio.Level = ST211030LevelA
	}
	enc := "L24"
	if audio.Depth == 16 {
		enc = "L16"
	}
	now := time.Now().Unix()
	vFmtp := vp.Fmtp()
	aFmtp := fmt.Sprintf("channel-order=%s", audio.ChannelOrder)
	tsRef := "localmac=00-00-00-00-00-00"
	if sync.PTP.Mode == PTPLocked || sync.PTP.Mode == PTPSlave {
		tsRef = fmt.Sprintf("ptp=IEEE1588-2008:traceable:domain-number=%d", sync.PTP.Domain)
	}
	fr := vp.ExactFramerate()
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110 multi-essence PGM
i=ST 2110-20 + ST 2110-30. System ST 2110-10 / PTP %s. TP=%s. Compliant=%v.
c=IN IP4 %s/32
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=group:FID v1 a1
a=x-gy-essences:2110-20,2110-30
a=x-gy-2110-21:TP=%s
a=x-gy-program-meta:st2110-program.json
a=ts-refclk:%s
a=mediaclk:direct=0
m=video %d RTP/AVP 96
a=mid:v1
a=rtpmap:96 raw/90000
a=fmtp:96 %s
a=framesize:96 %d-%d
a=framerate:%s
a=recvonly
m=audio %d RTP/AVP 97
a=mid:a1
a=rtpmap:97 %s/%d/%d
a=fmtp:97 %s
a=ptime:%.3f
a=recvonly
`, now, now, vHost, PTPProfileST2059, vp.TP, sync.Compliant, vHost, Version, vp.TP, tsRef,
		vPort, vFmtp, vp.Width, vp.Height, fr,
		aPort, enc, audio.Rate, audio.Channels, aFmtp, float64(audio.PtimeMs))
	return os.WriteFile(path, []byte(body), 0o644)
}

// keep old multi-essence for tests that call writeST2110MultiEssenceSDP
func writeST2110MultiEssenceSDP(path, vHost string, vPort int, aHost string, aPort, w, h, fps int, audio ST211030Opts, sync SyncClockReport) error {
	vp := DefaultST211020Params(w, h, fps)
	return writeST2110MultiEssenceSDPTight(path, vHost, vPort, aHost, aPort, vp, audio, sync)
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
