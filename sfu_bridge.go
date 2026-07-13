package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// RunSfuBridge — hub ↔ gy-sfu glyph/hex DC lanes with shared token auth.
//
// Hub → SFU:
//   vburst-frame.glyph → type:glyph (WS + media DC fan-out)
//   gyst hexlum        → type:glyph + type:hex
//   gyst forge-mark    → type:chat
//
// SFU → Hub (bidi, default on):
//   type:glyph → hub vburst-frame (terminals)
//   type:hex   → hub gyst hexlum
//   type:chat  → hub chat
//
// Token: ?token= on SFU WS and/or join.token (GY_SFU_TOKEN / --token).

func runSfuBridgeCmd(args []string) error {
	fs := newBridgeFlagSet("sfu-bridge")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, `gy sfu-bridge — hub ↔ gy-sfu glyph/hex DC + token auth

  gy sfu-bridge [flags]

  --hub       DOJO hub WS (default ws://127.0.0.1:9876/)
  --sfu       gy-sfu WS URL (or build from --sfu-host + room/nick/token)
  --sfu-host  host:port (default 127.0.0.1:9880) when --sfu empty
  --room      SFU room (default dojo)
  --nick      SFU nick (default sfu-bridge)
  --token     shared secret (default $GY_SFU_TOKEN)
  --bidi      SFU→hub reverse glyph/chat (default true)
  --dry-run   log only
  --quiet

Forwards (hub→sfu):
  hub vburst-frame.glyph  →  SFU type:glyph  (DC label glyph)
  hub gyst hexlum         →  SFU type:glyph + type:hex
  hub gyst forge-mark     →  SFU type:chat + meta mark

Reverse (sfu→hub, --bidi):
  SFU type:glyph  →  hub vburst-frame
  SFU type:hex    →  hub gyst hexlum
  SFU type:chat   →  hub chat

Example:
  export GY_SFU_TOKEN=$(gy sfu-token)
  gy serve
  make sfu-media && ./sfu/target/release/gy-sfu --token "$GY_SFU_TOKEN"
  gy sfu-bridge --token "$GY_SFU_TOKEN" --room dojo
  # browser: site/dojo.html  token field = same secret
`)
	}
	hub := fs.String("hub", envOr("GY_HUB", "ws://127.0.0.1:9876/"), "DOJO hub WebSocket")
	sfu := fs.String("sfu", "", "gy-sfu full WS URL (overrides host/room/nick)")
	sfuHost := fs.String("sfu-host", envOr("GY_SFU_HOST", "127.0.0.1:9880"), "gy-sfu host:port")
	room := fs.String("room", envOr("GY_SFU_ROOM", "dojo"), "SFU room")
	nick := fs.String("nick", "sfu-bridge", "SFU nick")
	token := fs.String("token", strings.TrimSpace(os.Getenv("GY_SFU_TOKEN")), "shared auth token")
	bidi := fs.Bool("bidi", !envTruthy("GY_SFU_BRIDGE_NO_BIDI"), "SFU→hub reverse")
	dry := fs.Bool("dry-run", false, "log only")
	quiet := fs.Bool("quiet", false, "less logging")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sfuURL := strings.TrimSpace(*sfu)
	if sfuURL == "" {
		sfuURL = BuildSfuWSURL(*sfuHost, *room, *nick, *token)
	} else {
		sfuURL = ensureWSQuery(sfuURL, map[string]string{
			"room": *room, "nick": *nick,
		})
		if *token != "" {
			sfuURL = ensureWSQuery(sfuURL, map[string]string{"token": *token})
		}
	}

	hubURL := ensureWSQuery(*hub, map[string]string{
		"role": "bridge",
		"nick": "sfu-bridge",
	})
	return RunSfuBridge(SfuBridgeOpts{
		HubWS:  hubURL,
		SfuWS:  sfuURL,
		Token:  *token,
		Room:   *room,
		Nick:   *nick,
		Bidi:   *bidi,
		DryRun: *dry,
		Quiet:  *quiet,
	})
}

// BuildSfuWSURL composes ws://host/ws?room=&nick=&token=
func BuildSfuWSURL(host, room, nick, token string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1:9880"
	}
	// allow host with or without scheme
	if !strings.Contains(host, "://") {
		host = "ws://" + host
	}
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Sprintf("ws://127.0.0.1:9880/ws?room=%s&nick=%s", url.QueryEscape(room), url.QueryEscape(nick))
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/ws"
	}
	q := u.Query()
	if room != "" {
		q.Set("room", room)
	}
	if nick != "" {
		q.Set("nick", nick)
	}
	if token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// SfuBridgeOpts configures hub ↔ SFU glyph/hex bridging.
type SfuBridgeOpts struct {
	HubWS  string
	SfuWS  string
	Token  string
	Room   string
	Nick   string
	Bidi   bool
	DryRun bool
	Quiet  bool
}

// sfuBridgeOut is one outbound SFU signaling/DC JSON message.
type sfuBridgeOut struct {
	Msg map[string]any
	Log string
}

// sfuBridgeStats process-local counters for doctor.
type sfuBridgeStats struct {
	glyphOut atomic.Int64
	hexOut   atomic.Int64
	chatOut  atomic.Int64
	glyphIn  atomic.Int64
	hexIn    atomic.Int64
	chatIn   atomic.Int64
	errors   atomic.Int64
	joined   atomic.Bool
}

var bridgeLiveStats sfuBridgeStats

// RunSfuBridge connects hub + SFU and forwards glyph/hexlum/forge marks.
func RunSfuBridge(opts SfuBridgeOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" || opts.SfuWS == "" {
		return fmt.Errorf("hub and sfu WS URLs required")
	}
	if opts.Room == "" {
		opts.Room = "dojo"
	}
	if opts.Nick == "" {
		opts.Nick = "sfu-bridge"
	}

	if !opts.Quiet {
		log.Printf("sfu-bridge · hub=%s", opts.HubWS)
		log.Printf("sfu-bridge · sfu=%s", redactTokenURL(opts.SfuWS))
		if opts.Token != "" {
			log.Printf("sfu-bridge · token=set · room=%s · bidi=%v", opts.Room, opts.Bidi)
		} else {
			log.Printf("sfu-bridge · token=open · room=%s · bidi=%v", opts.Room, opts.Bidi)
		}
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
				return fmt.Errorf("sfu connect timeout — gy-sfu running? token match GY_SFU_TOKEN?")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	// hub join
	_ = hub.sendJSON(map[string]any{
		"type": "join", "nick": opts.Nick, "role": "bridge",
	})

	// SFU join with token + glyph/hex/chat lanes (query token may already authed)
	join := map[string]any{
		"type":  "join",
		"room":  opts.Room,
		"nick":  opts.Nick,
		"lanes": []string{"glyph", "hex", "chat"},
	}
	if opts.Token != "" {
		join["token"] = opts.Token
	}
	if err := sfu.sendJSON(join); err != nil {
		bridgeLiveStats.errors.Add(1)
		return fmt.Errorf("sfu join: %w", err)
	}
	bridgeLiveStats.joined.Store(true)

	// SFU → hub (and drain welcome/errors)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-sfu.incoming:
				var msg map[string]any
				if err := json.Unmarshal(raw, &msg); err != nil {
					continue
				}
				typ, _ := msg["type"].(string)
				switch typ {
				case "welcome":
					if !opts.Quiet {
						auth, _ := msg["auth"].(bool)
						media, _ := msg["media"].(bool)
						log.Printf("sfu-bridge · welcome room=%v media=%v auth=%v", msg["room"], media, auth)
					}
				case "error":
					bridgeLiveStats.errors.Add(1)
					m, _ := msg["message"].(string)
					log.Printf("sfu-bridge · sfu error: %s", m)
				default:
					if !opts.Bidi {
						continue
					}
					outs := MapSfuMsgToHub(msg)
					for _, out := range outs {
						if opts.DryRun {
							log.Printf("sfu-bridge · dry reverse %s", out.Log)
							continue
						}
						if err := hub.sendJSON(out.Msg); err != nil {
							bridgeLiveStats.errors.Add(1)
							log.Printf("sfu-bridge · hub send: %v", err)
							continue
						}
						countBridgeIn(out.Msg)
						if !opts.Quiet {
							log.Printf("sfu-bridge · ← hub %s", out.Log)
						}
					}
				}
			}
		}
	}()

	if !opts.Quiet {
		log.Printf("sfu-bridge · linked · hub↔sfu glyph|hex|chat · token+DC ready")
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
					bridgeLiveStats.errors.Add(1)
					log.Printf("sfu-bridge · sfu send: %v", err)
					continue
				}
				countBridgeOut(out.Msg)
				if !opts.Quiet {
					log.Printf("sfu-bridge · → sfu %s", out.Log)
				}
			}
		}
	}
}

func countBridgeOut(msg map[string]any) {
	switch msg["type"] {
	case "glyph":
		bridgeLiveStats.glyphOut.Add(1)
	case "hex":
		bridgeLiveStats.hexOut.Add(1)
	case "chat":
		bridgeLiveStats.chatOut.Add(1)
	}
}

func countBridgeIn(msg map[string]any) {
	switch msg["type"] {
	case "vburst-frame":
		bridgeLiveStats.glyphIn.Add(1)
	case MeshTypeGYST, "gyst-frame":
		bridgeLiveStats.hexIn.Add(1)
	case "chat":
		bridgeLiveStats.chatIn.Add(1)
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

// MapSfuMsgToHub converts SFU glyph/hex/chat (WS or mirrored DC) → hub mesh.
func MapSfuMsgToHub(msg map[string]any) []sfuBridgeOut {
	if msg == nil {
		return nil
	}
	typ, _ := msg["type"].(string)
	switch typ {
	case "glyph":
		return mapSfuGlyphToHub(msg)
	case "hex":
		return mapSfuHexToHub(msg)
	case "chat":
		return mapSfuChatToHub(msg)
	default:
		return nil
	}
}

func mapSfuGlyphToHub(msg map[string]any) []sfuBridgeOut {
	n := 25
	if gn, ok := msg["n"].(float64); ok && gn > 0 {
		n = int(gn)
	}
	data, err := glyphToBytes(msg["data"])
	if err != nil || len(data) == 0 {
		return nil
	}
	n = inferGlyphN(n, len(data))
	from, _ := msg["from"].(string)
	if from == "" {
		from = "sfu"
	}
	// avoid echo loops from our own bridge nick
	if strings.Contains(strings.ToLower(from), "sfu-bridge") || strings.Contains(strings.ToLower(from), "hub-bridge") {
		return nil
	}
	// vburst-frame for burst/glyph consumers
	nums := bytesToJSONNums(data)
	return []sfuBridgeOut{{
		Msg: map[string]any{
			"type":   "vburst-frame",
			"from":   from,
			"glyphN": n,
			"glyph":  nums,
			"via":    "sfu-bridge",
			"t":      time.Now().UnixMilli(),
		},
		Log: fmt.Sprintf("vburst glyph n=%d len=%d from=%s", n, len(data), from),
	}}
}

func mapSfuHexToHub(msg map[string]any) []sfuBridgeOut {
	payload, _ := msg["payload"].(string)
	if payload == "" {
		return nil
	}
	pkt, err := DecodeHexLine(payload)
	if err != nil || pkt == nil {
		return nil
	}
	from, _ := msg["from"].(string)
	if from == "" {
		from = "sfu"
	}
	mesh := PacketToMesh(*pkt, from)
	mesh["via"] = "sfu-bridge"
	return []sfuBridgeOut{{
		Msg: mesh,
		Log: fmt.Sprintf("gyst hex from=%s", from),
	}}
}

func mapSfuChatToHub(msg map[string]any) []sfuBridgeOut {
	text, _ := msg["text"].(string)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	from, _ := msg["from"].(string)
	if from == "" {
		if n, ok := msg["nick"].(string); ok {
			from = n
		}
	}
	if from == "" {
		from = "sfu"
	}
	if strings.Contains(strings.ToLower(from), "sfu-bridge") {
		return nil
	}
	out := map[string]any{
		"type": "chat",
		"from": from,
		"text": text,
		"via":  "sfu-bridge",
		"t":    time.Now().UnixMilli(),
	}
	if role, ok := msg["role"].(string); ok && role != "" {
		out["role"] = role
	}
	if meta, ok := msg["meta"]; ok && meta != nil {
		out["meta"] = meta
	}
	return []sfuBridgeOut{{
		Msg: out,
		Log: fmt.Sprintf("chat from=%s %q", from, truncate(text, 40)),
	}}
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
		if pkt, err := MeshToPacket(msg); err == nil && pkt != nil && pkt.Kind == KindMeta {
			if mark, ok := ParseForgeMark(pkt.Payload); ok {
				return []sfuBridgeOut{sfuForgeMarkChat(from, mark)}
			}
		}
		return nil
	}

	if kind != "hexlum" && kind != "hex" {
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
	outs = append(outs, sfuBridgeOut{
		Msg: sfuGlyphMsg(n, data, from),
		Log: fmt.Sprintf("glyph n=%d len=%d from=%s (gyst hexlum)", n, len(data), sfuBridgeFrom(from)),
	})
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
	if mark, ok := ParseForgeFromMesh(msg); ok && mark.ID != "" {
		outs = append(outs, sfuForgeMarkChat(from, mark))
	}
	return outs
}

func sfuGlyphMsg(n int, data []byte, from string) map[string]any {
	return map[string]any{
		"type": "glyph",
		"n":    n,
		// JSON number array (not base64) — matches gy-sfu u8_seq + dojo DC
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
	case string:
		// base64 fallback
		return base64.StdEncoding.DecodeString(t)
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

func redactTokenURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Get("token") != "" {
		q.Set("token", "***")
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

// ── token mint + doctor ─────────────────────────────────────

// GenerateSfuToken returns a random 24-byte hex token for GY_SFU_TOKEN.
func GenerateSfuToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// fallback
		return fmt.Sprintf("gy-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func runSfuTokenCmd(args []string) error {
	// gy sfu-token [--print-export]
	export := false
	for _, a := range args {
		if a == "--export" || a == "-e" {
			export = true
		}
		if a == "-h" || a == "--help" {
			fmt.Print(`gy sfu-token — mint a shared SFU room token

  gy sfu-token           print token
  gy sfu-token --export  print export GY_SFU_TOKEN=… lines

Use the same value for:
  gy-sfu --token $TOKEN
  gy sfu-bridge --token $TOKEN
  site/dojo.html token field
`)
			return nil
		}
	}
	tok := GenerateSfuToken()
	if export {
		fmt.Printf("export GY_SFU_TOKEN=%s\n", tok)
		fmt.Printf("# gy-sfu --token \"$GY_SFU_TOKEN\"\n")
		fmt.Printf("# gy sfu-bridge --token \"$GY_SFU_TOKEN\" --room dojo\n")
		return nil
	}
	fmt.Println(tok)
	return nil
}

// ProbeSfuHealth hits gy-sfu /health (optional token not required for health).
func ProbeSfuHealth(base string, timeout time.Duration) (map[string]any, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "http://127.0.0.1:9880"
	}
	if strings.HasPrefix(base, "ws://") {
		base = "http://" + strings.TrimPrefix(base, "ws://")
	}
	if strings.HasPrefix(base, "wss://") {
		base = "https://" + strings.TrimPrefix(base, "wss://")
	}
	// host:port without scheme
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/health") {
		// strip /ws path if present
		if i := strings.Index(base, "/ws"); i > 0 {
			base = base[:i]
		}
		base = base + "/health"
	}
	client := &http.Client{Timeout: timeout}
	res, err := client.Get(base)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// FormatSfuDoctor multi-line for gy doctor sfu.
func FormatSfuDoctor() string {
	var b strings.Builder
	host := envOr("GY_SFU_HOST", "127.0.0.1:9880")
	tokenSet := strings.TrimSpace(os.Getenv("GY_SFU_TOKEN")) != ""
	fmt.Fprintf(&b, "sfu · host=%s · token=%v\n", host, tokenSet)
	fmt.Fprintf(&b, "  bridge   glyph↔DC · hub↔sfu · join.token + ?token=\n")
	fmt.Fprintf(&b, "  live     joined=%v  →glyph %d hex %d chat %d  ←glyph %d hex %d chat %d  err %d\n",
		bridgeLiveStats.joined.Load(),
		bridgeLiveStats.glyphOut.Load(), bridgeLiveStats.hexOut.Load(), bridgeLiveStats.chatOut.Load(),
		bridgeLiveStats.glyphIn.Load(), bridgeLiveStats.hexIn.Load(), bridgeLiveStats.chatIn.Load(),
		bridgeLiveStats.errors.Load())
	if h, err := ProbeSfuHealth(host, 1500*time.Millisecond); err != nil {
		fmt.Fprintf(&b, "  health   down · %v\n", err)
		b.WriteString("  tip      make sfu-media && ./sfu/target/release/gy-sfu --token $GY_SFU_TOKEN\n")
	} else {
		auth, _ := h["auth"].(bool)
		media, _ := h["media"].(bool)
		fmt.Fprintf(&b, "  health   ok · media=%v auth=%v\n", media, auth)
		if raw, err := json.Marshal(h); err == nil {
			fmt.Fprintf(&b, "  json     %s\n", truncate(string(raw), 120))
		}
	}
	b.WriteString("  cmds     gy sfu-token · gy sfu-bridge --token … · site/dojo.html\n")
	b.WriteString("  env      GY_SFU_TOKEN · GY_SFU_HOST · GY_SFU_ROOM · GY_HUB\n")
	b.WriteString("  lanes    glyph · hex · chat  (WebRTC DC + WS mirror)\n")
	return b.String()
}
