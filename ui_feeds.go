package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// renderLab — multi-feed wall beside (or above) chat with FPS/scale/style controls.
func (m *Model) renderLab(w, h int) string {
	if m.lab == nil {
		m.lab = newLabState()
		m.lab.On = true
	}
	l := m.lab
	l.ensureDefaults()

	// advance sim feeds
	m.tickLabSims()

	var parts []string
	// header is 2 lines (unix+drift, brand) — split so height accounting is exact
	for _, ln := range strings.Split(m.headerLine(w), "\n") {
		parts = append(parts, clampCells(ln, w))
	}
	// control strip (always)
	parts = append(parts, l.ControlStrip(w))
	parts = append(parts, clampCells(styDim().Render(l.BudgetLine()), w))
	// optional expanded lists
	if l.ShowList {
		list := l.ControlList(w)
		for _, ln := range strings.Split(list, "\n") {
			parts = append(parts, ln)
		}
	}

	used := len(parts) + 1 // + prompt
	remain := h - used
	if remain < 4 {
		remain = 4
	}

	body := m.renderLabBody(w, remain, l)
	for _, ln := range strings.Split(body, "\n") {
		parts = append(parts, clampCells(ln, w))
	}
	parts = append(parts, clampCells(m.footerPrompt(w), w))

	if len(parts) > h {
		parts = parts[:h]
		if h > 0 {
			parts[h-1] = clampCells(m.footerPrompt(w), w)
		}
	}
	return strings.Join(parts, "\n")
}

func (m *Model) renderLabBody(w, h int, l *LabState) string {
	switch l.Layout {
	case LayoutStack:
		return m.renderLabStack(w, h, l)
	case LayoutGrid:
		return m.renderLabGrid(w, h, l, false)
	case LayoutFocus:
		return m.renderLabFocus(w, h, l)
	default: // side
		return m.renderLabSide(w, h, l)
	}
}

// feeds | chat
func (m *Model) renderLabSide(w, h int, l *LabState) string {
	feedW := max(20, (w*3)/5)
	chatW := max(16, w-feedW-1)
	if feedW+1+chatW > w {
		chatW = w - feedW - 1
	}
	feedLines := m.renderFeedMosaic(feedW, h, l)
	chat := renderChatViewport(m.chat, m.nick, h, chatW)
	fl := strings.Split(feedLines, "\n")
	cl := strings.Split(chat, "\n")
	for len(fl) < h {
		fl = append(fl, strings.Repeat(" ", feedW))
	}
	for len(cl) < h {
		cl = append(cl, "")
	}
	sep := styDim().Render("│")
	var out []string
	for i := 0; i < h; i++ {
		left := padOrTrim(fl[i], feedW)
		right := clampCells(cl[i], chatW)
		out = append(out, left+sep+right)
	}
	return strings.Join(out, "\n")
}

// feeds above, chat below
func (m *Model) renderLabStack(w, h int, l *LabState) string {
	feedH := max(3, (h*2)/3)
	chatH := max(2, h-feedH)
	feed := m.renderFeedMosaic(w, feedH, l)
	chat := renderChatViewport(m.chat, m.nick, chatH, w)
	return feed + "\n" + chat
}

// mosaic, thin chat under
func (m *Model) renderLabGrid(w, h int, l *LabState, _ bool) string {
	chatH := 2
	if h > 10 {
		chatH = 3
	}
	feedH := max(3, h-chatH)
	feed := m.renderFeedMosaic(w, feedH, l)
	chat := renderChatViewport(m.chat, m.nick, chatH, w)
	return feed + "\n" + chat
}

// active feed large | chat
func (m *Model) renderLabFocus(w, h int, l *LabState) string {
	feedW := max(24, (w*2)/3)
	chatW := max(14, w-feedW-1)
	af := l.ActiveFeed()
	var feedBody string
	if af == nil || af.Frame == nil {
		feedBody = styDim().Render("no active feed — a sim · c cam")
		// pad height
		var lines []string
		for i := 0; i < h; i++ {
			if i == h/2 {
				lines = append(lines, clampCells(feedBody, feedW))
			} else {
				lines = append(lines, strings.Repeat(" ", feedW))
			}
		}
		feedBody = strings.Join(lines, "\n")
	} else {
		// full panel for active
		half := max(2, h-1)
		title := styDim().Render(fmt.Sprintf("%s · %s", af.Kind, af.Label))
		body := fitHalfBlock(RenderFrameH(af.Frame, l.Style, feedW, half-1), feedW, half-1)
		feedBody = clampCells(title, feedW) + "\n" + body
		// ensure h lines
		fl := strings.Split(feedBody, "\n")
		for len(fl) < h {
			fl = append(fl, strings.Repeat(" ", feedW))
		}
		if len(fl) > h {
			fl = fl[:h]
		}
		for i := range fl {
			fl[i] = padOrTrim(fl[i], feedW)
		}
		feedBody = strings.Join(fl, "\n")
	}
	chat := renderChatViewport(m.chat, m.nick, h, chatW)
	fl := strings.Split(feedBody, "\n")
	cl := strings.Split(chat, "\n")
	for len(cl) < h {
		cl = append(cl, "")
	}
	sep := styDim().Render("│")
	var out []string
	for i := 0; i < h; i++ {
		left := fl[i]
		if cellWidth(stripANSI(left)) < feedW {
			left = padOrTrim(left, feedW)
		}
		out = append(out, left+sep+clampCells(cl[i], chatW))
	}
	return strings.Join(out, "\n")
}

// renderFeedMosaic tiles all feeds into w×h cells.
func (m *Model) renderFeedMosaic(w, h int, l *LabState) string {
	n := len(l.Feeds)
	if n == 0 {
		msg := styDim().Render("lab empty · a +sim · c +cam · /watch url · L layout")
		var lines []string
		for i := 0; i < h; i++ {
			if i == h/2 {
				lines = append(lines, clampCells(msg, w))
			} else {
				lines = append(lines, strings.Repeat(" ", w))
			}
		}
		return strings.Join(lines, "\n")
	}

	gc, gr := tileGrid(n)
	// scale preset caps tile width
	tileW := max(12, min(l.Scale, w/gc))
	// leave gap 1 between tiles if space
	if tileW*gc > w {
		tileW = max(10, w/gc)
	}
	tileH := max(3, h/gr)
	if tileH*gr > h {
		tileH = max(2, h/gr)
	}
	// half-rows for image = tileH - 1 (title)
	half := max(1, tileH-1)

	// build each tile as []string of tileH lines
	tiles := make([][]string, n)
	for i := range l.Feeds {
		f := &l.Feeds[i]
		kind := f.Kind
		if kind == "" {
			kind = "empty"
		}
		title := kind + ":" + f.Label
		if f.IsEmpty() {
			title = fmt.Sprintf("· slot %d", i+1)
		}
		if i == l.Active {
			title = styLive().Render("► " + title)
		} else {
			title = styDim().Render("  " + title)
		}
		var body string
		if f.Frame != nil && !f.IsEmpty() {
			// per-tile style for news wall (GrokGlyph variety); else lab.Style
			st := l.Style
			if f.Kind == "news" && f.TileStyle >= 0 && f.TileStyle < PixelCount {
				st = f.TileStyle
			}
			name := st.String()
			if f.PluginStyle != "" {
				name = f.PluginStyle
			} else if l.PluginStyle != "" {
				name = l.PluginStyle
			}
			body = fitHalfBlock(RenderFrameNamed(f.Frame, name, st, tileW, half), tileW, half)
		} else {
			// empty placeholder — drop target for cam / video
			ph := make([]string, half)
			hint := "c=cam /watch=vid"
			for j := range ph {
				if j == half/2 {
					ph[j] = styDim().Render(truncate(hint, tileW))
				} else {
					ph[j] = styDim().Render(strings.Repeat("·", min(tileW, 12)))
				}
			}
			body = strings.Join(ph, "\n")
		}
		lines := []string{clampCells(title, tileW)}
		for _, ln := range strings.Split(body, "\n") {
			lines = append(lines, padOrTrim(ln, tileW))
		}
		for len(lines) < tileH {
			lines = append(lines, strings.Repeat(" ", tileW))
		}
		if len(lines) > tileH {
			lines = lines[:tileH]
		}
		tiles[i] = lines
	}

	// assemble grid rows
	var out []string
	for row := 0; row < gr; row++ {
		for ly := 0; ly < tileH; ly++ {
			var line strings.Builder
			for col := 0; col < gc; col++ {
				idx := row*gc + col
				if idx < n {
					line.WriteString(tiles[idx][ly])
				} else {
					line.WriteString(strings.Repeat(" ", tileW))
				}
				if col+1 < gc {
					line.WriteString(styDim().Render("│"))
				}
			}
			out = append(out, clampCells(line.String(), w))
		}
		if row+1 < gr {
			out = append(out, clampCells(styDim().Render(strings.Repeat("─", w)), w))
		}
	}
	// pad / trim to h
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	if len(out) > h {
		out = out[:h]
	}
	return strings.Join(out, "\n")
}

// tickLabSims paints procedural motion into sim feed frames at lab FPS/scale.
// Heavy styles (depth/gsplat/halftone) auto-throttle to keep stream responsive.
// News wall tiles pull snapshots from per-agency NewsTilePipe captures.
func (m *Model) tickLabSims() {
	if m.lab == nil || !m.lab.On {
		return
	}
	l := m.lab
	// throttle by FPS, scaled by style cost under live filters
	interval := StyleStreamInterval(l.Style, l.FPS)
	if !l.lastCap.IsZero() && time.Since(l.lastCap) < interval {
		return
	}
	l.lastCap = time.Now()

	// pixel size from scale, capped further under heavy styles (stream mitigation)
	pw, ph := StyleSimBudget(l.Style, l.Scale)
	t := float64(time.Now().UnixMilli())

	// news wall: pull live glyph frames + soft-recover dead pipes
	if l.News != nil && l.News.On {
		m.syncNewsWallFrames()
		m.recoverNewsWallTiles()
	}

	for i := range l.Feeds {
		f := &l.Feeds[i]
		switch f.Kind {
		case "sim":
			f.Frame = genSimFrame(pw, ph, t, f.Seed)
			if f.Forge != nil {
				StampFrame(f.Frame, *f.Forge)
			}
		case "cam":
			// shared cam frame if available
			if m.frame != nil && m.camOn {
				f.Frame = m.frame
			}
		case "watch":
			if m.vpipe != nil && m.frame != nil && (f.Frame == nil || f.Label == m.watchPath || strings.Contains(m.watchPath, f.Label)) {
				f.Frame = m.frame
			}
		case "news":
			// frames synced above from NewsTilePipe
		case "pcap":
			// multi-pcap orchestration: advance per lab FPS
			if len(f.PcapPkts) == 0 {
				continue
			}
			// step index ~ every render at lab FPS (renderLab is called each view)
			// use spin from model for phase
			step := m.spin / max(1, 20/max(1, l.FPS))
			f.PcapIdx = (step + f.Seed) % len(f.PcapPkts)
			p := f.PcapPkts[f.PcapIdx]
			if fr, err := FrameFromPacket(&p); err == nil && fr != nil {
				if f.Forge != nil {
					StampFrame(fr, *f.Forge)
				}
				f.Frame = fr
			}
		}
	}
	// also push primary cam into first cam slot
	if m.frame != nil {
		for i := range l.Feeds {
			if l.Feeds[i].Kind == "cam" {
				l.Feeds[i].Frame = m.frame
				break
			}
		}
	}
}

// genSimFrame — procedural RGB for multi-feed demos (unique seed phase).
func genSimFrame(w, h int, tMs float64, seed int) *FramePixels {
	rgb := make([]byte, w*h*3)
	t := tMs * 0.001
	phase := float64(seed) * 1.7
	cx := float64(w)*0.5 + math.Sin(t*0.7+phase)*float64(w)*0.08
	cy := float64(h)*0.42 + math.Cos(t*0.5+phase*0.3)*float64(h)*0.05
	faceR := math.Min(float64(w), float64(h)) * 0.22
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			nx := float64(x) / float64(w)
			ny := float64(y) / float64(h)
			r := 18 + ny*40 + math.Sin(nx*6+t+phase)*10
			g := 22 + ny*35 + math.Cos(nx*4-t*0.8+phase)*8
			b := 40 + nx*50 + math.Sin(t+ny*5+phase)*15
			dFace := math.Hypot(float64(x)-cx, float64(y)-cy)
			if dFace < faceR {
				k := 1 - dFace/faceR
				skin := 0.55 + 0.45*k
				r = r*(1-skin) + (160+float64(seed)*12)*skin
				g = g*(1-skin) + (130+20*k)*skin
				b = b*(1-skin) + (110+15*k)*skin
			}
			rgb[i] = byte(clamp255(r))
			rgb[i+1] = byte(clamp255(g))
			rgb[i+2] = byte(clamp255(b))
		}
	}
	return &FramePixels{W: w, H: h, RGB: rgb, Source: "sim", Stamp: int64(tMs)}
}
