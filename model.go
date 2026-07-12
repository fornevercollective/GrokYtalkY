package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	hwmidi "github.com/fornevercollective/grokytalky/midi"
	"github.com/fornevercollective/grokytalky/strudel"
)

// Model is the Bubble Tea v2 app state (cliamp-style Elm architecture).
// Version string lives in version.go (ldflags-overridable).
type Model struct {
	nick      string
	host      string
	width     int
	height    int
	connected bool
	status    string
	talking   bool
	remoteTX  string
	peers     []peerInfo
	chat      []chatLine
	input     string
	level     float64
	peak      float64
	bands     []float64
	frame     *FramePixels
	frameMeta string
	pixelMode PixelMode
	videoOn   bool
	camOn     bool
	err       string

	// ffmpeg video pipe (mp4/mkv/mov/URL → terminal pixels)
	vpipe     *VideoPipe
	vpipeSeq  uint64
	watchPath string

	client *MeshClient
	player *Player
	ptt    *PTTSession
	cancel context.CancelFunc
	prog   *tea.Program

	lastClip string
	camTick  int

	// signls/sektron-style MIDI + live translation
	midiBridge *hwmidi.Bridge
	xl8        TranslateConfig
	lastXL8    string
	midiOn     bool
	xl8On      bool

	// Strudel / Qbpm live coding
	live       *strudel.Engine
	liveCode   string
	liveHit    string // last hit for viz
	liveCycle  int64
	liveMode   bool // when true, Enter evals pattern instead of chat
	presets    []string

	// Charm / Grok prompt
	promptMode   PromptMode
	showHelp     bool
	grokCfg      GrokConfig
	grokHistory  []GrokMessage
	grokThinking bool
	spin         int
	compact      bool // companion dock (default) — not full takeover
	layoutW      int  // last stable layout width for resize debounce
	layoutH      int

	// Siri-sized video burst orb (Glyph Matrix walkie) — dual side-by-side streams
	burstMode       bool
	burstRemote     string // nick currently bursting video at us
	burstLocalFrame *FramePixels
	burstPeerFrame  *FramePixels
	glyphN          int   // 25 Phone(3) / 13 Phone(4a)
	lastGlyph       []int // last brightness grid for debug / Android bridge

	// Live depth + gsplat (ZipDepth sidecar / zip-lite / overview-style stack)
	depth *depthSession

	// Multi-feed video lab (FPS / scale / style / layout + feeds | chat)
	lab *LabState

	// Binary/hex/pcap packet player + recorder
	pktPlayer *PacketPlayer
	recorder  *RecordSession
}

type peerInfo struct {
	Nick    string
	Talking bool
}

type chatLine struct {
	From string
	Text string
	Sys  bool
	XL8  bool // translation line
}

// messages
type (
	wsStatusMsg string
	wsRawMsg    []byte
	tickMsg     time.Time
	audioLvlMsg struct {
		Level float64
		Bands []float64
		TX    bool
	}
	frameReady    struct{ F *FramePixels; Meta string }
	camSnapMsg    []byte
	errMsg        string
	transcriptMsg Transcript
	liveHitMsg    struct {
		Ev    strudel.Event
		Cycle int64
	}
	liveCycleMsg struct {
		Cycle int64
		CPS   float64
		Code  string
	}
	autoWatchMsg struct{ src string }
	grokReplyMsg struct {
		Text string
		Err  string
	}
)

// Options for NewModel
type Options struct {
	Nick      string
	Host      string
	MIDI      bool
	MIDIDev   string
	Translate bool
	XL8       TranslateConfig
	Live      bool // start in strudel live-coding mode
	Full      bool // full dashboard (default is compact companion)
	NoHub     bool
	Cam       bool
	Burst     bool // Siri-sized video burst orb (Glyph Matrix walkie)
	GlyphN    int  // matrix side (25 or 13); default 25
	Lab       bool // multi-feed video lab next to chat
}

func NewModel(opts Options) *Model {
	m := &Model{
		nick:      opts.Nick,
		host:      opts.Host,
		width:     80,
		height:    24,
		status:    "tab · ? · q",
		bands:     make([]float64, 32),
		pixelMode:  PixelHalf,
		videoOn:    opts.Cam || opts.Full || opts.Burst,
		camOn:      opts.Cam || opts.Burst,
		compact:    !opts.Full,
		burstMode:  opts.Burst,
		glyphN:     opts.GlyphN,
		depth:      newDepthSession(),
		lab:        newLabState(),
		recorder:   NewRecordSession(),
		player:     &Player{},
		midiOn:     opts.MIDI,
		xl8On:      opts.Translate,
		xl8:        opts.XL8,
		liveMode:   opts.Live,
		promptMode: ModeChat,
		liveCode:   `s("bd sd hh cp")`,
		presets: []string{
			`s("bd sd hh cp")`,
			`s("bd*4, [~ sd]*2")`,
			`setcps(0.6)\ns("bd*4, ~ sd, hh*8")`,
			`stack(s("bd*4"), note("c2 g2"))`,
			`s("bd*4"). /* house */\nbpm(124)\ns("bd*4, ~ sd")`,
			`note("c4 e4 g4 c5")`,
			`stack(s("bd*4, sd*2"), note("c3 e3 g3"))`,
		},
		grokCfg: loadGrokConfig(),
		chat: []chatLine{
			{Sys: true, Text: fmt.Sprintf("gy %s · companion", Version)},
		},
	}
	if m.glyphN != GlyphPhone3 && m.glyphN != GlyphPhone4a {
		m.glyphN = GlyphPhone3
	}
	if opts.Burst {
		// exact dual 25×25 needs ≥52 cols × 30 rows; 13×13 needs ≥28×18
		gn := m.glyphN
		if gn != GlyphPhone3 && gn != GlyphPhone4a {
			gn = GlyphPhone3
		}
		m.width = max(80, gn*2+8)
		m.height = max(32, gn+8)
		m.chat = []chatLine{
			{Sys: true, Text: fmt.Sprintf("burst · exact %d×%d full-color Glyph Matrix", gn, gn)},
			{Sys: true, Text: "1 cell = 1 LED · left ◎ you · right ◎ peer"},
		}
		m.status = fmt.Sprintf("burst · ◎ %d×%d color", gn, gn)
		m.camOn = true
		m.videoOn = true
	}
	if opts.Live {
		m.promptMode = ModeLive
		m.liveMode = true
	}

	// MIDI first so live sink can use it (quiet — status only)
	var mid hwmidi.Midi
	var dev int
	if opts.MIDI {
		mid = hwmidi.NewOptional()
		dev = hwmidi.FindDevice(mid.DeviceNames(), opts.MIDIDev)
		m.midiBridge = hwmidi.NewBridge(mid, dev)
		if len(mid.DeviceNames()) == 0 {
			m.midiOn = false
		}
	}

	// Live engine: always attach local audio (MIDI port alone is silent).
	var sinks []strudel.Sink
	audio := strudel.NewAudioSink()
	if audio.Enabled() {
		sinks = append(sinks, audio)
	} else {
		m.status = "no afplay/ffplay"
	}
	if mid != nil {
		ms := strudel.NewMIDISink(mid, dev)
		ms.OnHit(func(ev strudel.Event) {
			if m.prog != nil {
				m.prog.Send(liveHitMsg{Ev: ev})
			}
		})
		sinks = append(sinks, ms)
	}
	sinks = append(sinks, &strudel.FuncSink{Fn: func(ev strudel.Event, cyc int64) {
		if m.prog != nil {
			m.prog.Send(liveHitMsg{Ev: ev, Cycle: cyc})
		}
	}})
	m.live = strudel.NewEngine(&strudel.MultiSink{Sinks: sinks})
	m.live.SetOnCycle(func(cycle int64, cps float64, code string) {
		if m.prog != nil {
			m.prog.Send(liveCycleMsg{Cycle: cycle, CPS: cps, Code: code})
		}
	})
	_ = m.live.Eval(m.liveCode)

	if opts.Translate && !m.xl8.Enabled {
		m.xl8On = false
	}
	if m.grokCfg.APIKey != "" {
		m.status = m.grokCfg.ModeLabel()
	}
	if opts.Lab {
		m.lab.On = true
		m.compact = false
		m.lab.EnsurePlaceholders(4)
		// one sim demo so tiles aren't all empty on first paint
		m.lab.FillSimIntoActive()
		m.lab.NextFeed()
		m.chat = []chatLine{
			{Sys: true, Text: "lab · multi-feed next to chat"},
			{Sys: true, Text: LabKeysHelp()},
			{Sys: true, Text: "drop: select slot 1-6 → c cam · a sim · /watch url"},
		}
		m.status = "lab"
		m.videoOn = true
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		m.connectCmd(),
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second/20, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) SetProgram(p *tea.Program) { m.prog = p }

func (m Model) connectCmd() tea.Cmd {
	return func() tea.Msg {
		// actual connect happens in Run side-channel via AttachClient
		return nil
	}
}

// AttachClient wires the mesh client to send messages into the program.
func (m *Model) AttachClient(ctx context.Context, prog *tea.Program) {
	m.prog = prog
	c := NewMeshClient(m.host, m.nick)
	c.OnStatus = func(s string) {
		prog.Send(wsStatusMsg(s))
	}
	c.OnMessage = func(b []byte) {
		prog.Send(wsRawMsg(append([]byte(nil), b...)))
	}
	m.client = c
	go c.Run(ctx)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// use REAL terminal size only — never invent a larger width (wrap spool)
		nw, nh := msg.Width, msg.Height
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		if nw == m.layoutW && nh == m.layoutH {
			return m, nil
		}
		m.width, m.height = nw, nh
		m.layoutW, m.layoutH = nw, nh
		// always drop frame on resize so cam/watch resample to new scale
		if m.frame != nil {
			m.frame = nil
		}
		// reopen video pipe at new geometry when watching
		if m.vpipe != nil && m.vpipe.Running() && m.watchPath != "" {
			src := m.watchPath
			// watchPath may be title — keep last resolved via vpipe.Src if needed
			if m.vpipe.Src != "" && !isURL(src) && !isVideoPath(src) {
				// title only; can't re-open without original URL — leave pipe
			} else {
				m.vpipe.Stop()
				m.vpipe = nil
				return m.startWatch(src, true)
			}
		}
		return m, nil

	case tickMsg:
		m.spin++
		// spectrum decay (cliamp-style radio viz)
		m.level *= 0.88
		m.peak *= 0.93
		for i := range m.bands {
			m.bands[i] *= 0.90
			if m.bands[i] < 0.02 {
				m.bands[i] = 0
			}
		}
		// soft idle shimmer while pattern is running
		if m.live != nil && m.live.Playing() {
			pulseSpectrum(m.bands, 0.08+m.peak*0.2, m.spin)
		}
		var cmds []tea.Cmd
		// binary/hex/pcap packet player
		if m.pktPlayer != nil && m.pktPlayer.Playing() {
			if fr, seq, ok := m.pktPlayer.Snapshot(); ok && (seq != m.vpipeSeq || m.frame == nil) {
				m.vpipeSeq = seq
				m.frame = fr
				m.videoOn = true
				m.frameMeta = m.pktPlayer.StatusLine()
				m.status = m.pktPlayer.StatusLine()
				m.applyDepthToFrame()
				if m.recorder != nil && m.recorder.Active() {
					m.recorder.AddFrame(fr)
				}
			}
		} else if m.vpipe != nil && (m.vpipe.Alive() || m.vpipe.Paused() || m.vpipe.Running()) {
			// pull ffmpeg video pipe frames; scrub keeps last frame when paused
			if rgb, w, h, seq, ok := m.vpipe.Snapshot(); ok && (seq != m.vpipeSeq || m.frame == nil) {
				m.vpipeSeq = seq
				m.frame = RGBToFramePixels(rgb, w, h, m.watchPath)
				m.frameMeta = m.vpipe.StatusLine()
				m.pixelMode = PixelHalf
				m.videoOn = true
				m.applyDepthToFrame()
				if m.recorder != nil && m.recorder.Active() {
					m.recorder.AddFrame(m.frame)
				}
			}
			if m.vpipe.Alive() || m.vpipe.Paused() {
				m.status = m.vpipe.StatusLine()
			}
		} else if m.camOn && (m.vpipe == nil || !m.vpipe.Alive()) {
			m.camTick++
			// burst TX: snappier face frames; idle cam slower
			every := 8 // ~2.5 fps
			if m.burstMode && m.talking {
				every = 3 // ~6–7 fps for short video bursts
			}
			if m.camTick%every == 0 {
				if m.burstMode {
					cmds = append(cmds, m.captureBurstCamCmd())
				} else {
					cmds = append(cmds, m.captureCamCmd())
				}
			}
		}
		cmds = append(cmds, tickCmd())
		return m, tea.Batch(cmds...)

	case grokReplyMsg:
		m.grokThinking = false
		if msg.Err != "" {
			m.pushSys("grok error: " + msg.Err)
			return m, nil
		}
		m.chat = append(m.chat, chatLine{From: "grok", Text: msg.Text})
		m.trimChat()
		m.grokHistory = append(m.grokHistory, GrokMessage{Role: "assistant", Content: msg.Text})
		if len(m.grokHistory) > 24 {
			m.grokHistory = m.grokHistory[len(m.grokHistory)-24:]
		}
		// auto-eval fenced s(...) patterns from grok if present
		if pat := extractPattern(msg.Text); pat != "" {
			m.pushSys("grok → pattern: " + truncate(pat, 50))
			return m.evalLive(pat, true)
		}
		return m, nil

	case wsStatusMsg:
		s := string(msg)
		m.connected = strings.Contains(s, "connected")
		// keep status lean — no chat spam for mesh noise
		switch {
		case m.connected:
			m.status = m.nick
		case strings.Contains(s, "hello"):
			// ignore hub hello chatter
		default:
			m.status = truncate(s, 36)
		}
		return m, nil

	case wsRawMsg:
		return m.handleWS(msg)

	case audioLvlMsg:
		m.level = msg.Level
		m.peak = PeakHold(m.peak, msg.Level, 0.45, 0.12)
		if len(msg.Bands) > 0 {
			m.bands = msg.Bands
		}
		if m.midiOn && m.midiBridge != nil {
			if msg.TX {
				m.midiBridge.LevelTX(m.peak)
			} else {
				m.midiBridge.LevelRX(m.peak)
			}
		}
		return m, nil

	case frameReady:
		m.frame = msg.F
		m.frameMeta = msg.Meta
		// dual burst: tag local vs peer by meta
		if m.burstMode && msg.F != nil {
			if strings.HasPrefix(msg.Meta, "burst:") {
				m.burstPeerFrame = msg.F
				if nick := strings.TrimPrefix(msg.Meta, "burst:"); nick != "" {
					m.burstRemote = nick
				}
			} else if msg.Meta == "local" || msg.Meta == "burst-local" || m.talking || m.camOn {
				m.burstLocalFrame = msg.F
			}
		}
		m.applyDepthToFrame()
		if m.midiOn && m.midiBridge != nil {
			m.midiBridge.Frame()
		}
		return m, nil

	case camSnapMsg:
		if len(msg) == 0 {
			return m, nil
		}
		// local preview always
		maxW, maxH := m.videoCols(), m.videoPxH()
		if m.burstMode {
			maxW, maxH = 64, 64 // square tiles for dual burst
		}
		meta := "local"
		if m.burstMode {
			meta = "burst-local"
		}
		cmd := decodeFrameCmd(msg, meta, maxW, maxH)
		if m.client == nil {
			return m, cmd
		}
		if m.burstMode && m.talking {
			// decode sync for glyph so we can ship LED grid with the JPEG
			fp, err := decodeFrameJPEG(msg, maxW, maxH)
			if err == nil && fp != nil {
				m.frame = fp
				m.burstLocalFrame = fp
				gm := FrameToGlyph(fp, m.glyphN)
				m.lastGlyph = gm.IntColors()
				m.client.SendBurstFrame(msg, fp.W, fp.H, m.lastGlyph)
			} else {
				m.client.SendBurstFrame(msg, maxW, maxH, nil)
			}
			return m, nil
		}
		if m.burstMode && !m.talking {
			// still refresh local preview tile while idle in burst mode
			return m, cmd
		}
		if !m.burstMode {
			m.client.SendFrame("term:"+m.nick, 320, 200, msg)
		}
		return m, cmd

	case autoWatchMsg:
		return m.startWatch(msg.src, true)

	case liveHitMsg:
		m.liveHit = fmt.Sprintf("%s %s", msg.Ev.Kind, msg.Ev.Sound)
		m.liveCycle = msg.Cycle
		// flash spectrum on hits (cliamp radio viz)
		m.level = 0.75
		m.peak = PeakHold(m.peak, 0.75, 0.8, 0.2)
		hitPulse(m.bands, msg.Ev.Sound)
		return m, nil

	case liveCycleMsg:
		m.liveCycle = msg.Cycle
		// stay quiet — cycle shows on pattern line
		return m, nil

	case transcriptMsg:
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			m.pushSys("translate: (no speech)")
			return m, nil
		}
		m.lastXL8 = text
		line := "🌐 " + text
		m.chat = append(m.chat, chatLine{From: m.nick, Text: line, XL8: true})
		m.trimChat()
		if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "translate", "from": m.nick,
				"text": text, "lang": m.xl8.Lang, "to": map[bool]string{true: "en", false: m.xl8.Lang}[m.xl8.ToEN],
				"t": time.Now().UnixMilli(),
			})
			// also as chat so all peers see it
			m.client.SendChat(line)
		}
		if m.midiOn && m.midiBridge != nil {
			m.midiBridge.Translate()
		}
		return m, nil

	case errMsg:
		m.err = string(msg)
		m.pushSys(string(msg))
		return m, nil

	case tea.PasteMsg:
		// Terminal drag-drop / bracketed paste of file paths
		return m.handlePaste(msg.Content)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handlePaste processes Finder/Terminal drag-drop paths and multi-line pastes.
func (m *Model) handlePaste(content string) (tea.Model, tea.Cmd) {
	content = strings.TrimSpace(content)
	if content == "" {
		return m, nil
	}
	// media drops take priority over chat paste
	if LooksLikeDropPaste(content) {
		paths := ParseDroppedPaths(content)
		return m.ingestDroppedPaths(paths)
	}
	// plain text → append to input (bracketed paste)
	m.input += content
	return m, nil
}

// ingestDroppedPaths loads images into tiles and opens videos via watch/lab.
func (m *Model) ingestDroppedPaths(paths []string) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		return m, nil
	}
	// auto-open lab when dropping multiple files or when already in lab
	if len(paths) > 1 || (m.lab != nil && m.lab.On) {
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
		m.burstMode = false
		m.lab.EnsurePlaceholders(max(4, len(paths)))
	}

	var lastVideo string
	loaded := 0
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || !IsMediaPath(p) {
			continue
		}
		// still image → lab tile / single frame
		if IsImagePath(p) && !isURL(p) {
			maxW, maxH := m.videoCols(), m.videoPxH()
			if m.lab != nil && m.lab.On {
				maxW = max(48, m.lab.Scale)
				maxH = max(32, maxW*10/16)
			}
			fp, err := LoadImageFile(p, maxW, maxH)
			if err != nil {
				m.pushSys("drop image: " + filepath.Base(p) + " · " + err.Error())
				continue
			}
			m.frame = fp
			m.videoOn = true
			if m.lab != nil && m.lab.On {
				m.lab.FillActive("watch", filepath.Base(p), p, fp)
			}
			m.pushSys("drop img → " + filepath.Base(p))
			loaded++
			continue
		}
		// video / URL / stream
		lastVideo = p
		if m.lab != nil && m.lab.On {
			// reserve slot label; startWatch will fill frame
			m.lab.FillWatchIntoActive(filepath.Base(p), p, nil)
			m.lab.NextFeed() // next drop goes to next slot
		}
		loaded++
	}
	if lastVideo != "" {
		m.pushSys(fmt.Sprintf("drop %d media · playing last", loaded))
		return m.startWatch(lastVideo, true)
	}
	if loaded > 0 {
		m.status = fmt.Sprintf("dropped %d", loaded)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "ctrl+c", "ctrl+q":
		m.shutdown()
		return m, tea.Quit
	case "q":
		if m.input == "" && !m.talking && !m.showHelp {
			m.shutdown()
			return m, tea.Quit
		}
	case "esc":
		if m.showHelp {
			m.showHelp = false
			return m, nil
		}
		if m.talking {
			return m.stopPTT()
		}
		m.grokThinking = false
		m.input = ""
		return m, nil
	case "tab":
		if m.lab != nil && m.lab.On && m.input == "" {
			m.lab.NextFeed()
			if af := m.lab.ActiveFeed(); af != nil {
				m.status = "feed " + af.Label
			}
			return m, nil
		}
		m.promptMode = (m.promptMode + 1) % ModeCount
		m.liveMode = m.promptMode == ModeLive
		m.status = m.promptMode.String()
		return m, nil
	case "shift+tab":
		m.promptMode = (m.promptMode + ModeCount - 1) % ModeCount
		m.liveMode = m.promptMode == ModeLive
		m.status = m.promptMode.String()
		return m, nil
	}

	if m.input == "" {
		switch k {
		case " ":
			// packet / video scrub pause
			if m.pktPlayer != nil && m.pktPlayer.Playing() && !m.burstMode {
				m.pktPlayer.TogglePause()
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil && (m.vpipe.Alive() || m.vpipe.Paused()) && !m.burstMode {
				m.vpipe.TogglePause()
				m.status = m.vpipe.StatusLine()
				return m, nil
			}
			if m.burstMode || m.promptMode == ModeChat {
				return m.togglePTT()
			}
			m.input += " "
			return m, nil
		case "k":
			// pause / play (mpv-style) — ffmpeg pipe or packet player
			if m.pktPlayer != nil && m.pktPlayer.Playing() {
				m.pktPlayer.TogglePause()
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil && (m.vpipe.Alive() || m.vpipe.Paused() || m.frame != nil) {
				m.vpipe.TogglePause()
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case "j", "left":
			if m.pktPlayer != nil {
				m.pktPlayer.SeekRel(-12) // ~1s at 12fps packets
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil {
				if err := m.vpipe.SeekRel(-5 * time.Second); err != nil {
					m.status = "seek: " + err.Error()
				} else {
					m.status = m.vpipe.StatusLine()
				}
			}
			return m, nil
		case "l", "right":
			if m.pktPlayer != nil {
				m.pktPlayer.SeekRel(12)
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil && (m.vpipe.Alive() || m.vpipe.Paused() || m.vpipe.Running()) {
				if err := m.vpipe.SeekRel(5 * time.Second); err != nil {
					m.status = "seek: " + err.Error()
				} else {
					m.status = m.vpipe.StatusLine()
				}
				return m, nil
			}
			if k == "l" {
				m.promptMode = ModeLive
				m.liveMode = true
				m.status = "live"
			}
			return m, nil
		case "J", "shift+left":
			if m.pktPlayer != nil {
				m.pktPlayer.SeekRel(-60)
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil {
				_ = m.vpipe.SeekRel(-30 * time.Second)
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case "L", "shift+right":
			if m.lab != nil && m.lab.On && k == "L" {
				m.status = "layout " + m.lab.CycleLayout().String()
				return m, nil
			}
			if m.pktPlayer != nil {
				m.pktPlayer.SeekRel(60)
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil {
				_ = m.vpipe.SeekRel(30 * time.Second)
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case "0", "home":
			if m.pktPlayer != nil {
				m.pktPlayer.SeekIndex(0)
				m.status = m.pktPlayer.StatusLine()
				return m, nil
			}
			if m.vpipe != nil {
				_ = m.vpipe.Seek(0, 0)
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case "<":
			if m.vpipe != nil {
				_ = m.vpipe.NudgeRate(-1)
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case ">":
			if m.vpipe != nil {
				_ = m.vpipe.NudgeRate(1)
				m.status = m.vpipe.StatusLine()
			}
			return m, nil
		case "b":
			// toggle dual-stream burst (local | peer side-by-side)
			m.burstMode = !m.burstMode
			if m.burstMode {
				m.camOn = true
				m.videoOn = true
				m.compact = true
				m.status = "burst · you | peer"
				m.pushSys("burst dual: left=you · right=peer · space PTT")
			} else {
				m.status = "companion"
			}
			return m, nil
		case "d":
			// cycle live depth / gsplat (ZipDepth · zip-lite · gsplat stack)
			if m.depth == nil {
				m.depth = newDepthSession()
			}
			mode := m.depth.Cycle()
			m.status = formatDepthStatus(m.depth)
			if mode != DepthOff {
				m.videoOn = true
				m.camOn = true // need frames for mono depth
				m.applyDepthToFrame()
			}
			return m, nil
		case "V", "lab":
			// toggle multi-feed lab (feeds | chat)
			if m.lab == nil {
				m.lab = newLabState()
			}
			m.lab.On = !m.lab.On
			if m.lab.On {
				m.burstMode = false
				m.compact = false
				m.lab.EnsurePlaceholders(4)
				m.status = "lab · " + m.lab.Layout.String()
				m.pushSys(LabKeysHelp())
				m.pushSys(m.lab.BudgetLine())
			} else {
				m.status = "companion"
			}
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7":
			// lab: 1–6 select slot for quick fill; else pattern presets 1–7
			if m.lab != nil && m.lab.On && k[0] != '7' {
				n := int(k[0] - '0')
				m.lab.EnsurePlaceholders(n)
				m.lab.SelectSlot(n)
				m.status = fmt.Sprintf("slot %d · c cam · a sim · /watch", n)
				return m, nil
			}
			idx := int(k[0] - '1')
			if idx >= 0 && idx < len(m.presets) {
				return m.evalLive(m.presets[idx], true)
			}
			return m, nil
		case "[":
			if m.lab != nil && m.lab.On {
				m.status = fmt.Sprintf("scale %d · %s", m.lab.NudgeScale(-1), m.lab.BudgetLine())
			}
			return m, nil
		case "]":
			if m.lab != nil && m.lab.On {
				m.status = fmt.Sprintf("scale %d · %s", m.lab.NudgeScale(1), m.lab.BudgetLine())
			}
			return m, nil
		case ",":
			if m.lab != nil && m.lab.On {
				m.status = fmt.Sprintf("fps %d · %s", m.lab.NudgeFPS(-1), m.lab.BudgetLine())
			}
			return m, nil
		case ".":
			if m.lab != nil && m.lab.On {
				m.status = fmt.Sprintf("fps %d · %s", m.lab.NudgeFPS(1), m.lab.BudgetLine())
			}
			return m, nil
		case "a":
			if m.lab != nil && m.lab.On {
				// fill active placeholder with sim (or add)
				m.lab.FillSimIntoActive()
				m.status = "sim → slot"
			}
			return m, nil
		case "r":
			if m.lab != nil && m.lab.On {
				m.lab.ClearActive()
				m.status = "slot cleared"
			}
			return m, nil
		case "x":
			if m.lab != nil && m.lab.On {
				m.lab.RemoveActive()
				m.lab.EnsurePlaceholders(max(1, len(m.lab.Feeds)))
				m.status = fmt.Sprintf("feeds %d", len(m.lab.Feeds))
			}
			return m, nil
		case "o":
			if m.lab != nil && m.lab.On {
				m.lab.ShowList = !m.lab.ShowList
				m.status = map[bool]string{true: "list on", false: "list off"}[m.lab.ShowList]
			}
			return m, nil
		case "c":
			if m.lab != nil && m.lab.On {
				// quick: drop camera into active/empty placeholder
				m.lab.FillCamIntoActive()
				m.camOn = true
				m.videoOn = true
				m.status = "cam → slot"
				return m, nil
			}
			if m.vpipe != nil {
				m.vpipe.Stop()
				m.vpipe = nil
				m.watchPath = ""
			}
			m.camOn = !m.camOn
			if m.camOn {
				m.pixelMode = PixelHalf
				m.videoOn = true
				m.status = "cam on"
			} else {
				m.videoOn = false
				m.frame = nil
				m.status = "cam off"
			}
			return m, nil
		case "v":
			m.videoOn = !m.videoOn
			m.status = map[bool]string{true: "vid on", false: "vid off"}[m.videoOn]
			return m, nil
		case "m":
			if m.lab != nil && m.lab.On {
				st := m.lab.CycleStyle()
				m.pixelMode = st
				m.status = "style " + st.String()
				return m, nil
			}
			m.pixelMode = (m.pixelMode + 1) % PixelCount
			m.status = "style " + m.pixelMode.String()
			return m, nil
		case "tab":
			// handled above for mode cycle — lab feed focus when lab on + shift?
			// empty-input tab already cycles prompt modes; use ctrl+tab? use 'n' for next feed
			return m, nil
		case "n":
			if m.lab != nil && m.lab.On {
				m.lab.NextFeed()
				if af := m.lab.ActiveFeed(); af != nil {
					m.status = "feed " + af.Label
				}
			}
			return m, nil
		case "t":
			m.xl8On = !m.xl8On
			if m.xl8On && !m.xl8.Enabled {
				m.xl8 = defaultTranslateConfig()
				m.xl8On = m.xl8.Enabled
			}
			m.status = fmt.Sprintf("xl8 %v", m.xl8On)
			return m, nil
		case "i":
			m.midiOn = !m.midiOn
			if m.midiOn && m.midiBridge == nil {
				mid := hwmidi.NewOptional()
				m.midiBridge = hwmidi.NewBridge(mid, 0)
			}
			m.status = fmt.Sprintf("midi %v", m.midiOn)
			return m, nil
		case "g":
			m.promptMode = ModeGrok
			m.status = "grok · " + m.grokCfg.ModeLabel()
			return m, nil
		case "p":
			// pattern play; if video open and shift not - keep pattern
			_ = m.toggleLive()
			return m, nil
		case "?", "f1":
			m.showHelp = !m.showHelp
			return m, nil
		case "f", "F":
			m.compact = !m.compact
			if m.compact {
				m.status = "companion"
			} else {
				m.status = "full"
			}
			return m, nil
		}
	}

	switch k {
	case "enter":
		line := strings.TrimSpace(m.input)
		m.input = ""
		if line == "" {
			return m, nil
		}
		if strings.HasPrefix(line, "/") {
			return m.slash(line)
		}
		return m.dispatchPrompt(line)
	case "backspace":
		if len(m.input) > 0 {
			r := []rune(m.input)
			m.input = string(r[:len(r)-1])
		}
		return m, nil
	}

	if msg.Key().Text != "" && k != " " && k != "tab" {
		if m.talking {
			next, cmd := m.stopPTT()
			if nm, ok := next.(*Model); ok {
				nm.input += msg.Key().Text
				return nm, cmd
			}
			return next, cmd
		}
		m.input += msg.Key().Text
		return m, nil
	}
	if k == " " {
		m.input += " "
		return m, nil
	}
	if len(k) == 1 {
		m.input += k
	}
	return m, nil
}

func (m *Model) dispatchPrompt(line string) (tea.Model, tea.Cmd) {
	// drag-drop style path(s) typed/pasted into the prompt
	if LooksLikeDropPaste(line) {
		return m.ingestDroppedPaths(ParseDroppedPaths(line))
	}
	if isVideoPath(line) || looksLikeVideoArg(line) || IsImagePath(line) {
		if IsImagePath(line) && !isURL(line) {
			return m.ingestDroppedPaths([]string{line})
		}
		m.promptMode = ModeWatch
		return m.startWatch(line, true)
	}
	switch m.promptMode {
	case ModeLive:
		return m.evalLive(line, true)
	case ModeWatch:
		return m.startWatch(line, true)
	case ModeGrok:
		return m.askGrok(line)
	default:
		if looksLikePattern(line) {
			return m.evalLive(line, true)
		}
		if m.client != nil {
			m.client.SendChat(line)
		}
		if m.midiOn && m.midiBridge != nil {
			m.midiBridge.Chat()
		}
		m.chat = append(m.chat, chatLine{From: m.nick, Text: line})
		m.trimChat()
		return m, nil
	}
}

func (m *Model) askGrok(line string) (tea.Model, tea.Cmd) {
	m.chat = append(m.chat, chatLine{From: m.nick, Text: line})
	m.trimChat()
	m.grokHistory = append(m.grokHistory, GrokMessage{Role: "user", Content: line})
	m.grokThinking = true
	m.status = "grok…"
	cfg := m.grokCfg
	hist := append([]GrokMessage(nil), m.grokHistory...)
	if len(hist) > 0 {
		hist = hist[:len(hist)-1]
	}
	return m, func() tea.Msg {
		reply, err := AskGrok(cfg, hist, line)
		if err != nil {
			return grokReplyMsg{Err: err.Error()}
		}
		return grokReplyMsg{Text: reply}
	}
}

func extractPattern(text string) string {
	if i := strings.Index(text, "```"); i >= 0 {
		rest := text[i+3:]
		if j := strings.Index(rest, "```"); j > 0 {
			block := rest[:j]
			if nl := strings.IndexByte(block, '\n'); nl >= 0 {
				block = block[nl+1:]
			}
			block = strings.TrimSpace(block)
			if looksLikePattern(block) {
				return block
			}
		}
	}
	for _, pfx := range []string{"stack(", `s("`, "s('", `note("`, "setcps("} {
		if i := strings.Index(text, pfx); i >= 0 {
			chunk := text[i:]
			if nl := strings.IndexByte(chunk, '\n'); nl > 0 {
				chunk = chunk[:nl]
			}
			chunk = strings.TrimSpace(chunk)
			if looksLikePattern(chunk) {
				return chunk
			}
		}
	}
	return ""
}

func (m *Model) slash(line string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(line)
	cmd := strings.TrimPrefix(parts[0], "/")
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], " ")
	}
	switch cmd {
	case "quit", "exit", "q":
		m.shutdown()
		return m, tea.Quit
	case "who":
		names := make([]string, len(m.peers))
		for i, p := range m.peers {
			names[i] = p.Nick
		}
		m.pushSys("peers: " + strings.Join(names, ", "))
	case "nick":
		if arg != "" {
			m.nick = arg
			if m.client != nil {
				m.client.Nick = arg
				_ = m.client.SendJSON(map[string]any{"type": "nick", "nick": arg})
			}
			m.pushSys("nick → " + arg)
		}
	case "clip":
		if m.lastClip != "" {
			m.pushSys("clip: " + m.lastClip + "  (cliamp " + m.lastClip + ")")
			go func() { _ = exec.Command("cliamp", m.lastClip).Start() }()
		} else {
			m.pushSys("no clip yet — PTT first")
		}
	case "mode":
		m.pixelMode = (m.pixelMode + 1) % PixelCount
		m.pushSys("pixel: " + m.pixelMode.String())
	case "translate", "xl8":
		m.xl8On = !(arg == "off" || arg == "0")
		if m.xl8On {
			m.xl8 = defaultTranslateConfig()
			m.xl8On = m.xl8.Enabled
		}
		m.pushSys(fmt.Sprintf("translate %v model=%s", m.xl8On, m.xl8.Model))
	case "midi":
		m.midiOn = !(arg == "off" || arg == "0")
		m.pushSys(fmt.Sprintf("midi %v", m.midiOn))
	case "signls", "sektron":
		bin := cmd
		if arg != "" {
			bin = arg
		}
		go func() { _ = exec.Command(bin).Start() }()
		m.pushSys("spawned " + bin + " — MIDI in = GrokYtalkY")
	case "play":
		_ = m.toggleLive()
		return m, nil
	case "stop":
		if m.live != nil {
			m.live.Stop()
		}
		m.pushSys("■ live stop")
		return m, nil
	case "watch", "vplay", "movie", "video", "ytdl", "yt", "fill":
		src := arg
		if src == "" {
			m.pushSys("usage: /watch file|url|yt-link  (auto yt-dlp → active slot)")
			return m, nil
		}
		// ensure lab open when filling from slash in companion
		if cmd == "fill" && m.lab != nil {
			m.lab.On = true
		}
		return m.startWatch(src, true)
	case "cam":
		// quick cam into lab slot
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
		m.lab.FillCamIntoActive()
		m.camOn = true
		m.videoOn = true
		m.status = "cam → slot"
		m.pushSys("cam → slot " + fmt.Sprintf("%d", m.lab.Active+1))
		return m, nil
	case "resolve", "streams":
		src := arg
		if src == "" {
			m.pushSys("usage: /resolve https://…")
			return m, nil
		}
		r, err := ResolveMediaTimeout(src, 60*time.Second)
		if err != nil {
			m.pushSys("resolve: " + err.Error())
			return m, nil
		}
		m.pushSys(fmt.Sprintf("via %s · %s", r.Via, truncate(r.Title, 40)))
		m.pushSys("v " + truncate(r.Video, 60))
		if r.Audio != "" {
			m.pushSys("a " + truncate(r.Audio, 60))
		}
		return m, nil
	case "doctor":
		for _, ln := range strings.Split(strings.TrimSpace(StreamDoctor()), "\n") {
			if ln != "" {
				m.pushSys(ln)
			}
		}
		for _, ln := range strings.Split(strings.TrimSpace(DepthDoctorLine()), "\n") {
			if ln != "" {
				m.pushSys(ln)
			}
		}
		m.pushSys(DepthModesList())
		return m, nil
	case "depth", "zipdepth", "gsplat":
		if m.depth == nil {
			m.depth = newDepthSession()
		}
		switch {
		case cmd == "gsplat" || arg == "gsplat":
			m.depth.SetMode(DepthGsplat)
		case cmd == "zipdepth" || arg == "zipdepth" || arg == "zip":
			m.depth.SetMode(DepthZipDepth)
		case arg == "lite" || arg == "zip-lite":
			m.depth.SetMode(DepthZipLite)
		case arg == "off" || arg == "0":
			m.depth.SetMode(DepthOff)
		default:
			m.depth.Cycle()
		}
		if m.depth.Mode() != DepthOff {
			m.camOn = true
			m.videoOn = true
		}
		m.status = formatDepthStatus(m.depth)
		m.pushSys(m.status)
		m.applyDepthToFrame()
		return m, nil
	case "vstop", "watchstop":
		m.stopWatch()
		m.pushSys("■ video pipe stopped")
		return m, nil
	case "rec", "record":
		if m.recorder == nil {
			m.recorder = NewRecordSession()
		}
		if arg == "stop" || (m.recorder.Active() && arg == "") {
			m.recorder.Stop()
			m.pushSys(fmt.Sprintf("rec stop · %d packets", m.recorder.Count()))
			return m, nil
		}
		m.recorder.Start()
		m.pushSys("rec ● capturing frames/pcm → /export")
		return m, nil
	case "export", "encode":
		// /export out.gyst | out.gyhex | out.pcap
		if m.recorder == nil || m.recorder.Count() == 0 {
			// export current frame as single-packet file
			if m.frame == nil {
				m.pushSys("nothing to export — /rec first or load video")
				return m, nil
			}
			path := arg
			if path == "" {
				path = fmt.Sprintf("gy-frame-%d.gyst", time.Now().Unix())
			}
			p := PacketFromFramePixels(m.frame, 1)
			format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
			if format == "" {
				format = "gyst"
				path += ".gyst"
			}
			var err error
			switch format {
			case "gyhex", "hex":
				err = WriteGyHexFile(path, []StreamPacket{p}, nil)
			case "pcap":
				err = WritePCAP(path, []StreamPacket{p})
			default:
				err = WriteGystFile(path, []StreamPacket{p})
			}
			if err != nil {
				m.pushSys("export: " + err.Error())
			} else {
				m.pushSys("export → " + path)
			}
			return m, nil
		}
		path := arg
		if path == "" {
			path = fmt.Sprintf("gy-stream-%d.gyst", time.Now().Unix())
		}
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if format == "" {
			format = "gyst"
			path += ".gyst"
		}
		if err := m.recorder.Export(path, format); err != nil {
			m.pushSys("export: " + err.Error())
		} else {
			m.pushSys(fmt.Sprintf("export %d pkts → %s", m.recorder.Count(), path))
		}
		return m, nil
	case "load", "decode", "bin":
		// /load file.gyst|.gyhex|.pcap
		if arg == "" {
			m.pushSys("usage: /load stream.gyst|gyhex|pcap")
			return m, nil
		}
		return m.startPacketWatch(arg)
	case "hexdump":
		// /hexdump — dump current frame as gyhex lines to chat
		if m.frame == nil {
			m.pushSys("no frame")
			return m, nil
		}
		p := PacketFromFramePixels(m.frame, 1)
		line := EncodeHexLine(p)
		if len(line) > 120 {
			m.pushSys(line[:120] + "…")
		} else {
			m.pushSys(line)
		}
		m.pushSys(fmt.Sprintf("rgb %dx%d · %d bytes · use /export f.gyhex", m.frame.W, m.frame.H, len(p.Payload)))
		return m, nil
	case "pause", "vpause":
		if m.pktPlayer != nil {
			m.pktPlayer.TogglePause()
			m.pushSys(m.pktPlayer.StatusLine())
			return m, nil
		}
		if m.vpipe != nil {
			m.vpipe.TogglePause()
			m.pushSys(m.vpipe.StatusLine())
		}
		return m, nil
	case "seek":
		// /seek 90  or /seek +10  or /seek -30
		if m.vpipe == nil {
			m.pushSys("no video")
			return m, nil
		}
		arg = strings.TrimSpace(arg)
		if arg == "" {
			m.pushSys(m.vpipe.StatusLine())
			return m, nil
		}
		if strings.HasPrefix(arg, "+") || strings.HasPrefix(arg, "-") {
			sec, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				m.pushSys("usage: /seek +10 | /seek -30 | /seek 90")
				return m, nil
			}
			_ = m.vpipe.SeekRel(time.Duration(sec * float64(time.Second)))
		} else {
			sec, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				m.pushSys("usage: /seek 90")
				return m, nil
			}
			_ = m.vpipe.Seek(time.Duration(sec*float64(time.Second)), 0)
		}
		m.pushSys(m.vpipe.StatusLine())
		return m, nil
	case "rate", "speed":
		if m.vpipe == nil {
			m.pushSys("no video")
			return m, nil
		}
		if arg == "" {
			m.pushSys(fmt.Sprintf("rate %g×", m.vpipe.Rate()))
			return m, nil
		}
		r, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			m.pushSys("usage: /rate 1.5")
			return m, nil
		}
		_ = m.vpipe.SetRate(r)
		m.pushSys(m.vpipe.StatusLine())
		return m, nil
	case "vpipe", "vinfo":
		if m.vpipe != nil {
			m.pushSys(m.vpipe.StatusLine())
			m.pushSys(fmt.Sprintf("src %s · %dx%d · audio=%v", truncate(m.watchPath, 40), m.vpipe.W, m.vpipe.H, m.vpipe.HasAudio))
		} else {
			m.pushSys("vpipe idle — /watch movie.mp4")
		}
		return m, nil
	case "eval", "s":
		code := arg
		if code == "" {
			code = m.liveCode
		}
		return m.evalLive(code, true)
	case "cps":
		if arg != "" {
			code := fmt.Sprintf("setcps(%s)\n%s", arg, stripCPS(m.liveCode))
			return m.evalLive(code, m.live != nil && m.live.Playing())
		}
	case "preset":
		m.pushSys("presets 1-7: bd/sd, house, stack, notes…")
	case "grok", "ask":
		if arg == "" {
			m.promptMode = ModeGrok
			m.pushSys("mode grok · type a prompt")
			return m, nil
		}
		m.promptMode = ModeGrok
		return m.askGrok(arg)
	case "help", "?":
		m.showHelp = true
		return m, nil
	default:
		m.pushSys("unknown /" + cmd)
	}
	return m, nil
}

// startPacketWatch loads .gyst / .gyhex / .pcap / hex JSON and plays frames.
func (m *Model) startPacketWatch(path string) (tea.Model, tea.Cmd) {
	path = expandPath(strings.Trim(path, `"'`))
	pkts, err := LoadStreamFile(path)
	if err != nil {
		m.pushSys("bin load: " + err.Error())
		return m, nil
	}
	if len(pkts) == 0 {
		m.pushSys("bin load: empty")
		return m, nil
	}
	m.stopWatch()
	if m.pktPlayer != nil {
		m.pktPlayer.Stop()
	}
	m.pktPlayer = NewPacketPlayer(pkts)
	m.pktPlayer.onPCM = func(pcm []byte, sr, ch int) {
		if m.player != nil {
			m.player.Write(pcm, sr, ch)
		}
		if m.recorder != nil && m.recorder.Active() {
			m.recorder.AddPCM(pcm, sr, ch)
		}
	}
	m.camOn = false
	m.videoOn = true
	m.watchPath = path
	// prime first video frame
	for i, p := range pkts {
		if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
			m.pktPlayer.SeekIndex(i)
			if fr, err := FrameFromPacket(&p); err == nil {
				m.frame = fr
			}
			break
		}
	}
	m.pktPlayer.Play()
	m.status = m.pktPlayer.StatusLine()
	m.pushSys(fmt.Sprintf("▶ bin %s · %d packets · %s", filepath.Base(path), len(pkts), DetectStreamFile(path)))
	if m.lab != nil && m.lab.On {
		m.lab.FillWatchIntoActive(filepath.Base(path), path, m.frame)
	}
	return m, nil
}

// applyDepthToFrame runs live mono depth / gsplat on the current RGB frame.
func (m *Model) applyDepthToFrame() {
	if m.depth == nil || m.frame == nil {
		return
	}
	if m.depth.Mode() == DepthOff {
		return
	}
	m.depth.Process(m.frame)
	// burst / Glyph Matrix: prefer depth brightness when active
	if dm := m.depth.LastMap(); dm != nil && m.glyphN > 0 {
		gm := DepthToGlyph(dm, m.glyphN)
		m.lastGlyph = gm.IntColors()
	}
}

func (m *Model) startWatch(src string, withAudio bool) (tea.Model, tea.Cmd) {
	src = strings.Trim(src, `"'`)
	src = expandPath(src)
	// binary/hex/pcap stream files
	if IsStreamCodecPath(src) || DetectStreamFile(src) != "unknown" {
		if DetectStreamFile(src) != "unknown" {
			return m.startPacketWatch(src)
		}
	}
	// stop camera while watching file
	m.camOn = false
	if m.pktPlayer != nil {
		m.pktPlayer.Stop()
		m.pktPlayer = nil
	}
	if m.vpipe != nil {
		m.vpipe.Stop()
		m.vpipe = nil
	}
	m.status = "resolving…"
	m.pushSys("resolve " + truncate(src, 50))

	r, err := ResolveMediaTimeout(src, 90*time.Second)
	if err != nil {
		m.pushSys("watch: " + err.Error())
		m.status = "resolve fail"
		return m, nil
	}
	w := m.videoCols()
	h := m.videoPxH()
	vp, err := OpenVideoPipeResolved(r, w, h, withAudio)
	if err != nil {
		m.pushSys("watch: " + err.Error())
		return m, nil
	}
	m.vpipe = vp
	m.watchPath = r.Input
	if r.Title != "" {
		m.watchPath = r.Title
	}
	m.videoOn = true
	m.pixelMode = PixelHalf
	m.vpipeSeq = 0
	label := r.Title
	if label == "" {
		label = filepath.Base(r.Input)
	}
	via := r.Via
	if r.Audio != "" {
		via += "+a"
	}
	m.status = fmt.Sprintf("▶ %s", truncate(label, 28))
	m.pushSys(fmt.Sprintf("▶ %s · %s · %dx%d", truncate(label, 40), via, w, h))
	m.applyDepthToFrame()
	// multi-feed lab: drop video into active/empty placeholder
	if m.lab != nil && m.lab.On {
		m.lab.FillWatchIntoActive(truncate(label, 14), r.Input, m.frame)
		m.pushSys("vid → slot " + fmt.Sprintf("%d", m.lab.Active+1))
	}
	return m, nil
}

func (m *Model) stopWatch() {
	if m.vpipe != nil {
		m.vpipe.Stop()
		m.vpipe = nil
	}
	if m.pktPlayer != nil {
		m.pktPlayer.Stop()
		m.pktPlayer = nil
	}
	m.watchPath = ""
	m.vpipeSeq = 0
}

func looksLikePattern(line string) bool {
	low := strings.ToLower(line)
	if strings.HasPrefix(low, "s(") || strings.HasPrefix(low, "note(") ||
		strings.HasPrefix(low, "n(") || strings.HasPrefix(low, "stack(") ||
		strings.HasPrefix(low, "setcps") || strings.HasPrefix(low, "bpm(") ||
		strings.HasPrefix(low, "sound(") {
		return true
	}
	// bare drums
	for _, d := range []string{"bd", "sd", "hh", "cp"} {
		if strings.Contains(low, d) {
			return true
		}
	}
	return false
}

func looksLikeVideoArg(line string) bool {
	line = strings.TrimSpace(line)
	if isURL(line) || strings.HasPrefix(line, "ytdl://") || strings.HasPrefix(line, "yt-dlp://") {
		return true
	}
	// path with known container even without checking disk here
	return isVideoPath(line)
}

func stripCPS(code string) string {
	// drop existing setcps/bpm lines for re-eval
	var lines []string
	for _, ln := range strings.Split(code, "\n") {
		l := strings.TrimSpace(strings.ToLower(ln))
		if strings.HasPrefix(l, "setcps") || strings.HasPrefix(l, "bpm(") {
			continue
		}
		lines = append(lines, ln)
	}
	return strings.Join(lines, "\n")
}

func (m *Model) evalLive(code string, autoplay bool) (tea.Model, tea.Cmd) {
	code = strings.ReplaceAll(code, `\n`, "\n")
	if m.live == nil {
		m.pushSys("live engine missing")
		return m, nil
	}
	if err := m.live.Eval(code); err != nil {
		m.pushSys("pattern error: " + err.Error())
		return m, nil
	}
	m.liveCode = code
	m.status = "eval"
	// mesh sync (Qbpm jam style)
	if m.client != nil {
		_ = m.client.SendJSON(map[string]any{
			"type": "pattern", "code": code, "from": m.nick,
			"cps": m.live.CPS(), "t": time.Now().UnixMilli(),
		})
	}
	if autoplay {
		if !m.live.Playing() {
			m.live.Play()
			m.status = "live"
		}
	}
	return m, nil
}

func (m *Model) toggleLive() tea.Cmd {
	if m.live == nil {
		m.status = "no live"
		return nil
	}
	if m.live.Playing() {
		m.live.Stop()
		m.status = "stop"
		return nil
	}
	if m.live.Code() == "" {
		_ = m.live.Eval(m.liveCode)
	}
	m.live.Play()
	m.status = "play"
	return nil
}

func (m *Model) togglePTT() (tea.Model, tea.Cmd) {
	if m.talking {
		return m.stopPTT()
	}
	return m.startPTT()
}

func (m *Model) startPTT() (tea.Model, tea.Cmd) {
	if m.client == nil {
		m.status = "not connected"
		return m, nil
	}
	prog := m.prog
	burst := m.burstMode
	sess, err := startPTT(func(chunk []byte) {
		// soft gate near-silence (signls/sektron-style clean triggers)
		if SoftGate(chunk, 0.008) == nil {
			return
		}
		if m.client != nil {
			m.client.SendAudio(chunk)
		}
		lv := rmsLevel(chunk)
		if prog != nil {
			prog.Send(audioLvlMsg{Level: lv, Bands: bandLevels(chunk, 32), TX: true})
		}
	})
	if err != nil {
		m.pushSys("mic: " + err.Error())
		return m, nil
	}
	m.ptt = sess
	m.talking = true
	if burst {
		m.camOn = true
		m.videoOn = true
		m.client.SendBurstStart()
		m.client.SendPTT(true)
		m.status = "BURST"
	} else {
		m.client.SendPTT(true)
		m.status = "PTT"
	}
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(true, LevelToVelocity(0.5))
	}
	return m, nil
}

func (m *Model) stopPTT() (tea.Model, tea.Cmd) {
	var pcm []byte
	if m.ptt != nil {
		pcm = m.ptt.Stop()
		m.ptt = nil
		if len(pcm) > 0 {
			m.lastClip = writeLastClip(pcm)
		}
	}
	burst := m.burstMode
	m.talking = false
	m.level = 0
	if m.client != nil {
		if burst {
			m.client.SendBurstEnd()
		}
		m.client.SendPTT(false)
	}
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(false, LevelToVelocity(m.peak))
	}
	m.status = m.nick

	// live translation on released PTT clip
	if m.xl8On && m.xl8.Enabled && m.lastClip != "" && len(pcm) > sampleRate*2/4 {
		clip := m.lastClip
		cfg := m.xl8
		return m, func() tea.Msg {
			tr, err := TranscribeFile(cfg, clip)
			if err != nil {
				return errMsg("translate: " + err.Error())
			}
			return transcriptMsg(tr)
		}
	}
	return m, nil
}

func (m *Model) shutdown() {
	if m.talking {
		_, _ = m.stopPTT()
	}
	if m.live != nil {
		m.live.Stop()
	}
	m.stopWatch()
	if m.player != nil {
		m.player.Close()
	}
	if m.midiBridge != nil {
		m.midiBridge.Close()
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Model) handleWS(raw []byte) (tea.Model, tea.Cmd) {
	// frame packet
	if i := indexByte(raw, '\n'); i > 0 && raw[0] == '{' {
		var hdr map[string]any
		if json.Unmarshal(raw[:i], &hdr) == nil {
			if t, _ := hdr["type"].(string); t == "frame" {
				b64 := string(raw[i+1:])
				src, _ := hdr["src"].(string)
				meta := fmt.Sprintf("%s", src)
				if w, ok := hdr["w"].(float64); ok {
					if h, ok2 := hdr["h"].(float64); ok2 {
						meta = fmt.Sprintf("%s %.0f×%.0f", src, w, h)
					}
				}
				if !m.videoOn {
					m.frameMeta = meta
					return m, nil
				}
				jpeg, err := base64.StdEncoding.DecodeString(b64)
				if err != nil {
					return m, nil
				}
				return m, decodeFrameCmd(jpeg, meta, m.videoCols(), m.videoPxH())
			}
		}
	}

	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return m, nil
	}
	switch msg["type"] {
	case "hello":
		// quiet — status already shows nick when connected
	case "roster":
		m.peers = nil
		if arr, ok := msg["peers"].([]any); ok {
			for _, p := range arr {
				if pm, ok := p.(map[string]any); ok {
					nick, _ := pm["nick"].(string)
					talk, _ := pm["talking"].(bool)
					m.peers = append(m.peers, peerInfo{Nick: nick, Talking: talk})
				}
			}
		}
	case "join":
		if n, _ := msg["nick"].(string); n != "" {
			m.status = n + " +"
		}
	case "leave":
		if n, _ := msg["nick"].(string); n != "" {
			m.status = n + " −"
		}
	case "chat":
		from, _ := msg["from"].(string)
		text, _ := msg["text"].(string)
		m.chat = append(m.chat, chatLine{From: from, Text: text})
		m.trimChat()
	case "ptt":
		from, _ := msg["from"].(string)
		st, _ := msg["state"].(string)
		if st == "down" {
			m.remoteTX = from
			m.status = from + " TX"
		} else {
			m.remoteTX = ""
			if m.burstRemote == from {
				m.burstRemote = ""
			}
			m.status = "clear"
		}
	case "vburst-start":
		from, _ := msg["from"].(string)
		if from != "" && from != m.nick {
			m.burstRemote = from
			m.remoteTX = from
			m.status = from + " burst"
		}
	case "vburst-end":
		from, _ := msg["from"].(string)
		if from == m.burstRemote || from == m.remoteTX {
			m.burstRemote = ""
			m.remoteTX = ""
			// keep last peer frame frozen (don't nil) so dual view stays readable
			m.status = "clear"
		}
	case "vburst-frame":
		from, _ := msg["from"].(string)
		if from == m.nick {
			return m, nil
		}
		m.burstRemote = from
		m.remoteTX = from
		// optional glyph grid for Nothing Phone / local orb
		if g, ok := msg["glyph"].([]any); ok && len(g) > 0 {
			ints := make([]int, 0, len(g))
			for _, v := range g {
				switch n := v.(type) {
				case float64:
					ints = append(ints, int(n))
				case int:
					ints = append(ints, n)
				}
			}
			m.lastGlyph = ints
		}
		b64, _ := msg["b64"].(string)
		if b64 == "" {
			return m, nil
		}
		jpeg, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return m, nil
		}
		maxW, maxH := 64, 64
		if !m.burstMode {
			maxW, maxH = m.videoCols(), m.videoPxH()
		}
		// decode into peer tile (meta burst:nick) without replacing local cam frame path
		return m, decodeFrameCmd(jpeg, "burst:"+from, maxW, maxH)
	case "audio":
		from, _ := msg["from"].(string)
		if from == m.nick {
			return m, nil
		}
		fmtStr, _ := msg["fmt"].(string)
		if fmtStr != "" && fmtStr != "pcm16" && fmtStr != "s16le" {
			return m, nil
		}
		b64, _ := msg["b64"].(string)
		pcm, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return m, nil
		}
		sr := sampleRate
		if v, ok := msg["sr"].(float64); ok {
			sr = int(v)
		}
		ch := 1
		if v, ok := msg["ch"].(float64); ok {
			ch = int(v)
		}
		m.level = rmsLevel(pcm)
		m.peak = PeakHold(m.peak, m.level, 0.45, 0.12)
		m.bands = bandLevels(pcm, 32)
		if m.midiOn && m.midiBridge != nil {
			m.midiBridge.LevelRX(m.peak)
		}
		if m.player != nil {
			m.player.Write(pcm, sr, ch)
		}
	case "translate":
		from, _ := msg["from"].(string)
		text, _ := msg["text"].(string)
		if text != "" {
			m.chat = append(m.chat, chatLine{From: from, Text: "🌐 " + text, XL8: true})
			m.trimChat()
			if m.midiOn && m.midiBridge != nil {
				m.midiBridge.Translate()
			}
		}
	case "pattern", "jam":
		// Qbpm-style collab pattern sync
		code, _ := msg["code"].(string)
		from, _ := msg["from"].(string)
		if code != "" && from != m.nick {
			m.pushSys("◎ jam from " + from)
			_, _ = m.evalLive(code, true)
		}
	}
	return m, nil
}

func decodeFrameCmd(jpeg []byte, meta string, maxW, maxH int) tea.Cmd {
	return func() tea.Msg {
		fp, err := decodeFrameJPEG(jpeg, maxW, maxH)
		if err != nil {
			return errMsg("frame decode: " + err.Error())
		}
		fp.Source = meta
		return frameReady{F: fp, Meta: meta}
	}
}

// captureBurstCamCmd — small square face snap for Siri-orb / Glyph Matrix.
func (m *Model) captureBurstCamCmd() tea.Cmd {
	return func() tea.Msg {
		path := os.TempDir() + "/grokytalky-burst.jpg"
		// ~120² JPEG keeps mesh light for short video bursts
		var args []string
		if runtime.GOOS == "darwin" {
			args = []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "avfoundation", "-pixel_format", "nv12",
				"-framerate", "30", "-video_size", "640x480",
				"-i", "0:none",
				"-frames:v", "1",
				"-vf", "scale=120:120:force_original_aspect_ratio=increase,crop=120:120",
				"-q:v", "8",
				path,
			}
		} else {
			args = []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "v4l2", "-i", "/dev/video0",
				"-frames:v", "1",
				"-vf", "scale=120:120:force_original_aspect_ratio=increase,crop=120:120",
				"-q:v", "8",
				path,
			}
		}
		cmd := exec.Command("ffmpeg", args...)
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			// soft fail — try minimal capture
			args2 := []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "avfoundation", "-i", "0:none",
				"-frames:v", "1", "-vf", "scale=96:96", "-q:v", "10", path,
			}
			if runtime.GOOS != "darwin" {
				args2 = []string{
					"-hide_banner", "-loglevel", "error", "-y",
					"-f", "v4l2", "-i", "/dev/video0",
					"-frames:v", "1", "-vf", "scale=96:96", "-q:v", "10", path,
				}
			}
			if err2 := exec.Command("ffmpeg", args2...).Run(); err2 != nil {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) < 80 {
			return nil
		}
		return camSnapMsg(b)
	}
}

func (m *Model) captureCamCmd() tea.Cmd {
	// sample at terminal video scale (srcW×srcH) so boot fill stays sharp
	sc := m.computeVideoScale(m.width, m.height)
	sw, sh := sc.SrcW, sc.SrcH
	if sw < 80 {
		sw = max(80, m.videoCols())
	}
	if sh < 48 {
		sh = max(48, m.videoPxH())
	}
	if sh%2 != 0 {
		sh++
	}
	return func() tea.Msg {
		path := os.TempDir() + "/grokytalky-cam.jpg"
		scale := fmt.Sprintf("scale=%d:%d:flags=bicubic", sw, sh)
		var args []string
		if runtime.GOOS == "darwin" {
			args = []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "avfoundation",
				"-pixel_format", "nv12",
				"-framerate", "30",
				"-video_size", "640x480",
				"-i", "0:none",
				"-frames:v", "1",
				"-vf", scale,
				"-q:v", "5",
				path,
			}
		} else {
			args = []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "v4l2", "-i", "/dev/video0",
				"-frames:v", "1",
				"-vf", scale,
				"-q:v", "5",
				path,
			}
		}
		cmd := exec.Command("ffmpeg", args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			// fallback without pixel_format / video_size (older ffmpeg / busy cam)
			args2 := []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "avfoundation", "-framerate", "30", "-i", "0:none",
				"-frames:v", "1", "-vf", scale, "-q:v", "8", path,
			}
			if runtime.GOOS != "darwin" {
				args2 = []string{
					"-hide_banner", "-loglevel", "error", "-y",
					"-f", "v4l2", "-i", "/dev/video0",
					"-frames:v", "1", "-vf", scale, "-q:v", "8", path,
				}
			}
			cmd2 := exec.Command("ffmpeg", args2...)
			if err2 := cmd2.Run(); err2 != nil {
				return nil
			}
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) < 100 {
			return nil
		}
		return camSnapMsg(b)
	}
}

func (m *Model) pushSys(s string) {
	m.chat = append(m.chat, chatLine{Sys: true, Text: s})
	m.trimChat()
}

func (m *Model) trimChat() {
	if len(m.chat) > 100 {
		m.chat = m.chat[len(m.chat)-100:]
	}
}

func (m *Model) videoCols() int {
	sc := m.computeVideoScale(m.width, m.height)
	if sc.Cols > 0 {
		return sc.Cols
	}
	return max(24, safeCols(m.width))
}

func (m *Model) videoPxH() int {
	sc := m.computeVideoScale(m.width, m.height)
	if sc.SrcH > 0 {
		return sc.SrcH
	}
	if sc.HalfRows > 0 {
		return sc.HalfRows * 2
	}
	return max(8, min(48, m.height))
}

// lastScale used by status crumbs
func (m *Model) videoScaleLabel() string {
	return m.computeVideoScale(m.width, m.height).label()
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.renderCharm())
	// v2: alt screen on the View (prevents scrollback spool)
	v.AltScreen = true
	// bracketed paste so Finder/Terminal drag-drop arrives as PasteMsg
	v.DisableBracketedPasteMode = false
	return v
}
