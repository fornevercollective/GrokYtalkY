package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GrokYtalkY Stream Transport (GYST) — binary-level video/audio packets.
// Encode to .gyst (bin), .gyhex (text hex), .pcap (Wireshark-friendly USER0).
// Compatible in spirit with hexcast frames + overview liveHexCodec.

const (
	gystMagic   = "GYST"
	gystVersion = 1

	KindRGB24  uint8 = 1 // payload: w*h*3
	KindPCM16  uint8 = 2 // payload: s16le mono/stereo interleaved
	KindJPEG   uint8 = 3 // payload: jpeg bytes
	KindHexLum uint8 = 4 // payload: res*res luminance bytes (0–255)
	KindMeta   uint8 = 5 // payload: utf-8 json/text
)

// StreamPacket is one binary-level media unit.
type StreamPacket struct {
	Kind    uint8
	Flags   uint16
	Width   uint32 // or sample rate for PCM
	Height  uint32 // or channels for PCM
	Seq     uint32
	TimeMS  uint64
	Payload []byte
}

func (p StreamPacket) KindName() string {
	switch p.Kind {
	case KindRGB24:
		return "rgb24"
	case KindPCM16:
		return "pcm16"
	case KindJPEG:
		return "jpeg"
	case KindHexLum:
		return "hexlum"
	case KindMeta:
		return "meta"
	default:
		return fmt.Sprintf("kind%d", p.Kind)
	}
}

// HeaderSize fixed GYST header length.
const gystHeaderSize = 32

// EncodeBinary writes one GYST packet (header + payload).
func EncodeBinary(w io.Writer, p StreamPacket) error {
	if len(p.Payload) > 64<<20 {
		return fmt.Errorf("payload too large")
	}
	var hdr [gystHeaderSize]byte
	copy(hdr[0:4], gystMagic)
	hdr[4] = gystVersion
	hdr[5] = p.Kind
	binary.LittleEndian.PutUint16(hdr[6:8], p.Flags)
	binary.LittleEndian.PutUint32(hdr[8:12], p.Width)
	binary.LittleEndian.PutUint32(hdr[12:16], p.Height)
	binary.LittleEndian.PutUint32(hdr[16:20], p.Seq)
	binary.LittleEndian.PutUint64(hdr[20:28], p.TimeMS)
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(len(p.Payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(p.Payload) > 0 {
		_, err := w.Write(p.Payload)
		return err
	}
	return nil
}

// DecodeBinary reads one GYST packet from r.
func DecodeBinary(r io.Reader) (*StreamPacket, error) {
	var hdr [gystHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	if string(hdr[0:4]) != gystMagic {
		return nil, fmt.Errorf("bad magic (want GYST)")
	}
	if hdr[4] != gystVersion {
		return nil, fmt.Errorf("unsupported GYST version %d", hdr[4])
	}
	plen := binary.LittleEndian.Uint32(hdr[28:32])
	if plen > 64<<20 {
		return nil, fmt.Errorf("payload len %d too large", plen)
	}
	payload := make([]byte, plen)
	if plen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}
	return &StreamPacket{
		Kind:    hdr[5],
		Flags:   binary.LittleEndian.Uint16(hdr[6:8]),
		Width:   binary.LittleEndian.Uint32(hdr[8:12]),
		Height:  binary.LittleEndian.Uint32(hdr[12:16]),
		Seq:     binary.LittleEndian.Uint32(hdr[16:20]),
		TimeMS:  binary.LittleEndian.Uint64(hdr[20:28]),
		Payload: payload,
	}, nil
}

// EncodeHexLine writes a single-line hex representation.
// Format: :seq kind w h ts hexpayload
func EncodeHexLine(p StreamPacket) string {
	return fmt.Sprintf(":%d %s %d %d %d %s",
		p.Seq, p.KindName(), p.Width, p.Height, p.TimeMS,
		hex.EncodeToString(p.Payload))
}

// DecodeHexLine parses one gyhex line (skips comments / blank).
func DecodeHexLine(line string) (*StreamPacket, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "@") {
		return nil, nil
	}
	if !strings.HasPrefix(line, ":") {
		// raw hex blob (legacy) — treat as opaque
		b, err := hex.DecodeString(strings.ReplaceAll(line, " ", ""))
		if err != nil {
			return nil, err
		}
		return &StreamPacket{Kind: KindMeta, Payload: b}, nil
	}
	// :seq kind w h ts hex
	parts := strings.Fields(line[1:])
	if len(parts) < 6 {
		return nil, fmt.Errorf("hex line fields")
	}
	seq, _ := strconv.ParseUint(parts[0], 10, 32)
	kind := kindFromName(parts[1])
	w, _ := strconv.ParseUint(parts[2], 10, 32)
	h, _ := strconv.ParseUint(parts[3], 10, 32)
	ts, _ := strconv.ParseUint(parts[4], 10, 64)
	payload, err := hex.DecodeString(parts[5])
	if err != nil {
		return nil, err
	}
	return &StreamPacket{
		Kind: kind, Width: uint32(w), Height: uint32(h),
		Seq: uint32(seq), TimeMS: ts, Payload: payload,
	}, nil
}

func kindFromName(s string) uint8 {
	switch strings.ToLower(s) {
	case "rgb", "rgb24":
		return KindRGB24
	case "pcm", "pcm16":
		return KindPCM16
	case "jpeg", "jpg":
		return KindJPEG
	case "hex", "hexlum", "lum":
		return KindHexLum
	default:
		return KindMeta
	}
}

// WriteGyHexFile writes a text hex stream file.
func WriteGyHexFile(path string, packets []StreamPacket, meta map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	fmt.Fprintf(bw, "# GrokYtalkY gyhex stream\n")
	fmt.Fprintf(bw, "# GYST binary-compatible hex dump\n")
	if meta != nil {
		var parts []string
		for k, v := range meta {
			parts = append(parts, k+"="+v)
		}
		fmt.Fprintf(bw, "@meta %s\n", strings.Join(parts, " "))
	}
	for _, p := range packets {
		fmt.Fprintln(bw, EncodeHexLine(p))
	}
	return bw.Flush()
}

// ReadGyHexFile loads packets from .gyhex / .hex text.
func ReadGyHexFile(path string) ([]StreamPacket, map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	meta := map[string]string{}
	var packets []StreamPacket
	sc := bufio.NewScanner(f)
	// large lines (frames)
	buf := make([]byte, 0, 1024*1024)
	sc.Buffer(buf, 32<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "@meta") {
			for _, f := range strings.Fields(strings.TrimSpace(line))[1:] {
				if i := strings.IndexByte(f, '='); i > 0 {
					meta[f[:i]] = f[i+1:]
				}
			}
			continue
		}
		p, err := DecodeHexLine(line)
		if err != nil {
			return nil, nil, err
		}
		if p != nil {
			packets = append(packets, *p)
		}
	}
	return packets, meta, sc.Err()
}

// WriteGystFile writes concatenated binary packets.
func WriteGystFile(path string, packets []StreamPacket) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, p := range packets {
		if err := EncodeBinary(f, p); err != nil {
			return err
		}
	}
	return nil
}

// ReadGystFile reads all packets from a .gyst binary file.
func ReadGystFile(path string) ([]StreamPacket, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var packets []StreamPacket
	for {
		p, err := DecodeBinary(f)
		if err == io.EOF {
			break
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF && len(packets) > 0 {
				break
			}
			return packets, err
		}
		packets = append(packets, *p)
	}
	return packets, nil
}

// ── PCAP (LINKTYPE_USER0 = 147) wrapping GYST packets ────────

const pcapLinkUser0 = 147

// WritePCAP writes packets as a pcap capture (one GYST blob per packet).
func WritePCAP(path string, packets []StreamPacket) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// global header
	var gh [24]byte
	binary.LittleEndian.PutUint32(gh[0:4], 0xa1b2c3d4) // magic
	binary.LittleEndian.PutUint16(gh[4:6], 2)          // major
	binary.LittleEndian.PutUint16(gh[6:8], 4)          // minor
	// thiszone, sigfigs = 0
	binary.LittleEndian.PutUint32(gh[16:20], 65535) // snaplen
	binary.LittleEndian.PutUint32(gh[20:24], pcapLinkUser0)
	if _, err := f.Write(gh[:]); err != nil {
		return err
	}
	for _, p := range packets {
		var body strings.Builder // use buffer
		_ = body
		buf := &packetBuf{}
		if err := EncodeBinary(buf, p); err != nil {
			return err
		}
		data := buf.Bytes()
		ts := p.TimeMS
		if ts == 0 {
			ts = uint64(time.Now().UnixMilli())
		}
		sec := uint32(ts / 1000)
		usec := uint32((ts % 1000) * 1000)
		var ph [16]byte
		binary.LittleEndian.PutUint32(ph[0:4], sec)
		binary.LittleEndian.PutUint32(ph[4:8], usec)
		binary.LittleEndian.PutUint32(ph[8:12], uint32(len(data)))
		binary.LittleEndian.PutUint32(ph[12:16], uint32(len(data)))
		if _, err := f.Write(ph[:]); err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
	}
	return nil
}

type packetBuf struct{ b []byte }

func (p *packetBuf) Write(b []byte) (int, error) {
	p.b = append(p.b, b...)
	return len(b), nil
}
func (p *packetBuf) Bytes() []byte { return p.b }

// ReadPCAP extracts GYST packets from a .pcap (USER0 or raw GYST records).
func ReadPCAP(path string) ([]StreamPacket, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var gh [24]byte
	if _, err := io.ReadFull(f, gh[:]); err != nil {
		return nil, err
	}
	magic := binary.LittleEndian.Uint32(gh[0:4])
	be := false
	if magic == 0xd4c3b2a1 {
		be = true
	} else if magic != 0xa1b2c3d4 {
		// maybe not pcap — try as raw gyst
		f.Seek(0, 0)
		return ReadGystFile(path)
	}
	u32 := binary.LittleEndian.Uint32
	if be {
		u32 = binary.BigEndian.Uint32
	}
	var packets []StreamPacket
	for {
		var ph [16]byte
		if _, err := io.ReadFull(f, ph[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return packets, err
		}
		incl := u32(ph[8:12])
		if incl > 64<<20 {
			return packets, fmt.Errorf("pcap packet too large")
		}
		data := make([]byte, incl)
		if _, err := io.ReadFull(f, data); err != nil {
			return packets, err
		}
		payload := data
		if len(data) >= 4 && string(data[0:4]) != gystMagic {
			if i := indexGYST(data); i >= 0 {
				payload = data[i:]
			}
		}
		p2, err2 := DecodeBinary(newByteReader(payload))
		if err2 != nil {
			continue
		}
		packets = append(packets, *p2)
	}
	return packets, nil
}

func indexGYST(b []byte) int {
	for i := 0; i+4 <= len(b); i++ {
		if b[i] == 'G' && b[i+1] == 'Y' && b[i+2] == 'S' && b[i+3] == 'T' {
			return i
		}
	}
	return -1
}

type bytesReader struct{ b []byte }

func (r *bytesReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func newByteReader(b []byte) *bytesReader {
	return &bytesReader{b: b}
}

// ── conversions to/from FramePixels & PCM ────────────────────

// PacketFromRGB builds a KindRGB24 packet.
func PacketFromRGB(rgb []byte, w, h int, seq uint32, tms uint64) StreamPacket {
	cp := make([]byte, len(rgb))
	copy(cp, rgb)
	return StreamPacket{
		Kind: KindRGB24, Width: uint32(w), Height: uint32(h),
		Seq: seq, TimeMS: tms, Payload: cp,
	}
}

// PacketFromFramePixels from current display frame.
func PacketFromFramePixels(f *FramePixels, seq uint32) StreamPacket {
	if f == nil {
		return StreamPacket{Kind: KindMeta}
	}
	return PacketFromRGB(f.RGB, f.W, f.H, seq, uint64(time.Now().UnixMilli()))
}

// FrameFromPacket decodes rgb24/jpeg/hexlum → FramePixels.
func FrameFromPacket(p *StreamPacket) (*FramePixels, error) {
	if p == nil {
		return nil, fmt.Errorf("nil packet")
	}
	switch p.Kind {
	case KindRGB24:
		w, h := int(p.Width), int(p.Height)
		need := w * h * 3
		if w < 1 || h < 1 || len(p.Payload) < need {
			return nil, fmt.Errorf("rgb24 size mismatch")
		}
		cp := make([]byte, need)
		copy(cp, p.Payload[:need])
		return &FramePixels{W: w, H: h, RGB: cp, Source: "gyst"}, nil
	case KindJPEG:
		return decodeFrameJPEG(p.Payload, int(p.Width), int(p.Height))
	case KindHexLum:
		res := int(p.Width)
		if res < 1 {
			res = int(p.Height)
		}
		if res < 1 {
			// square from len
			n := len(p.Payload)
			res = 1
			for res*res < n {
				res++
			}
		}
		rgb := make([]byte, res*res*3)
		for i := 0; i < res*res && i < len(p.Payload); i++ {
			v := p.Payload[i]
			rgb[i*3], rgb[i*3+1], rgb[i*3+2] = v, v, v
		}
		return &FramePixels{W: res, H: res, RGB: rgb, Source: "hexlum"}, nil
	default:
		return nil, fmt.Errorf("not a video packet (%s)", p.KindName())
	}
}

// PacketFromHexLum overview/liveHex style: res² luminance.
func PacketFromHexLum(lum []byte, res int, seq uint32) StreamPacket {
	cp := make([]byte, len(lum))
	copy(cp, lum)
	return StreamPacket{
		Kind: KindHexLum, Width: uint32(res), Height: uint32(res),
		Seq: seq, TimeMS: uint64(time.Now().UnixMilli()), Payload: cp,
	}
}

// PacketFromPCM16 audio chunk.
func PacketFromPCM16(pcm []byte, sampleRate, channels int, seq uint32) StreamPacket {
	cp := make([]byte, len(pcm))
	copy(cp, pcm)
	return StreamPacket{
		Kind: KindPCM16, Width: uint32(sampleRate), Height: uint32(channels),
		Seq: seq, TimeMS: uint64(time.Now().UnixMilli()), Payload: cp,
	}
}

// OverviewHexFrame JSON compatible with liveHexCodec.
type OverviewHexFrame struct {
	Type    string `json:"type"`
	Hex     []int  `json:"hex"`
	Res     int    `json:"res"`
	Mode    string `json:"mode,omitempty"`
	T       int64  `json:"t,omitempty"`
	FeedKey string `json:"feedKey,omitempty"`
}

// EncodeOverviewHexJSON encodes luminance frame as overview hexframe JSON.
func EncodeOverviewHexJSON(lum []byte, res int, mode, feedKey string) ([]byte, error) {
	h := make([]int, len(lum))
	for i, v := range lum {
		h[i] = int(v)
	}
	return json.Marshal(OverviewHexFrame{
		Type: "hexframe", Hex: h, Res: res, Mode: mode,
		T: time.Now().UnixMilli(), FeedKey: feedKey,
	})
}

// DecodeOverviewHexJSON → StreamPacket hexlum.
func DecodeOverviewHexJSON(b []byte) (*StreamPacket, error) {
	var h OverviewHexFrame
	if err := json.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	if h.Type != "hexframe" && h.Type != "" {
		// allow missing type if hex+res present
		if len(h.Hex) == 0 {
			return nil, fmt.Errorf("not hexframe")
		}
	}
	lum := make([]byte, len(h.Hex))
	for i, v := range h.Hex {
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		lum[i] = byte(v)
	}
	res := h.Res
	if res < 1 {
		res = intSqrt(len(lum))
	}
	p := PacketFromHexLum(lum, res, 0)
	p.TimeMS = uint64(h.T)
	return &p, nil
}

func intSqrt(n int) int {
	if n <= 0 {
		return 1
	}
	x := n
	for {
		y := (x + n/x) / 2
		if y >= x {
			return x
		}
		x = y
	}
}

// DetectStreamFile returns format: gyst|gyhex|pcap|hexjson|unknown
func DetectStreamFile(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".gyst", ".bin", ".gybin":
		return "gyst"
	case ".gyhex", ".hex":
		return "gyhex"
	case ".pcap", ".pcapng":
		return "pcap"
	case ".json":
		return "hexjson"
	}
	// sniff magic
	f, err := os.Open(path)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
	var magic [4]byte
	_, _ = io.ReadFull(f, magic[:])
	if string(magic[:]) == gystMagic {
		return "gyst"
	}
	if magic[0] == 0xd4 && magic[1] == 0xc3 && magic[2] == 0xb2 && magic[3] == 0xa1 {
		return "pcap"
	}
	if magic[0] == 0xa1 && magic[1] == 0xb2 && magic[2] == 0xc3 && magic[3] == 0xd4 {
		return "pcap"
	}
	return "unknown"
}

// LoadStreamFile loads packets from gyst/gyhex/pcap/json.
func LoadStreamFile(path string) ([]StreamPacket, error) {
	switch DetectStreamFile(path) {
	case "gyst":
		return ReadGystFile(path)
	case "gyhex":
		pkts, _, err := ReadGyHexFile(path)
		return pkts, err
	case "pcap":
		return ReadPCAP(path)
	case "hexjson":
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		// single or NDJSON
		var all []StreamPacket
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			p, err := DecodeOverviewHexJSON([]byte(line))
			if err != nil {
				// try whole file
				p, err = DecodeOverviewHexJSON(b)
				if err != nil {
					return nil, err
				}
				return []StreamPacket{*p}, nil
			}
			all = append(all, *p)
		}
		return all, nil
	default:
		// try gyst then hex
		if pkts, err := ReadGystFile(path); err == nil && len(pkts) > 0 {
			return pkts, nil
		}
		if pkts, _, err := ReadGyHexFile(path); err == nil && len(pkts) > 0 {
			return pkts, nil
		}
		return nil, fmt.Errorf("unknown stream format: %s", path)
	}
}

// IsStreamCodecPath true for binary/hex/pcap media containers we own.
func IsStreamCodecPath(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".gyst", ".gyhex", ".gybin", ".pcap", ".hex":
		return true
	}
	return DetectStreamFile(p) != "unknown" && DetectStreamFile(p) != ""
}
