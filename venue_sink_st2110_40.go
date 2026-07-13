package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ST211040Sink — ST 2110-40 ANC egress from program bus.
//
// Clean start: OnProgram → ProgramBusToANC → JSONL sidecar + optional UDP/RTP.
// No full VANC inserter; DID 0x5F application packets for mark/tally/bus.
//
//	gy venue --sink st2110 --anc-rtp rtp://239.100.1.10:5008

// ST211040Opts configures 2110-40.
type ST211040Opts struct {
	RTP     string
	SDPPath string
	MetaDir string
	Quiet   bool
	Sync    SyncClockReport
}

type st211040Sink struct {
	opts    ST211040Opts
	mu      sync.Mutex
	conn    *net.UDPConn
	addr    *net.UDPAddr
	jsonl   string
	started bool
	seq     uint16
	lastBus ProgramBus
	pkts    uint64
}

// NewST211040Sink builds ANC venue sink.
func NewST211040Sink(opts ST211040Opts) (VenueSink, error) {
	if opts.MetaDir == "" {
		opts.MetaDir = filepath.Join(os.TempDir(), "gy-venue")
	}
	_ = os.MkdirAll(opts.MetaDir, 0o755)
	if opts.SDPPath == "" {
		opts.SDPPath = filepath.Join(opts.MetaDir, "gy-st2110-40.sdp")
	}
	jsonl := filepath.Join(opts.MetaDir, "st2110-40-anc.jsonl")
	sync := opts.Sync
	if sync.MediaClockHz == 0 {
		sync = DefaultSyncClockReport()
	}

	var udpAddr *net.UDPAddr
	if opts.RTP != "" {
		host, port, err := parseRTPURL(opts.RTP)
		if err != nil {
			return nil, err
		}
		udpAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
		if err != nil {
			return nil, err
		}
		if err := WriteST211040SDP(opts.SDPPath, host, port, sync); err != nil {
			return nil, err
		}
		if !opts.Quiet {
			log.Printf("venue · st2110-40 · ANC mark/tally/bus → %s", opts.RTP)
			log.Printf("venue · st2110-40 · SDP %s · jsonl %s", opts.SDPPath, jsonl)
		}
	} else if !opts.Quiet {
		log.Printf("venue · st2110-40 · jsonl only %s (set --anc-rtp for UDP)", jsonl)
	}

	return &st211040Sink{
		opts:  opts,
		addr:  udpAddr,
		jsonl: jsonl,
	}, nil
}

func (s *st211040Sink) Name() string { return "st2110-40" }

// OnProgram/OnHold/OnBlack: store bus only.
// ANC emission is solely VenueRuntime → OnANC(ProgramBusToANC) — avoids double fire.
func (s *st211040Sink) OnProgram(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.mu.Unlock()
}

func (s *st211040Sink) OnGlyph(VenueGlyphFrame) {}

func (s *st211040Sink) OnBlack(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.mu.Unlock()
}

func (s *st211040Sink) OnHold(bus ProgramBus) {
	s.mu.Lock()
	s.lastBus = bus
	s.mu.Unlock()
}

func (s *st211040Sink) OnANC(pkts []ANCPacket) {
	if len(pkts) == 0 {
		return
	}
	_ = WriteANCJSONL(s.jsonl, pkts)
	s.mu.Lock()
	s.pkts += uint64(len(pkts))
	n := s.pkts
	s.mu.Unlock()
	if !s.opts.Quiet && (n <= 3 || n%30 == 0) {
		for _, p := range pkts {
			log.Printf("venue · st2110-40 · %s", FormatANCPacket(p))
		}
	}
	s.sendRTP(pkts)
}

func (s *st211040Sink) sendRTP(pkts []ANCPacket) {
	if s.addr == nil {
		return
	}
	s.mu.Lock()
	if s.conn == nil {
		c, err := net.DialUDP("udp", nil, s.addr)
		if err != nil {
			s.mu.Unlock()
			if !s.opts.Quiet {
				log.Printf("venue · st2110-40 · udp: %v", err)
			}
			return
		}
		s.conn = c
		s.started = true
	}
	conn := s.conn
	seq := s.seq
	s.seq++
	s.mu.Unlock()

	payload := EncodeANCPayload(pkts)
	// minimal RTP header (12 bytes) + payload — lab framing
	pkt := make([]byte, 12+len(payload))
	pkt[0] = 0x80 // V=2
	pkt[1] = 100  // PT=100 smpte291
	pkt[2] = byte(seq >> 8)
	pkt[3] = byte(seq)
	// timestamp: ms * 90 (90000 Hz clock)
	ts := uint32(time.Now().UnixMilli() * 90)
	pkt[4] = byte(ts >> 24)
	pkt[5] = byte(ts >> 16)
	pkt[6] = byte(ts >> 8)
	pkt[7] = byte(ts)
	// SSRC fixed lab
	pkt[8], pkt[9], pkt[10], pkt[11] = 0x47, 0x59, 0x34, 0x30 // "GY40"
	copy(pkt[12:], payload)
	_, _ = conn.Write(pkt)
}

func (s *st211040Sink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	return nil
}
