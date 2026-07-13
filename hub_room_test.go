package main

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNormalizeMeshRoom(t *testing.T) {
	if NormalizeMeshRoom("") != DefaultMeshRoom {
		t.Fatal("empty")
	}
	if NormalizeMeshRoom("  US-CA-SF ") != "us-ca-sf" {
		t.Fatal(NormalizeMeshRoom("  US-CA-SF "))
	}
	if NormalizeMeshRoom("a/b c") != "a-b-c" {
		t.Fatal(NormalizeMeshRoom("a/b c"))
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	return port
}

func dialHub(t *testing.T, port, nick, room string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	u := "ws://127.0.0.1:" + port + "/?nick=" + nick + "&role=peer&room=" + room
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(websocket.StatusNormalClosure, "") })
	return c
}

func readJSON(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func drainUntil(t *testing.T, c *websocket.Conn, typ string, max int) map[string]any {
	t.Helper()
	for i := 0; i < max; i++ {
		m := readJSON(t, c)
		if m["type"] == typ {
			return m
		}
	}
	t.Fatalf("no message type %s", typ)
	return nil
}

func TestHubRoomIsolationChat(t *testing.T) {
	port := freePort(t)
	h := NewHub("127.0.0.1:"+port, true, "")
	go func() { _ = h.ListenAndServe() }()
	defer h.Close()
	time.Sleep(50 * time.Millisecond)

	a := dialHub(t, port, "alice", "room-a")
	b := dialHub(t, port, "bob", "room-b")
	// drain hello/roster/join noise
	_ = drainUntil(t, a, "hello", 5)
	_ = drainUntil(t, b, "hello", 5)

	// alice chat in room-a
	ctx := context.Background()
	_ = a.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
		"type": "chat", "text": "only-a", "from": "alice",
	}))

	// bob must NOT receive only-a (wait briefly)
	ctx2, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, data, err := b.Read(ctx2)
	if err == nil {
		var m map[string]any
		_ = json.Unmarshal(data, &m)
		if m["type"] == "chat" && m["text"] == "only-a" {
			t.Fatal("cross-room chat leak")
		}
	}

	// peer in same room receives
	a2 := dialHub(t, port, "ally", "room-a")
	_ = drainUntil(t, a2, "hello", 8)
	_ = a.Write(ctx, websocket.MessageText, mustJSON(map[string]any{
		"type": "chat", "text": "hello-a", "from": "alice",
	}))
	// ally may get join/roster first
	for i := 0; i < 12; i++ {
		m := readJSON(t, a2)
		if m["type"] == "chat" && m["text"] == "hello-a" {
			return
		}
	}
	t.Fatal("same-room chat not delivered")
}

func TestHubProgramPerRoom(t *testing.T) {
	port := freePort(t)
	h := NewHub("127.0.0.1:"+port, true, "")
	go func() { _ = h.ListenAndServe() }()
	defer h.Close()
	time.Sleep(50 * time.Millisecond)

	dirA := dialHub(t, port, "dir-a", "stage-a")
	dirB := dialHub(t, port, "dir-b", "stage-b")
	_ = drainUntil(t, dirA, "hello", 5)
	_ = drainUntil(t, dirB, "hello", 5)

	bus := NewProgramBus()
	bus.Take(ProgramSource{Source: ProgramSourceGyst, Nick: "cam-a", Label: "A"}, "dir-a")
	bus.SetCaption("CAPTION-A", "dir-a")
	msg := bus.MeshJSON("dir-a")
	raw, _ := json.Marshal(msg)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["type"] = "program"
	m["room"] = "stage-a"
	ctx := context.Background()
	_ = dirA.Write(ctx, websocket.MessageText, mustJSON(m))

	// late joiner to stage-a gets program
	late := dialHub(t, port, "viewer", "stage-a")
	var gotProg map[string]any
	for i := 0; i < 15; i++ {
		msg := readJSON(t, late)
		if msg["type"] == "program" {
			gotProg = msg
			break
		}
	}
	if gotProg == nil {
		t.Fatal("late join missing program for stage-a")
	}
	busGot, ok := ParseProgramBus(gotProg)
	if !ok || busGot.Caption != "CAPTION-A" {
		t.Fatalf("program bus %+v ok=%v", busGot, ok)
	}

	// stage-b must not have that program on join
	lateB := dialHub(t, port, "viewer-b", "stage-b")
	for i := 0; i < 8; i++ {
		ctx2, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		_, data, err := lateB.Read(ctx2)
		cancel()
		if err != nil {
			break
		}
		var msg map[string]any
		_ = json.Unmarshal(data, &msg)
		if msg["type"] == "program" {
			if b, ok := ParseProgramBus(msg); ok && b.Caption == "CAPTION-A" {
				t.Fatal("program leaked across rooms")
			}
		}
	}
	_ = dirB
}

func TestHubRoomListAPI(t *testing.T) {
	port := freePort(t)
	h := NewHub("127.0.0.1:"+port, true, "")
	go func() { _ = h.ListenAndServe() }()
	defer h.Close()
	time.Sleep(40 * time.Millisecond)
	_ = dialHub(t, port, "x", "dojo")
	time.Sleep(40 * time.Millisecond)
	rooms := h.roomList()
	found := false
	for _, r := range rooms {
		if r.ID == "dojo" && r.Peers >= 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("rooms=%+v", rooms)
	}
}
