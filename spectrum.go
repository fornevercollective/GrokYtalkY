package main

import (
	"math"
	"strings"
)

// Spectrum bars inspired by cliamp VisBars / VisAscii (lipgloss colored).
var barBlocks = []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

func rmsLevel(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	n := len(pcm) / 2
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := float64(int16(pcm[i])|int16(pcm[i+1])<<8) / 32768
		sum += s * s
	}
	return math.Sqrt(sum / float64(n))
}

// bandLevels splits PCM into crude energy bands (no full FFT — light + fast).
func bandLevels(pcm []byte, bands int) []float64 {
	out := make([]float64, bands)
	if len(pcm) < 4 || bands <= 0 {
		return out
	}
	samples := len(pcm) / 2
	win := max(1, samples/bands)
	for b := 0; b < bands; b++ {
		start := b * win
		var sum float64
		count := 0
		for i := 0; i < win && start+i < samples; i++ {
			off := (start + i) * 2
			s := float64(int16(pcm[off])|int16(pcm[off+1])<<8) / 32768
			sum += s * s
			count++
		}
		if count > 0 {
			out[b] = math.Min(1, math.Sqrt(sum/float64(count))*4.5)
		}
	}
	return out
}

func renderSpectrum(levels []float64, width int) string {
	if width <= 0 {
		width = 32
	}
	if len(levels) == 0 {
		return dimStyle.Render(strings.Repeat("░", width))
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		pos := float64(i) / float64(max(1, width-1)) * float64(len(levels)-1)
		idx := int(pos)
		if idx >= len(levels)-1 {
			idx = len(levels) - 1
		}
		frac := pos - float64(idx)
		var lv float64
		if idx+1 < len(levels) {
			lv = levels[idx]*(1-frac) + levels[idx+1]*frac
		} else {
			lv = levels[idx]
		}
		bi := int(lv * float64(len(barBlocks)-1))
		if bi < 0 {
			bi = 0
		}
		if bi >= len(barBlocks) {
			bi = len(barBlocks) - 1
		}
		b.WriteString(specStyle(lv).Render(barBlocks[bi]))
	}
	return b.String()
}

func renderVU(level float64, width int) string {
	if width <= 0 {
		width = 16
	}
	n := int(math.Min(float64(width), math.Round(level*float64(width)*3.2)))
	var b strings.Builder
	for i := 0; i < width; i++ {
		if i < n {
			t := float64(i) / float64(width)
			b.WriteString(specStyle(t + 0.15).Render("█"))
		} else {
			b.WriteString(dimStyle.Render("░"))
		}
	}
	return b.String()
}
