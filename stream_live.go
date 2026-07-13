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
	Src    string // file/url/sim/cam / .pcap|.gyst replay
	Hub    string // host:port
	Nick   string
	Kind   string // rgb24 | hexlum
	W, H   int
	HexN   int // hexlum side
	FPS    int
	Loop   bool
	Quiet  bool
	MaxSec int // 0 = until signal
}

// runStreamPubCmd: gy stream-pub [src] [flags]
func runStreamPubCmd(args []string) error {
	fs := newBridgeFlagSet("stream-pub")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy stream-pub — live headless GYST → hub (no file required)

  gy stream-pub [src] [flags]

  src:  video/url | sim | cam | stream.pcap|.gyst|.gyhex
  --hub host:port     default 127.0.0.1:9876
  --nick name         publisher nick
  --kind rgb24|hexlum default hexlum (DOJO aesthetic)
  --w --h             rgb capture size (default 80×48)
  --hex 25|13         hexlum grid (default 25)
  --fps 12
  --loop              loop file sources
  --max-sec N         stop after N seconds (0=forever)

Examples:
  gy serve
  gy stream-pub sim --kind hexlum --hex 25
  gy stream-pub clip.mp4 --kind rgb24 --w 96 --h 54 --fps 10 --loop
  gy stream-pub out.pcap --loop          # live replay of pcap packets
  # peers: gy  (renders incoming gyst frames)
`)
	}
	hub := fs.String("hub", "127.0.0.1:9876", "hub host:port")
	nick := fs.String("nick", "colossus", "publisher nick")
	kind := fs.String("kind", "hexlum", "rgb24|hexlum")
	w := fs.Int("w", 80, "rgb width")
	h := fs.Int("h", 48, "rgb height")
	hexN := fs.Int("hex", 25, "hexlum N×N")
	fps := fs.Int("fps", 12, "frames per second")
	loop := fs.Bool("loop", false, "loop file sources")
	quiet := fs.Bool("quiet", false, "less logging")
	maxSec := fs.Int("max-sec", 0, "stop after seconds (0=forever)")
	// Go flag stops at first non-flag — allow `src` before or after flags.
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
	return RunStreamPub(StreamPubOpts{
		Src: src, Hub: *hub, Nick: *nick, Kind: *kind,
		W: *w, H: *h, HexN: *hexN, FPS: *fps,
		Loop: *loop, Quiet: *quiet, MaxSec: *maxSec,
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
	client.OnStatus = func(s string) {
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "stream-pub · %s\n", s)
		}
	}
	go client.Run(ctx)
	// wait for connect
	time.Sleep(400 * time.Millisecond)

	if !opts.Quiet {
		fmt.Fprintf(os.Stderr, "stream-pub · src=%s kind=%s hub=%s nick=%s\n",
			opts.Src, opts.Kind, opts.Hub, opts.Nick)
	}

	// replay stream file
	if IsStreamCodecPath(opts.Src) || DetectStreamFile(opts.Src) != "unknown" {
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
			publishPacket(c, p, opts.Quiet)
			if !opts.Quiet && seq%uint32(opts.FPS) == 0 {
				fmt.Fprintf(os.Stderr, "stream-pub · seq=%d %s %dx%d\n", seq, p.KindName(), p.Width, p.Height)
			}
		}
	}
}

func publishStreamFile(ctx context.Context, c *MeshClient, opts StreamPubOpts) error {
	pkts, err := LoadStreamFile(expandPath(opts.Src))
	if err != nil {
		return err
	}
	if len(pkts) == 0 {
		return fmt.Errorf("no packets in %s", opts.Src)
	}
	delay := time.Second / time.Duration(opts.FPS)
	var seq uint32
	for {
		for _, p := range pkts {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			seq++
			p.Seq = seq
			p.TimeMS = uint64(time.Now().UnixMilli())
			// optional convert rgb→hexlum for DOJO
			if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") && p.Kind == KindRGB24 {
				lum := RGBToHexLum(p.Payload, int(p.Width), int(p.Height), opts.HexN)
				p = PacketFromHexLum(lum, opts.HexN, seq)
			}
			publishPacket(c, p, opts.Quiet)
			time.Sleep(delay)
		}
		if !opts.Loop {
			return nil
		}
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "stream-pub · loop %s\n", opts.Src)
		}
	}
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
		publishPacket(c, p, opts.Quiet)
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
		publishPacket(c, p, opts.Quiet)
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
