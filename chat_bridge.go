package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// ChatBridgeOpts: thin DOJO hub → public Space chat caption bridge.
// Host (or allowlisted) chat lines only — never dumps 1k viewers into the hub.
type ChatBridgeOpts struct {
	HubWS   string   // ws://127.0.0.1:9876/?role=bridge&nick=caption
	SpaceWS string   // ws://127.0.0.1:8787/ws?room=space:demo&nick=bridge&role=host
	Hosts   []string // empty = mirror all hub chat (dev); prod = host nicks only
	DryRun  bool     // log only, do not send to Space
	Quiet   bool
}

// runChatBridgeCmd parses flags after `gy chat-bridge`.
func runChatBridgeCmd(args []string) error {
	fs := flagNew("chat-bridge")
	hub := fs.String("hub", "ws://127.0.0.1:9876/", "DOJO hub WebSocket URL")
	space := fs.String("space", "ws://127.0.0.1:8787/ws?room=space:demo&nick=bridge&role=host",
		"public Space chat WS (CF worker or wrangler dev)")
	hosts := fs.String("hosts", "", "comma-separated host nicks to mirror (empty=all, dev only)")
	dry := fs.Bool("dry-run", false, "log captions without forwarding to Space")
	quiet := fs.Bool("quiet", false, "less logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var hostList []string
	for _, h := range strings.Split(*hosts, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hostList = append(hostList, h)
		}
	}
	// ensure hub has nick for join
	hubURL := ensureWSQuery(*hub, map[string]string{
		"role": "bridge",
		"nick": "caption-bridge",
	})
	return RunChatBridge(ChatBridgeOpts{
		HubWS:   hubURL,
		SpaceWS: *space,
		Hosts:   hostList,
		DryRun:  *dry,
		Quiet:   *quiet,
	})
}

// tiny flag helper without importing flag package name clash in run()
func flagNew(name string) *bridgeFlagSet {
	return newBridgeFlagSet(name)
}

// RunChatBridge connects hub + Space and forwards allowlisted chat.
func RunChatBridge(opts ChatBridgeOpts) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if opts.HubWS == "" || opts.SpaceWS == "" {
		return fmt.Errorf("hub and space WS URLs required")
	}
	if !opts.Quiet {
		log.Printf("chat-bridge · hub=%s", opts.HubWS)
		log.Printf("chat-bridge · space=%s", opts.SpaceWS)
		if len(opts.Hosts) == 0 {
			log.Printf("chat-bridge · hosts=* (dev — set --hosts for production)")
		} else {
			log.Printf("chat-bridge · hosts=%s", strings.Join(opts.Hosts, ","))
		}
		if opts.DryRun {
			log.Printf("chat-bridge · dry-run (no Space send)")
		}
	}

	space := &wsPipe{name: "space", url: opts.SpaceWS}
	hub := &wsPipe{name: "hub", url: opts.HubWS}

	errCh := make(chan error, 2)
	go func() { errCh <- space.loop(ctx) }()
	go func() { errCh <- hub.loop(ctx) }()

	// wait until both connected or fail
	deadline := time.After(8 * time.Second)
	for !space.ready() || !hub.ready() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil && ctx.Err() == nil {
				return err
			}
		case <-deadline:
			if !hub.ready() {
				return fmt.Errorf("hub connect timeout — is gy serve running?")
			}
			if !space.ready() {
				return fmt.Errorf("space connect timeout — is wrangler dev / CF worker up?")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}

	// join hub as bridge peer
	_ = hub.sendJSON(map[string]any{
		"type": "join", "nick": "caption-bridge", "role": "bridge",
	})
	if !opts.Quiet {
		log.Printf("chat-bridge · linked · forwarding host chat DOJO→Space")
	}

	// drain Space inbound (welcome/peer events) so the buffer never blocks
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-space.incoming:
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			if !opts.Quiet {
				log.Printf("chat-bridge · shutdown")
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
			typ, _ := msg["type"].(string)
			if typ != "chat" {
				continue
			}
			from, _ := msg["from"].(string)
			text, _ := msg["text"].(string)
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			if !shouldMirrorChat(from, opts.Hosts) {
				continue
			}
			out := map[string]any{
				"type": "chat",
				"text": text,
				"from": from,
				"t":    time.Now().UnixMilli(),
				"role": "host",
				"meta": map[string]any{"bridged": true, "source": "dojo-hub"},
			}
			if opts.DryRun {
				log.Printf("chat-bridge · dry %s: %s", from, truncateRunes(text, 80))
				continue
			}
			if err := space.sendJSON(out); err != nil {
				log.Printf("chat-bridge · space send: %v", err)
				continue
			}
			if !opts.Quiet {
				log.Printf("chat-bridge · → space %s: %s", from, truncateRunes(text, 60))
			}
		}
	}
}

func shouldMirrorChat(from string, hosts []string) bool {
	if len(hosts) == 0 {
		return true
	}
	from = strings.TrimSpace(from)
	for _, h := range hosts {
		if strings.EqualFold(h, from) {
			return true
		}
	}
	return false
}

func ensureWSQuery(raw string, kv map[string]string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Scheme == "" {
		u.Scheme = "ws"
	}
	q := u.Query()
	for k, v := range kv {
		if q.Get(k) == "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// --- minimal reconnecting WS pipe ---

type wsPipe struct {
	name string
	url  string

	mu   sync.Mutex
	conn *websocket.Conn
	ok   bool

	incoming chan []byte
}

func (p *wsPipe) ready() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ok
}

func (p *wsPipe) loop(ctx context.Context) error {
	if p.incoming == nil {
		p.incoming = make(chan []byte, 64)
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := p.session(ctx)
		p.mu.Lock()
		p.ok = false
		p.conn = nil
		p.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("chat-bridge · %s reconnect: %v", p.name, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (p *wsPipe) session(ctx context.Context) error {
	c, _, err := websocket.Dial(ctx, p.url, &websocket.DialOptions{})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.conn = c
	p.ok = true
	p.mu.Unlock()
	log.Printf("chat-bridge · %s connected", p.name)

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return err
		}
		select {
		case p.incoming <- append([]byte(nil), data...):
		default:
			// drop if slow
		}
	}
}

func (p *wsPipe) sendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.mu.Lock()
	c := p.conn
	p.mu.Unlock()
	if c == nil {
		return fmt.Errorf("%s not connected", p.name)
	}
	wctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Write(wctx, websocket.MessageText, b)
}
