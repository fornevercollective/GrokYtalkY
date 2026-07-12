package main

import (
	"math"
)

// Audio handling helpers inspired by live performance sequencers:
// soft gate, peak hold, and velocity mapping for MIDI (sektron/signls style).

// SoftGate drops near-silence chunks (noise floor).
func SoftGate(pcm []byte, threshold float64) []byte {
	if rmsLevel(pcm) < threshold {
		return nil
	}
	return pcm
}

// PeakHold decays a peak level toward current (UI + CC smoothing).
func PeakHold(prev, current, attack, release float64) float64 {
	if current > prev {
		return prev*(1-attack) + current*attack
	}
	return prev*(1-release) + current*release
}

// LevelToVelocity maps 0..1 RMS → MIDI 1..127 with gentle curve.
func LevelToVelocity(level float64) uint8 {
	if level <= 0 {
		return 1
	}
	// sqrt curve feels more natural for voice
	v := math.Sqrt(math.Min(1, level*3)) * 127
	if v < 1 {
		return 1
	}
	if v > 127 {
		return 127
	}
	return uint8(v)
}

// MonoMix averages stereo interleaved s16le to mono (if ever needed).
func MonoMix(pcm []byte, channels int) []byte {
	if channels <= 1 || len(pcm) < 4 {
		return pcm
	}
	samples := len(pcm) / (2 * channels)
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		var sum int32
		for c := 0; c < channels; c++ {
			off := (i*channels + c) * 2
			sum += int32(int16(pcm[off]) | int16(pcm[off+1])<<8)
		}
		m := int16(sum / int32(channels))
		out[i*2] = byte(m)
		out[i*2+1] = byte(m >> 8)
	}
	return out
}
