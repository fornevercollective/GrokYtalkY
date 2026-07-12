package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette mirrors cliamp's ANSI-first approach (adapts to terminal themes).
var (
	ColorTitle   color.Color = lipgloss.ANSIColor(14) // bright cyan
	ColorText    color.Color = lipgloss.ANSIColor(15)
	ColorDim     color.Color = lipgloss.ANSIColor(8)
	ColorAccent  color.Color = lipgloss.ANSIColor(11)
	ColorLive    color.Color = lipgloss.ANSIColor(10)
	ColorPTT     color.Color = lipgloss.ANSIColor(9)
	ColorChat    color.Color = lipgloss.ANSIColor(13) // bright magenta
	ColorMe      color.Color = lipgloss.ANSIColor(14)
	ColorError   color.Color = lipgloss.ANSIColor(9)
	SpectrumLow  color.Color = lipgloss.ANSIColor(10)
	SpectrumMid  color.Color = lipgloss.ANSIColor(11)
	SpectrumHigh color.Color = lipgloss.ANSIColor(9)
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(ColorTitle)
	dimStyle   = lipgloss.NewStyle().Foreground(ColorDim)
	textStyle  = lipgloss.NewStyle().Foreground(ColorText)
	liveStyle  = lipgloss.NewStyle().Bold(true).Foreground(ColorLive)
	pttStyle   = lipgloss.NewStyle().Bold(true).Foreground(ColorPTT).Reverse(true)
	chatWho    = lipgloss.NewStyle().Bold(true).Foreground(ColorChat)
	meWho      = lipgloss.NewStyle().Bold(true).Foreground(ColorMe)
	errStyle   = lipgloss.NewStyle().Foreground(ColorError)
	frameStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDim).
			Padding(0, 1)
	helpStyle = lipgloss.NewStyle().Foreground(ColorDim)
	inputStyle = lipgloss.NewStyle().Foreground(ColorAccent)
)

func specStyle(level float64) lipgloss.Style {
	switch {
	case level >= 0.6:
		return lipgloss.NewStyle().Foreground(SpectrumHigh)
	case level >= 0.3:
		return lipgloss.NewStyle().Foreground(SpectrumMid)
	default:
		return lipgloss.NewStyle().Foreground(SpectrumLow)
	}
}
