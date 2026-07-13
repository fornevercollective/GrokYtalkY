package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// Thin Glyph/IoT agent — same hub semantics as term clients, no TUI.
// Receives gyst hexlum + forge-mark meta; emits JSON lines (lattice pass-through).
//
//	gy agent --hub ws://127.0.0.1:9876/ --nick wall-1
//	GY_CAP=glyph-iot gy agent
func runGlyphAgentCmd(args []string) error {
	fs := newBridgeFlagSet("agent")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy agent — thin Glyph/IoT client (no TUI)

  gy agent [flags]

  --hub    hub WS (default ws://127.0.0.1:9876/)
  --nick   device nick (default gy-agent)
  --quiet  less logging
  --raw    print only lattice JSON lines (no logs on stderr)

Env:
  GY_CAP=glyph-iot   (default for agent)
  GY_ROLE=agent

Speaks hub join+cap · gyst hexlum · forge-mark. Lattice bytes pass through.
Backpressure: drops frames when inbox full (cap.bp).
`)
	}
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "DOJO hub WebSocket")
	nick := fs.String("nick", "gy-agent", "device nick")
	quiet := fs.Bool("quiet", false, "less logging")
	raw := fs.Bool("raw", false, "stdout lattice only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// default agent profile unless user overrode
	if os.Getenv("GY_CAP") == "" {
		_ = os.Setenv("GY_CAP", CapClassGlyphIoT)
	}
	if os.Getenv("GY_ROLE") == "" {
		_ = os.Setenv("GY_ROLE", "agent")
	}
	return RunGlyphAgent(GlyphAgentOpts{
		HubWS:  ensureWSQuery(*hub, map[string]string{"role": "agent", "nick": *nick}),
		Nick:   *nick,
		Quiet:  *quiet,
		RawOut: *raw,
	})
}

// GlyphAgentOpts configures the thin agent.
type GlyphAgentOpts struct {
	HubWS  string
	Nick   string
	Quiet  bool
	RawOut bool
}

// GlyphAgentEvent is one stdout JSON line for IoT consumers.
type GlyphAgentEvent struct {
	Type   string         `json:"type"` // glyph | forge-mark | program | status
	From   string         `json:"from,omitempty"`
	N      int            `json:"n,omitempty"`
	Data   []int          `json:"data,omitempty"` // lattice pass-through
	Mark   string         `json:"mark,omitempty"`
	Slot   int            `json:"slot,omitempty"`
	Source string         `json:"source,omitempty"`
	Forge  string         `json:"forge,omitempty"`
	T      int64          `json:"t,omitempty"`
	Mode   string         `json:"mode,omitempty"` // program bus mode
	OnAir  bool           `json:"on_air,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// RunGlyphAgent connects to hub and streams glyph/forge events to stdout.
func RunGlyphAgent(opts GlyphAgentOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" {
		return fmt.Errorf("hub required")
	}
	if opts.Nick == "" {
		opts.Nick = "gy-agent"
	}
	cap := DetectCapProfile(0, 0)
	if cap.Class != CapClassGlyphIoT {
		// force thin lanes even if detect differed
		applyCapClass(&cap, CapClassGlyphIoT)
	}
	bp := cap.Backpressure
	if bp < 1 {
		bp = 4
	}

	if !opts.Quiet && !opts.RawOut {
		log.Printf("agent · %s · %s", opts.Nick, cap.SummaryLine())
		log.Printf("agent · hub=%s", opts.HubWS)
	}

	conn, _, err := websocket.Dial(ctx, opts.HubWS, nil)
	if err != nil {
		return fmt.Errorf("hub dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// join with capability handshake
	join := cap.JoinFields(opts.Nick, "agent")
	b, _ := json.Marshal(join)
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	emitAgent(opts, GlyphAgentEvent{Type: "status", Meta: map[string]any{
		"event": "joined", "cap": cap.MeshMap(), "nick": opts.Nick,
	}})

	// backpressure queue
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
				// drop — backpressure sacred over blocking IoT pipe
				if !opts.Quiet && !opts.RawOut {
					log.Printf("agent · drop (bp=%d full)", bp)
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
			evs := MapHubMsgToAgentEvents(raw)
			for _, ev := range evs {
				emitAgent(opts, ev)
			}
		}
	}
}

// MapHubMsgToAgentEvents converts one hub frame to agent events (testable).
// Hexlum lattice bytes are not re-stamped.
func MapHubMsgToAgentEvents(raw []byte) []GlyphAgentEvent {
	var msg map[string]any
	if json.Unmarshal(raw, &msg) != nil {
		return nil
	}
	typ, _ := msg["type"].(string)
	from, _ := msg["from"].(string)
	var out []GlyphAgentEvent

	switch typ {
	case MeshTypeGYST, "gyst-frame":
		kind, _ := msg["kind"].(string)
		// forge meta
		if mark, ok := ParseForgeFromMesh(msg); ok && (kind == "meta" || kind == "") {
			b64, _ := msg["b64"].(string)
			if b64 == "" || kind == "meta" {
				out = append(out, GlyphAgentEvent{
					Type:   "forge-mark",
					From:   from,
					Mark:   mark.ID,
					Slot:   mark.Slot,
					Source: mark.Source,
					Forge:  mark.Forge,
					T:      mark.T,
				})
				return out
			}
		}
		if kind == "hexlum" || kind == "hex" {
			data, n, err := gystHexlumBytes(msg)
			if err != nil || len(data) == 0 {
				return out
			}
			n = inferGlyphN(n, len(data))
			// pass-through lattice
			nums := bytesToJSONNums(data)
			ev := GlyphAgentEvent{
				Type: "glyph",
				From: from,
				N:    n,
				Data: nums,
				T:    time.Now().UnixMilli(),
			}
			if mark, ok := ParseForgeFromMesh(msg); ok {
				ev.Mark = mark.ID
				ev.Slot = mark.Slot
				ev.Source = mark.Source
				ev.Forge = mark.Forge
			}
			out = append(out, ev)
		}
	case "vburst-frame":
		// optional glyph grid on burst
		if g, ok := msg["glyph"]; ok && g != nil {
			data, err := glyphToBytes(g)
			if err == nil && len(data) > 0 {
				n := 25
				if gn, ok := msg["glyphN"].(float64); ok && gn > 0 {
					n = int(gn)
				}
				n = inferGlyphN(n, len(data))
				out = append(out, GlyphAgentEvent{
					Type: "glyph",
					From: from,
					N:    n,
					Data: bytesToJSONNums(data),
					T:    time.Now().UnixMilli(),
				})
			}
		}
	case "program":
		if bus, ok := ParseProgramBus(msg); ok {
			eff := bus.EffectiveCaption()
			meta := map[string]any{
				"conductor": bus.Conductor,
				"seq":       bus.Seq,
				"program":   bus.Program,
				"preview":   bus.Preview,
				"venue":     bus.VenueAdapterHint(),
			}
			if !eff.IsEmpty() {
				meta["caption"] = eff.Text
				meta["caption_meta"] = eff
			}
			out = append(out, GlyphAgentEvent{
				Type:   "program",
				From:   from,
				Mode:   bus.Mode,
				Mark:   bus.Program.Mark,
				Slot:   bus.Program.Slot,
				Source: bus.Program.Source,
				OnAir:  bus.Mode == ProgramModeLive || bus.Mode == ProgramModeHold,
				T:      bus.T,
				Meta:   meta,
			})
			if !eff.IsEmpty() {
				out = append(out, GlyphAgentEvent{
					Type: "caption",
					From: from,
					T:    bus.T,
					Meta: map[string]any{
						"text": eff.Text, "lang": eff.Lang, "role": eff.Role,
						"speaker": eff.Speaker, "source": eff.Source,
						"display": eff.Display(), "seq": bus.Seq,
					},
				})
			}
		}
	case "caption":
		if cap, ok := ParseCaptionFromMesh(msg); ok && !cap.IsEmpty() {
			out = append(out, GlyphAgentEvent{
				Type: "caption",
				From: from,
				T:    time.Now().UnixMilli(),
				Meta: map[string]any{
					"text": cap.Text, "lang": cap.Lang, "role": cap.Role,
					"speaker": cap.Speaker, "source": cap.Source,
					"display": cap.Display(), "soft": true,
				},
			})
		}
	case "cap", "join":
		if cap, ok := ParseCapFromMesh(msg); ok {
			out = append(out, GlyphAgentEvent{
				Type: "status",
				From: from,
				Meta: map[string]any{"event": typ, "cap": cap.MeshMap()},
			})
		}
	}
	return out
}

func emitAgent(opts GlyphAgentOpts, ev GlyphAgentEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Println(string(b))
}
