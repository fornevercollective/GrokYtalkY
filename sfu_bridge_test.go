package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestMapHubVburstToSfuGlyph(t *testing.T) {
	msg := map[string]any{
		"type":    "vburst-frame",
		"from":    "alice",
		"glyphN":  float64(13),
		"glyph":   []any{float64(10), float64(200), float64(40)},
		"b64":     "xx",
	}
	// pad glyph to 13×13 for realistic size
	g := make([]any, 13*13)
	for i := range g {
		g[i] = float64(i % 200)
	}
	g[0] = float64(200)
	msg["glyph"] = g

	outs := MapHubMsgToSfu(msg)
	if len(outs) != 1 {
		t.Fatalf("outs %d", len(outs))
	}
	if outs[0].Msg["type"] != "glyph" {
		t.Fatalf("%v", outs[0].Msg)
	}
	if outs[0].Msg["n"] != 13 {
		t.Fatalf("n %v", outs[0].Msg["n"])
	}
	data, ok := outs[0].Msg["data"].([]int)
	if !ok || len(data) != 13*13 {
		t.Fatalf("data type/len %T %v", outs[0].Msg["data"], outs[0].Msg["data"])
	}
	if data[0] != 200 {
		t.Fatalf("lattice/cell0 %d", data[0])
	}
	// must not be base64 string
	raw, _ := json.Marshal(outs[0].Msg)
	if strings.Contains(string(raw), `"data":"`) {
		t.Fatalf("data should be JSON array not base64: %s", raw)
	}
}

func TestMapHubGystHexlumToGlyphAndHex(t *testing.T) {
	mark := NewForgeMark(1, "dojo.pcap", []byte("bridge-seed"))
	n := 25
	lum := make([]byte, n*n)
	for i := range lum {
		lum[i] = 80
	}
	StampHexLum(lum, n, mark)
	corner := lum[0]
	if corner != 40 && corner != 200 {
		t.Fatalf("stamp setup %d", corner)
	}

	pkt := PacketFromHexLum(lum, n, 9)
	mesh := PacketToMesh(pkt, "forger")
	// ensure type is gyst
	if mesh["type"] != MeshTypeGYST {
		t.Fatal(mesh["type"])
	}

	outs := MapHubMsgToSfu(mesh)
	if len(outs) < 2 {
		t.Fatalf("want glyph+hex, got %d %v", len(outs), logsOf(outs))
	}
	var gotGlyph, gotHex bool
	for _, o := range outs {
		switch o.Msg["type"] {
		case "glyph":
			gotGlyph = true
			data, _ := o.Msg["data"].([]int)
			if len(data) != n*n {
				t.Fatalf("glyph len %d", len(data))
			}
			if data[0] != int(corner) {
				t.Fatalf("lattice lost on glyph: %d want %d", data[0], corner)
			}
			if o.Msg["n"] != n {
				t.Fatalf("n %v", o.Msg["n"])
			}
		case "hex":
			gotHex = true
			payload, _ := o.Msg["payload"].(string)
			if !strings.Contains(payload, "hexlum") {
				t.Fatalf("hex payload %q", payload)
			}
			// decode gyhex line → lattice intact
			p, err := DecodeHexLine(payload)
			if err != nil || p == nil {
				t.Fatal(err)
			}
			if p.Kind != KindHexLum || len(p.Payload) != n*n {
				t.Fatalf("hex pkt %+v len %d", p, len(p.Payload))
			}
			if p.Payload[0] != corner {
				t.Fatalf("lattice lost on hex lane: %d", p.Payload[0])
			}
		}
	}
	if !gotGlyph || !gotHex {
		t.Fatalf("glyph=%v hex=%v outs=%v", gotGlyph, gotHex, logsOf(outs))
	}
}

func TestMapHubGystForgeMarkToChat(t *testing.T) {
	mark := NewForgeMark(3, "dojo.pcap", []byte("meta"))
	mesh := mark.MeshJSON("pub")
	outs := MapHubMsgToSfu(mesh)
	if len(outs) != 1 {
		t.Fatalf("%d %v", len(outs), logsOf(outs))
	}
	if outs[0].Msg["type"] != "chat" {
		t.Fatal(outs[0].Msg)
	}
	if outs[0].Msg["role"] != "forge" {
		t.Fatal(outs[0].Msg["role"])
	}
	meta, ok := outs[0].Msg["meta"].(map[string]any)
	if !ok {
		t.Fatal("meta")
	}
	if meta["mark"] != mark.ID {
		t.Fatalf("mark %v", meta["mark"])
	}
	text, _ := outs[0].Msg["text"].(string)
	if !strings.Contains(text, "cgf:") {
		t.Fatal(text)
	}
}

func TestMapHubGystHexlumWithEmbeddedMark(t *testing.T) {
	mark := NewForgeMark(2, "dojo.pcap", []byte{9})
	lum := make([]byte, 13*13)
	StampHexLum(lum, 13, mark)
	mesh := PacketToMesh(PacketFromHexLum(lum, 13, 1), "x")
	mesh["mark"] = mark.ID
	mesh["forge"] = mark.Forge
	mesh["slot"] = float64(2)
	mesh["source"] = mark.Source
	outs := MapHubMsgToSfu(mesh)
	// glyph + hex + chat forge
	if len(outs) < 3 {
		t.Fatalf("outs %d %v", len(outs), logsOf(outs))
	}
	kinds := map[string]int{}
	for _, o := range outs {
		kinds[o.Msg["type"].(string)]++
	}
	if kinds["glyph"] != 1 || kinds["hex"] != 1 || kinds["chat"] != 1 {
		t.Fatalf("kinds %v", kinds)
	}
}

func TestMapHubIgnoresNoise(t *testing.T) {
	if outs := MapHubMsgToSfu(map[string]any{"type": "chat", "text": "hi"}); len(outs) != 0 {
		t.Fatal(outs)
	}
	if outs := MapHubMsgToSfu(map[string]any{"type": "gyst", "kind": "rgb24", "b64": "YQ=="}); len(outs) != 0 {
		// no forge mark → skip rgb24
		t.Fatal(outs)
	}
}

func TestBytesToJSONNumsNotBase64(t *testing.T) {
	b := []byte{40, 200, 40}
	nums := bytesToJSONNums(b)
	raw, _ := json.Marshal(map[string]any{"data": nums})
	var parsed struct {
		Data []int `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data[1] != 200 {
		t.Fatal(parsed.Data)
	}
	// contrast: []byte would base64
	raw2, _ := json.Marshal(map[string]any{"data": b})
	if !strings.Contains(string(raw2), `"data":"`) {
		t.Fatalf("expected base64 for []byte control: %s", raw2)
	}
	_ = base64.StdEncoding
}

func TestInferGlyphN(t *testing.T) {
	if inferGlyphN(0, 169) != 13 {
		t.Fatal(inferGlyphN(0, 169))
	}
	if inferGlyphN(25, 625) != 25 {
		t.Fatal(inferGlyphN(25, 625))
	}
	if inferGlyphN(99, 169) != 13 {
		t.Fatal("prefer square from len")
	}
}

func logsOf(outs []sfuBridgeOut) []string {
	var s []string
	for _, o := range outs {
		s = append(s, o.Log)
	}
	return s
}

func TestBuildSfuWSURL(t *testing.T) {
	u := BuildSfuWSURL("127.0.0.1:9880", "dojo", "bridge", "secret")
	if !strings.Contains(u, "token=secret") || !strings.Contains(u, "room=dojo") {
		t.Fatal(u)
	}
	if !strings.HasPrefix(u, "ws://") {
		t.Fatal(u)
	}
	u2 := BuildSfuWSURL("ws://host:9/ws", "r", "n", "")
	if strings.Contains(u2, "token=") {
		t.Fatal(u2)
	}
}

func TestMapSfuGlyphToHub(t *testing.T) {
	g := make([]any, 13*13)
	for i := range g {
		g[i] = float64(i % 100)
	}
	outs := MapSfuMsgToHub(map[string]any{
		"type": "glyph", "n": float64(13), "data": g, "from": "browser",
	})
	if len(outs) != 1 || outs[0].Msg["type"] != "vburst-frame" {
		t.Fatalf("%+v", outs)
	}
	if outs[0].Msg["glyphN"] != 13 {
		t.Fatal(outs[0].Msg)
	}
	// loop guard
	if outs2 := MapSfuMsgToHub(map[string]any{
		"type": "glyph", "n": float64(13), "data": g, "from": "sfu-bridge",
	}); len(outs2) != 0 {
		t.Fatal("echo")
	}
}

func TestMapSfuChatToHub(t *testing.T) {
	outs := MapSfuMsgToHub(map[string]any{
		"type": "chat", "text": "hi dojo", "from": "alice", "nick": "alice",
	})
	if len(outs) != 1 || outs[0].Msg["type"] != "chat" {
		t.Fatal(outs)
	}
}

func TestMapSfuHexToHub(t *testing.T) {
	lum := make([]byte, 13*13)
	pkt := PacketFromHexLum(lum, 13, 1)
	line := EncodeHexLine(pkt)
	outs := MapSfuMsgToHub(map[string]any{
		"type": "hex", "payload": line, "from": "peer",
	})
	if len(outs) != 1 {
		t.Fatal(outs)
	}
	if outs[0].Msg["type"] != MeshTypeGYST && outs[0].Msg["type"] != "gyst" {
		// PacketToMesh type
		t.Logf("type %v", outs[0].Msg["type"])
	}
}

func TestGenerateSfuToken(t *testing.T) {
	a, b := GenerateSfuToken(), GenerateSfuToken()
	if a == "" || a == b {
		t.Fatalf("%q %q", a, b)
	}
	if len(a) < 16 {
		t.Fatal(a)
	}
}

func TestRedactTokenURL(t *testing.T) {
	r := redactTokenURL("ws://127.0.0.1:9880/ws?room=dojo&token=sekrit")
	if strings.Contains(r, "sekrit") {
		t.Fatal(r)
	}
	// Query.Encode may percent-escape ***
	if !strings.Contains(r, "***") && !strings.Contains(r, "%2A%2A%2A") {
		t.Fatal(r)
	}
}

func TestFormatSfuDoctor(t *testing.T) {
	doc := FormatSfuDoctor()
	if !strings.Contains(doc, "sfu") || !strings.Contains(doc, "glyph") {
		t.Fatal(doc)
	}
}
