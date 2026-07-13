package main

import (
	"fmt"
	"strings"
	"time"
)

// SMPTE ST 2110 suite coverage for venue adapters.
// Spec references are structural (not a license to redistribute SMPTE text).
//
// Essence family:
//   ST 2110-10  system timing & definitions (PTP / ST 2059)
//   ST 2110-20  uncompressed active video
//   ST 2110-21  traffic shaping / sender types (TPN/TPNL/TPW)
//   ST 2110-22  constant bit-rate compressed video
//   ST 2110-30  PCM digital audio (AES67 constrained)
//   ST 2110-31  AES3 transparent
//   ST 2110-40  ancillary data (ANC / captions / metadata)
//
// Sync:
//   ST 2059-1   alignment of essence to PTP epoch
//   ST 2059-2   PTP profile for professional media (required by 2110)

// ST2110 essence / system identifiers.
const (
	ST2110_10 = "ST 2110-10" // system + timing
	ST2110_20 = "ST 2110-20" // video
	ST2110_21 = "ST 2110-21" // traffic shaping
	ST2110_22 = "ST 2110-22" // CBR compressed video
	ST2110_30 = "ST 2110-30" // PCM audio
	ST2110_31 = "ST 2110-31" // AES3
	ST2110_40 = "ST 2110-40" // ANC
	ST2059_1  = "ST 2059-1"  // essence ↔ PTP
	ST2059_2  = "ST 2059-2"  // PTP media profile
)

// PTPProfileST2059 is the PTP profile ST 2110 requires (vs AES67 default).
const PTPProfileST2059 = "SMPTE ST 2059-2"

// ST2110_30 conformance levels (packet time × channel density).
// Level A: 1 ms ptime, ≤8 ch @ 48 kHz typical narrow path.
// Level B: 125 µs ptime options / higher density (facility).
// Level C: larger packs (gateway).
const (
	ST211030LevelA = "A" // 1 ms, common
	ST211030LevelB = "B"
	ST211030LevelC = "C"
)

// PTPMode describes sender clock participation.
type PTPMode string

const (
	PTPFreeRun PTPMode = "free-run" // no PTP — not 2110-compliant for production
	PTPSlave   PTPMode = "slave"    // follower-only (typical for gy venue sender)
	PTPMaster  PTPMode = "master"   // GM / boundary (facility)
	PTPLocked  PTPMode = "locked"   // slave with acceptable offset
)

// PTPStatus is the sync state advertised in SDP / doctor / program sidecar.
type PTPStatus struct {
	Mode      PTPMode `json:"mode"`
	Profile   string  `json:"profile"` // ST 2059-2
	Domain    int     `json:"domain"`
	OffsetNs  int64   `json:"offset_ns,omitempty"`
	Traceable bool    `json:"traceable"` // IEEE1588 traceable flag
	Interface string  `json:"interface,omitempty"`
	Note      string  `json:"note,omitempty"`
}

// SyncClockReport covers broadcast genlock / word-clock / media-clock dependencies.
type SyncClockReport struct {
	PTP            PTPStatus `json:"ptp"`
	MediaClockHz   int       `json:"media_clock_hz"`   // 48k audio / video timebase
	RTPOffsetZero  bool      `json:"rtp_offset_zero"`  // 2110-30: media↔RTP offset = 0
	VideoGenlock   string    `json:"video_genlock"`    // blackburst|tri-level|PTP-derived|none
	AudioWordClock string    `json:"audio_word_clock"` // house WC|AES|PTP-derived|none
	Essences       []string  `json:"essences"`         // ST 2110-* covered by this node
	Compliant      bool      `json:"compliant"`        // production-ready 2110 sync?
	Gaps           []string  `json:"gaps,omitempty"`
	Updated        int64     `json:"t"`
}

// DefaultPTPFreeRun is honest default for software venue without a grandmaster.
func DefaultPTPFreeRun() PTPStatus {
	return PTPStatus{
		Mode:    PTPFreeRun,
		Profile: PTPProfileST2059,
		Domain:  127, // common media domain placeholder
		Note:    "no PTP grandmaster attached — SDP uses ts-refclk:localmac; not production ST 2110 timing",
	}
}

// DefaultSyncClockReport builds doctor/venue sync coverage for this process.
func DefaultSyncClockReport() SyncClockReport {
	ptp := DefaultPTPFreeRun()
	gaps := []string{
		"PTP ST 2059-2 follower not locked (attach GM / ptp4l / facility BC)",
		"video genlock not derived from PTP (ST 2059-1 alignment pending)",
		"audio word-clock not locked to media clock (2110-30 requires media↔RTP offset 0)",
		"NMOS IS-04/05 registration not implemented (discovery/connection mgmt facility-side)",
	}
	return SyncClockReport{
		PTP:            ptp,
		MediaClockHz:   48000,
		RTPOffsetZero:  true, // we signal the requirement; hardware must honor
		VideoGenlock:   "none",
		AudioWordClock: "none",
		Essences:       []string{ST2110_10, ST2110_20, ST2110_21, ST2110_30, ST2110_40},
		Compliant:      false,
		Gaps:           gaps,
		Updated:        time.Now().UnixMilli(),
	}
}

// SyncClockWithPTPLocked marks facility-attached lock (env GY_PTP_LOCKED=1 later).
func SyncClockWithPTPLocked(domain int, offsetNs int64, iface string) SyncClockReport {
	r := DefaultSyncClockReport()
	r.PTP = PTPStatus{
		Mode:      PTPLocked,
		Profile:   PTPProfileST2059,
		Domain:    domain,
		OffsetNs:  offsetNs,
		Traceable: true,
		Interface: iface,
		Note:      "PTP follower locked to ST 2059-2 grandmaster",
	}
	r.VideoGenlock = "PTP-derived"
	r.AudioWordClock = "PTP-derived"
	r.Compliant = offsetNs < 1000 // 1 µs heuristic for software path
	if r.Compliant {
		r.Gaps = nil
	} else {
		r.Gaps = []string{fmt.Sprintf("PTP offset %d ns exceeds software lock budget", offsetNs)}
	}
	r.Updated = time.Now().UnixMilli()
	return r
}

// FormatSyncClockReport multi-line for gy doctor / venue.
func FormatSyncClockReport(r SyncClockReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ST 2110 sync · PTP %s · profile %s · domain %d\n",
		r.PTP.Mode, r.PTP.Profile, r.PTP.Domain)
	fmt.Fprintf(&b, "  media_clock %d Hz · rtp_offset_zero=%v · compliant=%v\n",
		r.MediaClockHz, r.RTPOffsetZero, r.Compliant)
	fmt.Fprintf(&b, "  genlock=%s · wordclock=%s\n", r.VideoGenlock, r.AudioWordClock)
	fmt.Fprintf(&b, "  essences: %s\n", strings.Join(r.Essences, ", "))
	if r.PTP.Note != "" {
		fmt.Fprintf(&b, "  ptp: %s\n", r.PTP.Note)
	}
	for _, g := range r.Gaps {
		fmt.Fprintf(&b, "  gap: %s\n", g)
	}
	return b.String()
}

// ST2110EssenceLine documents one suite part for help/docs.
type ST2110EssenceLine struct {
	ID      string
	Title   string
	Depends string
	GY      string // what GrokYtalkY implements
}

// ST2110SuiteTable is the basis coverage matrix.
func ST2110SuiteTable() []ST2110EssenceLine {
	return []ST2110EssenceLine{
		{ST2110_10, "System timing & definitions", ST2059_1 + " + " + ST2059_2 + " PTP", "SDP ts-refclk; SyncClockReport; free-run default"},
		{ST2110_20, "Uncompressed active video", ST2110_10 + " + " + ST2110_21, "tightened fmtp; raw RTP (uyvy/v210); optional 2022-7 dual-dest"},
		{ST2110_21, "Traffic shaping (TPN/TPNL/TPW)", ST2110_20, "TP= CLI --tp; software signals TPN (no hardware shaper)"},
		{ST2022_7, "Hitless dual-path protection", "diverse A/B network + 2022-7 RX", "--rtp-b tee dual destination; best-effort SSRC via single ffmpeg"},
		{ST2110_22, "CBR compressed video", ST2110_10, "lab profile H.264 RTP (not claiming 2110-22)"},
		{ST2110_30, "PCM digital audio (AES67 constrained)", ST2110_10 + " + ST 2059-2 (not AES67 PTP profile)", "L24/48000 RTP + channel-order + multi-essence SDP"},
		{ST2110_31, "AES3 transparent", ST2110_10, "not implemented (gateway facility)"},
		{ST2110_40, "Ancillary data", ST2110_10, "program bus → DID 0x5F mark/tally/bus ANC; --anc-rtp + OnANC"},
		{ST2059_1, "Align essence to PTP epoch", "IEEE 1588 / facility GM", "documented; lock via facility ptp4l/BC"},
		{ST2059_2, "PTP profile for pro media", "Grandmaster + BC/TC", "required by 2110; gy signals profile, does not run GM"},
	}
}

// FormatST2110SuiteTable for doctor.
func FormatST2110SuiteTable() string {
	var b strings.Builder
	b.WriteString("SMPTE ST 2110 suite coverage (GrokYtalkY venue)\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	for _, e := range ST2110SuiteTable() {
		fmt.Fprintf(&b, "%-14s %s\n", e.ID, e.Title)
		fmt.Fprintf(&b, "  depends: %s\n", e.Depends)
		fmt.Fprintf(&b, "  gy:      %s\n", e.GY)
	}
	return b.String()
}

// ── Camera tether matrix (major manufacturers) ───────────────

// CameraTether describes how a camera family reaches DOJO / venue IP.
type CameraTether struct {
	Mfr      string   `json:"mfr"`
	Family   string   `json:"family"`
	Tether   []string `json:"tether"`    // USB, TB, SDI, fiber, 2110, NDI, …
	ST2110   string   `json:"st2110"`    // native | converter | none
	PTP      string   `json:"ptp"`       // native | via-switcher | n/a
	Audio    string   `json:"audio"`     // embedded|AES|2110-30|USB
	Control  string   `json:"control"`   // RCP|OCU|USB|IP|none
	GYPath   string   `json:"gy_path"`   // how gy ingests today
	Notes    string   `json:"notes"`
}

// CameraTetherMatrix covers major cinema/broadcast manufacturers that tether.
// Not exhaustive SKU list — families + typical production paths.
func CameraTetherMatrix() []CameraTether {
	return []CameraTether{
		{
			Mfr: "Sony", Family: "Venice / Venice 2 / Burano",
			Tether: []string{"12G-SDI", "fiber (CBK)", "USB-C (limited)", "Network RX"},
			ST2110: "converter", PTP: "via-switcher",
			Audio: "embedded SDI / AES", Control: "RCP/IP",
			GYPath: "SDI→capture card→ffmpeg cam | SDI→2110 gateway→gy venue RX later",
			Notes:  "CineAlta live often SDI/fiber to switcher; 2110 via Sony IP Live / third-party gateways",
		},
		{
			Mfr: "Sony", Family: "FX6 / FX9 / FX3 / a7S",
			Tether: []string{"HDMI", "SDI (FX6/9)", "USB", "XAVC card"},
			ST2110: "converter", PTP: "n/a",
			Audio: "HDMI/SDI embed · XLR (FX)", Control: "USB/IP app",
			GYPath: "USB UVC / HDMI capture → gy cam | NDI bridge apps",
			Notes:  "Common hybrid docu; tether often Atomos/Video Assist then SDI",
		},
		{
			Mfr: "ARRI", Family: "Alexa 35 / Mini LF / 35 Live",
			Tether: []string{"12G-SDI", "fiber", "ARRIRAW live (product-dependent)"},
			ST2110: "converter", PTP: "via-switcher",
			Audio: "embedded / AES", Control: "ECS/IP",
			GYPath: "SDI→capture→ffmpeg · live production via OB truck 2110 spine",
			Notes:  "Large-sensor live typically baseband into IP gateways at truck",
		},
		{
			Mfr: "RED", Family: "V-RAPTOR / KOMODO / DSMC3",
			Tether: []string{"12G-SDI", "USB-C", "WiFi (monitor)", "media"},
			ST2110: "converter", PTP: "n/a",
			Audio: "SDI embed", Control: "RCP/USB",
			GYPath: "SDI or USB record proxy → gy watch/cam",
			Notes:  "Cinema-first; live IP via SDI→2110 converters",
		},
		{
			Mfr: "Blackmagic", Family: "URSA Cine / Studio Camera / Pocket",
			Tether: []string{"12G-SDI", "10GbE 2110 (select)", "USB", "HDMI", "Blackmagic 2110 IP Converter"},
			ST2110: "native", PTP: "native",
			Audio: "embed · XLR · 2110-30 via IP products", Control: "Camera Control · IP",
			GYPath: "SDI/USB cam · 2110 IP Converter → facility spine · gy venue on same fabric",
			Notes:  "Strongest native 2110 + PTP + NMOS story among cinema/prosumer lines",
		},
		{
			Mfr: "Canon", Family: "C500 II / C300 III / C70 / EOS R",
			Tether: []string{"12G-SDI", "HDMI", "USB", "XF-AVC"},
			ST2110: "converter", PTP: "via-switcher",
			Audio: "XLR/embed", Control: "Browser Remote / XC",
			GYPath: "SDI/HDMI capture → gy · multi-cam via ATEM then IP",
			Notes:  "Cinema EOS live often through switcher/converter for 2110",
		},
		{
			Mfr: "Panasonic", Family: "VariCam / EVA / Lumix BGH/S series",
			Tether: []string{"12G-SDI", "HDMI", "USB", "fiber (select)"},
			ST2110: "converter", PTP: "via-switcher",
			Audio: "XLR/embed", Control: "IP remote",
			GYPath: "SDI/HDMI → gy cam",
			Notes:  "Broadcast VariCam into truck IP; Lumix hybrid UVC/HDMI",
		},
		{
			Mfr: "Grass Valley / Mirage", Family: "LDX / LDX 100 series",
			Tether: []string{"SMPTE fiber", "3G/12G-SDI", "IP 2110 (system)"},
			ST2110: "native", PTP: "native",
			Audio: "2110-30 · embed", Control: "OCP/IP",
			GYPath: "Facility 2110 spine; gy venue as program consumer not camera CCU",
			Notes:  "Studio/OB cameras designed for 2110 fabrics",
		},
		{
			Mfr: "Ikegami", Family: "HDK / UHK studio",
			Tether: []string{"SMPTE fiber", "SDI", "IP options"},
			ST2110: "native", PTP: "native",
			Audio: "embed/2110", Control: "OCP",
			GYPath: "OB/studio 2110; gy on program/monitor path",
			Notes:  "Traditional broadcast chain into IP",
		},
		{
			Mfr: "Hitachi / For-A", Family: "studio / box cameras",
			Tether: []string{"fiber", "SDI", "IP"},
			ST2110: "native", PTP: "native",
			Audio: "embed/2110", Control: "OCP/IP",
			GYPath: "same as GV/Ikegami — facility spine",
			Notes:  "Enterprise broadcast",
		},
		{
			Mfr: "BirdDog / NewTek / Vizrt", Family: "P200 / P4K / NDI cams",
			Tether: []string{"NDI", "SDI", "HDMI", "Ethernet"},
			ST2110: "converter", PTP: "n/a",
			Audio: "NDI embed", Control: "NDI/IP",
			GYPath: "NDI→ffmpeg/NDI tools · gy sfu/ndi venue path",
			Notes:  "NDI-native; 2110 via convert if facility requires",
		},
		{
			Mfr: "PTZ Optics / Marshall / Lumens", Family: "PTZ",
			Tether: []string{"NDI|HX", "SDI", "HDMI", "USB", "IP RTSP"},
			ST2110: "converter", PTP: "n/a",
			Audio: "embed/USB", Control: "Visca/IP",
			GYPath: "RTSP/USB/NDI → gy watch/cam",
			Notes:  "House-of-worship / education; not usually native 2110",
		},
		{
			Mfr: "Apple / phones", Family: "iPhone Continuity / UVC dongles",
			Tether: []string{"USB", "Wireless", "HDMI (adapter)"},
			ST2110: "none", PTP: "n/a",
			Audio: "USB", Control: "none",
			GYPath: "gy cam UVC · Continuity Camera on macOS",
			Notes:  "Companion/dojo only — not broadcast genlock",
		},
	}
}

// FormatCameraTetherMatrix for doctor / help.
func FormatCameraTetherMatrix() string {
	var b strings.Builder
	b.WriteString("Camera tether matrix → GrokYtalkY / ST 2110 venue\n")
	b.WriteString(strings.Repeat("─", 72) + "\n")
	for _, c := range CameraTetherMatrix() {
		fmt.Fprintf(&b, "%s · %s\n", c.Mfr, c.Family)
		fmt.Fprintf(&b, "  tether:  %s\n", strings.Join(c.Tether, ", "))
		fmt.Fprintf(&b, "  st2110:  %s · ptp: %s · audio: %s\n", c.ST2110, c.PTP, c.Audio)
		fmt.Fprintf(&b, "  control: %s\n", c.Control)
		fmt.Fprintf(&b, "  gy path: %s\n", c.GYPath)
		if c.Notes != "" {
			fmt.Fprintf(&b, "  notes:   %s\n", c.Notes)
		}
		b.WriteByte('\n')
	}
	b.WriteString("PTP dependency: ST 2110 production requires ST 2059-2 follower lock;\n")
	b.WriteString("cameras without native PTP enter fabric via SDI→2110 gateway or switcher.\n")
	return b.String()
}

// PTP dependency summary for specs / SDP comments.
func FormatPTPDependencyBasis() string {
	return strings.TrimSpace(`
ST 2110 PTP / synclock basis
────────────────────────────
• ST 2110-10 mandates common timing: all essences (20/30/40) share PTP domain.
• PTP profile is SMPTE ST 2059-2 (not AES67 default / not vanilla IEEE default alone).
• ST 2059-1 maps RTP timestamps / media clocks to PTP epoch (video & audio aligned).
• ST 2110-30 (audio): AES67 transport subset + ST 2059-2 + media-clock↔RTP offset = 0
  + follower-only mode signaling; channel-order via SDP fmtp (SMPTE2110.(…)).
• Broadcast legacy: blackburst / tri-level genlock and audio word-clock are replaced
  (or slaved) by PTP-derived clocks on a 2110 fabric; hybrid plants use
  PTP→sync-pulse generators for SDI islands.
• Software gy venue: free-run + localmac ts-refclk until facility GM attached;
  production compliance requires locked follower (ptp4l / vendor BC / switch PTP).
• NMOS IS-04/05: discovery & connection management — facility, not essence RTP.
`)
}
