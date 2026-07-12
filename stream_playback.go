package main

import (
	"fmt"
	"sync"
	"time"
)

// PacketPlayer plays decoded StreamPackets as frames (and optional PCM).
// Used when /watch or drop loads .gyst / .gyhex / .pcap.
type PacketPlayer struct {
	mu       sync.Mutex
	packets  []StreamPacket
	idx      int
	playing  bool
	paused   bool
	rate     float64
	looping  bool
	frame    *FramePixels
	seq      uint64
	lastPCM  []byte
	pcmSR    int
	pcmCh    int
	stopCh   chan struct{}
	onFrame  func(*FramePixels)
	onPCM    func([]byte, int, int)
	posMS    uint64
	duration time.Duration
}

func NewPacketPlayer(packets []StreamPacket) *PacketPlayer {
	pp := &PacketPlayer{
		packets: packets,
		rate:    1,
		looping: true,
		stopCh:  make(chan struct{}),
	}
	// estimate duration from last timestamp or frame count @ 12fps
	var maxTS uint64
	var vidN int
	for _, p := range packets {
		if p.TimeMS > maxTS {
			maxTS = p.TimeMS
		}
		if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
			vidN++
		}
	}
	if maxTS > 0 && packets[0].TimeMS > 0 && maxTS > packets[0].TimeMS {
		pp.duration = time.Duration(maxTS-packets[0].TimeMS) * time.Millisecond
	} else if vidN > 0 {
		pp.duration = time.Duration(vidN) * time.Second / 12
	}
	return pp
}

func (pp *PacketPlayer) Play() {
	pp.mu.Lock()
	if pp.playing {
		pp.mu.Unlock()
		return
	}
	pp.playing = true
	pp.paused = false
	pp.stopCh = make(chan struct{})
	stop := pp.stopCh
	pp.mu.Unlock()
	go pp.runLoop(stop)
}

func (pp *PacketPlayer) Stop() {
	pp.mu.Lock()
	if !pp.playing {
		pp.mu.Unlock()
		return
	}
	pp.playing = false
	close(pp.stopCh)
	pp.mu.Unlock()
}

func (pp *PacketPlayer) TogglePause() {
	pp.mu.Lock()
	pp.paused = !pp.paused
	pp.mu.Unlock()
}

func (pp *PacketPlayer) Paused() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.paused
}

func (pp *PacketPlayer) Playing() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.playing
}

func (pp *PacketPlayer) SeekIndex(i int) {
	pp.mu.Lock()
	if i < 0 {
		i = 0
	}
	if i >= len(pp.packets) {
		i = len(pp.packets) - 1
	}
	if i < 0 {
		i = 0
	}
	pp.idx = i
	pp.applyAtLocked(i)
	pp.mu.Unlock()
}

func (pp *PacketPlayer) SeekRel(n int) {
	pp.mu.Lock()
	pp.idx += n
	if pp.idx < 0 {
		pp.idx = 0
	}
	if pp.idx >= len(pp.packets) {
		pp.idx = len(pp.packets) - 1
	}
	pp.applyAtLocked(pp.idx)
	pp.mu.Unlock()
}

func (pp *PacketPlayer) Snapshot() (*FramePixels, uint64, bool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if pp.frame == nil {
		return nil, pp.seq, false
	}
	return pp.frame.Clone(), pp.seq, true
}

func (pp *PacketPlayer) StatusLine() string {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	icon := "▶"
	if pp.paused {
		icon = "⏸"
	}
	if !pp.playing {
		icon = "■"
	}
	return fmt.Sprintf("%s pkt %d/%d  bin", icon, pp.idx+1, len(pp.packets))
}

func (pp *PacketPlayer) applyAtLocked(i int) {
	if i < 0 || i >= len(pp.packets) {
		return
	}
	p := &pp.packets[i]
	pp.posMS = p.TimeMS
	switch p.Kind {
	case KindRGB24, KindJPEG, KindHexLum:
		if f, err := FrameFromPacket(p); err == nil {
			pp.frame = f
			pp.seq++
			if pp.onFrame != nil {
				fr := f
				go pp.onFrame(fr)
			}
		}
	case KindPCM16:
		pp.lastPCM = p.Payload
		pp.pcmSR = int(p.Width)
		if pp.pcmSR <= 0 {
			pp.pcmSR = 16000
		}
		pp.pcmCh = int(p.Height)
		if pp.pcmCh <= 0 {
			pp.pcmCh = 1
		}
		if pp.onPCM != nil {
			go pp.onPCM(p.Payload, pp.pcmSR, pp.pcmCh)
		}
	}
}

func (pp *PacketPlayer) runLoop(stop <-chan struct{}) {
	// pace ~12 fps between video packets; pcm as encountered
	ticker := time.NewTicker(time.Second / 12)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			pp.mu.Lock()
			if pp.paused || len(pp.packets) == 0 {
				pp.mu.Unlock()
				continue
			}
			// advance to next video-bearing packet
			start := pp.idx
			for {
				pp.idx++
				if pp.idx >= len(pp.packets) {
					if pp.looping {
						pp.idx = 0
					} else {
						pp.idx = len(pp.packets) - 1
						pp.playing = false
						pp.mu.Unlock()
						return
					}
				}
				k := pp.packets[pp.idx].Kind
				if k == KindRGB24 || k == KindJPEG || k == KindHexLum || k == KindPCM16 {
					pp.applyAtLocked(pp.idx)
					break
				}
				if pp.idx == start {
					break
				}
			}
			pp.mu.Unlock()
		}
	}
}

// RecordSession collects packets for export.
type RecordSession struct {
	mu      sync.Mutex
	packets []StreamPacket
	seq     uint32
	active  bool
}

func NewRecordSession() *RecordSession {
	return &RecordSession{}
}

func (r *RecordSession) Start() {
	r.mu.Lock()
	r.active = true
	r.packets = nil
	r.seq = 0
	r.mu.Unlock()
}

func (r *RecordSession) Stop() {
	r.mu.Lock()
	r.active = false
	r.mu.Unlock()
}

func (r *RecordSession) Active() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *RecordSession) Add(p StreamPacket) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	r.seq++
	p.Seq = r.seq
	if p.TimeMS == 0 {
		p.TimeMS = uint64(time.Now().UnixMilli())
	}
	r.packets = append(r.packets, p)
}

func (r *RecordSession) AddFrame(f *FramePixels) {
	if f == nil {
		return
	}
	r.mu.Lock()
	seq := r.seq
	r.mu.Unlock()
	r.Add(PacketFromFramePixels(f, seq))
}

func (r *RecordSession) AddPCM(pcm []byte, sr, ch int) {
	r.mu.Lock()
	seq := r.seq
	r.mu.Unlock()
	r.Add(PacketFromPCM16(pcm, sr, ch, seq))
}

func (r *RecordSession) Packets() []StreamPacket {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]StreamPacket, len(r.packets))
	copy(out, r.packets)
	return out
}

func (r *RecordSession) Export(path, format string) error {
	pkts := r.Packets()
	if len(pkts) == 0 {
		return fmt.Errorf("no packets recorded")
	}
	switch format {
	case "gyst", "bin", "":
		return WriteGystFile(path, pkts)
	case "gyhex", "hex":
		return WriteGyHexFile(path, pkts, map[string]string{
			"packets": fmt.Sprintf("%d", len(pkts)),
			"app":     "grokytalky",
		})
	case "pcap":
		return WritePCAP(path, pkts)
	default:
		return fmt.Errorf("format %s (use gyst|gyhex|pcap)", format)
	}
}

func (r *RecordSession) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.packets)
}
