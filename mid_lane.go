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
type MidLaneEnvelope struct {
	Type    string         `json:"type"` // mid-lane
	Room    string         `json:"room"`
	Lane    string         `json:"lane"` // mid|hex|glyph|program
	From    string         `json:"from,omitempty"`
	T       int64          `json:"t"`
	Seq     uint32         `json:"seq,omitempty"`
	Program map[string]any `json:"program,omitempty"` // bus snapshot when present
	Gyst    map[string]any `json:"gyst,omitempty"`    // hexlum/meta pass-through
	Caption string         `json:"caption,omitempty"`
	Mark    string         `json:"mark,omitempty"`
	Mode    string         `json:"mode,omitempty"`
	Via     string         `json:"via"` // gy-mid-lane
}

// MidLaneOpts configures the edge publisher.
type MidLaneOpts struct {
	HubWS   string
	Room    string
	EdgeURL string // https://… or http://…
	Token   string // Bearer / X-GY-Edge-Token
	Quiet   bool
	DryRun  bool
	// which hub events to forward
	Program bool // type:program
	Hexlum  bool // type:gyst kind=hexlum
	AllGyst bool // any gyst
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

Forwards compact mid-lane JSON for Cloudflare / custom edge SFU ladders.
Does not encode 1080p. Pair with hub rooms + program-per-room.

Example:
  gy serve
  gy mid-lane --room dojo --edge https://worker.example/mid --token secret
  # conductor: /take · edge receives {type:mid-lane,lane:program,…}
`)
	}
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "hub WebSocket")
	room := fs.String("room", "", "mesh room (default global or from hub URL)")
	edge := fs.String("edge", os.Getenv("GY_EDGE_URL"), "edge HTTPS ingest URL")
	token := fs.String("token", os.Getenv("GY_EDGE_TOKEN"), "edge bearer token")
	noProg := fs.Bool("no-program", false, "skip program bus events")
	noHex := fs.Bool("no-hexlum", false, "skip hexlum gyst")
	allGyst := fs.Bool("all-gyst", false, "forward all gyst kinds")
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
		HubWS:   hubURL,
		Room:    r,
		EdgeURL: strings.TrimSpace(*edge),
		Token:   strings.TrimSpace(*token),
		Quiet:   *quiet,
		DryRun:  *dry,
		Program: !*noProg,
		Hexlum:  !*noHex,
		AllGyst: *allGyst,
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
		if bus, ok := ParseProgramBus(msg); ok {
			env.Seq = bus.Seq
			env.Mode = bus.Mode
			env.Mark = bus.Program.Mark
			if eff := bus.EffectiveCaption(); !eff.IsEmpty() {
				env.Caption = eff.Display()
			}
			// compact program map for edge
			raw, _ := json.Marshal(msg)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			env.Program = m
		} else {
			env.Program = msg
		}
		return env, true
	case "gyst", "gyst-frame":
		kind, _ := msg["kind"].(string)
		if opts.AllGyst {
			env.Lane = kind
			if env.Lane == "" {
				env.Lane = "gyst"
			}
		} else if opts.Hexlum && (kind == "hexlum" || kind == "hex") {
			env.Lane = LaneHex
		} else {
			return env, false
		}
		if v, ok := msg["seq"].(float64); ok {
			env.Seq = uint32(v)
		}
		env.Gyst = msg
		return env, true
	case "mid-lane":
		// already edge-shaped — re-post if wanted
		env.Lane = coalesce(msg["lane"], "mid")
		return env, true
	default:
		return env, false
	}
}

func postMidLane(ctx context.Context, client *http.Client, opts MidLaneOpts, env MidLaneEnvelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	if opts.DryRun {
		log.Printf("mid-lane · dry %s", truncate(string(body), 160))
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.EdgeURL, bytes.NewReader(body))
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
