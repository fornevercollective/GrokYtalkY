package main

// Terminal video scale — fill the space *between* chrome (header/viz/chat/prompt)
// and the terminal edges. Capture resolution tracks display scale so half-blocks
// stay sharp from small docks to full-height watch.

// videoScale describes one paint of the video panel.
type videoScale struct {
	// terminal cells
	Cols     int // half-block columns (= pixel width)
	HalfRows int // terminal rows of ▀ (= pixel height / 2)
	// source sample size for cam/ffmpeg (pixels)
	SrcW int
	SrcH int
	// chat lines after video
	ChatLines int
	// true when video panel is shown
	Active bool
}

// chromeLines: fixed UI above/below the video pane.
func (m *Model) chromeLines() (above, below int) {
	// above: unix clock line + brand header + viz (+ optional pattern)
	// headerLine is now 2 rows (unix+drift, then ◈ gy …)
	above = 3
	if m.showPatternLine() {
		above++
	}
	// below: prompt always
	below = 1
	return above, below
}

// computeVideoScale fills remaining terminal with video when video is on.
// When video is off, returns Active=false and a generous chat budget.
func (m *Model) computeVideoScale(termW, termH int) videoScale {
	w := safeCols(termW)
	if w < 20 {
		w = max(12, termW)
	}
	if termH < 8 {
		termH = 8
	}

	above, below := m.chromeLines()
	// reserve minimum chat so log doesn't vanish
	minChat := 2
	if termH < 14 {
		minChat = 1
	}
	// when video off — all free space is chat
	if !m.videoOn || m.frame == nil {
		chat := termH - above - below
		if chat < minChat {
			chat = minChat
		}
		if chat > 16 {
			chat = 16
		}
		return videoScale{Cols: w, HalfRows: 0, ChatLines: chat, Active: false}
	}

	// free rows for video + chat
	free := termH - above - below
	if free < 3 {
		free = 3
	}

	// Prefer video: give chat a fixed band, rest to half-block rows
	chat := minChat
	if m.compact {
		// companion: keep a bit more chat (3–4) on tall terms
		chat = min(4, max(minChat, free/5))
	} else {
		// full: thinner chat, max video
		chat = min(3, max(minChat, free/6))
	}
	half := free - chat
	if half < 2 {
		half = 2
		chat = max(1, free-half)
	}

	// Full-width half-blocks (use almost entire terminal width)
	cols := w
	if m.compact && !m.videoFocus() {
		// companion without focus still uses full width for video — user asked
		// to fully leverage scale; only burst stays small.
		cols = w
	}

	// Aspect clamp: don't make a super-tall strip for very wide terminals.
	// 16:9 half-rows ≈ cols * 9/32 ; allow up to ~2× that when height allows.
	// Mobile social (double-stack GrokGlyph) prefers portrait ~1:2.
	ideal := max(3, (cols*9)/32)
	maxTall := ideal * 2
	if m.watchMobile {
		// portrait double-stack: half-rows ≈ cols (pixel aspect ~1:2)
		gn := m.glyphN
		if gn < 13 {
			gn = 25
		}
		ideal = MobilePortraitHalfRows(cols, gn)
		maxTall = ideal
		if maxTall > free-1 {
			maxTall = free - 1
		}
		if maxTall < 6 {
			maxTall = min(6, free-1)
		}
	}
	if maxTall < half {
		// use available height up to maxTall, rest → chat
		extra := half - maxTall
		half = maxTall
		chat += extra
	}
	// On short terminals, never exceed free-chat
	if half+chat > free {
		half = free - chat
	}
	if half < 2 {
		half = 2
	}
	if half > free-1 {
		half = free - 1
		chat = max(1, free-half)
	}

	// Source capture: match display scale (pixel H = 2 * half-rows)
	// Cap sample size for cam FPS; still proportional to panel.
	srcW := cols
	srcH := half * 2
	// upsample sample a bit for sharper half-blocks when panel is large
	if srcW >= 48 {
		srcW = min(160, srcW*2)
		srcH = min(96, srcH*2)
	}
	if srcH%2 != 0 {
		srcH++
	}
	if srcH < 4 {
		srcH = 4
	}

	return videoScale{
		Cols:      cols,
		HalfRows:  half,
		SrcW:      srcW,
		SrcH:      srcH,
		ChatLines: chat,
		Active:    true,
	}
}

// videoFocus — treat as “video first” when watching, cam, depth, or burst.
func (m *Model) videoFocus() bool {
	if m.burstMode {
		return true
	}
	if m.vpipe != nil && m.vpipe.Running() {
		return true
	}
	if m.camOn || m.videoOn {
		return true
	}
	if m.depth != nil && m.depth.Mode() != DepthOff {
		return true
	}
	return false
}

// scaleLabel short status crumb e.g. "80×12"
func (s videoScale) label() string {
	if !s.Active {
		return ""
	}
	return itoa(s.Cols) + "×" + itoa(s.HalfRows)
}
