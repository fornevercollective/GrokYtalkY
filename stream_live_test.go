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
