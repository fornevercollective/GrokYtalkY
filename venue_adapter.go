package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// Venue adapter stub — consumes hub type:program + glyph/hexlum for venue sinks.
// Lattice identity is pass-through. NDI / ST 2110 / LED walls implement VenueSink later.
//
//	gy venue --hub ws://127.0.0.1:9876/
//	gy venue --dry-run --json   # stdout frames when on-air

// VenueSink is the pluggable egress for venue / Sphere / soundstage devices.
// Implement NDI, ST 2110, Spout, or LED drivers against this contract only.
type VenueSink interface {
	Name() string
	// OnProgram updates on-air selection (mode, mark, slot). Does not carry pixels.
	OnProgram(bus ProgramBus)
	// OnGlyph delivers lattice/grid when on-air (pass-through bytes).
	OnGlyph(frame VenueGlyphFrame)
	// OnBlack safe slate / black (mode=black).
	OnBlack(bus ProgramBus)
	// OnHold freeze last frame (mode=hold) — sinks should stop advancing.
	OnHold(bus ProgramBus)
	Close() error
}

// VenueGlyphFrame is one on-air glyph/hexlum delivery (lattice untouched).
type VenueGlyphFrame struct {
	From   string
	N      int
	Data   []byte // raw lum; not re-stamped
	Mark   string // program mark if known
	Slot   int
	Seq    uint32
	T      int64
	OnAir  bool
	Mode   string
	Source string // forge|gyst|burst|…
}

// VenueOpts configures the venue adapter process.
type VenueOpts struct {
	HubWS   string
	Nick    string
	Quiet   bool
	DryRun  bool // force log-only
	JSONOut bool // also emit JSON lines on stdout (like agent)
	// SinkKind: log | ndi | st2110 | comma-list (e.g. "ndi,st2110,log")
	SinkKind string
	// NDI
	NDIName     string
	NDIFallback string // udp/mpegts when libndi missing
	// ST 2110
	RTP     string
	SDPPath string
	// Shared raster
	Width  int
	Height int
	FPS    int
	// Sink optional; nil → built from SinkKind
	Sink VenueSink
}

// LogVenueSink is the default stub — logs program cuts and frame stats.
// Replace with NDIVenueSink / ST2110VenueSink when wiring real egress.
type LogVenueSink struct {
	Quiet   bool
	JSONOut bool
	mu      sync.Mutex
	lastBus ProgramBus
	frames  uint64
	lastN   int
	held    bool
}

func (s *LogVenueSink) Name() string { return "log-stub" }

func (s *LogVenueSink) OnProgram(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.held = bus.Mode == ProgramModeHold
	s.mu.Unlock()
	if s.Quiet {
		return
	}
	log.Printf("venue · PGM %s", FormatProgramLine(bus))
	if s.JSONOut {
		emitVenueJSON(map[string]any{
			"type": "program", "mode": bus.Mode, "seq": bus.Seq,
			"mark": bus.Program.Mark, "slot": bus.Program.Slot,
			"source": bus.Program.Source, "conductor": bus.Conductor,
			"venue": bus.VenueAdapterHint(),
		})
	}
}

func (s *LogVenueSink) OnGlyph(frame VenueGlyphFrame) {
	s.mu.Lock()
	s.frames++
	s.lastN = frame.N
	n := s.frames
	s.mu.Unlock()
	if s.Quiet && !s.JSONOut {
		return
	}
	if !s.Quiet && (n == 1 || n%36 == 0) {
		log.Printf("venue · glyph n=%d len=%d mark=%s from=%s frames=%d",
			frame.N, len(frame.Data), ShortMarkID(frame.Mark), frame.From, n)
	}
	if s.JSONOut {
		emitVenueJSON(map[string]any{
			"type": "glyph", "n": frame.N, "len": len(frame.Data),
			"mark": frame.Mark, "slot": frame.Slot, "from": frame.From,
			"on_air": frame.OnAir, "mode": frame.Mode, "source": frame.Source,
			// lattice pass-through as int array (same as SFU/agent)
			"data": bytesToJSONNums(frame.Data),
			"t":    frame.T,
		})
	}
}

func (s *LogVenueSink) OnBlack(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.held = false
	s.mu.Unlock()
	if !s.Quiet {
		log.Printf("venue · BLACK · safe slate (seq=%d)", bus.Seq)
	}
	if s.JSONOut {
		emitVenueJSON(map[string]any{"type": "black", "seq": bus.Seq, "mode": bus.Mode})
	}
}

func (s *LogVenueSink) OnHold(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.held = true
	s.mu.Unlock()
	if !s.Quiet {
		log.Printf("venue · HOLD · freeze last frame (seq=%d mark=%s)",
			bus.Seq, ShortMarkID(bus.Program.Mark))
	}
	if s.JSONOut {
		emitVenueJSON(map[string]any{
			"type": "hold", "seq": bus.Seq, "mark": bus.Program.Mark,
		})
	}
}

func (s *LogVenueSink) Close() error { return nil }

func emitVenueJSON(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Println(string(b))
}

// VenueRuntime joins hub as a venue sink and drives VenueSink from program bus.
type VenueRuntime struct {
	opts VenueOpts
	sink VenueSink
	mu   sync.Mutex
	bus  ProgramBus
	// last glyph kept for hold
	lastFrame *VenueGlyphFrame
}

// runVenueCmd is `gy venue`.
func runVenueCmd(args []string) error {
	fs := newBridgeFlagSet("venue")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy venue — venue adapter (program bus + glyph → NDI / ST 2110)

  gy venue [flags]

  --hub       hub WS (default ws://127.0.0.1:9876/)
  --nick      sink nick (default venue)
  --sink      log|ndi|st2110|comma-list  (default log)
  --ndi-name  NDI source name (default GrokYtalkY-PGM)
  --ndi-udp   fallback MPEG-TS UDP if libndi_newtek missing
  --rtp       ST 2110 lab RTP URL (default rtp://239.100.1.10:5004)
  --sdp       path to write SDP (default $TMPDIR/gy-venue/gy-st2110.sdp)
  --width --height --fps   raster (default 1280x720@30)
  --json      also emit program/glyph JSON on stdout
  --quiet
  --dry-run   force log sink only

Follows type:program. On-air hexlum/glyph only. Lattice pass-through
(nearest-neighbor upscale for NDI/2110 — no re-stamp).

Example:
  gy serve
  # conductor: /forge … · /conductor claim · /take 1
  gy venue --sink ndi
  gy venue --sink st2110 --sdp /tmp/gy.sdp
  gy venue --sink ndi,st2110,log --json
`)
	}
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "DOJO hub WebSocket")
	nick := fs.String("nick", "venue", "venue sink nick")
	sinkKind := fs.String("sink", "log", "log|ndi|st2110|comma-list")
	ndiName := fs.String("ndi-name", "GrokYtalkY-PGM", "NDI source name")
	ndiUDP := fs.String("ndi-udp", "udp://127.0.0.1:13000?pkt_size=1316", "NDI fallback MPEG-TS")
	rtp := fs.String("rtp", "rtp://239.100.1.10:5004", "ST 2110 lab RTP URL")
	sdp := fs.String("sdp", "", "SDP output path")
	width := fs.Int("width", VenueDefaultW, "output width")
	height := fs.Int("height", VenueDefaultH, "output height")
	fps := fs.Int("fps", VenueDefaultFPS, "output fps")
	quiet := fs.Bool("quiet", false, "less logging")
	dry := fs.Bool("dry-run", false, "force log sink only")
	jsonOut := fs.Bool("json", false, "stdout JSON lines")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Getenv("GY_CAP") == "" {
		_ = os.Setenv("GY_CAP", CapClassBridge)
	}
	if os.Getenv("GY_ROLE") == "" {
		_ = os.Setenv("GY_ROLE", "bridge")
	}
	kind := *sinkKind
	if *dry {
		kind = "log"
	}
	return RunVenue(VenueOpts{
		HubWS:       ensureWSQuery(*hub, map[string]string{"role": "venue", "nick": *nick}),
		Nick:        *nick,
		Quiet:       *quiet,
		DryRun:      *dry,
		JSONOut:     *jsonOut,
		SinkKind:    kind,
		NDIName:     *ndiName,
		NDIFallback: *ndiUDP,
		RTP:         *rtp,
		SDPPath:     *sdp,
		Width:       *width,
		Height:      *height,
		FPS:         *fps,
	})
}

// RunVenue connects hub and drives the venue sink until cancelled.
func RunVenue(opts VenueOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" {
		return fmt.Errorf("hub required")
	}
	if opts.Nick == "" {
		opts.Nick = "venue-stub"
	}
	sink := opts.Sink
	if sink == nil {
		var err error
		sink, err = BuildVenueSink(opts)
		if err != nil {
			return err
		}
	}

	cap := DetectCapProfile(0, 0)
	applyCapClass(&cap, CapClassBridge)
	cap.Role = "venue"
	cap.Forge = true
	bp := cap.Backpressure
	if bp < 8 {
		bp = 32
	}

	if !opts.Quiet {
		log.Printf("venue · sink=%s · %s", sink.Name(), cap.SummaryLine())
		log.Printf("venue · hub=%s", opts.HubWS)
		log.Printf("venue · waiting for type:program + on-air glyph (lattice pass-through)")
	}

	conn, _, err := websocket.Dial(ctx, opts.HubWS, nil)
	if err != nil {
		return fmt.Errorf("hub dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	defer func() { _ = sink.Close() }()

	join := cap.JoinFields(opts.Nick, "venue")
	join["role"] = "venue"
	b, _ := json.Marshal(join)
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}

	rt := &VenueRuntime{opts: opts, sink: sink, bus: NewProgramBus()}

	inbox := make(chan []byte, bp)
	errCh := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			select {
			case inbox <- append([]byte(nil), data...):
			default:
				if !opts.Quiet {
					log.Printf("venue · drop (bp full)")
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errCh:
			if ctx.Err() != nil {
				return nil
			}
			return err
		case raw := <-inbox:
			rt.handleHubRaw(raw)
		}
	}
}

func (rt *VenueRuntime) handleHubRaw(raw []byte) {
	var msg map[string]any
	if json.Unmarshal(raw, &msg) != nil {
		return
	}
	typ, _ := msg["type"].(string)
	from, _ := msg["from"].(string)

	switch typ {
	case "program":
		bus, ok := ParseProgramBus(msg)
		if !ok {
			return
		}
		rt.mu.Lock()
		// ignore stale seq
		if bus.Seq > 0 && rt.bus.Seq > 0 && bus.Seq < rt.bus.Seq {
			rt.mu.Unlock()
			return
		}
		rt.bus = bus
		rt.mu.Unlock()

		switch bus.Mode {
		case ProgramModeBlack:
			rt.sink.OnBlack(bus)
		case ProgramModeHold:
			rt.sink.OnHold(bus)
			// re-assert last frame for sinks that need a refresh
			rt.mu.Lock()
			lf := rt.lastFrame
			rt.mu.Unlock()
			if lf != nil {
				f := *lf
				f.Mode = ProgramModeHold
				f.OnAir = true
				rt.sink.OnGlyph(f)
			}
		default:
			rt.sink.OnProgram(bus)
		}

	case MeshTypeGYST, "gyst-frame":
		rt.handleGyst(msg, from)
	case "vburst-frame":
		rt.handleVburst(msg, from)
	}
}

func (rt *VenueRuntime) handleGyst(msg map[string]any, from string) {
	kind, _ := msg["kind"].(string)
	// forge meta only updates context — program bus is authority for on-air
	if kind == "meta" {
		return
	}
	if kind != "hexlum" && kind != "hex" {
		return
	}
	data, n, err := gystHexlumBytes(msg)
	if err != nil || len(data) == 0 {
		return
	}
	n = inferGlyphN(n, len(data))
	mark := ""
	slot := 0
	if mk, ok := ParseForgeFromMesh(msg); ok {
		mark = mk.ID
		slot = mk.Slot
	}

	rt.mu.Lock()
	bus := rt.bus
	rt.mu.Unlock()

	// black: never deliver pixels
	if bus.Mode == ProgramModeBlack {
		return
	}
	// hold: only redeliver if we already had a frame (handled in OnHold)
	// live: require on-air match when program has nick/mark; if slate with no identity, pass all glyph
	onAir := venueOnAir(bus, from, mark, slot)
	if !onAir {
		return
	}
	// hold mode: don't advance with new frames
	if bus.Mode == ProgramModeHold {
		return
	}

	frame := VenueGlyphFrame{
		From:   from,
		N:      n,
		Data:   append([]byte(nil), data...), // pass-through copy
		Mark:   firstNonEmpty(mark, bus.Program.Mark),
		Slot:   slotOr(slot, bus.Program.Slot),
		T:      time.Now().UnixMilli(),
		OnAir:  true,
		Mode:   bus.Mode,
		Source: bus.Program.Source,
	}
	if v, ok := msg["seq"].(float64); ok {
		frame.Seq = uint32(v)
	}
	rt.mu.Lock()
	cp := frame
	rt.lastFrame = &cp
	rt.mu.Unlock()
	rt.sink.OnGlyph(frame)
}

func (rt *VenueRuntime) handleVburst(msg map[string]any, from string) {
	g, ok := msg["glyph"]
	if !ok || g == nil {
		return
	}
	data, err := glyphToBytes(g)
	if err != nil || len(data) == 0 {
		return
	}
	n := 25
	if gn, ok := msg["glyphN"].(float64); ok && gn > 0 {
		n = int(gn)
	}
	n = inferGlyphN(n, len(data))

	rt.mu.Lock()
	bus := rt.bus
	rt.mu.Unlock()
	if bus.Mode == ProgramModeBlack || bus.Mode == ProgramModeHold {
		return
	}
	if !venueOnAir(bus, from, "", 0) {
		// burst: match by nick when program nick set
		return
	}
	frame := VenueGlyphFrame{
		From: from, N: n, Data: append([]byte(nil), data...),
		Mark: bus.Program.Mark, Slot: bus.Program.Slot,
		T: time.Now().UnixMilli(), OnAir: true, Mode: bus.Mode,
		Source: ProgramSourceBurst,
	}
	rt.mu.Lock()
	cp := frame
	rt.lastFrame = &cp
	rt.mu.Unlock()
	rt.sink.OnGlyph(frame)
}

// venueOnAir decides if a frame should hit the sink.
// Empty program identity (slate) → accept all glyph when live (preview room).
// With mark/nick → require IsOnAir match. Lattice never filtered by content hash rewrite.
func venueOnAir(bus ProgramBus, from, mark string, slot int) bool {
	if bus.Mode == ProgramModeBlack {
		return false
	}
	p := bus.Program
	// no specific program yet — open feed (studio warm-up)
	if p.Mark == "" && p.Nick == "" && (p.Source == "" || p.Source == ProgramSourceSlate) {
		return bus.Mode == ProgramModeLive || bus.Mode == ""
	}
	if bus.IsOnAir(from, mark, slot) {
		return true
	}
	// if frame has no mark but nick matches program nick
	if p.Nick != "" && from == p.Nick && mark == "" {
		return true
	}
	return false
}

func slotOr(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// BuildVenueSink constructs log / NDI / ST 2110 sinks (comma-list OK).
func BuildVenueSink(opts VenueOpts) (VenueSink, error) {
	kind := strings.TrimSpace(opts.SinkKind)
	if kind == "" || opts.DryRun {
		kind = "log"
	}
	parts := strings.Split(kind, ",")
	var sinks []VenueSink
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		s, err := NewVenueSink(p, opts)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, s)
	}
	if len(sinks) == 0 {
		return &LogVenueSink{Quiet: opts.Quiet, JSONOut: opts.JSONOut}, nil
	}
	if len(sinks) == 1 {
		return sinks[0], nil
	}
	return &multiVenueSink{sinks: sinks}, nil
}

// NewVenueSink builds one named sink (log|ndi|st2110|2110).
func NewVenueSink(kind string, opts VenueOpts) (VenueSink, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "log", "stub", "dry":
		return &LogVenueSink{Quiet: opts.Quiet, JSONOut: opts.JSONOut}, nil
	case "ndi":
		return NewNDIVenueSink(NDIOpts{
			Name:        opts.NDIName,
			Width:       opts.Width,
			Height:      opts.Height,
			FPS:         opts.FPS,
			Quiet:       opts.Quiet,
			FallbackUDP: opts.NDIFallback,
		})
	case "st2110", "2110", "st-2110":
		return NewST2110VenueSink(ST2110Opts{
			RTP:     opts.RTP,
			SDPPath: opts.SDPPath,
			Width:   opts.Width,
			Height:  opts.Height,
			FPS:     opts.FPS,
			Quiet:   opts.Quiet,
		})
	case "spout":
		return nil, fmt.Errorf("spout sink not built (mac/win GPU IPC) — use ndi or st2110")
	default:
		return nil, fmt.Errorf("unknown venue sink %q (log|ndi|st2110)", kind)
	}
}
