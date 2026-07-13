package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Live GYST mesh type — same envelope family as vburst, packet payload = GYST kind.
const MeshTypeGYST = "gyst"

// MeshGystMsg is the hub JSON for live binary/hex stream packets.
type MeshGystMsg struct {
	Type string `json:"type"` // "gyst"
	From string `json:"from"`
	Kind string `json:"kind"` // rgb24|hexlum|jpeg|pcm16
	W    int    `json:"w"`
	H    int    `json:"h"`
	Seq  uint32 `json:"seq"`
	T    int64  `json:"t"`
	B64  string `json:"b64"` // payload base64
	// optional raw hexlum ints for tiny grids (alternative to b64)
	Data []int `json:"data,omitempty"`
}

// PacketToMesh maps StreamPacket → mesh JSON object.
func PacketToMesh(p StreamPacket, from string) map[string]any {
	msg := map[string]any{
		"type": MeshTypeGYST,
		"from": from,
		"kind": p.KindName(),
		"w":    p.Width,
		"h":    p.Height,
		"seq":  p.Seq,
		"t":    int64(p.TimeMS),
		"b64":  base64.StdEncoding.EncodeToString(p.Payload),
	}
	// hexlum small grids: also emit data[] for Glyph/bridge consumers
	if p.Kind == KindHexLum && len(p.Payload) <= 49*49 {
		data := make([]int, len(p.Payload))
		for i, b := range p.Payload {
			data[i] = int(b)
		}
		msg["data"] = data
		msg["glyphN"] = int(p.Width)
		if p.Width == 0 {
			msg["glyphN"] = int(p.Height)
		}
	}
	return msg
}

// MeshToPacket parses a gyst hub message into StreamPacket.
func MeshToPacket(msg map[string]any) (*StreamPacket, error) {
	if t, _ := msg["type"].(string); t != MeshTypeGYST && t != "gyst-frame" {
		return nil, fmt.Errorf("not gyst")
	}
	kindName, _ := msg["kind"].(string)
	var kind uint8
	if kindName != "" {
		kind = kindFromName(kindName)
	} else if k, ok := msg["kind"].(float64); ok {
		kind = uint8(k)
	}
	if kind == 0 || (kindName == "" && kind == KindMeta) {
		kind = KindRGB24
	}
	w, h := 0, 0
	if v, ok := msg["w"].(float64); ok {
		w = int(v)
	}
	if v, ok := msg["h"].(float64); ok {
		h = int(v)
	}
	var seq uint32
	if v, ok := msg["seq"].(float64); ok {
		seq = uint32(v)
	}
	var tms uint64
	if v, ok := msg["t"].(float64); ok {
		tms = uint64(v)
	}
	var payload []byte
	if b64, ok := msg["b64"].(string); ok && b64 != "" {
		var err error
		payload, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, err
		}
	} else if arr, ok := msg["data"].([]any); ok && len(arr) > 0 {
		payload = make([]byte, len(arr))
		for i, x := range arr {
			if f, ok := x.(float64); ok {
				payload[i] = byte(int(f) & 0xff)
			}
		}
		if w < 1 {
			// square
			n := 1
			for n*n < len(payload) {
				n++
			}
			w, h = n, n
		}
		if kind == KindRGB24 {
			kind = KindHexLum
		}
	} else {
		return nil, fmt.Errorf("gyst missing b64/data")
	}
	return &StreamPacket{
		Kind: kind, Width: uint32(w), Height: uint32(h),
		Seq: seq, TimeMS: tms, Payload: payload,
	}, nil
}

// RGBToHexLum downsamples RGB24 → n×n luminance (terminal/Glyph lane).
func RGBToHexLum(rgb []byte, w, h, n int) []byte {
	if n < 4 {
		n = 25
	}
	if w < 1 || h < 1 || len(rgb) < w*h*3 {
		return make([]byte, n*n)
	}
	out := make([]byte, n*n)
	for y := 0; y < n; y++ {
		sy0 := y * h / n
		sy1 := (y + 1) * h / n
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < n; x++ {
			sx0 := x * w / n
			sx1 := (x + 1) * w / n
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var sum, cnt int
			for sy := sy0; sy < sy1 && sy < h; sy++ {
				for sx := sx0; sx < sx1 && sx < w; sx++ {
					i := (sy*w + sx) * 3
					r, g, b := int(rgb[i]), int(rgb[i+1]), int(rgb[i+2])
					sum += (r*299 + g*587 + b*114) / 1000
					cnt++
				}
			}
			if cnt < 1 {
				cnt = 1
			}
			out[y*n+x] = byte(sum / cnt)
		}
	}
	return out
}

// StreamPubOpts headless live publisher.
type StreamPubOpts struct {
	Src      string // file/url/sim/cam/stdin|- / .pcap|.gyst|.gyhex replay
	Hub      string // host:port
	Nick     string
	Room     string // mesh room (default GY_ROOM / global)
	Kind     string // auto | rgb24 | hexlum
	W, H     int
	HexN     int  // hexlum side
	FPS      int  // fallback pace when packet timestamps missing/equal
	Loop     bool // default true for stream codec files (pcap/gyst)
	Quiet    bool
	MaxSec   int  // 0 = until signal
	Pace     string // auto | fps | ts  (ts = use packet TimeMS deltas)
	Colossus bool // preset: pcap loop + hexlum aesthetic when possible
	// DualPub also emits vburst-frame glyph lattice for burst/Glyph consumers (hexlum only).
	DualPub bool
}

// runStreamPubCmd: gy stream-pub [src] [flags]
// Aliases colossus / stream-live use DOJO pcap-loop defaults.
func runStreamPubCmd(args []string) error {
	// detect alias for presets (caller passes only args after command)
	colossus := false
	// when invoked as `gy colossus`, main still routes here; optional flag
	fs := newBridgeFlagSet("stream-pub")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy stream-pub — live headless GYST/hexlum → hub (no file required)

  gy stream-pub [src] [flags]
  gy colossus file.pcap [flags]   # DOJO pcap loop preset

  src:  video/url | sim | cam | stdin|- | stream.pcap|.gyst|.gyhex
  --hub host:port     default 127.0.0.1:9876
  --nick name         publisher nick (default colossus)
  --room id           mesh room (default GY_ROOM / global)
  --kind auto|rgb24|hexlum   auto = keep packet kind from pcap
  --w --h             rgb capture size (default 80×48)
  --hex 25|13         hexlum grid when converting (default 25)
  --fps 12            pace when timestamps missing
  --pace auto|ts|fps  auto: use pcap TimeMS deltas when present
  --loop / --no-loop  stream files default to loop
  --max-sec N         stop after N seconds (0=forever)
  --dual              also emit vburst glyph for burst/Glyph peers (hexlum)
  --colossus          force pcap-loop preset

Examples:
  gy serve
  gy stream-pub sim --kind hexlum --hex 25 --room dojo
  gy stream-pub cam --kind hexlum --dual
  gy stream-pub - --w 80 --h 48 --kind hexlum   # raw RGB24 stdin
  gy colossus capture.pcap --hub 127.0.0.1:9876
  gy stream-pub clip.mp4 --kind rgb24 --w 96 --h 54 --loop
  # peers: gy join 127.0.0.1:9876  (renders type:gyst)
`)
	}
	hub := fs.String("hub", "127.0.0.1:9876", "hub host:port")
	nick := fs.String("nick", "colossus", "publisher nick")
	room := fs.String("room", "", "mesh room (default GY_ROOM / global)")
	kind := fs.String("kind", "auto", "auto|rgb24|hexlum")
	w := fs.Int("w", 80, "rgb width")
	h := fs.Int("h", 48, "rgb height")
	hexN := fs.Int("hex", 25, "hexlum N×N")
	fps := fs.Int("fps", 12, "fallback frames per second")
	pace := fs.String("pace", "auto", "auto|ts|fps")
	loop := fs.Bool("loop", false, "loop file sources (default on for pcap/gyst)")
	noLoop := fs.Bool("no-loop", false, "disable loop even for stream files")
	col := fs.Bool("colossus", false, "DOJO pcap loop preset")
	dual := fs.Bool("dual", false, "dual-publish hexlum as vburst glyph for burst peers")
	quiet := fs.Bool("quiet", false, "less logging")
	maxSec := fs.Int("max-sec", 0, "stop after seconds (0=forever)")
	src, flagArgs := splitSrcAndFlags(args)
	if err := fs.Parse(flagArgs); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if src == "" {
		if rest := fs.Args(); len(rest) > 0 {
			src = rest[0]
		} else {
			src = "sim"
		}
	}
	colossus = *col || colossus
	path := expandPath(src)
	isStream := IsStreamCodecPath(path) || DetectStreamFile(path) != "unknown"
	doLoop := *loop
	if isStream && !*noLoop {
		// Colossus/DOJO: pcap loops by default
		doLoop = true
	}
	if *noLoop {
		doLoop = false
	}
	k := *kind
	if colossus {
		if k == "auto" {
			k = "auto" // keep packet kinds from capture
		}
		if *nick == "colossus" {
			// keep
		}
		doLoop = !*noLoop
	}
	// sim default kind hexlum when auto
	if (src == "sim" || src == "test" || src == "") && k == "auto" {
		k = "hexlum"
	}
	return RunStreamPub(StreamPubOpts{
		Src: src, Hub: *hub, Nick: *nick, Room: *room, Kind: k,
		W: *w, H: *h, HexN: *hexN, FPS: *fps,
		Loop: doLoop, Quiet: *quiet, MaxSec: *maxSec,
		Pace: *pace, Colossus: colossus || isStream, DualPub: *dual,
	})
}

// RunStreamPub publishes live packets to the hub until signal/max-sec.
func RunStreamPub(opts StreamPubOpts) error {
	if opts.FPS < 1 {
		opts.FPS = 12
	}
	if opts.W < 8 {
		opts.W = 80
	}
	if opts.H < 4 {
		opts.H = 48
	}
	if opts.HexN < 5 {
		opts.HexN = 25
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if opts.MaxSec > 0 {
		var c2 context.CancelFunc
		ctx, c2 = context.WithTimeout(ctx, time.Duration(opts.MaxSec)*time.Second)
		defer c2()
	}

	client := NewMeshClient(opts.Hub, opts.Nick)
	if opts.Room != "" {
		client.Room = NormalizeMeshRoom(opts.Room)
	}
	// headless publisher identity for lattice/cap consumers
	client.Role = "publisher"
	if client.Cap.Role == "" || client.Cap.Role == "term" {
		client.Cap.Role = "publisher"
	}
	client.OnStatus = func(s string) {
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "stream-pub · %s\n", s)
		}
	}
	go client.Run(ctx)
	// wait for connect
	time.Sleep(400 * time.Millisecond)

	path := expandPath(opts.Src)
	isStream := opts.Src != "-" && opts.Src != "stdin" &&
		(IsStreamCodecPath(path) || DetectStreamFile(path) != "unknown")
	if isStream && !opts.Loop && opts.Colossus {
		opts.Loop = true // safety
	}

	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · src=%s kind=%s hub=%s room=%s nick=%s loop=%v pace=%s dual=%v\n",
			opts.Src, opts.Kind, opts.Hub, client.Room, opts.Nick, opts.Loop, opts.Pace, opts.DualPub)
	}

	// raw RGB24 stdin — external headless producer (no file)
	if opts.Src == "-" || opts.Src == "stdin" {
		return publishStdinRGB(ctx, client, opts)
	}

	// replay stream file (pcap / gyst / gyhex) — Colossus/DOJO loop
	if isStream {
		return publishStreamFile(ctx, client, opts)
	}

	// sim
	if opts.Src == "" || opts.Src == "sim" || opts.Src == "test" {
		return publishSim(ctx, client, opts)
	}

	// live ffmpeg
	return publishFFmpeg(ctx, client, opts)
}

func publishPacket(c *MeshClient, p StreamPacket, quiet bool) {
	msg := PacketToMesh(p, c.Nick)
	if err := c.SendJSON(msg); err != nil && !quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · send: %v\n", err)
	}
}

// publishPacketDual sends formal gyst and optionally a vburst glyph lattice for burst UIs.
func publishPacketDual(c *MeshClient, p StreamPacket, opts StreamPubOpts) {
	publishPacket(c, p, opts.Quiet)
	if !opts.DualPub || p.Kind != KindHexLum || len(p.Payload) == 0 {
		return
	}
	n := int(p.Width)
	if n < 1 {
		n = opts.HexN
	}
	glyph := make([]int, len(p.Payload))
	for i, b := range p.Payload {
		glyph[i] = int(b)
	}
	// tiny JPEG-less burst frame: empty b64 + glyph for lattice consumers
	msg := map[string]any{
		"type":   string(BurstFrame),
		"from":   c.Nick,
		"fmt":    "hexlum",
		"glyph":  glyph,
		"glyphN": n,
		"w":      n,
		"h":      n,
		"t":      time.Now().UnixMilli(),
		"via":    "stream-pub-dual",
	}
	if err := c.SendJSON(msg); err != nil && !opts.Quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · dual: %v\n", err)
	}
}

// publishStdinRGB reads fixed W×H×3 RGB24 frames from stdin and fans out gyst/hexlum.
// External producers: `ffmpeg … -f rawvideo - | gy stream-pub - --w 80 --h 48 --kind hexlum`
func publishStdinRGB(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	w, h := opts.W, opts.H
	if h%2 != 0 {
		h++
	}
	frameSize := w * h * 3
	if frameSize < 1 {
		return fmt.Errorf("stdin: invalid frame size")
	}
	buf := make([]byte, frameSize)
	var seq uint32
	// optional FPS throttle (0 = as-fast-as-pipe)
	var tick <-chan time.Time
	if opts.FPS > 0 {
		t := time.NewTicker(time.Second / time.Duration(opts.FPS))
		defer t.Stop()
		tick = t.C
	}
	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · stdin RGB24 %dx%d kind=%s (Ctrl-C stop)\n", w, h, opts.Kind)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if _, err := io.ReadFull(os.Stdin, buf); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if !opts.Quiet {
					fmt.Fprintf(os.Stderr, "stream-pub · stdin EOF · seq=%d\n", seq)
				}
				return nil
			}
			return err
		}
		if tick != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-tick:
			}
		}
		seq++
		cp := make([]byte, frameSize)
		copy(cp, buf)
		var p StreamPacket
		if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") {
			lum := RGBToHexLum(cp, w, h, opts.HexN)
			p = PacketFromHexLum(lum, opts.HexN, seq)
		} else {
			p = PacketFromRGB(cp, w, h, seq, uint64(time.Now().UnixMilli()))
		}
		publishPacketDual(c, p, opts)
		if !opts.Quiet && seq%uint32(max(1, opts.FPS)) == 0 {
			fmt.Fprintf(os.Stderr, "stream-pub · stdin seq=%d %s\n", seq, p.KindName())
		}
	}
}

func publishSim(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	tick := time.NewTicker(time.Second / time.Duration(opts.FPS))
	defer tick.Stop()
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-tick.C:
			seq++
			fp := genSimFrame(opts.W, opts.H, float64(t.UnixMilli()), int(seq))
			var p StreamPacket
			if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") {
				lum := RGBToHexLum(fp.RGB, fp.W, fp.H, opts.HexN)
				p = PacketFromHexLum(lum, opts.HexN, seq)
			} else {
				p = PacketFromFramePixels(fp, seq)
			}
			publishPacketDual(c, p, opts)
			if !opts.Quiet && seq%uint32(opts.FPS) == 0 {
				fmt.Fprintf(os.Stderr, "stream-pub · seq=%d %s %dx%d\n", seq, p.KindName(), p.Width, p.Height)
			}
		}
	}
}

func publishStreamFile(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	path := expandPath(opts.Src)
	pkts, err := LoadStreamFile(path)
	if err != nil {
		return err
	}
	if len(pkts) == 0 {
		return fmt.Errorf("no packets in %s", opts.Src)
	}
	// video-only packets for pacing (skip pure pcm for frame clock)
	video := make([]StreamPacket, 0, len(pkts))
	for _, p := range pkts {
		if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
			video = append(video, p)
		}
	}
	if len(video) == 0 {
		video = pkts // meta/pcm-only still publish
	}

	useTS := opts.Pace == "ts" || (opts.Pace == "auto" || opts.Pace == "")
	if useTS && !packetTimelineUseful(video) {
		useTS = false
	}
	fpsDelay := time.Second / time.Duration(opts.FPS)
	if fpsDelay < time.Millisecond {
		fpsDelay = time.Millisecond
	}

	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · loaded %d packets (%d video) from %s · loop=%v pace=%s\n",
			len(pkts), len(video), path, opts.Loop, map[bool]string{true: "ts", false: "fps"}[useTS])
	}

	var seq uint32
	var loops int
	for {
		var prevTS uint64
		for i, p := range video {
			select {
			case <-ctx.Done():
				if !opts.Quiet {
					fmt.Fprintf(os.Stderr, "stream-pub · stopped after %d loops · seq=%d\n", loops, seq)
				}
				return nil
			default:
			}
			// pace
			if useTS && i > 0 && p.TimeMS > prevTS {
				d := time.Duration(p.TimeMS-prevTS) * time.Millisecond
				if d > 2*time.Second {
					d = 2 * time.Second // clamp gaps
				}
				if d > 0 {
					timer := time.NewTimer(d)
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil
					case <-timer.C:
					}
				}
			} else if i > 0 || loops > 0 {
				timer := time.NewTimer(fpsDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
			if p.TimeMS > 0 {
				prevTS = p.TimeMS
			}

			seq++
			out := transformPubPacket(p, opts, seq)
			publishPacketDual(c, out, opts)
			if !opts.Quiet && seq%uint32(max(1, opts.FPS)) == 0 {
				fmt.Fprintf(os.Stderr, "stream-pub · seq=%d %s %dx%d loop=%d\n",
					seq, out.KindName(), out.Width, out.Height, loops)
			}
		}
		loops++
		if !opts.Loop {
			if !opts.Quiet {
				fmt.Fprintf(os.Stderr, "stream-pub · finished %d packets (no loop)\n", seq)
			}
			return nil
		}
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "stream-pub · loop %d · %s\n", loops, path)
		}
	}
}

// transformPubPacket applies kind conversion (rgb→hexlum) and seq/time stamps.
func transformPubPacket(p StreamPacket, opts StreamPubOpts, seq uint32) StreamPacket {
	p.Seq = seq
	p.TimeMS = uint64(time.Now().UnixMilli())
	k := strings.ToLower(opts.Kind)
	if k == "auto" || k == "" {
		return p
	}
	if strings.HasPrefix(k, "hex") && p.Kind == KindRGB24 {
		lum := RGBToHexLum(p.Payload, int(p.Width), int(p.Height), opts.HexN)
		return PacketFromHexLum(lum, opts.HexN, seq)
	}
	if strings.HasPrefix(k, "hex") && p.Kind == KindJPEG {
		// decode then hexlum if possible
		if fp, err := FrameFromPacket(&p); err == nil && fp != nil {
			lum := RGBToHexLum(fp.RGB, fp.W, fp.H, opts.HexN)
			return PacketFromHexLum(lum, opts.HexN, seq)
		}
	}
	return p
}

func packetTimelineUseful(pkts []StreamPacket) bool {
	if len(pkts) < 2 {
		return false
	}
	var prev uint64
	gaps := 0
	for i, p := range pkts {
		if p.TimeMS == 0 {
			continue
		}
		if i > 0 && prev > 0 && p.TimeMS > prev {
			d := p.TimeMS - prev
			if d >= 5 && d <= 2000 {
				gaps++
			}
		}
		prev = p.TimeMS
	}
	return gaps >= 1
}

func publishFFmpeg(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	src := expandPath(opts.Src)
	r, err := ResolveMediaTimeout(src, 90*time.Second)
	if err != nil {
		// cam?
		if src == "cam" || src == "camera" {
			return publishCam(ctx, c, opts)
		}
		return err
	}
	in := r.Video
	w, h := opts.W, opts.H
	if h%2 != 0 {
		h++
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-re",
	}
	if opts.Loop {
		args = append(args, "-stream_loop", "-1")
	}
	args = append(args,
		"-i", in,
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic,fps=%d,format=rgb24", w, h, opts.FPS),
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"pipe:1",
	)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	frameSize := w * h * 3
	buf := make([]byte, frameSize)
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		if _, err := io.ReadFull(stdout, buf); err != nil {
			if opts.Loop && err != io.EOF {
				return err
			}
			if !opts.Loop {
				return nil
			}
			// restart handled by stream_loop
			if err == io.EOF {
				return nil
			}
			return err
		}
		seq++
		cp := make([]byte, frameSize)
		copy(cp, buf)
		var p StreamPacket
		if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") {
			lum := RGBToHexLum(cp, w, h, opts.HexN)
			p = PacketFromHexLum(lum, opts.HexN, seq)
		} else {
			p = PacketFromRGB(cp, w, h, seq, uint64(time.Now().UnixMilli()))
		}
		publishPacketDual(c, p, opts)
		if !opts.Quiet && seq%uint32(opts.FPS) == 0 {
			fmt.Fprintf(os.Stderr, "stream-pub · seq=%d %s\n", seq, p.KindName())
		}
	}
}

func publishCam(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	// single-frame snap loop via existing burst cam path is heavy; use ffmpeg avfoundation/v4l2
	w, h := opts.W, opts.H
	if h%2 != 0 {
		h++
	}
	var args []string
	if isDarwin() {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-f", "avfoundation", "-framerate", fmt.Sprintf("%d", opts.FPS),
			"-video_size", "640x480", "-i", "0:none",
			"-an",
			"-vf", fmt.Sprintf("scale=%d:%d:flags=bicubic,format=rgb24", w, h),
			"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1",
		}
	} else {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-f", "v4l2", "-i", "/dev/video0",
			"-an",
			"-vf", fmt.Sprintf("scale=%d:%d:fps=%d,format=rgb24", w, h, opts.FPS),
			"-f", "rawvideo", "-pix_fmt", "rgb24", "pipe:1",
		}
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()
	frameSize := w * h * 3
	buf := make([]byte, frameSize)
	var seq uint32
	for {
		if _, err := io.ReadFull(stdout, buf); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		seq++
		cp := make([]byte, frameSize)
		copy(cp, buf)
		var p StreamPacket
		if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") {
			lum := RGBToHexLum(cp, w, h, opts.HexN)
			p = PacketFromHexLum(lum, opts.HexN, seq)
		} else {
			p = PacketFromRGB(cp, w, h, seq, uint64(time.Now().UnixMilli()))
		}
		publishPacketDual(c, p, opts)
	}
}

func isDarwin() bool {
	return runtime.GOOS == "darwin"
}

// EncodeGystB64 encodes one packet as base64 of full GYST blob (optional transport).
func EncodeGystB64(p StreamPacket) (string, error) {
	var buf bytes.Buffer
	if err := EncodeBinary(&buf, p); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// DecodeGystB64 reverse of EncodeGystB64.
func DecodeGystB64(s string) (*StreamPacket, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	return DecodeBinary(bytes.NewReader(raw))
}

// splitSrcAndFlags pulls a positional source path out so flags can follow it.
func splitSrcAndFlags(args []string) (src string, flags []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// boolean flags take no value
			name := strings.TrimLeft(a, "-")
			if name == "loop" || name == "quiet" || name == "help" || name == "h" {
				continue
			}
			// --flag=value form
			if strings.Contains(a, "=") {
				continue
			}
			// take next as value if present
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		if src == "" {
			src = a
		} else {
			// extra positionals → keep as flags leftovers for Parse
			flags = append(flags, a)
		}
	}
	return src, flags
}

// ensure json used
var _ = json.Marshal
