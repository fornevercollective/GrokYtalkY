package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
)

const (
	sampleRate = 16000
	channels   = 1
	chunkMS    = 160
)

// PTTSession captures mic via ffmpeg → PCM s16le chunks.
type PTTSession struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	mu     sync.Mutex
	chunks [][]byte
	closed bool
}

func startPTT(onChunk func([]byte)) (*PTTSession, error) {
	var args []string
	if runtime.GOOS == "darwin" {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-f", "avfoundation", "-i", ":0",
			"-ac", "1", "-ar", "16000",
			"-f", "s16le", "pipe:1",
		}
	} else {
		args = []string{
			"-hide_banner", "-loglevel", "error",
			"-f", "pulse", "-i", "default",
			"-ac", "1", "-ar", "16000",
			"-f", "s16le", "pipe:1",
		}
	}
	cmd := exec.Command("ffmpeg", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &PTTSession{cmd: cmd, stdout: stdout}
	go func() {
		chunkBytes := sampleRate * channels * 2 * chunkMS / 1000
		buf := make([]byte, chunkBytes)
		for {
			_, err := io.ReadFull(stdout, buf)
			if err != nil {
				return
			}
			cp := make([]byte, len(buf))
			copy(cp, buf)
			s.mu.Lock()
			if !s.closed {
				s.chunks = append(s.chunks, cp)
			}
			s.mu.Unlock()
			if onChunk != nil {
				onChunk(cp)
			}
		}
	}()
	return s, nil
}

func (s *PTTSession) Stop() []byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.closed = true
	chunks := s.chunks
	s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	if len(chunks) == 0 {
		return nil
	}
	return bytes.Join(chunks, nil)
}

// Player streams PCM to ffplay.
type Player struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	mu    sync.Mutex
	// Duck attenuates RX while local TX (full-duplex walkie mitigation).
	// 0 = mute RX, 1 = full level. Default 1.
	Duck float64
}

// SetDuck sets RX attenuation (0–1). Used when duplex/open-mic is TX-active.
func (p *Player) SetDuck(d float64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if d < 0 {
		d = 0
	}
	if d > 1 {
		d = 1
	}
	p.Duck = d
}

// duckPCM scales s16le mono/stereo in-place copy by gain 0–1.
func duckPCM(pcm []byte, gain float64) []byte {
	if gain >= 0.99 || len(pcm) < 2 {
		return pcm
	}
	if gain <= 0.01 {
		return make([]byte, len(pcm)) // silence
	}
	out := make([]byte, len(pcm))
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[i:]))
		v := int(float64(s) * gain)
		if v > 32767 {
			v = 32767
		}
		if v < -32768 {
			v = -32768
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(v)))
	}
	return out
}

func (p *Player) Write(pcm []byte, sr, ch int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Duck: 0 = mute RX (duplex TX), (0,1) = attenuate, >=1 or unset(0 default first) = full.
	// Convention: Duck==0 with never-set is ambiguous; callers SetDuck(1) at Player create.
	if p.Duck < 0 {
		p.Duck = 1
	}
	if p.Duck == 0 {
		// explicit mute while local open-mic TX
		return
	}
	if p.Duck < 0.99 {
		pcm = duckPCM(pcm, p.Duck)
	}
	if p.cmd == nil || p.stdin == nil {
		if sr <= 0 {
			sr = sampleRate
		}
		if ch <= 0 {
			ch = 1
		}
		cmd := exec.Command("ffplay",
			"-hide_banner", "-loglevel", "error", "-nodisp", "-autoexit",
			"-f", "s16le", "-ar", itoa(sr), "-ac", itoa(ch),
			"-i", "pipe:0",
		)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return
		}
		if err := cmd.Start(); err != nil {
			return
		}
		p.cmd = cmd
		p.stdin = stdin
		go func() {
			_ = cmd.Wait()
			p.mu.Lock()
			p.cmd = nil
			p.stdin = nil
			p.mu.Unlock()
		}()
	}
	_, _ = p.stdin.Write(pcm)
}

func (p *Player) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.cmd = nil
	p.stdin = nil
}

func pcmToWAV(pcm []byte, sr, ch int) []byte {
	if sr <= 0 {
		sr = sampleRate
	}
	if ch <= 0 {
		ch = 1
	}
	dataSize := len(pcm)
	buf := make([]byte, 44+dataSize)
	copy(buf[0:], []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:], []byte("WAVE"))
	copy(buf[12:], []byte("fmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], uint16(ch))
	binary.LittleEndian.PutUint32(buf[24:], uint32(sr))
	binary.LittleEndian.PutUint32(buf[28:], uint32(sr*ch*2))
	binary.LittleEndian.PutUint16(buf[32:], uint16(ch*2))
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf
}

func writeLastClip(pcm []byte) string {
	path := os.TempDir() + "/grokytalky-last.wav"
	_ = os.WriteFile(path, pcmToWAV(pcm, sampleRate, channels), 0o644)
	return path
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
