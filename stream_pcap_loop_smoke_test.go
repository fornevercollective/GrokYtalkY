package main

import (
  "context"
  "encoding/json"
  "path/filepath"
  "testing"
  "time"

  "github.com/coder/websocket"
)

func TestColossusPcapLoopSmoke(t *testing.T) {
  if testing.Short() {
    t.Skip("short")
  }
  dir := t.TempDir()
  path := filepath.Join(dir, "dojo.pcap")
  t0 := uint64(time.Now().UnixMilli())
  var pkts []StreamPacket
  for i := 0; i < 8; i++ {
    lum := make([]byte, 13*13)
    for j := range lum {
      lum[j] = byte((i*7 + j) % 200)
    }
    p := PacketFromHexLum(lum, 13, uint32(i+1))
    p.TimeMS = t0 + uint64(i)*50
    pkts = append(pkts, p)
  }
  if err := WritePCAP(path, pkts); err != nil {
    t.Fatal(err)
  }

  // hub
  h := NewHub("127.0.0.1:0", true, "")
  // need actual listen - use fixed port
  port := "19991"
  hub := NewHub("127.0.0.1:"+port, true, "")
  go func() { _ = hub.ListenAndServe() }()
  defer hub.Close()
  time.Sleep(150 * time.Millisecond)
  _ = h

  ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
  defer cancel()

  // viewer
  got := make(chan map[string]any, 4)
  go func() {
    c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+port+"/?role=peer&nick=viewer", nil)
    if err != nil {
      return
    }
    defer c.Close(websocket.StatusNormalClosure, "")
    _ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"join","nick":"viewer"}`))
    for {
      _, data, err := c.Read(ctx)
      if err != nil {
        return
      }
      var m map[string]any
      if json.Unmarshal(data, &m) != nil {
        continue
      }
      if m["type"] == "gyst" {
        select {
        case got <- m:
        default:
        }
      }
    }
  }()
  time.Sleep(100 * time.Millisecond)

  // publisher (two loops max via max-sec)
  pubCtx, pubCancel := context.WithTimeout(ctx, 2*time.Second)
  defer pubCancel()
  go func() {
    _ = RunStreamPub(StreamPubOpts{
      Src: path, Hub: "127.0.0.1:" + port, Nick: "colossus",
      Kind: "auto", FPS: 20, Loop: true, Quiet: true, Pace: "ts",
      HexN: 13, Colossus: true,
    })
  }()

  select {
  case m := <-got:
    if m["kind"] != "hexlum" {
      t.Fatalf("kind %v", m["kind"])
    }
    t.Logf("colossus pcap loop OK kind=%v w=%v seq=%v", m["kind"], m["w"], m["seq"])
  case <-time.After(3 * time.Second):
    t.Fatal("timeout waiting for gyst from pcap loop")
  }
  _ = pubCtx
}
