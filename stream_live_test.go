package main

import (
	"encoding/json"
	"testing"
)

func TestPacketToMeshRoundTrip(t *testing.T) {
	rgb := make([]byte, 8*6*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	p := PacketFromRGB(rgb, 8, 6, 3, 1000)
	msg := PacketToMesh(p, "colossus")
	if msg["type"] != MeshTypeGYST {
		t.Fatal(msg["type"])
	}
	// JSON cycle like hub
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	got, err := MeshToPacket(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindRGB24 || got.Width != 8 || got.Height != 6 {
		t.Fatalf("%+v", got)
	}
	if len(got.Payload) != len(rgb) {
		t.Fatalf("payload %d", len(got.Payload))
	}
	fp, err := FrameFromPacket(got)
	if err != nil || fp.W != 8 {
		t.Fatal(err, fp)
	}
}

func TestHexLumMeshAndDownsample(t *testing.T) {
	rgb := make([]byte, 40*40*3)
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			i := (y*40 + x) * 3
			rgb[i] = 200
			rgb[i+1] = 40
			rgb[i+2] = 80
		}
	}
	lum := RGBToHexLum(rgb, 40, 40, 25)
	if len(lum) != 625 {
		t.Fatalf("len %d", len(lum))
	}
	p := PacketFromHexLum(lum, 25, 1)
	msg := PacketToMesh(p, "dojo")
	if msg["data"] == nil {
		t.Fatal("expected data[] for hexlum")
	}
	b, _ := json.Marshal(msg)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	got, err := MeshToPacket(m)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindHexLum {
		t.Fatal(got.KindName())
	}
	fp, err := FrameFromPacket(got)
	if err != nil || fp.W != 25 {
		t.Fatal(err, fp)
	}
}

func TestPublishPacketDualHex(t *testing.T) {
	// dual pub builds vburst glyph only for hexlum — pure unit via PacketToMesh
	lum := make([]byte, 25)
	for i := range lum {
		lum[i] = byte(i * 10)
	}
	p := PacketFromHexLum(lum, 5, 1)
	msg := PacketToMesh(p, "pub")
	if msg["type"] != MeshTypeGYST || msg["kind"] != "hexlum" {
		t.Fatalf("%v", msg)
	}
	if msg["data"] == nil {
		t.Fatal("hexlum data[]")
	}
}

func TestGystB64Blob(t *testing.T) {
	p := PacketFromHexLum([]byte{1, 2, 3, 4}, 2, 9)
	s, err := EncodeGystB64(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeGystB64(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Seq != 9 || got.Kind != KindHexLum {
		t.Fatalf("%+v", got)
	}
}

func TestPacketTimelineAndTransform(t *testing.T) {
	pkts := []StreamPacket{
		{Kind: KindHexLum, Width: 13, Height: 13, TimeMS: 1000, Payload: make([]byte, 169)},
		{Kind: KindHexLum, Width: 13, Height: 13, TimeMS: 1083, Payload: make([]byte, 169)},
		{Kind: KindHexLum, Width: 13, Height: 13, TimeMS: 1166, Payload: make([]byte, 169)},
	}
	if !packetTimelineUseful(pkts) {
		t.Fatal("expected useful timeline")
	}
	flat := []StreamPacket{
		{Kind: KindHexLum, TimeMS: 0, Payload: []byte{1}},
		{Kind: KindHexLum, TimeMS: 0, Payload: []byte{2}},
	}
	if packetTimelineUseful(flat) {
		t.Fatal("zero timestamps not useful")
	}
	rgb := make([]byte, 20*20*3)
	p := PacketFromRGB(rgb, 20, 20, 1, 0)
	out := transformPubPacket(p, StreamPubOpts{Kind: "hexlum", HexN: 13}, 7)
	if out.Kind != KindHexLum || out.Seq != 7 || len(out.Payload) != 169 {
		t.Fatalf("%s seq=%d len=%d", out.KindName(), out.Seq, len(out.Payload))
	}
	// auto keeps kind
	keep := transformPubPacket(p, StreamPubOpts{Kind: "auto"}, 2)
	if keep.Kind != KindRGB24 {
		t.Fatal(keep.KindName())
	}
}

func TestPcapRoundTripLoad(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/loop.pcap"
	var pkts []StreamPacket
	t0 := uint64(1_700_000_000_000)
	for i := 0; i < 6; i++ {
		lum := make([]byte, 13*13)
		for j := range lum {
			lum[j] = byte((i*10 + j) % 255)
		}
		pkts = append(pkts, PacketFromHexLum(lum, 13, uint32(i+1)))
		pkts[i].TimeMS = t0 + uint64(i)*100
	}
	if err := WritePCAP(path, pkts); err != nil {
		t.Fatal(err)
	}
	got, err := LoadStreamFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Kind != KindHexLum {
		t.Fatal(got[0].KindName())
	}
	if !packetTimelineUseful(got) {
		// TimeMS may be preserved through pcap
		t.Log("timeline from pcap:", got[0].TimeMS, got[1].TimeMS)
	}
}
