package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Companion layout: small dock that sits beside Grok terminal — not a full takeover.
// All lines are hard-clamped to terminal width to stop wrap-spool glitches.

func (m *Model) renderCharm() string {
	w := m.width
	h := m.height
	if w < 40 {
		w = 40
	}
	if h < 10 {
		h = 10
	}

	if m.showHelp {
		return stableView(helpOverlay(min(w, 72), min(h, 24)), w, h)
	}

	// companion = compact by default
	if m.compact {
		return stableView(m.renderCompanion(w, h), w, h)
	}
	return stableView(m.renderFull(w, h), w, h)
}

func (m *Model) renderCompanion(w, h int) string {
	// Fixed chrome: header(1)+tabs(1)+body+status(1)+prompt(1)
	// Video max 6 half-rows when enabled
	const maxVidHalf = 6
	vidCols := min(w-2, 56)

	// header — one line
	conn := "○"
	if m.connected {
		conn = styLive().Render("●")
	} else {
		conn = styDim().Render("○")
	}
	mode := m.promptMode.String()
	flags := []string{}
	if m.live != nil && m.live.Playing() {
		flags = append(flags, "pat")
	}
	if m.vpipe != nil && m.vpipe.Running() {
		flags = append(flags, "vid")
	}
	if m.camOn {
		flags = append(flags, "cam")
	}
	if m.grokThinking {
		flags = append(flags, spinnerFrame(m.spin))
	}
	flagStr := ""
	if len(flags) > 0 {
		flagStr = styDim().Render(" " + strings.Join(flags, ","))
	}
	header := clampCells(
		styTitle().Render("◈ gy")+" "+conn+" "+
			styDim().Render(mode)+flagStr+" "+
			styDim().Render("v"+version),
		w,
	)

	tabs := clampCells(modeTabs(m.promptMode, w), w)

	var parts []string
	parts = append(parts, header, tabs)

	// optional tiny video dock
	used := 2 + 1 + 1 // header tabs status prompt
	if m.videoOn && m.frame != nil {
		halfRows := maxVidHalf
		// leave room for chat
		if h-used-halfRows < 3 {
			halfRows = max(2, h-used-3)
		}
		body := RenderFrame(m.frame, PixelHalf, vidCols)
		// force exact geometry
		body = fitHalfBlock(body, vidCols, halfRows)
		title := clampCells(styDim().Render(m.videoTitleShort()), w)
		parts = append(parts, title)
		for _, ln := range strings.Split(body, "\n") {
			parts = append(parts, clampCells(ln, w))
		}
		used += 1 + halfRows
	}

	// one-line pattern status
	if m.live != nil {
		st := "○"
		if m.live.Playing() {
			st = "▶"
		}
		pat := fmt.Sprintf("%s %s cyc=%d %s", st, truncate(strings.ReplaceAll(m.liveCode, "\n", " "), w-24), m.liveCycle, m.liveHit)
		parts = append(parts, clampCells(styDim().Render(pat), w))
		used++
	}

	// stream / log — remaining lines
	remain := h - used - 1 // -1 for prompt; status merged into prompt area
	if remain < 2 {
		remain = 2
	}
	chat := renderChatViewport(m.chat, m.nick, remain, w)
	for _, ln := range strings.Split(chat, "\n") {
		parts = append(parts, clampCells(ln, w))
	}

	// status + prompt as last two conceptual lines — combine status into dim prefix
	status := m.status
	if m.grokThinking {
		status = spinnerFrame(m.spin) + " grok · " + status
	}
	parts = append(parts, clampCells(styDim().Render(truncate(status, w)), w))

	prompt := promptLine(m.promptMode, m.nick, m.input, m.grokThinking, w)
	if m.talking {
		prompt = styErr().Reverse(true).Render(" PTT ") + styDim().Render(" space release")
	}
	parts = append(parts, clampCells(prompt, w))

	// join without extra blank lines
	return strings.Join(parts, "\n")
}

func (m *Model) videoTitleShort() string {
	if m.vpipe != nil && m.vpipe.Running() {
		return fmt.Sprintf("vid %s %s", filepath.Base(m.watchPath), m.pixelMode.String())
	}
	if m.frame != nil {
		return fmt.Sprintf("cam %dx%d", m.frame.W, m.frame.H)
	}
	return "vid off"
}

func (m *Model) renderFull(w, h int) string {
	// fuller dashboard but still clamp-stable
	header := clampCells(lipgloss.JoinHorizontal(lipgloss.Left,
		styTitle().Render("◈ GrokYtalkY"),
		" ",
		map[bool]string{true: styLive().Render("●"), false: styDim().Render("○")}[m.connected],
		" ",
		styDim().Render(m.promptMode.String()),
	), w)
	tabs := clampCells(modeTabs(m.promptMode, w), w)

	parts := []string{header, tabs}

	vidHalf := min(10, max(4, h/4))
	vidCols := min(w-4, 72)
	if m.videoOn && m.frame != nil {
		body := fitHalfBlock(RenderFrame(m.frame, PixelHalf, vidCols), vidCols, vidHalf)
		parts = append(parts, clampCells(styDim().Render(m.videoTitleShort()), w))
		for _, ln := range strings.Split(body, "\n") {
			parts = append(parts, clampCells(ln, w))
		}
	}

	if m.live != nil {
		parts = append(parts, clampCells(m.renderLiveOneLine(w), w))
	}

	remain := h - len(parts) - 2
	if remain < 3 {
		remain = 3
	}
	chat := renderChatViewport(m.chat, m.nick, remain, w)
	for _, ln := range strings.Split(chat, "\n") {
		parts = append(parts, clampCells(ln, w))
	}
	parts = append(parts, clampCells(styDim().Render(m.status), w))
	parts = append(parts, clampCells(promptLine(m.promptMode, m.nick, m.input, m.grokThinking, w), w))
	return strings.Join(parts, "\n")
}

func (m *Model) renderLiveOneLine(w int) string {
	st := "○"
	if m.live.Playing() {
		st = "▶"
	}
	return styDim().Render(fmt.Sprintf("%s strudel cyc=%d %s | %s",
		st, m.liveCycle, m.liveHit, truncate(strings.ReplaceAll(m.liveCode, "\n", " "), w-28)))
}

// kept for any callers
func (m *Model) renderLiveCharm(w int) string {
	return m.renderLiveOneLine(w)
}

func (m *Model) videoTitle() string {
	return m.videoTitleShort()
}
