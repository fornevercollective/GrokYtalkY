package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// MeshClient is the walkie peer WebSocket client.
type MeshClient struct {
	URL  string
	Nick string

	mu   sync.Mutex
	conn *websocket.Conn
	send chan []byte

	OnMessage func([]byte)
	OnStatus  func(string)
}

func NewMeshClient(host, nick string) *MeshClient {
	if host == "" {
		host = "127.0.0.1:9876"
	}
	u := url.URL{
		Scheme:   "ws",
		Host:     host,
		Path:     "/",
		RawQuery: "role=peer&nick=" + url.QueryEscape(nick),
	}
	return &MeshClient{
		URL:  u.String(),
		Nick: nick,
		send: make(chan []byte, 64),
	}
}

func (c *MeshClient) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.session(ctx); err != nil {
			if c.OnStatus != nil {
				c.OnStatus("reconnect… " + err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
			continue
		}
	}
}

func (c *MeshClient) session(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.URL, nil)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	if c.OnStatus != nil {
		c.OnStatus("connected")
	}
	_ = c.SendJSON(map[string]any{"type": "join", "nick": c.Nick, "role": "term"})

	errCh := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case msg, ok := <-c.send:
				if !ok {
					errCh <- fmt.Errorf("send closed")
					return
				}
				wctx, cancel := context.WithTimeout(ctx, 3*time.Second)
				err := conn.Write(wctx, websocket.MessageText, msg)
				cancel()
				if err != nil {
					errCh <- err
					return
				}
			}
		}
	}()
	go func() {
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if c.OnMessage != nil {
				c.OnMessage(data)
			}
		}
	}()

	err = <-errCh
	c.mu.Lock()
	c.conn = nil
	c.mu.Unlock()
	_ = conn.Close(websocket.StatusNormalClosure, "")
	return err
}

func (c *MeshClient) SendJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.SendRaw(b)
}

func (c *MeshClient) SendRaw(b []byte) error {
	select {
	case c.send <- b:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

func (c *MeshClient) SendChat(text string) {
	_ = c.SendJSON(map[string]any{"type": "chat", "text": text, "from": c.Nick, "t": time.Now().UnixMilli()})
}

func (c *MeshClient) SendPTT(down bool) {
	st := "up"
	if down {
		st = "down"
	}
	_ = c.SendJSON(map[string]any{"type": "ptt", "state": st, "from": c.Nick})
}

func (c *MeshClient) SendAudio(pcm []byte) {
	_ = c.SendJSON(map[string]any{
		"type": "audio",
		"fmt":  "pcm16",
		"b64":  base64.StdEncoding.EncodeToString(pcm),
		"sr":   sampleRate,
		"ch":   channels,
		"from": c.Nick,
		"t":    time.Now().UnixMilli(),
	})
}

func (c *MeshClient) SendFrame(src string, w, h int, jpeg []byte) {
	b64 := base64.StdEncoding.EncodeToString(jpeg)
	hdr, _ := json.Marshal(map[string]any{
		"type": "frame",
		"v":    "walkie-go",
		"src":  src,
		"w":    w,
		"h":    h,
		"sz":   len(jpeg),
		"t":    float64(time.Now().UnixMilli()) / 1000,
	})
	pkt := append(hdr, '\n')
	pkt = append(pkt, []byte(b64)...)
	_ = c.SendRaw(pkt)
}

// ── Video burst (Siri-sized walkie face) ─────────────────────

func (c *MeshClient) SendBurstStart() {
	_ = c.SendJSON(map[string]any{
		"type": string(BurstStart), "from": c.Nick, "t": time.Now().UnixMilli(),
	})
}

func (c *MeshClient) SendBurstEnd() {
	_ = c.SendJSON(map[string]any{
		"type": string(BurstEnd), "from": c.Nick, "t": time.Now().UnixMilli(),
	})
}

// SendBurstFrame ships a small JPEG + optional glyph matrix brightness for Nothing Phone.
func (c *MeshClient) SendBurstFrame(jpeg []byte, w, h int, glyph []int) {
	msg := map[string]any{
		"type": string(BurstFrame),
		"from": c.Nick,
		"fmt":  "jpeg",
		"b64":  base64.StdEncoding.EncodeToString(jpeg),
		"w":    w,
		"h":    h,
		"t":    time.Now().UnixMilli(),
	}
	if len(glyph) > 0 {
		msg["glyph"] = glyph
		// N×N: 25→625, 13→169
		n := 25
		if len(glyph) <= 169 {
			n = 13
		} else if len(glyph) < 625 {
			// nearest square root (integer)
			for i := 1; i*i <= len(glyph); i++ {
				n = i
			}
		}
		msg["glyphN"] = n
	}
	_ = c.SendJSON(msg)
}

