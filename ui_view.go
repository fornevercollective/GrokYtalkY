package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Companion layout: lean chrome; video expands to fill the scale *between*
// header/viz and chat/prompt. Capture resolution tracks that scale.

func (m *Model) renderCharm() string {
	w := m.width
	h := m.height
	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 24
	}
	cw := safeCols(w)

	if m.showHelp {
		return stableView(helpOverlay(min(cw, 72), min(h, 22)), w, h)
	}
	if m.burstMode {
		return stableView(m.renderBurstOrb(cw, h), w, h)
	}
	// multi-feed lab: feeds next to chat + FPS/scale/style lists
	if m.lab != nil && m.lab.On {
		return stableView(m.renderLab(cw, h), w, h)
	}
	if m.compact {
		return stableView(m.renderCompanion(cw, h), w, h)
	}
	return stableView(m.renderFull(cw, h), w, h)
}

// renderBurstOrb — Siri-sized popup (fixed footprint).
func (m *Model) renderBurstOrb(w, h int) string {
	cols := min(OrbCols, w)
	rows := min(OrbRows, max(8, h-3))
	if rows > OrbRows {
		rows = OrbRows
	}

	face := m.frame
	tx := m.talking
	rx := m.burstRemote
	if rx == "" {
		rx = m.remoteTX
	}

	orb := RenderOrb(cols, rows, face, maxF(m.level, m.peak*0.8), m.bands, tx, rx, m.nick)
	status := BurstStatusLine(cols, tx, rx, m.nick, len(m.peers))
	pad := (w - cols) / 2
	if pad < 0 {
		pad = 0
	}
	prefix := strings.Repeat(" ", pad)

	var parts []string
	topPad := max(0, (h-rows-2)/3)
	for i := 0; i < topPad && i < 4; i++ {
		parts = append(parts, "")
	}
	for _, ln := range strings.Split(orb, "\n") {
		parts = append(parts, clampCells(prefix+ln, w))
	}
	parts = append(parts, clampCells(prefix+status, w))
	hint := styDim().Render("b exit · q quit · glyph ")
	hint += styDim().Render(fmt.Sprintf("%d×%d", m.glyphN, m.glyphN))
	parts = append(parts, clampCells(prefix+hint, w))
	return strings.Join(parts, "\n")
}

func (m *Model) renderCompanion(w, h int) string {
	sc := m.computeVideoScale(w, h)
	return m.renderVideoChrome(w, h, sc)
}

func (m *Model) renderFull(w, h int) string {
	// full mode: same scaler but prefers even more vertical video
	sc := m.computeVideoScale(w, h)
	if sc.Active && sc.HalfRows < h/2 {
		// reclaim chat into video when tall
		extra := min(sc.ChatLines-2, h/4)
		if extra > 0 {
			sc.HalfRows += extra
			sc.ChatLines -= extra
			sc.SrcH = sc.HalfRows * 2
			if sc.SrcW < sc.Cols*2 && sc.Cols >= 48 {
				sc.SrcW = min(160, sc.Cols*2)
				sc.SrcH = min(96, sc.HalfRows*2)
			}
		}
	}
	return m.renderVideoChrome(w, h, sc)
}

// renderVideoChrome — shared layout: header · viz · [video fill] · chat · prompt
func (m *Model) renderVideoChrome(w, h int, sc videoScale) string {
	parts := []string{
		clampCells(m.headerLine(w), w),
		clampCells(m.vizLine(w), w),
	}

	if sc.Active && m.frame != nil {
		body := fitHalfBlock(
			RenderFrame(m.frame, m.pixelMode, sc.Cols),
			sc.Cols,
			sc.HalfRows,
		)
		for _, ln := range strings.Split(body, "\n") {
			// video lines already sc.Cols; pad to full terminal width with spaces
			// (not extra ▀) so geometry is stable
			parts = append(parts, padOrTrim(ln, w))
		}
	}

	if m.showPatternLine() {
		parts = append(parts, clampCells(m.renderLiveOneLine(w), w))
	}

	chatN := sc.ChatLines
	if chatN < 1 {
		chatN = 1
	}
	// don't overflow remaining terminal
	used := len(parts) + 1 // +prompt
	if used+chatN > h {
		chatN = max(1, h-used)
	}
	chat := renderChatViewport(m.chat, m.nick, chatN, w)
	for _, ln := range strings.Split(chat, "\n") {
		parts = append(parts, clampCells(ln, w))
	}

	parts = append(parts, clampCells(m.footerPrompt(w), w))

	if len(parts) > h {
		// keep header + video head + prompt
		keep := make([]string, 0, h)
		keep = append(keep, parts[0])
		tail := parts[len(parts)-1]
		mid := parts[1 : len(parts)-1]
		budget := h - 2
		if budget < 0 {
			budget = 0
		}
		if len(mid) > budget {
			// prefer keeping video (front of mid) over chat tail
			mid = mid[:budget]
		}
		keep = append(keep, mid...)
		if h > 1 {
			keep = append(keep, tail)
		}
		parts = keep
	}
	return strings.Join(parts, "\n")
}

// headerLine: ◈ gy ● chat·live·grok·watch  flags        crumb
func (m *Model) headerLine(w int) string {
	conn := styDim().Render("○")
	if m.connected {
		conn = styLive().Render("●")
	}
	if m.talking {
		conn = styErr().Reverse(true).Render("TX")
	} else if m.remoteTX != "" {
		conn = styAccent().Render("RX")
	}

	title := styTitle().Render("◈ gy") + " " + conn

	modes := modePills(m.promptMode)
	if cellWidth(stripANSI(title+"  "+modes))+12 > w {
		modes = modePillsCompact(m.promptMode)
	}

	left := title + "  " + modes

	var flags []string
	if m.camOn {
		flags = append(flags, "cam")
	}
	if m.vpipe != nil && m.vpipe.Running() {
		flags = append(flags, "vid")
	}
	if m.live != nil && m.live.Playing() {
		flags = append(flags, "pat")
	}
	if m.midiOn {
		flags = append(flags, "midi")
	}
	if m.depth != nil && m.depth.Mode() != DepthOff {
		flags = append(flags, m.depth.Mode().String())
	}
	if sc := m.videoScaleLabel(); sc != "" && m.videoOn {
		flags = append(flags, sc)
	}
	if m.grokThinking {
		flags = append(flags, spinnerFrame(m.spin))
	}
	mid := ""
	if len(flags) > 0 {
		mid = styDim().Render(" " + strings.Join(flags, "·"))
	}

	crumb := m.statusCrumb()
	line := left + mid
	if crumb != "" {
		need := cellWidth(stripANSI(line)) + 1 + cellWidth(crumb)
		if need <= w {
			gap := w - need
			if gap < 1 {
				gap = 1
			}
			line = left + mid + strings.Repeat(" ", gap) + styDim().Render(crumb)
		}
	}
	return clampCells(line, w)
}

func (m *Model) statusCrumb() string {
	s := strings.TrimSpace(m.status)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "connected as ", "")
	s = strings.ReplaceAll(s, " → ", "@")
	return truncate(s, 18)
}

// vizLine: spectrum + vu — single line, hard-clamped
func (m *Model) vizLine(w int) string {
	fixed := 2
	hit := ""
	if m.liveHit != "" && m.live != nil && m.live.Playing() {
		hit = " " + truncate(m.liveHit, 10)
	}
	meta := ""
	if m.videoOn && m.frame != nil {
		meta = " " + m.pixelMode.String()
		if m.depth != nil && m.depth.Mode() != DepthOff {
			meta += "·" + m.depth.Mode().String()
		}
	}
	vuW := 6
	if w < 40 {
		vuW = 4
	}
	fixed += 1 + vuW + cellWidth(hit) + cellWidth(meta)
	specW := w - fixed - 1
	if specW < 8 {
		specW = 8
		vuW = 4
		hit = ""
		meta = ""
		specW = max(6, w-2-1-vuW)
	}

	spec := renderSpectrum(m.bands, specW)
	vu := renderVU(maxF(m.level, m.peak*0.85), vuW)

	label := styDim().Render("♪")
	if m.talking {
		label = styErr().Render("♪")
	} else if m.live != nil && m.live.Playing() {
		label = styLive().Render("♪")
	}

	line := label + " " + spec + " " + vu
	if hit != "" {
		line += styDim().Render(hit)
	}
	if meta != "" {
		line += styDim().Render(meta)
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return clampCells(line, w)
}

func (m *Model) footerPrompt(w int) string {
	if m.talking {
		return clampCells(
			styErr().Reverse(true).Render(" PTT ") + " " +
				styDim().Render("space release"),
			w,
		)
	}
	return clampCells(promptLine(m.promptMode, m.nick, m.input, m.grokThinking, w), w)
}

func (m *Model) showPatternLine() bool {
	if m.live == nil {
		return false
	}
	if m.promptMode == ModeLive {
		return true
	}
	return m.live.Playing()
}

func (m *Model) renderLiveOneLine(w int) string {
	st := styDim().Render("○")
	if m.live != nil && m.live.Playing() {
		st = styLive().Render("▶")
	}
	code := truncate(strings.ReplaceAll(m.liveCode, "\n", " "), max(8, w-16))
	return clampCells(
		st+" "+styDim().Render(code)+styDim().Render(fmt.Sprintf(" ·%d", m.liveCycle)),
		w,
	)
}

func (m *Model) videoTitleShort() string {
	if m.vpipe != nil && m.vpipe.Running() {
		return fmt.Sprintf("vid %s", filepath.Base(m.watchPath))
	}
	if m.frame != nil {
		return fmt.Sprintf("cam %dx%d", m.frame.W, m.frame.H)
	}
	return ""
}

func (m *Model) renderLiveCharm(w int) string {
	return m.renderLiveOneLine(w)
}

func (m *Model) videoTitle() string {
	return m.videoTitleShort()
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
