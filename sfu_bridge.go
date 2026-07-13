package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// RunSfuBridge forwards hub glyphs into a gy-sfu room:
//   - vburst-frame.glyph → SFU type:glyph
//   - gyst hexlum (forge lattice on wire) → SFU type:glyph + type:hex
//   - gyst forge-mark meta → SFU type:chat (meta carries mark)
func runSfuBridgeCmd(args []string) error {
	fs := newBridgeFlagSet("sfu-bridge")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy sfu-bridge — hub → gy-sfu glyph/hex DC lanes

  gy sfu-bridge [flags]

  --hub      DOJO hub WS (default ws://127.0.0.1:9876/)
  --sfu      gy-sfu WS including room/nick/token
  --dry-run  log only
  --quiet

Forwards:
  hub vburst-frame.glyph  →  SFU type:glyph
  hub gyst hexlum         →  SFU type:glyph + type:hex  (lattice preserved)
  hub gyst forge-mark     →  SFU type:chat + meta mark

Example:
  gy serve
  make sfu-media && ./sfu/target/release/gy-sfu --token secret
  gy sfu-bridge --sfu 'ws://127.0.0.1:9880/ws?room=dojo&nick=bridge&token=secret'
  # publisher: /forge examples/dojo.pcap  or  gy burst
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

// SfuBridgeOpts configures hub → SFU glyph/hex bridging.
type SfuBridgeOpts struct {
	HubWS  string
	SfuWS  string
	DryRun bool
	Quiet  bool
}

// sfuBridgeOut is one outbound SFU signaling/DC JSON message.
type sfuBridgeOut struct {
	Msg map[string]any
	Log string
}

// RunSfuBridge connects hub + SFU and forwards glyph/hexlum/forge marks.
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
		log.Printf("sfu-bridge · linked · vburst|gyst hexlum|forge-mark → SFU glyph|hex|chat")
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
			outs := MapHubMsgToSfu(msg)
			for _, out := range outs {
				if opts.DryRun {
					log.Printf("sfu-bridge · dry %s", out.Log)
					continue
				}
				if err := sfu.sendJSON(out.Msg); err != nil {
					log.Printf("sfu-bridge · sfu send: %v", err)
					continue
				}
				if !opts.Quiet {
					log.Printf("sfu-bridge · → sfu %s", out.Log)
				}
			}
		}
	}
}

// MapHubMsgToSfu converts one hub mesh JSON object into zero or more SFU messages.
// Pure / testable — lattice bytes pass through unchanged on glyph+hex lanes.
func MapHubMsgToSfu(msg map[string]any) []sfuBridgeOut {
	if msg == nil {
		return nil
	}
	typ, _ := msg["type"].(string)
	switch typ {
	case "vburst-frame":
		return mapVburstToSfu(msg)
	case MeshTypeGYST, "gyst-frame":
		return mapGystToSfu(msg)
	default:
		return nil
	}
}

func mapVburstToSfu(msg map[string]any) []sfuBridgeOut {
	glyphRaw, ok := msg["glyph"]
	if !ok || glyphRaw == nil {
		return nil
	}
	n := 25
	if gn, ok := msg["glyphN"].(float64); ok && gn > 0 {
		n = int(gn)
	}
	data, err := glyphToBytes(glyphRaw)
	if err != nil || len(data) == 0 {
		return nil
	}
	n = inferGlyphN(n, len(data))
	from, _ := msg["from"].(string)
	return []sfuBridgeOut{{
		Msg: sfuGlyphMsg(n, data, from),
		Log: fmt.Sprintf("glyph n=%d len=%d from=%s (vburst)", n, len(data), sfuBridgeFrom(from)),
	}}
}

func mapGystToSfu(msg map[string]any) []sfuBridgeOut {
	from, _ := msg["from"].(string)
	kind, _ := msg["kind"].(string)

	// forge-mark meta (no frame) → chat with meta for Space/SFU consumers
	if mark, ok := ParseForgeFromMesh(msg); ok && (kind == "meta" || kind == "") {
		b64, _ := msg["b64"].(string)
		if b64 == "" || kind == "meta" {
			return []sfuBridgeOut{sfuForgeMarkChat(from, mark)}
		}
	}
	if kind == "meta" {
		if mark, ok := ParseForgeFromMesh(msg); ok {
			return []sfuBridgeOut{sfuForgeMarkChat(from, mark)}
		}
		// raw meta packet via b64
		if pkt, err := MeshToPacket(msg); err == nil && pkt != nil && pkt.Kind == KindMeta {
			if mark, ok := ParseForgeMark(pkt.Payload); ok {
				return []sfuBridgeOut{sfuForgeMarkChat(from, mark)}
			}
		}
		return nil
	}

	// stamped hexlum (and optional forge fields on same envelope)
	if kind != "hexlum" && kind != "hex" {
		// non-hexlum gyst: still surface forge mark if present
		if mark, ok := ParseForgeFromMesh(msg); ok {
			return []sfuBridgeOut{sfuForgeMarkChat(from, mark)}
		}
		return nil
	}

	data, n, err := gystHexlumBytes(msg)
	if err != nil || len(data) == 0 {
		return nil
	}
	n = inferGlyphN(n, len(data))

	var outs []sfuBridgeOut
	// glyph DC / signaling lane — lattice bytes unchanged
	outs = append(outs, sfuBridgeOut{
		Msg: sfuGlyphMsg(n, data, from),
		Log: fmt.Sprintf("glyph n=%d len=%d from=%s (gyst hexlum)", n, len(data), sfuBridgeFrom(from)),
	})
	// hex lane — gyhex line preserves full packet + lattice payload
	pkt := PacketFromHexLum(data, n, gystSeq(msg))
	if t, ok := msg["t"].(float64); ok && t > 0 {
		pkt.TimeMS = uint64(t)
	}
	hexLine := EncodeHexLine(pkt)
	outs = append(outs, sfuBridgeOut{
		Msg: map[string]any{
			"type":    "hex",
			"payload": hexLine,
		},
		Log: fmt.Sprintf("hex len=%d n=%d from=%s (gyst hexlum)", len(hexLine), n, sfuBridgeFrom(from)),
	})
	// if forge mark tags ride the hexlum envelope, also chat once-ish (cheap; consumers dedupe)
	if mark, ok := ParseForgeFromMesh(msg); ok && mark.ID != "" {
		outs = append(outs, sfuForgeMarkChat(from, mark))
	}
	return outs
}

func sfuGlyphMsg(n int, data []byte, from string) map[string]any {
	return map[string]any{
		"type": "glyph",
		"n":    n,
		// JSON number array (not base64) — matches gy-sfu ClientMsg::Glyph Vec<u8>
		"data": bytesToJSONNums(data),
		"from": sfuBridgeFrom(from),
	}
}

func sfuForgeMarkChat(from string, mark ForgeMark) sfuBridgeOut {
	text := FormatMarkLine(mark)
	return sfuBridgeOut{
		Msg: map[string]any{
			"type": "chat",
			"text": text,
			"from": sfuBridgeFrom(from),
			"role": "forge",
			"meta": map[string]any{
				"type":    "forge-mark",
				"forge":   mark.Forge,
				"mark":    mark.ID,
				"slot":    mark.Slot,
				"source":  mark.Source,
				"content": mark.Content,
				"v":       mark.Version,
				"via":     "sfu-bridge",
			},
		},
		Log: fmt.Sprintf("chat forge %s slot=%d from=%s", ShortMarkID(mark.ID), mark.Slot, sfuBridgeFrom(from)),
	}
}

// gystHexlumBytes extracts lattice/grid bytes from a hub gyst hexlum message.
func gystHexlumBytes(msg map[string]any) (data []byte, n int, err error) {
	if w, ok := msg["w"].(float64); ok && w > 0 {
		n = int(w)
	}
	if n < 1 {
		if h, ok := msg["h"].(float64); ok && h > 0 {
			n = int(h)
		}
	}
	if gn, ok := msg["glyphN"].(float64); ok && gn > 0 {
		n = int(gn)
	}
	// prefer data[] (glyph-friendly ints) then b64 payload
	if raw, ok := msg["data"]; ok && raw != nil {
		data, err = glyphToBytes(raw)
		if err == nil && len(data) > 0 {
			return data, n, nil
		}
	}
	if b64, ok := msg["b64"].(string); ok && b64 != "" {
		data, err = base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, 0, err
		}
		return data, n, nil
	}
	// MeshToPacket fallback
	pkt, err := MeshToPacket(msg)
	if err != nil || pkt == nil {
		return nil, 0, fmt.Errorf("no hexlum payload")
	}
	if pkt.Kind != KindHexLum {
		return nil, 0, fmt.Errorf("not hexlum")
	}
	n = int(pkt.Width)
	if n < 1 {
		n = int(pkt.Height)
	}
	return pkt.Payload, n, nil
}

func gystSeq(msg map[string]any) uint32 {
	if v, ok := msg["seq"].(float64); ok && v > 0 {
		return uint32(v)
	}
	return 1
}

func inferGlyphN(hint, dataLen int) int {
	if hint > 0 && hint*hint == dataLen {
		return hint
	}
	switch dataLen {
	case 13 * 13:
		return 13
	case 25 * 25:
		return 25
	case 37 * 37:
		return 37
	case 49 * 49:
		return 49
	}
	if dataLen > 0 {
		// nearest square side
		n := 1
		for n*n < dataLen {
			n++
		}
		if n*n == dataLen {
			return n
		}
	}
	if hint > 0 {
		return hint
	}
	return 25
}

// bytesToJSONNums encodes lum/glyph grids as JSON number arrays for gy-sfu.
// (encoding/json would base64 []byte, which ClientMsg::Glyph rejects.)
func bytesToJSONNums(b []byte) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
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
			case int:
				out = append(out, byte(n&0xff))
			}
		}
		return out, nil
	case []byte:
		return t, nil
	case []int:
		out := make([]byte, len(t))
		for i, n := range t {
			out[i] = byte(n & 0xff)
		}
		return out, nil
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
