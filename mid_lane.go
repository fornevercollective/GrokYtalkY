package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Edge mid-lane hook — side-car that watches hub program + gyst and POSTs a
// compact mid-lane envelope to an edge worker (CF / custom). Never carries
// full HD; never claims program authority.
//
//	gy mid-lane --hub ws://127.0.0.1:9876/?room=global \
//	  --edge https://edge.example/api/mid \
//	  --token $GY_EDGE_TOKEN

// MidLaneEnvelope is the stable JSON edge ingest contract.
// Ladder: glyph/hex (terminal) · mid (web tiles) · full (CF Calls/WHIP HD — edge only).
type MidLaneEnvelope struct {
	Type     string         `json:"type"` // mid-lane
	Room     string         `json:"room"`
	Lane     string         `json:"lane"` // program|hex|glyph|mid|full
	From     string         `json:"from,omitempty"`
	T        int64          `json:"t"`
	Seq      uint32         `json:"seq,omitempty"`
	Program  map[string]any `json:"program,omitempty"`
	Gyst     map[string]any `json:"gyst,omitempty"`
	Caption  string         `json:"caption,omitempty"`
	Mark     string         `json:"mark,omitempty"`
	Mode     string         `json:"mode,omitempty"`
	Ladder   string         `json:"ladder,omitempty"`    // glyph|mid|full
	WhipURL  string         `json:"whip_url,omitempty"`  // optional HD WHIP publish hint
	PlayURL  string         `json:"playback_url,omitempty"` // HLS/WHEP viewer hint
	Via      string         `json:"via"`                    // gy-mid-lane
}

// MidLaneOpts configures the edge publisher.
type MidLaneOpts struct {
	HubWS    string
	Room     string
	EdgeURL  string // https://… mid ingest
	Token    string
	Quiet    bool
	DryRun   bool
	Program  bool
	Hexlum   bool
	AllGyst  bool
	// HD ladder (never on hub): announce WHIP/playback URLs with program events
	WhipURL  string // GY_CALLS_WHIP_URL — Cloudflare Calls / WHIP ingest
	PlayURL  string // GY_CALLS_PLAYBACK_URL — viewer HLS/WHEP
	FullEdge string // optional second POST for lane=full metadata
}

func runMidLaneCmd(args []string) error {
	fs := newBridgeFlagSet("mid-lane")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy mid-lane — edge mid-lane hook (hub → HTTP edge ingest)

  gy mid-lane [flags]

  --hub      hub WS (include ?room= for tenancy; default global)
  --room     mesh room override (default from URL or global)
  --edge     HTTPS ingest URL (required unless --dry-run)
  --token    edge auth (env GY_EDGE_TOKEN)
  --no-program  skip type:program
  --no-hexlum   skip hexlum gyst
  --all-gyst    forward all gyst kinds
  --dry-run  log envelopes only
  --quiet

Forwards compact mid-lane JSON for Cloudflare / custom edge ladders.
Does not put 1080p on the hub. Optional --whip / --playback announce HD Calls ladder.

Example:
  gy serve
  cd edge/mid-lane && npx wrangler dev --port 8788
  gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid
  # HD (edge only): --whip https://calls…/whip --playback https://…/play.m3u8
`)
	}
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "hub WebSocket")
	room := fs.String("room", "", "mesh room (default global or from hub URL)")
	edge := fs.String("edge", os.Getenv("GY_EDGE_URL"), "edge HTTPS ingest URL")
	token := fs.String("token", os.Getenv("GY_EDGE_TOKEN"), "edge bearer token")
	noProg := fs.Bool("no-program", false, "skip program bus events")
	noHex := fs.Bool("no-hexlum", false, "skip hexlum gyst")
	allGyst := fs.Bool("all-gyst", false, "forward all gyst kinds")
	whip := fs.String("whip", os.Getenv("GY_CALLS_WHIP_URL"), "CF Calls / WHIP URL (HD ladder hint)")
	play := fs.String("playback", os.Getenv("GY_CALLS_PLAYBACK_URL"), "viewer HLS/WHEP URL")
	fullEdge := fs.String("full-edge", os.Getenv("GY_EDGE_FULL_URL"), "optional POST for lane=full metadata")
	dry := fs.Bool("dry-run", false, "log only")
	quiet := fs.Bool("quiet", false, "less logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	r := NormalizeMeshRoom(*room)
	if *room == "" {
		// try extract from hub query
		if i := strings.Index(*hub, "room="); i >= 0 {
			rest := (*hub)[i+5:]
			if j := strings.IndexAny(rest, "&"); j >= 0 {
				rest = rest[:j]
			}
			r = NormalizeMeshRoom(rest)
		}
	}
	hubURL := ensureWSQuery(*hub, map[string]string{
		"role": "bridge",
		"nick": "mid-lane",
		"room": r,
	})
	return RunMidLane(MidLaneOpts{
		HubWS:    hubURL,
		Room:     r,
		EdgeURL:  strings.TrimSpace(*edge),
		Token:    strings.TrimSpace(*token),
		Quiet:    *quiet,
		DryRun:   *dry,
		Program:  !*noProg,
		Hexlum:   !*noHex,
		AllGyst:  *allGyst,
		WhipURL:  strings.TrimSpace(*whip),
		PlayURL:  strings.TrimSpace(*play),
		FullEdge: strings.TrimSpace(*fullEdge),
	})
}

// RunMidLane connects hub and POSTs mid-lane envelopes to the edge URL.
func RunMidLane(opts MidLaneOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" {
		return fmt.Errorf("hub WS required")
	}
	if !opts.DryRun && opts.EdgeURL == "" {
		return fmt.Errorf("edge URL required (or --dry-run); set --edge or GY_EDGE_URL")
	}
	room := NormalizeMeshRoom(opts.Room)
	if !opts.Quiet {
		log.Printf("mid-lane · hub=%s room=%s", opts.HubWS, room)
		if opts.DryRun {
			log.Printf("mid-lane · dry-run")
		} else {
			log.Printf("mid-lane · edge=%s", opts.EdgeURL)
		}
		if opts.WhipURL != "" {
			log.Printf("mid-lane · HD ladder whip=%s", opts.WhipURL)
		}
	}

	hub := &wsPipe{name: "hub", url: opts.HubWS}
	errCh := make(chan error, 1)
	go func() { errCh <- hub.loop(ctx) }()

	deadline := time.After(8 * time.Second)
	for !hub.ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case <-deadline:
			return fmt.Errorf("hub connect timeout — gy serve?")
		case <-time.After(40 * time.Millisecond):
		}
	}
	_ = hub.sendJSON(map[string]any{
		"type": "join", "nick": "mid-lane", "role": "bridge", "room": room,
	})

	client := &http.Client{Timeout: 8 * time.Second}
	for {
		select {
		case <-ctx.Done():
			if !opts.Quiet {
				log.Printf("mid-lane · shutdown")
			}
			return nil
		case err := <-errCh:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case raw := <-hub.incoming:
			var msg map[string]any
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			// ignore other rooms if stamped
			if r, _ := msg["room"].(string); r != "" && NormalizeMeshRoom(r) != room {
				continue
			}
			env, ok := MapHubToMidLane(msg, room, opts)
			if !ok {
				continue
			}
			if err := postMidLane(ctx, client, opts, env); err != nil {
				log.Printf("mid-lane · edge: %v", err)
				continue
			}
			if !opts.Quiet {
				log.Printf("mid-lane · → edge lane=%s seq=%d", env.Lane, env.Seq)
			}
		}
	}
}

// MapHubToMidLane builds an edge envelope from a hub message (pure; testable).
func MapHubToMidLane(msg map[string]any, room string, opts MidLaneOpts) (MidLaneEnvelope, bool) {
	typ, _ := msg["type"].(string)
	from, _ := msg["from"].(string)
	env := MidLaneEnvelope{
		Type: "mid-lane",
		Room: NormalizeMeshRoom(room),
		From: from,
		T:    time.Now().UnixMilli(),
		Via:  "gy-mid-lane",
	}
	if t, ok := msg["t"].(float64); ok && t > 0 {
		env.T = int64(t)
	}

	switch typ {
	case "program":
		if !opts.Program {
			return env, false
		}
		env.Lane = "program"
		env.Ladder = "mid"
		if bus, ok := ParseProgramBus(msg); ok {
			env.Seq = bus.Seq
			env.Mode = bus.Mode
			env.Mark = bus.Program.Mark
			if eff := bus.EffectiveCaption(); !eff.IsEmpty() {
				env.Caption = eff.Display()
			}
			raw, _ := json.Marshal(msg)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			env.Program = m
		} else {
			env.Program = msg
		}
		// HD ladder hints (edge/CF Calls — never encode HD here)
		env.WhipURL = opts.WhipURL
		env.PlayURL = opts.PlayURL
		if opts.WhipURL != "" || opts.PlayURL != "" {
			env.Ladder = "full"
		}
		return env, true
	case "gyst", "gyst-frame":
		kind, _ := msg["kind"].(string)
		if opts.AllGyst {
			env.Lane = kind
			if env.Lane == "" {
				env.Lane = "gyst"
			}
			env.Ladder = "mid"
		} else if opts.Hexlum && (kind == "hexlum" || kind == "hex") {
			env.Lane = LaneHex
			env.Ladder = "glyph" // terminal / Glyph scale
		} else {
			return env, false
		}
		if v, ok := msg["seq"].(float64); ok {
			env.Seq = uint32(v)
		}
		env.Gyst = msg
		return env, true
	case "mid-lane":
		env.Lane = coalesce(msg["lane"], "mid")
		env.Ladder = coalesce(msg["ladder"], "mid")
		return env, true
	default:
		return env, false
	}
}

func postMidLane(ctx context.Context, client *http.Client, opts MidLaneOpts, env MidLaneEnvelope) error {
	if err := postMidLaneURL(ctx, client, opts, opts.EdgeURL, env); err != nil {
		return err
	}
	// optional second hop: full-ladder metadata (WHIP/playback announce)
	if opts.FullEdge != "" && (env.Lane == "program" || env.Ladder == "full") {
		full := env
		full.Lane = "full"
		full.Ladder = "full"
		_ = postMidLaneURL(ctx, client, opts, opts.FullEdge, full)
	}
	return nil
}

func postMidLaneURL(ctx context.Context, client *http.Client, opts MidLaneOpts, edge string, env MidLaneEnvelope) error {
	if edge == "" {
		return nil
	}
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if opts.DryRun {
		log.Printf("mid-lane · dry %s → %s", env.Lane, truncate(string(body), 140))
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, edge, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "GrokYtalkY-mid-lane/"+Version)
	if opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+opts.Token)
		req.Header.Set("X-GY-Edge-Token", opts.Token)
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("edge HTTP %s", res.Status)
	}
	return nil
}
