package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGYSTRoundTrip(t *testing.T) {
	rgb := make([]byte, 8*6*3)
	for i := range rgb {
		rgb[i] = byte(i)
	}
	p := PacketFromRGB(rgb, 8, 6, 1, 1000)
	var buf bytes.Buffer
	if err := EncodeBinary(&buf, p); err != nil {
		t.Fatal(err)
	}
	out, err := DecodeBinary(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != KindRGB24 || out.Width != 8 || out.Height != 6 {
		t.Fatalf("%+v", out)
	}
	if len(out.Payload) != len(rgb) {
		t.Fatal(len(out.Payload))
	}
	f, err := FrameFromPacket(out)
	if err != nil || f.W != 8 {
		t.Fatal(err, f)
	}
}

func TestGyHexRoundTrip(t *testing.T) {
	p := PacketFromRGB([]byte{1, 2, 3, 4, 5, 6}, 1, 2, 7, 99)
	line := EncodeHexLine(p)
	q, err := DecodeHexLine(line)
	if err != nil || q == nil {
		t.Fatal(err)
	}
	if q.Seq != 7 || q.Kind != KindRGB24 {
		t.Fatalf("%+v", q)
	}
}

func TestFileFormats(t *testing.T) {
	dir := t.TempDir()
	rgb := make([]byte, 4*4*3)
	pkts := []StreamPacket{PacketFromRGB(rgb, 4, 4, 1, 1)}

	gyst := filepath.Join(dir, "t.gyst")
	if err := WriteGystFile(gyst, pkts); err != nil {
		t.Fatal(err)
	}
	got, err := ReadGystFile(gyst)
	if err != nil || len(got) != 1 {
		t.Fatal(err, len(got))
	}

	gyhex := filepath.Join(dir, "t.gyhex")
	if err := WriteGyHexFile(gyhex, pkts, map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	got, _, err = ReadGyHexFile(gyhex)
	if err != nil || len(got) != 1 {
		t.Fatal(err)
	}

	pcap := filepath.Join(dir, "t.pcap")
	if err := WritePCAP(pcap, pkts); err != nil {
		t.Fatal(err)
	}
	got, err = ReadPCAP(pcap)
	if err != nil || len(got) != 1 {
		t.Fatal(err, len(got))
	}

	if DetectStreamFile(gyst) != "gyst" {
		t.Fatal(DetectStreamFile(gyst))
	}
	if DetectStreamFile(gyhex) != "gyhex" {
		t.Fatal(DetectStreamFile(gyhex))
	}
	if DetectStreamFile(pcap) != "pcap" {
		t.Fatal(DetectStreamFile(pcap))
	}
}

func TestOverviewHexJSON(t *testing.T) {
	lum := make([]byte, 16)
	for i := range lum {
		lum[i] = byte(i * 10)
	}
	b, err := EncodeOverviewHexJSON(lum, 4, "gray", "cam")
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeOverviewHexJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindHexLum || int(p.Width) != 4 {
		t.Fatalf("%+v", p)
	}
	f, err := FrameFromPacket(p)
	if err != nil || f.W != 4 {
		t.Fatal(err)
	}
}

func TestLoadStreamFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.gyst")
	_ = WriteGystFile(p, []StreamPacket{PacketFromRGB(make([]byte, 12), 2, 2, 1, 1)})
	pkts, err := LoadStreamFile(p)
	if err != nil || len(pkts) != 1 {
		t.Fatal(err)
	}
	// missing
	if _, err := LoadStreamFile(filepath.Join(dir, "nope.gyst")); err == nil {
		t.Fatal("want err")
	}
	_ = os.ErrNotExist
}
