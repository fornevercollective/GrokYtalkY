package strudel

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

const sfxRate = 44100

// AudioSink synthesizes drum/note hits and plays them locally (afplay/ffplay).
// MIDI virtual ports alone make no sound — this is the audible path.
type AudioSink struct {
	mu    sync.Mutex
	dir   string
	cache map[string]string // key → wav path
	play  string            // afplay | ffplay
	enabled bool
}

func NewAudioSink() *AudioSink {
	dir := filepath.Join(os.TempDir(), "grokytalky-sfx")
	_ = os.MkdirAll(dir, 0o755)
	play := "ffplay"
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("afplay"); err == nil {
			play = "afplay"
		}
	}
	if _, err := exec.LookPath(play); err != nil {
		if _, err2 := exec.LookPath("ffplay"); err2 == nil {
			play = "ffplay"
		} else {
			return &AudioSink{enabled: false}
		}
	}
	s := &AudioSink{
		dir:     dir,
		cache:   make(map[string]string),
		play:    play,
		enabled: true,
	}
	// prebake common drums
	for _, d := range []string{"bd", "sd", "hh", "oh", "cp", "rim", "rd"} {
		_, _ = s.ensureDrum(d)
	}
	return s
}

func (s *AudioSink) Enabled() bool { return s != nil && s.enabled }

func (s *AudioSink) Hit(ev Event, cycle int64) {
	if s == nil || !s.enabled {
		return
	}
	var path string
	var err error
	if ev.Kind == "note" || (ev.MIDI > 0 && ev.Kind != "drum") {
		path, err = s.ensureNote(ev.MIDI, ev.Vel)
	} else {
		name := ev.Sound
		if name == "" {
			name = midiDrumName(ev.MIDI)
		}
		path, err = s.ensureDrum(name)
	}
	if err != nil || path == "" {
		return
	}
	go s.playFile(path)
}

func (s *AudioSink) playFile(path string) {
	var cmd *exec.Cmd
	if s.play == "afplay" {
		// -q 1 = lower CPU; volume 1
		cmd = exec.Command("afplay", path)
	} else {
		cmd = exec.Command("ffplay", "-hide_banner", "-loglevel", "error",
			"-nodisp", "-autoexit", "-volume", "80", path)
	}
	_ = cmd.Run()
}

func (s *AudioSink) ensureDrum(name string) (string, error) {
	name = normalizeDrum(name)
	s.mu.Lock()
	if p, ok := s.cache["d:"+name]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	pcm := synthDrum(name)
	path := filepath.Join(s.dir, "drum-"+name+".wav")
	if err := writeWAV(path, pcm, sfxRate); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cache["d:"+name] = path
	s.mu.Unlock()
	return path, nil
}

func (s *AudioSink) ensureNote(midi int, vel uint8) (string, error) {
	if midi <= 0 {
		midi = 60
	}
	if midi > 127 {
		midi = 127
	}
	key := "n:" + itoa(midi)
	s.mu.Lock()
	if p, ok := s.cache[key]; ok {
		s.mu.Unlock()
		return p, nil
	}
	s.mu.Unlock()

	pcm := synthNote(midi, 0.18)
	path := filepath.Join(s.dir, "note-"+itoa(midi)+".wav")
	if err := writeWAV(path, pcm, sfxRate); err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cache[key] = path
	s.mu.Unlock()
	return path, nil
}

func normalizeDrum(name string) string {
	n := name
	if i := indexByteStr(n, ':'); i > 0 {
		n = n[:i]
	}
	switch n {
	case "kick", "k", "36":
		return "bd"
	case "snare", "sn", "38":
		return "sd"
	case "hat", "ch", "42":
		return "hh"
	case "openhat", "46":
		return "oh"
	case "clap", "39":
		return "cp"
	default:
		if n == "" {
			return "bd"
		}
		return n
	}
}

func midiDrumName(m int) string {
	switch m {
	case 36:
		return "bd"
	case 38:
		return "sd"
	case 42:
		return "hh"
	case 46:
		return "oh"
	case 39:
		return "cp"
	case 37:
		return "rim"
	case 51:
		return "rd"
	default:
		return "bd"
	}
}

// --- synthesis (simple 808-ish) ---

func synthDrum(name string) []int16 {
	switch name {
	case "bd":
		return synthKick(0.18)
	case "sd":
		return synthSnare(0.14)
	case "hh":
		return synthHat(0.045, false)
	case "oh":
		return synthHat(0.18, true)
	case "cp":
		return synthClap(0.12)
	case "rim":
		return synthRim(0.06)
	case "rd":
		return synthRide(0.25)
	default:
		return synthKick(0.12)
	}
}

func synthKick(dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	// pitch envelope 140Hz → 40Hz
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		env := math.Exp(-t * 18)
		freq := 140 * math.Exp(-t*22)
		phase += 2 * math.Pi * freq / sfxRate
		// click + body
		click := 0.0
		if t < 0.004 {
			click = (1 - t/0.004) * 0.5
		}
		s := math.Sin(phase)*0.9*env + click*noise1(i)*0.3
		out[i] = clamp16(s * 0.95)
	}
	return out
}

func synthSnare(dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		env := math.Exp(-t * 22)
		toneEnv := math.Exp(-t * 30)
		phase += 2 * math.Pi * 180 / sfxRate
		tone := math.Sin(phase) * 0.35 * toneEnv
		nz := noise1(i) * 0.75 * env
		out[i] = clamp16((tone + nz) * 0.85)
	}
	return out
}

func synthHat(dur float64, open bool) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	decay := 55.0
	if open {
		decay = 12.0
	}
	// simple highpassed noise via differentiator
	prev := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		env := math.Exp(-t * decay)
		raw := noise1(i)
		hp := raw - prev
		prev = raw
		out[i] = clamp16(hp * env * 0.55)
	}
	return out
}

func synthClap(dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		// multi-burst
		env := 0.0
		for _, off := range []float64{0, 0.012, 0.024} {
			tt := t - off
			if tt >= 0 {
				env += math.Exp(-tt * 40)
			}
		}
		out[i] = clamp16(noise1(i) * env * 0.35)
	}
	return out
}

func synthRim(dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		env := math.Exp(-t * 50)
		phase += 2 * math.Pi * 900 / sfxRate
		s := math.Sin(phase)*0.4 + noise1(i)*0.2
		out[i] = clamp16(s * env)
	}
	return out
}

func synthRide(dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	p1, p2 := 0.0, 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		env := math.Exp(-t * 6)
		p1 += 2 * math.Pi * 440 / sfxRate
		p2 += 2 * math.Pi * 660 / sfxRate
		s := (math.Sin(p1) + math.Sin(p2)*0.5 + noise1(i)*0.15) * env * 0.25
		out[i] = clamp16(s)
	}
	return out
}

func synthNote(midi int, dur float64) []int16 {
	n := int(dur * sfxRate)
	out := make([]int16, n)
	freq := 440.0 * math.Pow(2, float64(midi-69)/12)
	phase := 0.0
	for i := 0; i < n; i++ {
		t := float64(i) / sfxRate
		// soft pluck
		env := math.Exp(-t*8) * (1 - math.Exp(-t*200))
		phase += 2 * math.Pi * freq / sfxRate
		// mild harmonics
		s := math.Sin(phase)*0.7 + math.Sin(phase*2)*0.2 + math.Sin(phase*3)*0.1
		out[i] = clamp16(s * env * 0.7)
	}
	return out
}

func noise1(i int) float64 {
	// deterministic LCG noise
	x := uint32(i)*1664525 + 1013904223
	return float64(int32(x))/float64(1<<31)
}

func clamp16(s float64) int16 {
	if s > 1 {
		s = 1
	}
	if s < -1 {
		s = -1
	}
	return int16(s * 30000)
}

func writeWAV(path string, pcm []int16, rate int) error {
	dataSize := len(pcm) * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(s))
	}
	return os.WriteFile(path, buf, 0o644)
}

func indexByteStr(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var neg bool
	if n < 0 {
		neg = true
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
