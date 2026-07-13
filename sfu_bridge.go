package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// RunSfuBridge forwards hub vburst-frame.glyph into a gy-sfu room (type:glyph).
func runSfuBridgeCmd(args []string) error {
	fs := newBridgeFlagSet("sfu-bridge")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy sfu-bridge — hub vburst glyph → gy-sfu room

  gy sfu-bridge [flags]

  --hub      DOJO hub WS (default ws://127.0.0.1:9876/)
  --sfu      gy-sfu WS including room/nick/token
  --dry-run  log only
  --quiet

Example:
  gy serve
  make sfu-media && ./sfu/target/release/gy-sfu --token secret
  gy sfu-bridge --sfu 'ws://127.0.0.1:9880/ws?room=dojo&nick=bridge&token=secret'
  gy burst   # hold space → glyph fans out to dojo.html peers
`)
	}
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "DOJO hub WebSocket")
	sfu := fs.String("sfu", "ws://127.0.0.1:9880/ws?room=dojo&nick=hub-bridge", "gy-sfu WS")
	dry := fs.Bool("dry-run", false, "log only")
	quiet := fs.Bool("quiet", false, "less logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	hubURL := ensureWSQuery(*hub, map[string]string{
		"role": "bridge",
		"nick": "sfu-bridge",
	})
	return RunSfuBridge(SfuBridgeOpts{
		HubWS:  hubURL,
		SfuWS:  *sfu,
		DryRun: *dry,
		Quiet:  *quiet,
	})
}

// SfuBridgeOpts configures hub → SFU glyph bridging.
type SfuBridgeOpts struct {
	HubWS  string
	SfuWS  string
	DryRun bool
	Quiet  bool
}

// RunSfuBridge connects hub + SFU and forwards glyph grids from bursts.
func RunSfuBridge(opts SfuBridgeOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" || opts.SfuWS == "" {
		return fmt.Errorf("hub and sfu WS URLs required")
	}
	if !opts.Quiet {
		log.Printf("sfu-bridge · hub=%s", opts.HubWS)
		log.Printf("sfu-bridge · sfu=%s", opts.SfuWS)
	}

	sfu := &wsPipe{name: "sfu", url: opts.SfuWS}
	hub := &wsPipe{name: "hub", url: opts.HubWS}

	errCh := make(chan error, 2)
	go func() { errCh <- sfu.loop(ctx) }()
	go func() { errCh <- hub.loop(ctx) }()

	deadline := time.After(8 * time.Second)
	for !sfu.ready() || !hub.ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case <-deadline:
			if !hub.ready() {
				return fmt.Errorf("hub connect timeout — gy serve?")
			}
			if !sfu.ready() {
				return fmt.Errorf("sfu connect timeout — gy-sfu + token?")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	_ = hub.sendJSON(map[string]any{
		"type": "join", "nick": "sfu-bridge", "role": "bridge",
	})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-sfu.incoming:
			}
		}
	}()

	if !opts.Quiet {
		log.Printf("sfu-bridge · linked · vburst-frame.glyph → SFU type:glyph")
	}

	for {
		select {
		case <-ctx.Done():
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
			typ, _ := msg["type"].(string)
			if typ != "vburst-frame" {
				continue
			}
			glyphRaw, ok := msg["glyph"]
			if !ok || glyphRaw == nil {
				continue
			}
			n := 25
			if gn, ok := msg["glyphN"].(float64); ok && gn > 0 {
				n = int(gn)
			}
			data, err := glyphToBytes(glyphRaw)
			if err != nil || len(data) == 0 {
				continue
			}
			if n*n != len(data) {
				switch len(data) {
				case 13 * 13:
					n = 13
				case 25 * 25:
					n = 25
				case 37 * 37:
					n = 37
				case 49 * 49:
					n = 49
				}
			}
			from, _ := msg["from"].(string)
			out := map[string]any{
				"type": "glyph",
				"n":    n,
				"data": data,
				"from": sfuBridgeFrom(from),
			}
			if opts.DryRun {
				log.Printf("sfu-bridge · dry glyph n=%d len=%d from=%s", n, len(data), from)
				continue
			}
			if err := sfu.sendJSON(out); err != nil {
				log.Printf("sfu-bridge · sfu send: %v", err)
				continue
			}
			if !opts.Quiet {
				log.Printf("sfu-bridge · → sfu glyph n=%d len=%d from=%s", n, len(data), from)
			}
		}
	}
}

func glyphToBytes(v any) ([]byte, error) {
	switch t := v.(type) {
	case []any:
		out := make([]byte, 0, len(t))
		for _, x := range t {
			switch n := x.(type) {
			case float64:
				out = append(out, byte(int(n)&0xff))
			case json.Number:
				i, _ := n.Int64()
				out = append(out, byte(i&0xff))
			}
		}
		return out, nil
	case []byte:
		return t, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		var arr []int
		if err := json.Unmarshal(b, &arr); err != nil {
			return nil, err
		}
		out := make([]byte, len(arr))
		for i, n := range arr {
			out[i] = byte(n & 0xff)
		}
		return out, nil
	}
}

func sfuBridgeFrom(from string) string {
	if strings.TrimSpace(from) != "" {
		return from
	}
	return "hub"
}
