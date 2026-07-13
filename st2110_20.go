package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// ST 2110-20 video parameter object — SDP fmtp + ffmpeg mapping.
// Tightens essence signaling per ST 2110-20 / ST 2110-21 conventions.
// Does not implement hardware packetizers; signals sender type for receivers.

// ST 2110-21 traffic / sender types (TP= in fmtp).
const (
	ST2110TPN  = "2110TPN"  // narrow — linear send, software typical
	ST2110TPNL = "2110TPNL" // narrow linear
	ST2110TPW  = "2110TPW"  // wide — burstier; not claimed by gy software
)

// ST 2110-20 packing mode.
const (
	ST2110PMGPM = "2110GPM" // general packing mode
	ST2110PMBPM = "2110BPM" // block packing mode
)

// Colorimetry / transfer / range tokens used in 2110-20 fmtp.
const (
	ColorBT709  = "BT709"
	ColorBT2020 = "BT2020"
	TCSSDR      = "SDR"
	TCSHLG      = "HLG"
	TCSPQ       = "PQ"
	RangeNARROW = "NARROW"
	RangeFULL   = "FULL"
)

// ST211020Params is the tightened 2110-20 video description.
type ST211020Params struct {
	Width      int
	Height     int
	FPSNum     int    // e.g. 30 or 30000
	FPSDen     int    // e.g. 1 or 1001
	Sampling   string // YCbCr-4:2:2 | YCbCr-4:2:0 | YCbCr-4:4:4
	Depth      int    // 8 | 10
	Colorimetry string
	TCS        string // SDR | HLG | PQ
	RANGE      string // NARROW | FULL
	PM         string // 2110GPM | 2110BPM
	TP         string // 2110TPN | 2110TPNL | 2110TPW
	SSN        string // ST2110-20:2017
	Interlace  bool
	PAR        string // pixel aspect 1:1 default
	// Optional ST 2110-21-ish bandwidth notes (informational in x-gy attrs)
	MaxUDP int // bytes; 0 = omit
}

// DefaultST211020Params 1280×720p30 4:2:2 8-bit BT709 SDR narrow TPN.
func DefaultST211020Params(w, h, fps int) ST211020Params {
	if w < 16 {
		w = VenueDefaultW
	}
	if h < 16 {
		h = VenueDefaultH
	}
	if fps < 1 {
		fps = VenueDefaultFPS
	}
	return ST211020Params{
		Width: w, Height: h,
		FPSNum: fps, FPSDen: 1,
		Sampling:    "YCbCr-4:2:2",
		Depth:       8,
		Colorimetry: ColorBT709,
		TCS:         TCSSDR,
		RANGE:       RangeNARROW,
		PM:          ST2110PMGPM,
		TP:          ST2110TPN,
		SSN:         "ST2110-20:2017",
		Interlace:   false,
		PAR:         "1:1",
	}
}

// ExactFramerate returns ST 2110 exactframerate token (integer or N/D).
func (p ST211020Params) ExactFramerate() string {
	if p.FPSDen <= 1 {
		return strconv.Itoa(max(1, p.FPSNum))
	}
	return fmt.Sprintf("%d/%d", p.FPSNum, p.FPSDen)
}

// FPSFloat approximate fps for ptime / ffmpeg -r.
func (p ST211020Params) FPSFloat() float64 {
	den := p.FPSDen
	if den < 1 {
		den = 1
	}
	num := p.FPSNum
	if num < 1 {
		num = 30
	}
	return float64(num) / float64(den)
}

// FFmpegRateArg for -r flag.
func (p ST211020Params) FFmpegRateArg() string {
	if p.FPSDen > 1 {
		return fmt.Sprintf("%d/%d", p.FPSNum, p.FPSDen)
	}
	return strconv.Itoa(max(1, p.FPSNum))
}

// PixFmt maps sampling+depth → ffmpeg pix_fmt for rawvideo.
func (p ST211020Params) PixFmt() string {
	s := strings.ToUpper(p.Sampling)
	if p.Depth >= 10 {
		if strings.Contains(s, "4:2:0") {
			return "yuv420p10le"
		}
		if strings.Contains(s, "4:4:4") {
			return "gbrp10le" // approximate; facility may prefer v210 for 422
		}
		return "v210" // 10-bit 4:2:2 packed — common in broadcast
	}
	if strings.Contains(s, "4:2:0") {
		return "yuv420p"
	}
	if strings.Contains(s, "4:4:4") {
		return "gbrp"
	}
	return "uyvy422"
}

// Fmtp builds a=fmtp:<pt> value for ST 2110-20.
func (p ST211020Params) Fmtp() string {
	if p.Sampling == "" {
		p.Sampling = "YCbCr-4:2:2"
	}
	if p.Depth < 8 {
		p.Depth = 8
	}
	if p.Colorimetry == "" {
		p.Colorimetry = ColorBT709
	}
	if p.TCS == "" {
		p.TCS = TCSSDR
	}
	if p.RANGE == "" {
		p.RANGE = RangeNARROW
	}
	if p.PM == "" {
		p.PM = ST2110PMGPM
	}
	if p.TP == "" {
		p.TP = ST2110TPN
	}
	if p.SSN == "" {
		p.SSN = "ST2110-20:2017"
	}
	if p.PAR == "" {
		p.PAR = "1:1"
	}
	il := 0
	if p.Interlace {
		il = 1
	}
	parts := []string{
		fmt.Sprintf("sampling=%s", p.Sampling),
		fmt.Sprintf("width=%d", p.Width),
		fmt.Sprintf("height=%d", p.Height),
		fmt.Sprintf("exactframerate=%s", p.ExactFramerate()),
		fmt.Sprintf("depth=%d", p.Depth),
		fmt.Sprintf("colorimetry=%s", p.Colorimetry),
		fmt.Sprintf("TCS=%s", p.TCS),
		fmt.Sprintf("RANGE=%s", p.RANGE),
		fmt.Sprintf("PM=%s", p.PM),
		fmt.Sprintf("SSN=%s", p.SSN),
		fmt.Sprintf("TP=%s", p.TP),
		fmt.Sprintf("interlace=%d", il),
		"segmented=0",
		fmt.Sprintf("PAR=%s", p.PAR),
	}
	return strings.Join(parts, "; ")
}

// NormalizeTP maps CLI aliases to ST 2110-21 tokens.
func NormalizeTP(tp string) string {
	switch strings.ToUpper(strings.TrimSpace(tp)) {
	case "", "TPN", "N", "NARROW", ST2110TPN:
		return ST2110TPN
	case "TPNL", "NL", "NARROWLINEAR", ST2110TPNL:
		return ST2110TPNL
	case "TPW", "W", "WIDE", ST2110TPW:
		return ST2110TPW
	default:
		return ST2110TPN
	}
}

// ParseExactFPS parses "30", "29.97", "30000/1001" into num/den.
func ParseExactFPS(s string) (num, den int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 30, 1, nil
	}
	if strings.Contains(s, "/") {
		parts := strings.SplitN(s, "/", 2)
		num, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return 0, 0, err
		}
		den, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || den < 1 {
			return 0, 0, fmt.Errorf("bad fps den")
		}
		return num, den, nil
	}
	// decimal
	if strings.Contains(s, ".") {
		switch s {
		case "29.97", "29.970":
			return 30000, 1001, nil
		case "59.94", "59.940":
			return 60000, 1001, nil
		case "23.976", "23.98":
			return 24000, 1001, nil
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, err
	}
	if n < 1 {
		n = 30
	}
	return n, 1, nil
}

// WriteST211020SDPTight writes tightened 2110-20 SDP from params + sync.
// Optional hitless dual-dest via ST20227Config (appends x-gy-2022-7 attrs).
func WriteST211020SDPTight(path, host string, port int, p ST211020Params, sync SyncClockReport) error {
	return WriteST211020SDPTightEx(path, host, port, p, sync, ST20227Config{})
}

// WriteST211020SDPTightEx same as Tight with optional 2022-7 dual-path announce.
func WriteST211020SDPTightEx(path, host string, port int, p ST211020Params, sync SyncClockReport, hitless ST20227Config) error {
	if p.Width < 1 {
		p = DefaultST211020Params(p.Width, p.Height, int(p.FPSFloat()))
	}
	now := time.Now().Unix()
	fr := p.ExactFramerate()
	fmtp := p.Fmtp()
	tsRef := "localmac=00-00-00-00-00-00"
	if sync.PTP.Mode == PTPLocked || sync.PTP.Mode == PTPSlave {
		tsRef = fmt.Sprintf("ptp=IEEE1588-2008:traceable:domain-number=%d", sync.PTP.Domain)
	}
	// ptime ≈ 1/fps in ms for progressive
	ptMs := 1000.0 / p.FPSFloat()
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-20 PGM
i=ST 2110-20 uncompressed active video %dx%d@%s %s depth=%d. TP=%s (ST 2110-21 sender type). Lattice nearest-neighbor; program bus mark in st2110-program.json. PTP profile %s; compliant=%v.
c=IN IP4 %s/32
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-profile:2110-20
a=x-gy-2110-21:TP=%s;note=software-TPN-no-hardware-shaper
a=x-gy-program-meta:st2110-program.json
a=x-gy-lattice:pass-through
a=x-gy-pixfmt:%s
a=ts-refclk:%s
a=mediaclk:direct=0
a=source-filter: incl IN IP4 * %s
m=video %d RTP/AVP 96
a=rtpmap:96 raw/90000
a=fmtp:96 %s
a=framesize:96 %d-%d
a=framerate:%s
a=recvonly
a=ptime:%.3f
`, now, now, host,
		p.Width, p.Height, fr, p.Sampling, p.Depth, p.TP, PTPProfileST2059, sync.Compliant,
		host, Version, p.TP, p.PixFmt(), tsRef, host, port, fmtp,
		p.Width, p.Height, fr, ptMs)
	body = AppendST20227SDPAttrs(body, hitless)
	// secondary media connection note for receivers scanning dual dest
	if hitless.Enabled {
		if _, p2, err := parseRTPURL(hitless.Secondary); err == nil {
			_ = p2
			body += fmt.Sprintf("a=x-gy-2022-7-secondary-port:%d\n", p2)
		}
	}
	return os.WriteFile(path, []byte(body), 0o644)
}
