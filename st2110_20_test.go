package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestST211020FmtpTight(t *testing.T) {
	p := DefaultST211020Params(1920, 1080, 30)
	p.TP = ST2110TPN
	p.TCS = TCSSDR
	p.RANGE = RangeNARROW
	f := p.Fmtp()
	for _, want := range []string{
		"sampling=YCbCr-4:2:2",
		"width=1920",
		"height=1080",
		"exactframerate=30",
		"depth=8",
		"colorimetry=BT709",
		"TCS=SDR",
		"RANGE=NARROW",
		"PM=2110GPM",
		"SSN=ST2110-20:2017",
		"TP=2110TPN",
		"interlace=0",
		"PAR=1:1",
	} {
		if !strings.Contains(f, want) {
			t.Fatalf("fmtp missing %q in %s", want, f)
		}
	}
}

func TestParseExactFPS(t *testing.T) {
	n, d, err := ParseExactFPS("30000/1001")
	if err != nil || n != 30000 || d != 1001 {
		t.Fatal(n, d, err)
	}
	n, d, err = ParseExactFPS("29.97")
	if err != nil || n != 30000 || d != 1001 {
		t.Fatal(n, d, err)
	}
	n, d, err = ParseExactFPS("30")
	if err != nil || n != 30 || d != 1 {
		t.Fatal(n, d, err)
	}
	p := DefaultST211020Params(1280, 720, 30)
	p.FPSNum, p.FPSDen = 30000, 1001
	if p.ExactFramerate() != "30000/1001" {
		t.Fatal(p.ExactFramerate())
	}
}

func TestNormalizeTP(t *testing.T) {
	if NormalizeTP("tpn") != ST2110TPN {
		t.Fatal(NormalizeTP("tpn"))
	}
	if NormalizeTP("TPW") != ST2110TPW {
		t.Fatal(NormalizeTP("TPW"))
	}
}

func TestPixFmtDepth10(t *testing.T) {
	p := DefaultST211020Params(1280, 720, 30)
	p.Depth = 10
	if p.PixFmt() != "v210" {
		t.Fatal(p.PixFmt())
	}
	p.Sampling = "YCbCr-4:2:0"
	if p.PixFmt() != "yuv420p10le" {
		t.Fatal(p.PixFmt())
	}
}

func TestWriteST211020SDPTight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20.sdp")
	p := DefaultST211020Params(1280, 720, 30)
	p.FPSNum, p.FPSDen = 30000, 1001
	p.TP = ST2110TPNL
	if err := WriteST211020SDPTight(path, "239.1.1.1", 5004, p, DefaultSyncClockReport()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	for _, want := range []string{
		"exactframerate=30000/1001",
		"TP=2110TPNL",
		"TCS=SDR",
		"RANGE=NARROW",
		"x-gy-2110-21:TP=2110TPNL",
		"x-gy-pixfmt:uyvy422",
		"raw/90000",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestBuildVenueSink211020TightFlags(t *testing.T) {
	dir := t.TempDir()
	sdp := filepath.Join(dir, "t.sdp")
	s, err := BuildVenueSink(VenueOpts{
		SinkKind: "st2110", Quiet: true,
		RTP: "rtp://127.0.0.1:5004", SDPPath: sdp,
		Width: 64, Height: 36, FPS: 30,
		ST2110Prof:     ST2110Profile211020,
		ST2110TP:       "TPN",
		ST2110FPSExact: "29.97",
		ST2110Sampling: "YCbCr-4:2:2",
		ST2110Depth:    8,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Name() != "st2110-20" {
		t.Fatal(s.Name())
	}
	body, _ := os.ReadFile(sdp)
	if !strings.Contains(string(body), "30000/1001") {
		t.Fatal(string(body))
	}
}
