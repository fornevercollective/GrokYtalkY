package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestST20227FromURLs(t *testing.T) {
	c := ST20227FromURLs("rtp://239.1.1.1:5004", "")
	if c.Enabled {
		t.Fatal("secondary empty")
	}
	c = ST20227FromURLs("rtp://239.1.1.1:5004", "rtp://239.1.2.1:5004")
	if !c.Enabled || c.Mode != "tee" {
		t.Fatal(c)
	}
	line := FormatST20227Line(c)
	if !strings.Contains(line, "dual-dest") {
		t.Fatal(line)
	}
}

func TestFFmpegTeeRTPPayload(t *testing.T) {
	s := ffmpegTeeRTPPayload("rtp://a:1", "rtp://b:2", 96)
	if !strings.Contains(s, "rtp://a:1") || !strings.Contains(s, "rtp://b:2") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "payload_type=96") || !strings.Contains(s, "|") {
		t.Fatal(s)
	}
}

func TestAppendST20227SDPAttrs(t *testing.T) {
	body := "v=0\na=tool:x\nm=video 5004 RTP/AVP 96\n"
	c := ST20227FromURLs("rtp://a:1", "rtp://b:2")
	out := AppendST20227SDPAttrs(body, c)
	if !strings.Contains(out, "x-gy-2022-7:enabled=1") {
		t.Fatal(out)
	}
	// attrs before m=
	if strings.Index(out, "x-gy-2022-7") > strings.Index(out, "m=video") {
		t.Fatal("expected 2022-7 attrs before m=")
	}
}

func TestBuildST211020ArgsWithTee(t *testing.T) {
	p := DefaultST211020Params(64, 36, 30)
	args := buildST211020FFmpegArgsFromParams(p, "rtp://127.0.0.1:5004", "rtp://127.0.0.1:5005")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-f tee") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "5004") || !strings.Contains(joined, "5005") {
		t.Fatal(joined)
	}
}

func TestST2110With20227SDP(t *testing.T) {
	dir := t.TempDir()
	sdp := filepath.Join(dir, "h.sdp")
	s, err := NewST2110VenueSink(ST2110Opts{
		RTP: "rtp://127.0.0.1:5004", RTPB: "rtp://127.0.0.1:5005",
		SDPPath: sdp, MetaDir: dir,
		Width: 64, Height: 36, FPS: 15, Quiet: true,
		Profile: ST2110Profile211020,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !strings.Contains(s.Name(), "2022-7") {
		t.Fatal(s.Name())
	}
	b, _ := os.ReadFile(sdp)
	body := string(b)
	if !strings.Contains(body, "x-gy-2022-7:enabled=1") {
		t.Fatal(body)
	}
	side := filepath.Join(dir, "st2022-7.json")
	if _, err := os.Stat(side); err != nil {
		t.Fatal("sidecar", err)
	}
}

func TestST20227Basis(t *testing.T) {
	s := FormatST20227Basis()
	if !strings.Contains(s, "2022-7") || !strings.Contains(s, "hitless") {
		t.Fatal(s)
	}
}
