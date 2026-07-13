package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ST 2110-40 — ancillary data (ANC) over IP.
//
// Cleanest starting point for GrokYtalkY:
//   program bus → ST 291-style DID/SDID packets → VenueSink.OnANC
//   optional RTP companion + SDP media line + JSONL sidecar
//
// We pack forge mark / tally-mode / compact bus as application ANC (not
// full CEA-708 captions). Facility can map these DID/SDIDs into VANC
// inserters or NMOS metadata. Full caption engines remain optional later.
//
// Spec basis: ST 2110-40 carries ST 291 ANC in RTP; ST 2110-10 timing applies.
// DID/SDID below are application-private (user data range) for gy — documented.

// Application DID (user-defined range; not a standard SMPTE registry entry).
const (
	ANC_DID_GY = 0x5F // user application

	// SDID kinds for program-bus → ANC
	ANC_SDID_MARK  = 0x01 // UTF-8 cgf:… mark identity
	ANC_SDID_TALLY = 0x02 // mode + slot + flags (binary)
	ANC_SDID_BUS   = 0x03 // compact JSON program bus snapshot

	// VANC-ish line hints (software; real inserters remap)
	ANC_LINE_MARK  = 9
	ANC_LINE_TALLY = 10
	ANC_LINE_BUS   = 11
)

// Tally / mode codes in SDID_TALLY UDW[0].
const (
	ANCTallyLive  = 1
	ANCTallyHold  = 2
	ANCTallyBlack = 3
	ANCTallyPreview = 4 // preview armed (optional second packet)
)

// ANCPacket is one ST 291-style ancillary unit for venue sinks.
type ANCPacket struct {
	DID   byte   `json:"did"`
	SDID  byte   `json:"sdid"`
	Line  int    `json:"line"` // VANC line hint
	UDW   []byte `json:"udw"`  // user data words (bytes for gy)
	Kind  string `json:"kind"` // mark|tally|bus
	T     int64  `json:"t"`
	Seq   uint32 `json:"seq,omitempty"` // program bus seq
	Note  string `json:"note,omitempty"`
}

// Packed returns DID|SDID|DC|UDW|CS (8-bit simplified; not 10-bit SDI words).
// Clean lab framing for RTP/file; HW path can expand to 10-bit ST 291.
func (p ANCPacket) Packed() []byte {
	dc := byte(len(p.UDW))
	out := make([]byte, 0, 3+len(p.UDW)+1)
	out = append(out, p.DID, p.SDID, dc)
	out = append(out, p.UDW...)
	var cs byte
	cs = p.DID + p.SDID + dc
	for _, b := range p.UDW {
		cs += b
	}
	// ST 291 checksum is 9-bit over 9-bit words; we use 8-bit sum complement for lab
	out = append(out, ^cs)
	return out
}

// ProgramBusToANC builds the minimal ANC set from on-air bus state.
// Call on every take/hold/black/preview — this is the clean capture point.
func ProgramBusToANC(bus ProgramBus) []ANCPacket {
	t := bus.T
	if t == 0 {
		t = time.Now().UnixMilli()
	}
	var pkts []ANCPacket

	// 1) forge mark as UTF-8 UDW (identity pass-through — not lattice pixels)
	if mark := bus.Program.Mark; mark != "" {
		pkts = append(pkts, ANCPacket{
			DID: ANC_DID_GY, SDID: ANC_SDID_MARK, Line: ANC_LINE_MARK,
			UDW: []byte(truncate(mark, 64)), Kind: "mark", T: t, Seq: bus.Seq,
			Note: "cgf mark-as-ANC",
		})
	}

	// 2) tally / mode binary
	mode := ANCTallyLive
	switch bus.Mode {
	case ProgramModeHold:
		mode = ANCTallyHold
	case ProgramModeBlack:
		mode = ANCTallyBlack
	}
	udw := []byte{byte(mode), byte(bus.Program.Slot & 0xff)}
	// flags: bit0 = has mark, bit1 = preview armed
	var flags byte
	if bus.Program.Mark != "" {
		flags |= 1
	}
	if bus.Preview != nil {
		flags |= 2
	}
	udw = append(udw, flags)
	// conductor nick (len-prefixed, max 16)
	cn := []byte(truncate(bus.Conductor, 16))
	udw = append(udw, byte(len(cn)))
	udw = append(udw, cn...)
	pkts = append(pkts, ANCPacket{
		DID: ANC_DID_GY, SDID: ANC_SDID_TALLY, Line: ANC_LINE_TALLY,
		UDW: udw, Kind: "tally", T: t, Seq: bus.Seq,
		Note: fmt.Sprintf("mode=%s slot=%d", bus.Mode, bus.Program.Slot),
	})

	// 3) compact bus JSON (for automation; keep small)
	type snap struct {
		Mode string `json:"mode"`
		Src  string `json:"source,omitempty"`
		Mark string `json:"mark,omitempty"`
		Slot int    `json:"slot,omitempty"`
		Cond string `json:"conductor,omitempty"`
		Seq  uint32 `json:"seq"`
	}
	s := snap{
		Mode: bus.Mode, Src: bus.Program.Source, Mark: bus.Program.Mark,
		Slot: bus.Program.Slot, Cond: bus.Conductor, Seq: bus.Seq,
	}
	jb, _ := json.Marshal(s)
	if len(jb) > 200 {
		jb = jb[:200]
	}
	pkts = append(pkts, ANCPacket{
		DID: ANC_DID_GY, SDID: ANC_SDID_BUS, Line: ANC_LINE_BUS,
		UDW: jb, Kind: "bus", T: t, Seq: bus.Seq,
		Note: "program bus snapshot",
	})
	return pkts
}

// FormatANCPacket one-line doctor/log.
func FormatANCPacket(p ANCPacket) string {
	return fmt.Sprintf("ANC DID=%02X SDID=%02X line=%d kind=%s len=%d seq=%d %s",
		p.DID, p.SDID, p.Line, p.Kind, len(p.UDW), p.Seq, p.Note)
}

// WriteST211040SDP writes ST 2110-40 media description (application ANC).
func WriteST211040SDP(path, host string, port int, sync SyncClockReport) error {
	now := time.Now().Unix()
	tsRef := "localmac=00-00-00-00-00-00"
	if sync.PTP.Mode == PTPLocked || sync.PTP.Mode == PTPSlave {
		tsRef = fmt.Sprintf("ptp=IEEE1588-2008:traceable:domain-number=%d", sync.PTP.Domain)
	}
	// smpte291 payload naming per common 2110-40 practice
	body := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=GrokYtalkY ST2110-40 ANC
i=ST 2110-40 ancillary — program bus mark/tally/bus as application DID 0x5F SDID 01/02/03. Not CEA-708 captions. PTP %s.
c=IN IP4 %s/32
t=0 0
a=tool:GrokYtalkY/%s
a=type:broadcast
a=x-gy-profile:2110-40
a=x-gy-anc-did:0x5F
a=x-gy-anc-sdid-mark:0x01
a=x-gy-anc-sdid-tally:0x02
a=x-gy-anc-sdid-bus:0x03
a=x-gy-program-meta:st2110-40-anc.jsonl
a=ts-refclk:%s
a=mediaclk:direct=0
m=video %d RTP/AVP 100
a=rtpmap:100 smpte291/90000
a=fmtp:100 VPID_Code=132
a=recvonly
`, now, now, host, PTPProfileST2059, host, Version, tsRef, port)
	return os.WriteFile(path, []byte(body), 0o644)
}

// AppendANCToMultiEssence adds a third m= line for 2110-40 into an existing bundle SDP body.
func AppendANCToMultiEssence(body string, host string, port int) string {
	if strings.Contains(body, "x-gy-profile:2110-40") || strings.Contains(body, "smpte291") {
		return body
	}
	// upgrade essences list if present
	body = strings.Replace(body, "2110-20,2110-30", "2110-20,2110-30,2110-40", 1)
	if !strings.Contains(body, "2110-40") {
		body = strings.Replace(body, "a=x-gy-essences:2110-20,2110-30\n", "a=x-gy-essences:2110-20,2110-30,2110-40\n", 1)
	}
	anc := fmt.Sprintf(`m=video %d RTP/AVP 100
a=mid:anc1
a=rtpmap:100 smpte291/90000
a=fmtp:100 VPID_Code=132
a=x-gy-anc:DID=0x5F
a=recvonly
`, port)
	// FID group
	if strings.Contains(body, "a=group:FID v1 a1") {
		body = strings.Replace(body, "a=group:FID v1 a1", "a=group:FID v1 a1 anc1", 1)
	}
	return body + anc
}

// ANCKindName human label.
func ANCKindName(sdid byte) string {
	switch sdid {
	case ANC_SDID_MARK:
		return "mark"
	case ANC_SDID_TALLY:
		return "tally"
	case ANC_SDID_BUS:
		return "bus"
	default:
		return fmt.Sprintf("sdid-%02x", sdid)
	}
}

// ParseTallyUDW decodes SDID_TALLY payload.
func ParseTallyUDW(udw []byte) (mode byte, slot int, flags byte, conductor string) {
	if len(udw) < 3 {
		return 0, 0, 0, ""
	}
	mode, slot, flags = udw[0], int(udw[1]), udw[2]
	if len(udw) > 3 {
		n := int(udw[3])
		if n > 0 && 4+n <= len(udw) {
			conductor = string(udw[4 : 4+n])
		}
	}
	return
}

// EncodeRTPPayload wraps packed ANC for lab RTP (length-prefixed units).
func EncodeANCPayload(pkts []ANCPacket) []byte {
	var out []byte
	for _, p := range pkts {
		raw := p.Packed()
		hdr := make([]byte, 4)
		binary.BigEndian.PutUint16(hdr[0:2], uint16(p.Line))
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(raw)))
		out = append(out, hdr...)
		out = append(out, raw...)
	}
	return out
}

// WriteANCJSONL appends packets to sidecar log.
func WriteANCJSONL(path string, pkts []ANCPacket) error {
	if path == "" || len(pkts) == 0 {
		return nil
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, p := range pkts {
		if err := enc.Encode(p); err != nil {
			return err
		}
	}
	return nil
}
