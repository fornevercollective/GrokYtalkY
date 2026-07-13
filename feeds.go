package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Multi-feed video lab — FPS / scale / style / layout controls with feeds
// listed beside chat (overview VFL / site vwall vocabulary).

const MaxLabFeeds = 12 // news wall mosaic (was 6); still soft-capped in UI helpers

// FeedLayout arranges the video wall relative to chat.
type FeedLayout int

const (
	LayoutSide FeedLayout = iota // feeds | chat (default lab)
	LayoutStack                  // feeds above, chat below
	LayoutGrid                   // mosaic only, thin chat under
	LayoutFocus                  // active feed large + chat side
	LayoutCount
)

func (l FeedLayout) String() string {
	switch l {
	case LayoutSide:
		return "side"
	case LayoutStack:
		return "stack"
	case LayoutGrid:
		return "grid"
	case LayoutFocus:
		return "focus"
	default:
		return "?"
	}
}

// Scale presets (display cell width target) — mirrors site vwall 32–128.
var scalePresets = []int{32, 48, 64, 80, 96, 112, 128}

// FPS presets for cam/lab redraw.
var fpsPresets = []int{4, 6, 8, 12, 15, 20, 24, 30}

// FeedSlot is one simulcast tile (sim / camera / watch / remote / pcap / news / empty).
type FeedSlot struct {
	ID    string
	Label string
	Kind  string // empty | sim | cam | watch | remote | burst | pcap | news
	Frame *FramePixels
	Seed  int
	// WatchSrc keeps original path/URL for re-open / capacity accounting
	WatchSrc string
	// Per-tile pixel style (news wall GrokGlyph variety); 0 = use lab.Style
	TileStyle PixelMode
	// PluginStyle optional plugin painter name (invert|mirror|heatmap|…)
	PluginStyle string
	// Pcap loop (multi-pcap orchestration)
	PcapPkts []StreamPacket
	PcapIdx  int
	// Forge watermark (Cursor-Grok Forge NFT-style marker)
	Forge *ForgeMark
}

// IsEmpty placeholder waiting for cam/video.
func (f *FeedSlot) IsEmpty() bool {
	return f == nil || f.Kind == "" || f.Kind == "empty"
}

// LabState holds multi-feed + camera controls.
type LabState struct {
	On       bool
	Feeds    []FeedSlot
	Active   int // index into Feeds
	FPS      int
	Scale    int // target tile cols
	Style    PixelMode
	// PluginStyle when set, paint uses plugin registry instead of Style alone
	PluginStyle string
	Layout      FeedLayout
	ShowList    bool // list control options in chrome
	// cam capture interval derived from FPS
	lastCap time.Time
	uid     int
	// News wall (GrokGlyph-style multi-agency glyph mosaic)
	News *NewsWallState
}

func newLabState() *LabState {
	l := &LabState{
		FPS:      12,
		Scale:    64,
		Style:    PixelHalf,
		Layout:   LayoutSide,
		ShowList: true,
		Active:   0,
	}
	// start with empty placeholders so cam/video can drop in quickly
	l.EnsurePlaceholders(4)
	return l
}

// EnsurePlaceholders grows empty slots up to n (capped at MaxLabFeeds).
func (l *LabState) EnsurePlaceholders(n int) {
	if l == nil {
		return
	}
	if n > MaxLabFeeds {
		n = MaxLabFeeds
	}
	for len(l.Feeds) < n {
		l.uid++
		id := fmt.Sprintf("slot-%d", l.uid)
		l.Feeds = append(l.Feeds, FeedSlot{
			ID: id, Label: fmt.Sprintf("·%d", len(l.Feeds)+1),
			Kind: "empty", Seed: l.uid,
		})
	}
}

// FillActive sets the active slot (or first empty) to kind with optional frame/src.
// Quick path: c → cam into placeholder, /watch → video into placeholder.
func (l *LabState) FillActive(kind, label, watchSrc string, frame *FramePixels) *FeedSlot {
	if l == nil {
		return nil
	}
	l.ensureDefaults()
	// prefer active if empty/sim; else first empty; else append
	idx := l.Active
	if idx < 0 || idx >= len(l.Feeds) {
		idx = -1
	}
	if idx >= 0 {
		f := &l.Feeds[idx]
		if f.IsEmpty() || f.Kind == "sim" || kind == "cam" || kind == "watch" {
			// overwrite active placeholder/sim (or force cam/watch on active)
			goto fill
		}
	}
	for i := range l.Feeds {
		if l.Feeds[i].IsEmpty() {
			idx = i
			goto fill
		}
	}
	if len(l.Feeds) >= MaxLabFeeds {
		// replace active
		if l.Active >= 0 && l.Active < len(l.Feeds) {
			idx = l.Active
		} else {
			idx = 0
		}
		goto fill
	}
	l.uid++
	l.Feeds = append(l.Feeds, FeedSlot{ID: fmt.Sprintf("f%d", l.uid), Seed: l.uid})
	idx = len(l.Feeds) - 1
fill:
	l.Active = idx
	f := &l.Feeds[idx]
	if f.ID == "" {
		l.uid++
		f.ID = fmt.Sprintf("f%d", l.uid)
	}
	f.Kind = kind
	if label != "" {
		f.Label = truncate(label, 14)
	} else {
		f.Label = kind
	}
	f.WatchSrc = watchSrc
	if frame != nil {
		f.Frame = frame
	}
	if kind != "pcap" {
		f.PcapPkts = nil
		f.PcapIdx = 0
	}
	return f
}

// FillPcapIntoSlot loads a stream file into slot (1-based) for multi-pcap orchestration.
// Applies Cursor-Grok Forge watermark identity per slot.
func (l *LabState) FillPcapIntoSlot(slot1 int, path string) (*FeedSlot, error) {
	if l == nil {
		return nil, fmt.Errorf("no lab")
	}
	l.ensureDefaults()
	pkts, err := LoadStreamFile(expandPath(path))
	if err != nil {
		return nil, err
	}
	video := make([]StreamPacket, 0, len(pkts))
	for _, p := range pkts {
		if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
			video = append(video, p)
		}
	}
	if len(video) == 0 {
		return nil, fmt.Errorf("no video packets in %s", path)
	}
	// ensure slot exists
	for len(l.Feeds) < slot1 {
		l.EnsurePlaceholders(len(l.Feeds) + 1)
	}
	if slot1 < 1 || slot1 > MaxLabFeeds {
		return nil, fmt.Errorf("slot 1–%d", MaxLabFeeds)
	}
	idx := slot1 - 1
	l.Active = idx
	f := &l.Feeds[idx]
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	// content commitment from first payload
	var content []byte
	if len(video[0].Payload) > 0 {
		content = video[0].Payload
		if len(content) > 256 {
			content = content[:256]
		}
	}
	mark := NewForgeMark(slot1, base, content)
	f.Kind = "pcap"
	f.Label = truncate(base, 12)
	f.WatchSrc = path
	f.PcapPkts = video
	f.PcapIdx = 0
	f.Forge = &mark
	// prime first frame with watermark
	if fr, err := FrameFromPacket(&video[0]); err == nil && fr != nil {
		StampFrame(fr, mark)
		f.Frame = fr
	}
	return f, nil
}

// ClearPcapFields when clearing slot
func (l *LabState) clearSlotMedia(i int) {
	if i < 0 || i >= len(l.Feeds) {
		return
	}
	l.Feeds[i].PcapPkts = nil
	l.Feeds[i].PcapIdx = 0
	l.Feeds[i].Forge = nil
}

// FillCamIntoActive drops camera into active/empty placeholder.
func (l *LabState) FillCamIntoActive() *FeedSlot {
	return l.FillActive("cam", "cam", "", nil)
}

// FillWatchIntoActive drops resolved video into active/empty placeholder.
func (l *LabState) FillWatchIntoActive(label, src string, frame *FramePixels) *FeedSlot {
	return l.FillActive("watch", label, src, frame)
}

// FillSimIntoActive quick procedural feed into placeholder.
func (l *LabState) FillSimIntoActive() *FeedSlot {
	l.uid++
	return l.FillActive("sim", fmt.Sprintf("sim-%d", l.uid), "", nil)
}

// SelectSlot 1-based index (keys 1–6).
func (l *LabState) SelectSlot(n int) bool {
	if l == nil || n < 1 || n > len(l.Feeds) {
		return false
	}
	l.Active = n - 1
	return true
}

// ClearActive resets slot to empty placeholder.
func (l *LabState) ClearActive() {
	if l == nil || len(l.Feeds) == 0 {
		return
	}
	i := l.Active
	if i < 0 || i >= len(l.Feeds) {
		return
	}
	l.Feeds[i].Kind = "empty"
	l.Feeds[i].Label = fmt.Sprintf("·%d", i+1)
	l.Feeds[i].Frame = nil
	l.Feeds[i].WatchSrc = ""
	l.clearSlotMedia(i)
}

// BudgetLine human-readable live data budget for current lab settings.
func (l *LabState) BudgetLine() string {
	if l == nil {
		return ""
	}
	l.ensureDefaults()
	n := len(l.Feeds)
	if n == 0 {
		n = 1
	}
	// display pixels ≈ scale × (scale*10/16), RGB24 × FPS × feeds × style cost
	pw := l.Scale
	ph := max(12, pw*10/16)
	bytesPerFrame := pw * ph * 3
	cost := float64(l.Style.StyleCost())
	// heavy styles downsample work frames (~0.6–1.0× paint budget)
	if l.Style.HeavyStyle() {
		cost *= 0.75
	}
	effFPS := float64(l.FPS)
	if l.Style.HeavyStyle() {
		effFPS = math.Min(effFPS, 8)
	}
	bps := float64(bytesPerFrame)*effFPS*float64(n)*8*math.Max(1, cost*0.35)
	mbps := bps / 1e6
	// mesh JPEG burst estimate ~15KB @ min(fps,8)
	jfps := l.FPS
	if jfps > 8 {
		jfps = 8
	}
	jMbps := float64(n*15*1024*jfps) * 8 / 1e6
	return fmt.Sprintf("budget ~%.1f Mbps RGB tiles (%dx%d@%d ×%d) · mesh JPEG ~%.1f Mbps",
		mbps, pw, ph, l.FPS, n, jMbps)
}

func (l *LabState) ensureDefaults() {
	if l.FPS < 1 {
		l.FPS = 12
	}
	if l.Scale < 16 {
		l.Scale = 64
	}
	if l.Style < 0 || l.Style >= PixelCount {
		l.Style = PixelHalf
	}
}

func (l *LabState) AddSim() {
	if l == nil || len(l.Feeds) >= MaxLabFeeds {
		return
	}
	l.uid++
	id := fmt.Sprintf("sim-%d", l.uid)
	l.Feeds = append(l.Feeds, FeedSlot{
		ID: id, Label: id, Kind: "sim", Seed: l.uid,
	})
	l.Active = len(l.Feeds) - 1
}

func (l *LabState) AddCam() {
	if l == nil || len(l.Feeds) >= MaxLabFeeds {
		return
	}
	l.uid++
	id := fmt.Sprintf("cam-%d", l.uid)
	l.Feeds = append(l.Feeds, FeedSlot{
		ID: id, Label: id, Kind: "cam", Seed: l.uid,
	})
	l.Active = len(l.Feeds) - 1
}

func (l *LabState) AddWatch(label string, frame *FramePixels) {
	if l == nil || len(l.Feeds) >= MaxLabFeeds {
		return
	}
	l.uid++
	id := fmt.Sprintf("vid-%d", l.uid)
	if label == "" {
		label = id
	}
	l.Feeds = append(l.Feeds, FeedSlot{
		ID: id, Label: truncate(label, 14), Kind: "watch",
		Frame: frame, Seed: l.uid,
	})
	l.Active = len(l.Feeds) - 1
}

func (l *LabState) RemoveActive() {
	if l == nil || len(l.Feeds) == 0 {
		return
	}
	i := l.Active
	if i < 0 || i >= len(l.Feeds) {
		i = 0
	}
	l.Feeds = append(l.Feeds[:i], l.Feeds[i+1:]...)
	if l.Active >= len(l.Feeds) {
		l.Active = len(l.Feeds) - 1
	}
	if l.Active < 0 {
		l.Active = 0
	}
}

func (l *LabState) NextFeed() {
	if l == nil || len(l.Feeds) == 0 {
		return
	}
	l.Active = (l.Active + 1) % len(l.Feeds)
}

func (l *LabState) CycleLayout() FeedLayout {
	l.Layout = (l.Layout + 1) % LayoutCount
	return l.Layout
}

// CycleStyle advances built-in PixelMode, then enabled plugin painters, then wraps.
func (l *LabState) CycleStyle() PixelMode {
	names := Plugins().StyleNames()
	// already on a plugin painter → advance / exit plugin ladder
	if l.PluginStyle != "" {
		for i, n := range names {
			if strings.EqualFold(n, l.PluginStyle) {
				if i+1 < len(names) {
					l.PluginStyle = names[i+1]
					return l.Style
				}
				break
			}
		}
		l.PluginStyle = ""
		l.Style = PixelHalf
		return l.Style
	}
	next := (l.Style + 1) % PixelCount
	// finished built-ins → first plugin style if any
	if next == 0 && len(names) > 0 {
		l.PluginStyle = names[0]
		return l.Style
	}
	l.Style = next
	return l.Style
}

func (l *LabState) NudgeScale(dir int) int {
	// dir +1 / -1 along presets
	idx := 0
	for i, s := range scalePresets {
		if s >= l.Scale {
			idx = i
			break
		}
		idx = i
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(scalePresets) {
		idx = len(scalePresets) - 1
	}
	l.Scale = scalePresets[idx]
	return l.Scale
}

func (l *LabState) NudgeFPS(dir int) int {
	idx := 0
	for i, f := range fpsPresets {
		if f >= l.FPS {
			idx = i
			break
		}
		idx = i
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(fpsPresets) {
		idx = len(fpsPresets) - 1
	}
	l.FPS = fpsPresets[idx]
	return l.FPS
}

func (l *LabState) ActiveFeed() *FeedSlot {
	if l == nil || len(l.Feeds) == 0 {
		return nil
	}
	if l.Active < 0 || l.Active >= len(l.Feeds) {
		l.Active = 0
	}
	return &l.Feeds[l.Active]
}

// ControlStrip lists FPS / scale / style / layout options (active marked).
func (l *LabState) ControlStrip(width int) string {
	if l == nil {
		return ""
	}
	l.ensureDefaults()
	// compact always-on line + media supervisor health
	mh := Media().Health()
	line := fmt.Sprintf("fps %s  scale %s  style %s  layout %s  feeds %d/%d  · %s",
		listMark(fpsPresets, l.FPS),
		listMark(scalePresets, l.Scale),
		listStyles(l.Style),
		listLayouts(l.Layout),
		len(l.Feeds), MaxLabFeeds,
		FormatMediaHealthChrome(mh),
	)
	if l.News != nil && l.News.On {
		line += "  R restart · K kill tile"
	}
	return clampCells(styDim().Render(line), width)
}

// ControlList multi-line expanded option listing for lab.
func (l *LabState) ControlList(width int) string {
	if l == nil || !l.ShowList {
		return ""
	}
	var b strings.Builder
	b.WriteString(styDim().Render("fps    "))
	b.WriteString(listMark(fpsPresets, l.FPS))
	b.WriteByte('\n')
	b.WriteString(styDim().Render("scale  "))
	b.WriteString(listMark(scalePresets, l.Scale))
	b.WriteByte('\n')
	b.WriteString(styDim().Render("style  "))
	b.WriteString(listStyles(l.Style))
	b.WriteByte('\n')
	b.WriteString(styDim().Render("layout "))
	b.WriteString(listLayouts(l.Layout))
	b.WriteByte('\n')
	// feed list
	b.WriteString(styDim().Render("feeds  "))
	if len(l.Feeds) == 0 {
		b.WriteString(styDim().Render("(none — a sim · c cam · /watch)"))
	} else {
		for i, f := range l.Feeds {
			tag := f.Kind
			if i == l.Active {
				b.WriteString(styLive().Render(fmt.Sprintf("[%s:%s] ", tag, f.Label)))
			} else {
				b.WriteString(styDim().Render(fmt.Sprintf("%s:%s ", tag, f.Label)))
			}
		}
	}
	out := b.String()
	// clamp each line
	var lines []string
	for _, ln := range strings.Split(out, "\n") {
		lines = append(lines, clampCells(ln, width))
	}
	return strings.Join(lines, "\n")
}

func listMark(presets []int, cur int) string {
	var parts []string
	for _, p := range presets {
		s := fmt.Sprintf("%d", p)
		if p == cur {
			parts = append(parts, styAccent().Bold(true).Render(s))
		} else {
			parts = append(parts, styDim().Render(s))
		}
	}
	return strings.Join(parts, styDim().Render("·"))
}

func listStyles(cur PixelMode) string {
	var parts []string
	for i := PixelMode(0); i < PixelCount; i++ {
		s := i.String()
		if i == cur {
			parts = append(parts, styAccent().Bold(true).Render(s))
		} else {
			parts = append(parts, styDim().Render(s))
		}
	}
	return strings.Join(parts, styDim().Render("·"))
}

func listLayouts(cur FeedLayout) string {
	var parts []string
	for i := FeedLayout(0); i < LayoutCount; i++ {
		s := i.String()
		if i == cur {
			parts = append(parts, styAccent().Bold(true).Render(s))
		} else {
			parts = append(parts, styDim().Render(s))
		}
	}
	return strings.Join(parts, styDim().Render("·"))
}

// KeysHelp for lab mode.
func LabKeysHelp() string {
	return "lab: drop files · 1-6 slot · c cam · a sim · /watch · [ ] scale · , . fps · m style · L layout · r clear"
}

// tileGrid returns cols/rows for n feeds.
func tileGrid(n int) (cols, rows int) {
	switch {
	case n <= 1:
		return 1, 1
	case n == 2:
		return 2, 1
	case n <= 4:
		return 2, 2
	default:
		return 3, 2
	}
}
