package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// BitChat transport — dual-path offline mesh (BLE) + optional Nostr geohash.
//
// Native apps: https://github.com/permissionlesstech/bitchat
//              https://github.com/jackjackbits/bitchat-1
//
// GrokYtalkY does not reimplement BLE in-process. Instead:
//   • Hub exposes /api/bitchat/* for a native adapter to inject/poll messages
//   • Mesh peers use type:bitchat-* and chat with meta.via=bitchat
//   • Terminal: gy bitchat doctor|bridge|send|sim
//   • Site: site/bitchat-bridge.js tags dual-path chat for sphere/GrokGlyph
//
// Payload sizes stay text/control-sized (not multi-cam video). Glyph media
// stays on Wi‑Fi hub; BitChat carries coordination when hub is unreachable.

const (
	bitChatEgressMax = 256
	bitChatLogMax    = 128
)

// BitChatEnvelope is the hub-facing dual-path message (JSON over HTTP/WS).
type BitChatEnvelope struct {
	ID        string         `json:"id,omitempty"`
	Type      string         `json:"type"` // chat|presence|control|dm|system
	Text      string         `json:"text,omitempty"`
	From      string         `json:"from"`
	To        string         `json:"to,omitempty"` // dm target
	Room      string         `json:"room,omitempty"`
	Channel   string         `json:"channel,omitempty"` // mesh#bluetooth | geohash block#…
	Transport string         `json:"transport,omitempty"` // ble|nostr|bridge|sim
	Action    string         `json:"action,omitempty"`    // control: cast-start|cast-stop|…
	Geohash   string         `json:"geohash,omitempty"`
	T         int64          `json:"t,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// BitChatPeer is a recently seen offline/mesh identity.
type BitChatPeer struct {
	Nick      string `json:"nick"`
	Transport string `json:"transport"`
	Channel   string `json:"channel,omitempty"`
	LastSeen  int64  `json:"last_seen"`
	Room      string `json:"room,omitempty"`
}

// BitChatBus process-wide bridge state.
type BitChatBus struct {
	mu sync.Mutex

	enabled    bool
	hub        *Hub
	peers      map[string]*BitChatPeer
	egress     []BitChatEnvelope // hub → native adapter
	recent     []BitChatEnvelope // last N for doctor / API
	ingressN   int64
	egressN    int64
	bridgeN    int // connected bridge peers (role=bitchat-bridge)
	lastIn     time.Time
	lastOut    time.Time
	defaultRoom string
	defaultChan string
}

var (
	bitChatOnce sync.Once
	bitChatBus  *BitChatBus
)

// BitChat returns the process-wide bus.
func BitChat() *BitChatBus {
	bitChatOnce.Do(func() {
		room := strings.TrimSpace(os.Getenv("GY_ROOM"))
		if room == "" {
			room = "global"
		}
		ch := strings.TrimSpace(os.Getenv("GY_BITCHAT_CHANNEL"))
		if ch == "" {
			ch = "mesh#bluetooth"
		}
		en := true
		if v := strings.TrimSpace(os.Getenv("GY_BITCHAT")); v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
			en = false
		}
		bitChatBus = &BitChatBus{
			enabled:     en,
			peers:       map[string]*BitChatPeer{},
			defaultRoom: NormalizeMeshRoom(room),
			defaultChan: ch,
		}
	})
	return bitChatBus
}

// AttachHub wires room fan-out for ingress messages.
func (b *BitChatBus) AttachHub(h *Hub) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.hub = h
	b.mu.Unlock()
}

// Enabled reports whether dual-path is active.
func (b *BitChatBus) Enabled() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enabled
}

// Snapshot for API / doctor.
func (b *BitChatBus) Snapshot() map[string]any {
	if b == nil {
		return map[string]any{"ok": false, "enabled": false}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	peers := make([]BitChatPeer, 0, len(b.peers))
	for _, p := range b.peers {
		peers = append(peers, *p)
	}
	recent := make([]BitChatEnvelope, len(b.recent))
	copy(recent, b.recent)
	var lastIn, lastOut string
	if !b.lastIn.IsZero() {
		lastIn = b.lastIn.UTC().Format(time.RFC3339)
	}
	if !b.lastOut.IsZero() {
		lastOut = b.lastOut.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"ok":            true,
		"enabled":       b.enabled,
		"default_room":  b.defaultRoom,
		"default_chan":  b.defaultChan,
		"bridges":       b.bridgeN,
		"peers":         peers,
		"peer_n":        len(peers),
		"ingress_n":     b.ingressN,
		"egress_n":      b.egressN,
		"egress_queued": len(b.egress),
		"last_ingress":  lastIn,
		"last_egress":   lastOut,
		"recent":        recent,
		"native": map[string]any{
			"ios":     "https://github.com/permissionlesstech/bitchat",
			"fork":    "https://github.com/jackjackbits/bitchat-1",
			"android": "https://github.com/permissionlesstech/bitchat-android",
			"site":    "https://bitchat.free",
		},
		"api": map[string]string{
			"status":  "GET /api/bitchat",
			"ingress": "POST /api/bitchat/ingress",
			"egress":  "GET /api/bitchat/egress",
			"send":    "POST /api/bitchat/send",
		},
		"note": "BLE mesh requires native BitChat app or adapter posting to /api/bitchat/ingress",
	}
}

// BridgeConnected adjusts bridge peer count.
func (b *BitChatBus) BridgeConnected(delta int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.bridgeN += delta
	if b.bridgeN < 0 {
		b.bridgeN = 0
	}
	b.mu.Unlock()
}

func (b *BitChatBus) touchPeer(from, transport, channel, room string) {
	if from == "" {
		return
	}
	p, ok := b.peers[from]
	if !ok {
		p = &BitChatPeer{Nick: from}
		b.peers[from] = p
	}
	p.LastSeen = time.Now().UnixMilli()
	if transport != "" {
		p.Transport = transport
	}
	if channel != "" {
		p.Channel = channel
	}
	if room != "" {
		p.Room = room
	}
}

func (b *BitChatBus) pushRecent(env BitChatEnvelope) {
	b.recent = append(b.recent, env)
	if len(b.recent) > bitChatLogMax {
		b.recent = b.recent[len(b.recent)-bitChatLogMax:]
	}
}

// NormalizeIngress fills defaults and validates.
func (b *BitChatBus) NormalizeIngress(env *BitChatEnvelope) error {
	if env == nil {
		return fmt.Errorf("nil envelope")
	}
	env.Type = strings.ToLower(strings.TrimSpace(env.Type))
	if env.Type == "" {
		env.Type = "chat"
	}
	env.From = strings.TrimSpace(env.From)
	if env.From == "" {
		return fmt.Errorf("missing from")
	}
	// normalize nick for mesh
	if !strings.HasPrefix(env.From, "bt:") && !strings.HasPrefix(env.From, "nostr:") {
		// keep as-is if already namespaced by adapter
		if env.Transport == "ble" || env.Transport == "sim" {
			env.From = "bt:" + env.From
		} else if env.Transport == "nostr" {
			env.From = "nostr:" + env.From
		}
	}
	if env.Room == "" {
		env.Room = b.defaultRoom
	}
	env.Room = NormalizeMeshRoom(env.Room)
	if env.Channel == "" {
		env.Channel = b.defaultChan
	}
	if env.Transport == "" {
		env.Transport = "bridge"
	}
	if env.T == 0 {
		env.T = time.Now().UnixMilli()
	}
	if env.ID == "" {
		env.ID = fmt.Sprintf("bc-%d", env.T)
	}
	if env.Type == "chat" && strings.TrimSpace(env.Text) == "" {
		return fmt.Errorf("chat needs text")
	}
	return nil
}

// Ingress accepts a message from native BitChat adapter or sim and fans out to hub mesh.
func (b *BitChatBus) Ingress(env BitChatEnvelope) error {
	if b == nil || !b.Enabled() {
		return fmt.Errorf("bitchat disabled (GY_BITCHAT=0)")
	}
	b.mu.Lock()
	if err := b.NormalizeIngress(&env); err != nil {
		b.mu.Unlock()
		return err
	}
	b.ingressN++
	b.lastIn = time.Now()
	b.touchPeer(env.From, env.Transport, env.Channel, env.Room)
	b.pushRecent(env)
	h := b.hub
	b.mu.Unlock()

	if h != nil {
		h.FanoutBitChat(env)
	}
	return nil
}

// EnqueueEgress queues a hub-origin message for the native adapter to carry over BLE/Nostr.
func (b *BitChatBus) EnqueueEgress(env BitChatEnvelope) {
	if b == nil || !b.Enabled() {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if env.T == 0 {
		env.T = time.Now().UnixMilli()
	}
	if env.Room == "" {
		env.Room = b.defaultRoom
	}
	if env.Channel == "" {
		env.Channel = b.defaultChan
	}
	if env.Transport == "" {
		env.Transport = "wifi-hub"
	}
	b.egress = append(b.egress, env)
	if len(b.egress) > bitChatEgressMax {
		b.egress = b.egress[len(b.egress)-bitChatEgressMax:]
	}
	b.egressN++
	b.lastOut = time.Now()
	b.pushRecent(env)
}

// DrainEgress returns and clears up to n queued egress messages.
func (b *BitChatBus) DrainEgress(n int) []BitChatEnvelope {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || n > len(b.egress) {
		n = len(b.egress)
	}
	if n == 0 {
		return nil
	}
	out := make([]BitChatEnvelope, n)
	copy(out, b.egress[:n])
	b.egress = b.egress[n:]
	return out
}

// PeekEgress returns a copy without draining.
func (b *BitChatBus) PeekEgress(n int) []BitChatEnvelope {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if n <= 0 || n > len(b.egress) {
		n = len(b.egress)
	}
	out := make([]BitChatEnvelope, n)
	copy(out, b.egress[:n])
	return out
}

// MeshFromEnvelope builds hub mesh maps for broadcast.
func MeshFromBitChat(env BitChatEnvelope) []map[string]any {
	meta := map[string]any{
		"via":       "bitchat",
		"transport": env.Transport,
		"channel":   env.Channel,
	}
	if env.Geohash != "" {
		meta["geohash"] = env.Geohash
	}
	if env.Meta != nil {
		for k, v := range env.Meta {
			meta[k] = v
		}
	}
	var out []map[string]any
	switch env.Type {
	case "presence":
		out = append(out, map[string]any{
			"type": "bitchat-presence",
			"from": env.From,
			"room": env.Room,
			"t":    env.T,
			"meta": meta,
		})
	case "control":
		out = append(out, map[string]any{
			"type":   "bitchat-control",
			"from":   env.From,
			"room":   env.Room,
			"action": env.Action,
			"text":   env.Text,
			"t":      env.T,
			"meta":   meta,
		})
	case "dm":
		out = append(out, map[string]any{
			"type": "bitchat-dm",
			"from": env.From,
			"to":   env.To,
			"text": env.Text,
			"room": env.Room,
			"t":    env.T,
			"meta": meta,
		})
	default: // chat + system
		out = append(out, map[string]any{
			"type": "bitchat-chat",
			"from": env.From,
			"text": env.Text,
			"room": env.Room,
			"t":    env.T,
			"meta": meta,
		})
		// also ordinary chat so existing UIs show it
		out = append(out, map[string]any{
			"type": "chat",
			"from": env.From,
			"text": env.Text,
			"room": env.Room,
			"t":    env.T,
			"meta": meta,
		})
	}
	return out
}

// FormatBitChatDoctor multi-line status.
func FormatBitChatDoctor() string {
	b := BitChat()
	snap := b.Snapshot()
	var sb strings.Builder
	sb.WriteString("bitchat (BLE mesh · Nostr dual-path · bridge)\n")
	if !b.Enabled() {
		sb.WriteString("  status    disabled (GY_BITCHAT=0)\n")
		return sb.String()
	}
	sb.WriteString(fmt.Sprintf("  status    enabled · bridges %v · peers %v\n", snap["bridges"], snap["peer_n"]))
	sb.WriteString(fmt.Sprintf("  channel   %v · room %v\n", snap["default_chan"], snap["default_room"]))
	sb.WriteString(fmt.Sprintf("  traffic   in %v · out %v · egressq %v\n", snap["ingress_n"], snap["egress_n"], snap["egress_queued"]))
	if li, _ := snap["last_ingress"].(string); li != "" {
		sb.WriteString("  last in   " + li + "\n")
	}
	if lo, _ := snap["last_egress"].(string); lo != "" {
		sb.WriteString("  last out  " + lo + "\n")
	}
	sb.WriteString("  hub api   GET /api/bitchat · POST /api/bitchat/ingress · GET egress\n")
	sb.WriteString("  cli       gy bitchat doctor|bridge|send|sim\n")
	sb.WriteString("  native    permissionlesstech/bitchat · bitchat-android · bitchat.free\n")
	sb.WriteString("  note      video/glyphs stay on Wi‑Fi hub; BitChat carries chat/control offline\n")
	return sb.String()
}
