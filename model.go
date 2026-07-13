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
	// social / mobile watch
	watchMobile bool   // portrait double-stack GrokGlyph scale
	watchLive   bool   // primary is live/broadcast
	watchSocial string // platform/@handle for status

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
	helpPage     int // multi-page help overlay (tab while ? open)
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
	glyphN          int // display matrix N (13/25 hardware, 37/49 terminal hi-res)
	glyphScale      int // cells per LED pitch; 0 = auto-fit terminal
	glyphAspect     GlyphAspect // square | phone-v double-stack
	lastGlyph       []int // last brightness grid for debug / Android bridge

	// Cursor-Grok Forge dual Glyph receive (live hub meta + stamped hexlum)
	forgeRX     *ForgeMark // latest peer mark (dual right chrome)
	forgeRXFrom string     // peer nick that owns forgeRX
	forgeLocal  *ForgeMark // local multi-pcap mark for dual left chrome
	// Dual-local multi-slot rotate: cycle lab pcap tiles into burstLocalFrame
	forgeRotateOn    bool // true when multi-pcap forge active (2+ slots or forced)
	forgeHoldLeft    bool // pause rotate; stick current left slot
	forgeLocalIdx    int  // index into marked pcap slots (0-based)
	forgeRotateEvery int  // ticks between slot hops (0 → ForgeDualRotateTicks)

	// Capability profile — term/IoT handshake (lanes, glyph N, backpressure)
	cap       CapProfile
	peerCaps  map[string]CapProfile // nick → cap

	// Conductor / program bus — on-air control (venue adapters consume later)
	conductor bool       // this peer claimed conductor
	program   ProgramBus // room program state

	// Live depth + gsplat (ZipDepth sidecar / zip-lite / overview-style stack)
	depth *depthSession

	// Multi-feed video lab (FPS / scale / style / layout + feeds | chat)
	lab *LabState

	// Binary/hex/pcap packet player + recorder
	pktPlayer *PacketPlayer
	recorder  *RecordSession

	// Live TUI ingest: hub stream-pub cancel (Colossus/DOJO pcap or sim)
	streamPubCancel context.CancelFunc

	// Full-duplex walkie: open mic + RX duck (latency-mitigated peer audio)
	duplexOn bool // continuous mic (vs PTT half-duplex)
	// Mesh MIDI peer sync (Strudel hits + walkie → hub type:midi)
	meshMIDI bool
	// Grok overlay lane on glyph streams (caption / effect / prompt)
	overlay *GrokOverlayState
	streamPubSrc    string
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
	// socialLazyTickMsg slowly reserves secondary social content into lab slots.
	socialLazyTickMsg struct {
		Items []LazyMediaItem
		Index int
		Tag   string // platform/@handle
	}
	// newsWallLoadMsg starts next broadcaster pipe after stagger.
	newsWallLoadMsg struct {
		Index int
		Region string
		MaxN  int
	}
	newsWallReadyMsg struct {
		Index int
		Label string
		Pipe  *NewsTilePipe
		Err   string
	}
	grokReplyMsg struct {
		Text    string
		Err     string
		Overlay bool        // true = caption/effect/prompt lane
		Mode    OverlayMode // caption|effect|prompt
		// Orchestrate true → parse STYLE/CAPTION/PATTERN/… take lines
		Orchestrate bool
		Vision      bool   // vision-sourced take
		Feed        string // focus feed label
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
	Burst      bool // Siri-sized video burst orb (Glyph Matrix walkie)
	GlyphN     int  // matrix side (13/25 hardware, 37/49 hi-res); default 25
	GlyphScale int  // cells/LED pitch (0=auto, 1–8); scales display like GDK setScale
	Lab        bool // multi-feed video lab next to chat
}

func NewModel(opts Options) *Model {
	cap := DetectCapProfile(80, 24)
	glyphN := NormalizeGlyphN(opts.GlyphN)
	if opts.GlyphN == 0 || opts.GlyphN == GlyphPhone3 {
		// honor capability lean/mono default when user left default
		if cap.GlyphN > 0 && cap.Class != CapClassTermFull {
			glyphN = cap.GlyphN
		}
	}
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
		glyphN:      glyphN,
		glyphScale:  opts.GlyphScale,
		glyphAspect: GlyphAspectSquare,
		cap:        cap,
		peerCaps:   make(map[string]CapProfile),
		program:    NewProgramBus(),
		depth:      newDepthSession(),
		lab:        newLabState(),
		recorder:   NewRecordSession(),
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
		grokCfg:  loadGrokConfig(),
		overlay:  newGrokOverlayState(),
		meshMIDI: true, // jam dock: share Strudel hits over mesh by default
		player:   &Player{Duck: 1},
		chat: []chatLine{
			{Sys: true, Text: fmt.Sprintf("gy %s · companion", Version)},
			{Sys: true, Text: cap.SummaryLine()},
		},
	}
	m.glyphN = NormalizeGlyphN(m.glyphN)
	if m.glyphScale < 0 {
		m.glyphScale = 0
	}
	if m.glyphScale > GlyphScaleMax {
		m.glyphScale = GlyphScaleMax
	}
	if opts.Burst {
		// Prefer requested N; real WindowSizeMsg will fit full circles.
		// Default model is 80×24 — dual 25 does not fit (auto → 13).
		gn := m.glyphN
		// keep default 80×24 until real terminal size arrives (don't invent 32 rows)
		dev := GlyphDeviceN(gn)
		m.chat = []chatLine{
			{Sys: true, Text: fmt.Sprintf("burst · prefer %d×%d (device %d) · auto-fit full circles to window", gn, gn, dev)},
			{Sys: true, Text: "80×24 → 13×13 dual · larger term → 25×25 · [ ] scale · g res · space PTT"},
		}
		m.status = fmt.Sprintf("burst · prefer ◎ %d×%d · auto-fit window", gn, gn)
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
		// mesh MIDI peer sync — note hits for jam docks
		if m.meshMIDI && m.client != nil {
			note, ch, vel := strudelHitToMIDI(ev)
			if note >= 0 {
				m.client.SendMIDINote(MeshMIDINoteOn, ch, note, vel, "strudel")
				// schedule short note-off so peers don't stick
				go func(n, c, v int) {
					time.Sleep(80 * time.Millisecond)
					if m.client != nil {
						m.client.SendMIDINote(MeshMIDINoteOff, c, n, 0, "strudel")
					}
				}(note, ch, vel)
			}
		}
	}})
	m.live = strudel.NewEngine(&strudel.MultiSink{Sinks: sinks})
	m.live.SetOnCycle(func(cycle int64, cps float64, code string) {
		if m.prog != nil {
			m.prog.Send(liveCycleMsg{Cycle: cycle, CPS: cps, Code: code})
		}
		// share tempo every ~4 cycles for jam phase soft-lock
		if m.meshMIDI && m.client != nil && cycle%4 == 0 {
			m.client.SendMIDI(BuildMeshMIDITempo(m.nick, cps, cycle))
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
	cmds := []tea.Cmd{m.connectCmd(), tickCmd()}
	// vision auto-loop when GY_VISION=1
	if Vision().Enabled() {
		cmds = append(cmds, m.visionLoopCmd())
	}
	return tea.Batch(cmds...)
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
	// advertise live terminal capability (handshake)
	if m.cap.Class != "" {
		c.Cap = m.cap
		c.Role = m.cap.Role
	}
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
		// capability handshake: refresh profile + announce to hub
		m.refreshCapFromGeom(nw, nh)
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
		// forge multi-pcap: advance slot frames + dual-local left rotate
		// (burst view skips renderLab, so tick must drive pcap + left pane)
		if m.lab != nil && m.lab.On {
			m.tickLabSims()
		}
		m.tickForgeDualLocal()
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
			if m.overlay != nil {
				m.overlay.MarkBusy(false)
			}
			return m, nil
		}
		// overlay lane replies apply as caption/effect, not always full chat history
		if msg.Overlay {
			return m.applyGrokOverlayReply(msg.Text, msg.Mode)
		}
		if msg.Orchestrate {
			take := ParseGrokTake(msg.Text)
			if msg.Vision {
				take.Vision = true
			}
			mod, cmd := m.applyGrokTake(take)
			// re-arm auto vision loop when enabled
			if Vision().Enabled() {
				return mod, tea.Batch(cmd, m.visionLoopCmd())
			}
			return mod, cmd
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

	case visionTickMsg:
		if !Vision().Enabled() {
			return m, nil
		}
		// skip when media supervisor is saturated
		h := Media().Health()
		if h.Max > 0 && h.Alive >= h.Max {
			return m, m.visionLoopCmd()
		}
		mod, cmd := m.startVisionTake("auto")
		return mod, tea.Batch(cmd, m.visionLoopCmd())

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
		// dual burst / forge RX: peer tiles always stick so dual Glyph can open late
		if msg.F != nil {
			if strings.HasPrefix(msg.Meta, "burst:") || strings.HasPrefix(msg.Meta, "forge:") {
				m.burstPeerFrame = msg.F
				nick := msg.Meta
				if strings.HasPrefix(nick, "burst:") {
					nick = strings.TrimPrefix(nick, "burst:")
				} else {
					nick = strings.TrimPrefix(nick, "forge:")
				}
				if i := strings.IndexByte(nick, ' '); i > 0 {
					nick = nick[:i]
				}
				if nick != "" {
					m.burstRemote = nick
				}
			} else if m.burstMode && (msg.Meta == "local" || msg.Meta == "burst-local" || m.talking || m.camOn) {
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
		// local preview always — style caps keep heavy filters from stalling stream
		maxW, maxH := m.styleDecodeWH(m.videoCols(), m.videoPxH())
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
				// mesh / Nothing hardware uses device N (25 or 13), not terminal hi-res
				gm := FrameToGlyph(fp, GlyphDeviceN(m.glyphN))
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

	case socialLazyTickMsg:
		return m.applySocialLazy(msg)

	case newsWallLoadMsg:
		return m.loadNewsWallIndex(msg)

	case newsWallReadyMsg:
		return m.applyNewsWallReady(msg)

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

	case statusSysMsg:
		m.pushSys(string(msg))
		m.status = truncate(string(msg), 28)
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
		// help overlay: cycle help pages (keys · stream · forge · venue · cli · docs)
		if m.showHelp {
			m.helpPage = (m.helpPage + 1) % HelpPageCount
			m.status = "help · " + HelpPageTitle(m.helpPage)
			return m, nil
		}
		if m.lab != nil && m.lab.On && m.input == "" {
			m.lab.NextFeed()
			if af := m.lab.ActiveFeed(); af != nil {
				m.status = "feed " + af.Label
			}
			return m, nil
		}
		return m.applyPromptMode((m.promptMode + 1) % ModeCount)
	case "shift+tab":
		if m.showHelp {
			m.helpPage = (m.helpPage + HelpPageCount - 1) % HelpPageCount
			m.status = "help · " + HelpPageTitle(m.helpPage)
			return m, nil
		}
		return m.applyPromptMode((m.promptMode + ModeCount - 1) % ModeCount)
	}

	if m.input == "" {
		// TABS fast keys: 1–7 (lab slots 1–6 win when lab on); letters V/b/P always
		if mode, ok := ModeFromFastKey(k); ok {
			labSlots := m.lab != nil && m.lab.On && len(k) == 1 && k[0] >= '1' && k[0] <= '6'
			if !labSlots {
				return m.applyPromptMode(mode)
			}
		}
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
				m.status = fmt.Sprintf("burst · ◎ %d×%d · [ ] scale · g res", m.glyphN, m.glyphN)
				m.pushSys("burst · Nothing Glyph dual · [ ] scale · g res · space PTT")
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
			if m.burstMode {
				m.glyphScale = nudgeGlyphScale(m.glyphScale, m.width, m.height, m.glyphN, -1)
				m.status = glyphScaleStatus(m.glyphScale, m.glyphN, m.width, m.height)
				return m, nil
			}
			if m.lab != nil && m.lab.On {
				m.status = fmt.Sprintf("scale %d · %s", m.lab.NudgeScale(-1), m.lab.BudgetLine())
			}
			return m, nil
		case "]":
			if m.burstMode {
				m.glyphScale = nudgeGlyphScale(m.glyphScale, m.width, m.height, m.glyphN, +1)
				m.status = glyphScaleStatus(m.glyphScale, m.glyphN, m.width, m.height)
				return m, nil
			}
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
				return m, nil
			}
			// empty-input o: PiP pop-out to macOS QuickTime (when watching)
			if m.vpipe != nil || m.watchPath != "" {
				return m.popOutPlayer("")
			}
			return m, nil
		case "O":
			// always PiP pop-out (even in lab)
			return m.popOutPlayer("")
		case "R":
			// restart active news tile / media recovery
			m.restartNewsTileActive()
			return m, nil
		case "K":
			// kill active news tile pipe (poster remains)
			if m.lab != nil && m.lab.News != nil && m.lab.News.On {
				m.killNewsTileActive()
				return m, nil
			}
			// kill watch pipe
			if m.vpipe != nil {
				m.stopWatch()
				m.pushSys("media · watch stopped")
				m.status = "watch off"
			}
			return m, nil
		case "H":
			// media health detail
			h := Media().Health()
			for _, ln := range strings.Split(strings.TrimRight(FormatMediaHealthDetail(h), "\n"), "\n") {
				m.pushSys(ln)
			}
			m.status = FormatMediaHealthChrome(h)
			return m, nil
		case "*":
			// Grok orchestrate take on current feeds
			return m.startGrokOrchestrate("")
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
				if m.lab.PluginStyle != "" {
					m.status = "style plugin:" + m.lab.PluginStyle
				} else {
					m.status = "style " + st.String()
				}
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
		case "G":
			// toggle square ↔ phone vertical double-stack Glyph aspect
			m.glyphAspect = cycleGlyphAspect(m.glyphAspect)
			if m.glyphAspect == GlyphAspectPhoneV {
				// snap to hardware phone sizes only
				if m.glyphN != GlyphPhone4a && m.glyphN != GlyphPhone3 {
					m.glyphN = GlyphPhone3
				}
			}
			m.status = FormatGlyphResLabel(m.glyphN, m.glyphAspect)
			m.pushSys(m.status + " · G toggle aspect · g cycle N")
			return m, nil
		case "g":
			// burst or phone-v: cycle matrix resolution; else open Grok prompt
			if m.burstMode || m.glyphAspect == GlyphAspectPhoneV || m.promptMode == ModeBurst || m.promptMode == ModePhone {
				if m.glyphAspect == GlyphAspectPhoneV {
					m.glyphN = cycleGlyphResPhoneV(m.glyphN)
				} else {
					m.glyphN = cycleGlyphRes(m.glyphN)
				}
				m.status = FormatGlyphResLabel(m.glyphN, m.glyphAspect)
				m.pushSys(m.status + " · LEDs " + itoa(glyphActiveCount(GlyphDeviceN(m.glyphN))))
				return m, nil
			}
			m.promptMode = ModeGrok
			m.status = "grok · " + m.grokCfg.ModeLabel()
			return m, nil
		case "p":
			// pattern play; if video open and shift not - keep pattern
			_ = m.toggleLive()
			return m, nil
		case "?", "f1":
			m.showHelp = !m.showHelp
			if m.showHelp {
				m.helpPage = 0
				m.status = "help · " + HelpPageTitle(0) + " · tab pages"
			}
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
	case ModeLab:
		// lab: paths fill active slot; else chat
		if isVideoPath(line) || looksLikeVideoArg(line) || ParseSocialQuery(line) != nil {
			return m.startWatch(line, true)
		}
		if m.client != nil {
			m.client.SendChat(line)
		}
		m.chat = append(m.chat, chatLine{From: m.nick, Text: line})
		m.trimChat()
		return m, nil
	case ModePhone:
		// phone tab: treat line as hub host or social/watch
		if strings.Contains(line, ":") || strings.HasPrefix(line, "ws") || ParseSocialQuery(line) != nil {
			return m.startWatch(line, true)
		}
		m.pushSys(FormatModeHelp())
		return m, nil
	case ModeBurst:
		// burst: chat still works while dual Glyph open
		if m.client != nil {
			m.client.SendChat(line)
		}
		m.chat = append(m.chat, chatLine{From: m.nick, Text: line})
		m.trimChat()
		return m, nil
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

// applyPromptMode switches TAB strip mode and arms related views.
func (m *Model) applyPromptMode(mode PromptMode) (tea.Model, tea.Cmd) {
	if mode < 0 || mode >= ModeCount {
		mode = ModeChat
	}
	m.promptMode = mode
	m.liveMode = mode == ModeLive
	switch mode {
	case ModeLab:
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
		m.burstMode = false
		m.compact = false
		m.lab.EnsurePlaceholders(4)
		m.status = "lab · " + m.lab.Layout.String()
		m.pushSys("tab lab · 1-6 slots · V toggle · m style · L layout")
	case ModeBurst:
		m.burstMode = true
		if m.lab != nil {
			m.lab.On = false
		}
		m.camOn = true
		m.videoOn = true
		m.status = "burst · " + FormatGlyphResLabel(m.glyphN, m.glyphAspect)
		m.pushSys("tab burst · g res · G phone-v · [ ] scale · space PTT")
	case ModePhone:
		m.burstMode = false
		if m.lab != nil {
			m.lab.On = false
		}
		// prefer fixed phone vertical glyph for cast UX
		m.glyphAspect = GlyphAspectPhoneV
		if m.glyphN != GlyphPhone4a && m.glyphN != GlyphPhone3 {
			m.glyphN = GlyphPhone3
		}
		m.status = "phone · " + FormatGlyphResLabel(m.glyphN, m.glyphAspect)
		port := 9876
		if m.host != "" {
			port = ParseHubPort(m.host)
		}
		info := BuildLanInfo(port, "")
		m.pushSys("phone cast · " + info.Phone)
		m.pushSys(FormatGlyphResLabel(m.glyphN, m.glyphAspect) + " · g cycle 13/25 · G aspect")
	case ModeLive:
		if m.lab != nil {
			m.lab.On = false
		}
		m.burstMode = false
		m.status = "live"
	case ModeGrok:
		if m.lab != nil {
			m.lab.On = false
		}
		m.burstMode = false
		m.status = "grok"
	case ModeWatch:
		if m.lab != nil {
			m.lab.On = false
		}
		m.burstMode = false
		m.status = "watch"
	default: // chat
		if m.lab != nil {
			m.lab.On = false
		}
		m.burstMode = false
		m.status = "chat"
	}
	return m, nil
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
		if m.lab != nil && m.lab.On {
			st := m.lab.CycleStyle()
			m.pixelMode = st
			if m.lab.PluginStyle != "" {
				m.pushSys("pixel plugin: " + m.lab.PluginStyle)
			} else {
				m.pushSys("pixel: " + st.String())
			}
		} else {
			m.pixelMode = (m.pixelMode + 1) % PixelCount
			m.pushSys("pixel: " + m.pixelMode.String())
		}
	case "plugin", "plugins":
		return m.handlePluginCmd(arg)
	case "space", "spaces", "xspace":
		return m.handleSpaceCmd(arg)
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
			m.pushSys("usage: /watch file|url|yt-link|@user|twitch:user  (live first · yt-dlp)")
			return m, nil
		}
		// ensure lab open when filling from slash in companion
		if cmd == "fill" && m.lab != nil {
			m.lab.On = true
		}
		return m.startWatch(src, true)
	case "lan", "phone", "wifi":
		// same-WiFi phone → terminal join banner
		port := 9876
		if m.host != "" {
			port = ParseHubPort(m.host)
		}
		info := BuildLanInfo(port, "")
		for _, line := range strings.Split(strings.TrimRight(FormatLanBanner(info), "\n"), "\n") {
			m.pushSys(line)
		}
		m.status = "phone cast"
		return m, nil
	case "duplex", "openmic", "fullduplex":
		m.duplexOn = !m.duplexOn
		if m.duplexOn {
			m.pushSys("duplex ON · space = open mic (RX ducked) · mesh audio+MIDI")
			m.status = "duplex"
			// auto-start open mic when enabling
			return m.startPTT()
		}
		m.pushSys("duplex OFF · space = PTT half-duplex")
		if m.talking {
			return m.stopPTT()
		}
		return m, nil
	case "meshmidi", "midi-mesh":
		m.meshMIDI = !m.meshMIDI
		m.pushSys(fmt.Sprintf("mesh MIDI %s · Strudel hits + walkie → hub type:midi",
			map[bool]string{true: "ON", false: "OFF"}[m.meshMIDI]))
		return m, nil
	case "overlay", "grok-cap", "grokcap", "grok-fx", "grokfx":
		return m.handleOverlayCmd(cmd, arg)
	case "orch", "orchestrate", "take-grok", "gtake", "grok-take":
		return m.startGrokOrchestrate(arg)
	case "vision", "see", "gv", "vision-take":
		return m.startVisionTake(arg)
	case "newswall", "news-wall", "news", "vwall", "agencies":
		return m.startNewsWall(arg)
	case "newswall-stop", "news-stop", "stopnews":
		m.stopNewsWall()
		m.pushSys("news wall stop")
		m.status = "news off"
		return m, nil
	case "media", "pipes", "ffmpeg":
		h := Media().Health()
		for _, ln := range strings.Split(strings.TrimRight(FormatMediaHealthDetail(h), "\n"), "\n") {
			m.pushSys(ln)
		}
		m.status = FormatMediaHealthChrome(h)
		return m, nil
	case "media-kill", "kill-media":
		n := Media().KillKind(MediaKindNews)
		if m.vpipe != nil {
			m.stopWatch()
			n++
		}
		Media().Shutdown()
		m.pushSys(fmt.Sprintf("media · killed supervised procs (+%d news)", n))
		return m, nil
	case "restart", "media-restart":
		m.restartNewsTileActive()
		return m, nil
	case "social", "handle":
		src := arg
		if src == "" {
			m.pushSys("usage: /social @user | twitch:user | yt:@channel | tt:@user")
			m.pushSys("  live/broadcast first · other content lazy-fills lab slots")
			return m, nil
		}
		if !strings.HasPrefix(src, "@") && ParseSocialQuery(src) == nil && !strings.Contains(src, ":") && !strings.Contains(src, "/") {
			src = "@" + strings.TrimPrefix(src, "@")
		}
		// open lab for lazy secondary stacks
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
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
	case "colossus", "stream-pub", "pcap-loop", "gyst-pub":
		// Live TUI ingest: local loop + optional hub publish (Colossus/DOJO)
		// /colossus [path|sim]   /colossus stop
		// /colossus multi a.pcap b.pcap …  → multi-pcap lab + forge marks
		arg = strings.TrimSpace(arg)
		if arg == "stop" || arg == "off" || arg == "0" {
			m.stopStreamPub()
			if m.pktPlayer != nil {
				m.pktPlayer.Stop()
			}
			m.pushSys("■ colossus / stream-pub stop")
			m.status = "stream stop"
			return m, nil
		}
		if strings.HasPrefix(arg, "multi ") || arg == "multi" || strings.HasPrefix(arg, "forge ") {
			rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(arg, "multi"), "forge"))
			rest = strings.TrimSpace(rest)
			paths := strings.Fields(rest)
			if len(paths) == 0 {
				// default sample in all empty slots
				if _, err := os.Stat("examples/dojo.pcap"); err == nil {
					paths = []string{"examples/dojo.pcap"}
				}
			}
			return m.startMultiPcapForge(paths)
		}
		src := arg
		if src == "" {
			// prefer repo sample, then last watch path
			if _, err := os.Stat("examples/dojo.pcap"); err == nil {
				src = "examples/dojo.pcap"
			} else if m.watchPath != "" && (IsStreamCodecPath(m.watchPath) || DetectStreamFile(m.watchPath) != "unknown") {
				src = m.watchPath
			} else {
				src = "sim"
			}
		}
		return m.startColossusIngest(src)
	case "conductor", "dir", "director":
		// /conductor claim|release|status — own the program bus
		return m.handleConductorCmd(strings.TrimSpace(arg))
	case "take":
		// /take [slot|next] — cut to program (conductor)
		return m.handleTakeCmd(strings.TrimSpace(arg))
	case "preview":
		// /preview [slot] | clear — arm/disarm PVW (ANC preview + tally flag)
		return m.handlePreviewCmd(strings.TrimSpace(arg))
	case "caption", "cap":
		// /caption text | /caption clear — on-air caption → ANC SDID 0x05
		return m.handleCaptionCmd(strings.TrimSpace(arg))
	case "hold":
		// /hold — freeze program (venue hold last frame)
		return m.handleProgramMode(ProgramModeHold)
	case "black", "slate":
		// /black — safe black/slate for venue sinks
		return m.handleProgramMode(ProgramModeBlack)
	case "program", "pgm", "onair":
		// /program [status] — show program bus
		m.pushProgramStatus()
		return m, nil
	case "forge":
		// /forge a.pcap b.pcap …  — multi-pcap NFT-style forge marks
		// /forge status | stop | next | hold | rotate
		arg = strings.TrimSpace(arg)
		if arg == "stop" || arg == "off" {
			m.stopStreamPub()
			if m.lab != nil {
				for i := range m.lab.Feeds {
					if m.lab.Feeds[i].Kind == "pcap" {
						m.lab.Feeds[i].Kind = "empty"
						m.lab.Feeds[i].Frame = nil
						m.lab.clearSlotMedia(i)
					}
				}
			}
			m.forgeLocal = nil
			m.forgeRotateOn = false
			m.forgeHoldLeft = false
			m.forgeLocalIdx = 0
			// keep forgeRX / peer frame so dual still shows last remote mark
			m.pushSys("■ forge multi-pcap stop")
			return m, nil
		}
		if arg == "status" || arg == "" {
			m.pushForgeStatus()
			return m, nil
		}
		if arg == "next" || arg == "n" {
			m.forgeHoldLeft = true
			m.stepForgeDualLocal(+1)
			m.pushSys(fmt.Sprintf("◈ forge left → %s", FormatForgeLocalLine(m.forgeLocal, m.forgeLocalIdx, true)))
			return m, nil
		}
		if arg == "prev" || arg == "p" {
			m.forgeHoldLeft = true
			m.stepForgeDualLocal(-1)
			m.pushSys(fmt.Sprintf("◈ forge left → %s", FormatForgeLocalLine(m.forgeLocal, m.forgeLocalIdx, true)))
			return m, nil
		}
		if arg == "hold" {
			m.forgeHoldLeft = true
			m.pushSys("◈ forge left hold · /forge rotate to resume")
			return m, nil
		}
		if arg == "rotate" || arg == "rot" {
			m.forgeHoldLeft = false
			m.forgeRotateOn = true
			m.pushSys("◈ forge left rotate · dual local multi-slot")
			return m, nil
		}
		paths := strings.Fields(arg)
		return m.startMultiPcapForge(paths)
	case "stream-stop":
		m.stopStreamPub()
		if m.pktPlayer != nil {
			m.pktPlayer.Stop()
		}
		m.pushSys("■ stream-stop")
		return m, nil
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
	case "pip", "popout", "pop-out", "qt", "quicktime":
		return m.popOutPlayer(arg)
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

// startColossusIngest: local packet loop (if stream file) + hub live publish.
// One-window DOJO: /colossus examples/dojo.pcap
func (m *Model) startColossusIngest(src string) (tea.Model, tea.Cmd) {
	src = strings.Trim(src, `"'`)
	low := strings.ToLower(src)
	isSim := low == "sim" || low == "test" || low == "cam" || low == "camera"
	path := expandPath(src)

	// stop prior hub publisher
	m.stopStreamPub()

	// Local loop for stream files (pcap/gyst) — same player as /load
	if !isSim {
		if IsStreamCodecPath(path) || DetectStreamFile(path) != "unknown" {
			mod, cmd := m.startPacketWatch(path)
			m = mod.(*Model)
			// continue to hub publish
			_ = cmd
		} else if !isVideoPath(path) && !looksLikeVideoArg(path) {
			// try as stream anyway
			if _, err := LoadStreamFile(path); err != nil {
				m.pushSys("colossus: " + err.Error())
				m.pushSys("usage: /colossus examples/dojo.pcap | sim | path.pcap")
				return m, nil
			}
			return m.startPacketWatch(path)
		}
	}

	// Hub publish when mesh client exists
	if m.client == nil {
		if isSim {
			m.pushSys("colossus sim needs hub — connect first or /load path.pcap for local")
		} else {
			m.pushSys("▶ local colossus loop · connect mesh to also publish gyst")
		}
		m.status = "colossus local"
		return m, nil
	}

	kind := "auto"
	if isSim {
		kind = "hexlum"
	}
	hub := m.host
	if hub == "" {
		hub = "127.0.0.1:9876"
	}
	opts := StreamPubOpts{
		Src: src, Hub: hub, Nick: m.nick,
		Kind: kind, W: 80, H: 48, HexN: m.glyphN, FPS: 12,
		Loop: true, Quiet: true, Pace: "auto", Colossus: true,
	}
	if m.glyphN >= 13 {
		opts.HexN = m.glyphN
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.streamPubCancel = cancel
	m.streamPubSrc = src
	go func() {
		_ = m.publishViaMeshClient(ctx, opts)
	}()

	m.videoOn = true
	m.pushSys(fmt.Sprintf("◎ colossus → hub %s · src=%s · /colossus stop", hub, src))
	m.status = "colossus " + filepath.Base(src)
	return m, nil
}

func (m *Model) stopStreamPub() {
	if m.streamPubCancel != nil {
		m.streamPubCancel()
		m.streamPubCancel = nil
	}
	m.streamPubSrc = ""
}

// ForgeDualRotateTicks — dwell per left slot at 20Hz tick (~1s).
const ForgeDualRotateTicks = 20

// refreshCapFromGeom updates local CapProfile after resize and announces type:cap.
func (m *Model) refreshCapFromGeom(cols, rows int) {
	prev := m.cap.Class
	prevN := m.cap.GlyphN
	// preserve user GY_CAP override via Detect
	m.cap = DetectCapProfile(cols, rows)
	// keep forge flag
	m.cap.Forge = true
	if m.client != nil {
		m.client.Cap = m.cap
		_ = m.client.SendJSON(m.cap.CapAnnounce(m.nick))
	}
	// only sys when class or glyph N changes meaningfully
	if prev != m.cap.Class || prevN != m.cap.GlyphN {
		m.pushSys(m.cap.SummaryLine())
	}
}

// applyRoomGlyphN sets local glyph preference from room min (publish-side courtesy).
// Does not re-stamp lattice; only guides local dual / hexlum encode size.
func (m *Model) applyRoomGlyphN() {
	if m.cap.Class == CapClassGlyphIoT {
		return
	}
	var peers []CapProfile
	for _, c := range m.peerCaps {
		peers = append(peers, c)
	}
	want := RoomGlyphN(m.cap.GlyphN, peers)
	// never upscale past what this terminal can dual-fit
	want = PreferGlyphNForGeom(want, m.width, m.height, m.cap.Dual || m.burstMode)
	if want > 0 && want != m.glyphN && (m.cap.Class == CapClassTermLean || len(peers) > 0) {
		// only soft-adjust lean rooms / when peers present
		if m.cap.Class != CapClassTermFull || want < m.glyphN {
			m.glyphN = want
		}
	}
}

// startMultiPcapForge loads each path into a lab slot with Cursor-Grok Forge watermarks
// and publishes watermarked hexlum + forge-mark meta to the hub when connected.
func (m *Model) startMultiPcapForge(paths []string) (tea.Model, tea.Cmd) {
	if len(paths) == 0 {
		m.pushSys("usage: /forge a.pcap b.pcap …  or /colossus multi a.pcap b.pcap")
		return m, nil
	}
	if m.lab == nil {
		m.lab = newLabState()
	}
	m.lab.On = true
	m.compact = false
	m.stopStreamPub()

	var marks []ForgeMark
	n := 0
	for i, p := range paths {
		if i >= MaxLabFeeds {
			m.pushSys(fmt.Sprintf("forge: max %d slots — truncated", MaxLabFeeds))
			break
		}
		slot := i + 1
		f, err := m.lab.FillPcapIntoSlot(slot, p)
		if err != nil {
			m.pushSys(fmt.Sprintf("forge slot %d: %s", slot, err.Error()))
			continue
		}
		n++
		if f.Forge != nil {
			marks = append(marks, *f.Forge)
			m.pushSys(FormatMarkLine(*f.Forge))
		}
	}
	if n == 0 {
		m.pushSys("forge: no pcaps loaded")
		return m, nil
	}

	// dual-local multi-slot rotate on left Glyph pane
	m.forgeLocalIdx = 0
	m.forgeHoldLeft = false
	m.forgeRotateOn = n >= 1
	if m.forgeRotateEvery <= 0 {
		m.forgeRotateEvery = ForgeDualRotateTicks
	}
	// multi-slot → open dual Glyph so left rotate is visible (peer right free for RX)
	if n >= 2 && !m.burstMode {
		m.burstMode = true
		m.videoOn = true
		m.pushSys("◈ forge dual-local rotate · left slots · peer RX holds right · /forge hold|next")
	}
	m.applyForgeDualLocalSlot(0)

	// hub publish: rotate slots, stamp forge mark + frames
	if m.client != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.streamPubCancel = cancel
		m.streamPubSrc = "forge-multi"
		pathsCopy := append([]string{}, paths...)
		go m.publishForgeMulti(ctx, pathsCopy, marks)
		m.pushSys(fmt.Sprintf("◈ forge multi-pcap ×%d → hub · Cursor-Grok Forge marks · /forge stop", n))
	} else {
		m.pushSys(fmt.Sprintf("◈ forge multi-pcap ×%d local lab · dual-left rotate · /forge stop", n))
	}
	m.status = fmt.Sprintf("forge ×%d", n)
	return m, nil
}

// forgePcapSlots returns marked pcap lab feeds in slot order (for dual-left rotate).
func (m *Model) forgePcapSlots() []*FeedSlot {
	if m.lab == nil {
		return nil
	}
	var out []*FeedSlot
	for i := range m.lab.Feeds {
		f := &m.lab.Feeds[i]
		if f.Kind == "pcap" && f.Forge != nil {
			out = append(out, f)
		}
	}
	return out
}

// tickForgeDualLocal cycles burstLocalFrame among forge pcap slots.
// Does not touch burstPeerFrame / forgeRX (peer right holds).
func (m *Model) tickForgeDualLocal() {
	if !m.forgeRotateOn || m.lab == nil {
		return
	}
	// during PTT TX, leave cam on left
	if m.talking {
		return
	}
	slots := m.forgePcapSlots()
	if len(slots) == 0 {
		return
	}
	every := m.forgeRotateEvery
	if every <= 0 {
		every = ForgeDualRotateTicks
	}
	if !m.forgeHoldLeft && len(slots) > 1 {
		m.forgeLocalIdx = (m.spin / every) % len(slots)
	}
	if m.forgeLocalIdx < 0 {
		m.forgeLocalIdx = 0
	}
	m.applyForgeDualLocalSlot(m.forgeLocalIdx % len(slots))
}

// stepForgeDualLocal moves left slot by delta (hold mode).
func (m *Model) stepForgeDualLocal(delta int) {
	slots := m.forgePcapSlots()
	if len(slots) == 0 {
		return
	}
	m.forgeLocalIdx = (m.forgeLocalIdx + delta) % len(slots)
	if m.forgeLocalIdx < 0 {
		m.forgeLocalIdx += len(slots)
	}
	m.applyForgeDualLocalSlot(m.forgeLocalIdx)
}

// applyForgeDualLocalSlot paints lab slot i into dual left + primary frame.
func (m *Model) applyForgeDualLocalSlot(i int) {
	slots := m.forgePcapSlots()
	if len(slots) == 0 {
		return
	}
	if i < 0 || i >= len(slots) {
		i = 0
	}
	m.forgeLocalIdx = i
	f := slots[i]
	// ensure frame stamped for this slot (tickLabSims may have refreshed)
	if f.Frame == nil && len(f.PcapPkts) > 0 {
		p := f.PcapPkts[f.PcapIdx%len(f.PcapPkts)]
		if fr, err := FrameFromPacket(&p); err == nil && fr != nil {
			if f.Forge != nil {
				StampFrame(fr, *f.Forge)
			}
			f.Frame = fr
		}
	}
	if f.Frame != nil {
		m.burstLocalFrame = f.Frame
		m.frame = f.Frame
		m.videoOn = true
		if f.Frame.W == f.Frame.H && f.Frame.W <= 49 {
			m.pixelMode = PixelHex
		}
	}
	if f.Forge != nil {
		cp := *f.Forge
		m.forgeLocal = &cp
	}
	// active lab highlight follows left dual
	if m.lab != nil {
		for j := range m.lab.Feeds {
			if &m.lab.Feeds[j] == f {
				m.lab.Active = j
				break
			}
		}
	}
}

// acceptForgeRX stores peer forge mark for dual Glyph chrome. True when ID/from is new.
func (m *Model) acceptForgeRX(from string, mark ForgeMark) bool {
	if mark.ID == "" {
		return false
	}
	isNew := m.forgeRX == nil || m.forgeRX.ID != mark.ID || m.forgeRXFrom != from
	cp := mark
	m.forgeRX = &cp
	m.forgeRXFrom = from
	return isNew
}

// ensureBurstForForgeRX opens dual Glyph on first forge hexlum frame. True if just opened.
func (m *Model) ensureBurstForForgeRX(from string) bool {
	if m.burstMode {
		if from != "" {
			m.burstRemote = from
		}
		return false
	}
	m.burstMode = true
	m.compact = false
	if from != "" {
		m.burstRemote = from
		m.remoteTX = from
	}
	return true
}

// ── Conductor / program bus ──────────────────────────────────

func (m *Model) handleConductorCmd(arg string) (tea.Model, tea.Cmd) {
	switch arg {
	case "", "status", "st":
		m.pushProgramStatus()
		return m, nil
	case "claim", "on", "take-control":
		m.conductor = true
		m.program.Conductor = m.nick
		m.program.T = time.Now().UnixMilli()
		m.publishProgramBus()
		m.pushSys("◈ conductor claimed · /take · /preview · /caption · /hold · /black")
		return m, nil
	case "release", "off":
		was := m.conductor
		m.conductor = false
		if was {
			m.pushSys("◈ conductor released")
		}
		// do not clear bus — last program holds for venue
		return m, nil
	default:
		m.pushSys("usage: /conductor claim|release|status")
		return m, nil
	}
}

func (m *Model) handleTakeCmd(arg string) (tea.Model, tea.Cmd) {
	if !m.ensureConductor() {
		return m, nil
	}
	src, ok := m.resolveProgramSource(arg)
	if !ok {
		m.pushSys("usage: /take [slot|next] · need forge slot or preview armed")
		return m, nil
	}
	m.program.Take(src, m.nick)
	// dual-local: cut left to taken forge slot when slot known
	if src.Source == ProgramSourceForge && src.Slot > 0 {
		m.forgeHoldLeft = true
		slots := m.forgePcapSlots()
		for i, f := range slots {
			if f.Forge != nil && f.Forge.Slot == src.Slot {
				m.applyForgeDualLocalSlot(i)
				break
			}
		}
	}
	m.publishProgramBus()
	m.pushSys("◈ TAKE " + FormatProgramSource(m.program.Program))
	m.status = fmt.Sprintf("PGM %s", FormatProgramSource(m.program.Program))
	return m, nil
}

func (m *Model) handlePreviewCmd(arg string) (tea.Model, tea.Cmd) {
	if !m.ensureConductor() {
		return m, nil
	}
	if arg == "clear" || arg == "off" || arg == "none" {
		m.program.ClearPreview(m.nick)
		m.publishProgramBus()
		m.pushSys("◈ PVW clear · tally preview flag off")
		return m, nil
	}
	src, ok := m.resolveProgramSource(arg)
	if !ok {
		m.pushSys("usage: /preview [slot|next] | /preview clear")
		return m, nil
	}
	m.program.SetPreview(src, m.nick)
	m.publishProgramBus()
	m.pushSys("◈ PVW " + FormatProgramSource(src) + " · ANC preview · /take to cut")
	return m, nil
}

func (m *Model) handleCaptionCmd(arg string) (tea.Model, tea.Cmd) {
	if !m.ensureConductor() {
		return m, nil
	}
	if arg == "" {
		eff := m.program.EffectiveCaption()
		if !eff.IsEmpty() {
			m.pushSys("◈ " + FormatCaptionLine(eff))
		} else {
			m.pushSys("usage: /caption text · /caption clear")
			m.pushSys("  /caption lang=en role=lower speaker=alice Hello")
			m.pushSys("  /caption en: Hello world")
		}
		return m, nil
	}
	cap, clear, err := ParseCaptionArg(arg)
	if err != nil {
		m.pushSys("caption: " + err.Error())
		return m, nil
	}
	if clear {
		m.program.SetCaptionRich(CaptionPayload{}, m.nick)
		m.publishProgramBus()
		m.pushSys("◈ caption clear · no caption ANC")
		return m, nil
	}
	m.program.SetCaptionRich(cap, m.nick)
	m.publishProgramBus()
	m.pushSys("◈ caption → ANC · " + FormatCaptionLine(cap))
	return m, nil
}

func (m *Model) handleProgramMode(mode string) (tea.Model, tea.Cmd) {
	if !m.ensureConductor() {
		return m, nil
	}
	switch mode {
	case ProgramModeHold:
		m.program.Hold(m.nick)
		m.pushSys("◈ HOLD program · venue freezes last frame")
	case ProgramModeBlack:
		m.program.Black(m.nick)
		m.pushSys("◈ BLACK · safe slate for venue sinks")
	default:
		return m, nil
	}
	m.publishProgramBus()
	m.status = "PGM " + m.program.Mode
	return m, nil
}

func (m *Model) ensureConductor() bool {
	if m.conductor {
		return true
	}
	// auto-claim on first take if no one else is conductor
	if m.program.Conductor == "" || m.program.Conductor == m.nick {
		m.conductor = true
		m.program.Conductor = m.nick
		m.pushSys("◈ conductor auto-claim · /conductor release to drop")
		return true
	}
	m.pushSys(fmt.Sprintf("program bus owned by %s · /conductor claim to steal", m.program.Conductor))
	return false
}

// resolveProgramSource parses /take /preview args into a ProgramSource.
func (m *Model) resolveProgramSource(arg string) (ProgramSource, bool) {
	arg = strings.TrimSpace(arg)
	// empty: use preview if armed, else current forge local, else forge RX
	if arg == "" || arg == "next" {
		if m.program.Preview != nil && arg == "" {
			return *m.program.Preview, true
		}
		if m.forgeLocal != nil {
			return SourceFromForge(m.nick, m.forgeLocal, LaneGlyph), true
		}
		if m.forgeRX != nil {
			return SourceFromForge(m.forgeRXFrom, m.forgeRX, LaneGlyph), true
		}
		if m.program.Preview != nil {
			return *m.program.Preview, true
		}
		return ProgramSource{}, false
	}
	// numeric slot
	if slot, err := strconv.Atoi(arg); err == nil && slot > 0 {
		for _, f := range m.forgePcapSlots() {
			if f.Forge != nil && f.Forge.Slot == slot {
				return SourceFromForge(m.nick, f.Forge, LaneGlyph), true
			}
		}
		// index into slots (1-based among marked)
		slots := m.forgePcapSlots()
		if slot <= len(slots) && slots[slot-1].Forge != nil {
			return SourceFromForge(m.nick, slots[slot-1].Forge, LaneGlyph), true
		}
		m.pushSys(fmt.Sprintf("no forge slot %d", slot))
		return ProgramSource{}, false
	}
	// nick — take their forge RX if matching
	if m.forgeRX != nil && (arg == m.forgeRXFrom || arg == "rx" || arg == "peer") {
		return SourceFromForge(m.forgeRXFrom, m.forgeRX, LaneGlyph), true
	}
	return ProgramSource{
		Source: ProgramSourceGyst,
		Nick:   arg,
		Lane:   LaneGyst,
		Label:  arg,
	}, true
}

func (m *Model) publishProgramBus() {
	m.program.V = 1
	if m.program.T == 0 {
		m.program.T = time.Now().UnixMilli()
	}
	if m.client == nil {
		return
	}
	_ = m.client.SendJSON(m.program.MeshJSON(m.nick))
}

func (m *Model) applyProgramBus(bus ProgramBus, from string) {
	// accept higher seq or any if we have none
	if bus.Seq > 0 && m.program.Seq > 0 && bus.Seq < m.program.Seq {
		return
	}
	prevCap := m.program.EffectiveCaption()
	m.program = bus
	if from != "" && bus.Conductor == "" {
		m.program.Conductor = from
	}
	// follow dual: when program is forge from us, hold left on that slot
	if m.conductor && bus.Program.Source == ProgramSourceForge && bus.Program.Slot > 0 {
		for i, f := range m.forgePcapSlots() {
			if f.Forge != nil && f.Forge.Slot == bus.Program.Slot {
				m.forgeHoldLeft = true
				m.applyForgeDualLocalSlot(i)
				break
			}
		}
	}
	// surface caption changes (chat-bridge --program-caption · /caption remote)
	nextCap := m.program.EffectiveCaption()
	if nextCap.Text != prevCap.Text || nextCap.Speaker != prevCap.Speaker || nextCap.Lang != prevCap.Lang {
		if nextCap.IsEmpty() {
			if !prevCap.IsEmpty() {
				m.pushSys("◈ caption clear (mesh)")
			}
		} else {
			m.pushSys("◈ " + FormatCaptionLine(nextCap))
		}
	}
}

func (m *Model) pushProgramStatus() {
	m.pushSys(FormatProgramLine(m.program))
	if m.program.Preview != nil {
		m.pushSys("◈ PVW " + FormatProgramSource(*m.program.Preview))
	}
	if eff := m.program.EffectiveCaption(); !eff.IsEmpty() {
		m.pushSys("◈ " + FormatCaptionLine(eff))
	}
	who := "viewer"
	if m.conductor {
		who = "conductor"
	}
	m.pushSys(fmt.Sprintf("◈ you: %s · %s", who, m.program.VenueAdapterHint()))
}

func (m *Model) pushForgeStatus() {
	if m.lab == nil {
		m.pushSys("forge: lab off")
		return
	}
	n := 0
	for _, f := range m.lab.Feeds {
		if f.Kind == "pcap" && f.Forge != nil {
			n++
			m.pushSys(FormatMarkLine(*f.Forge))
		}
	}
	if n == 0 {
		m.pushSys("forge: no marked pcap slots · /forge a.pcap b.pcap")
	} else {
		rot := "hold"
		if m.forgeRotateOn && !m.forgeHoldLeft {
			rot = "rotate"
		} else if m.forgeRotateOn && m.forgeHoldLeft {
			rot = "hold"
		}
		left := FormatForgeLocalLine(m.forgeLocal, m.forgeLocalIdx, m.forgeHoldLeft)
		m.pushSys(fmt.Sprintf("forge: %d marked slots · %s · left %s · %s", n, ForgeName, left, rot))
		if m.forgeRX != nil {
			m.pushSys(fmt.Sprintf("forge RX peer: %s · dual right holds", FormatMarkLine(*m.forgeRX)))
		}
	}
}

// publishForgeMulti cycles watermarked packets from each path to the hub.
func (m *Model) publishForgeMulti(ctx context.Context, paths []string, marks []ForgeMark) {
	type lane struct {
		pkts []StreamPacket
		mark ForgeMark
		idx  int
	}
	var lanes []lane
	for i, path := range paths {
		pkts, err := LoadStreamFile(expandPath(path))
		if err != nil {
			continue
		}
		var video []StreamPacket
		for _, p := range pkts {
			if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
				video = append(video, p)
			}
		}
		if len(video) == 0 {
			continue
		}
		mark := ForgeMark{}
		if i < len(marks) {
			mark = marks[i]
		} else {
			mark = NewForgeMark(i+1, filepath.Base(path), video[0].Payload)
		}
		lanes = append(lanes, lane{pkts: video, mark: mark})
	}
	if len(lanes) == 0 || m.client == nil {
		return
	}

	// emit forge-mark meta once per lane
	for _, ln := range lanes {
		_ = m.client.SendJSON(ln.mark.MeshJSON(m.nick))
	}

	tick := time.NewTicker(time.Second / 12)
	defer tick.Stop()
	var seq uint32
	laneI := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ln := &lanes[laneI%len(lanes)]
			laneI++
			p := ln.pkts[ln.idx%len(ln.pkts)]
			ln.idx++
			seq++
			// convert to hexlum for compact watermarked glyph lane
			var out StreamPacket
			if p.Kind == KindHexLum {
				out = p
				StampHexLum(out.Payload, int(out.Width), ln.mark)
			} else if fp, err := FrameFromPacket(&p); err == nil && fp != nil {
				StampFrame(fp, ln.mark)
				// prefer 25 hexlum for forge watermark broadcast
				n := 25
				if m.glyphN == 13 {
					n = 13
				}
				lum := RGBToHexLum(fp.RGB, fp.W, fp.H, n)
				StampHexLum(lum, n, ln.mark)
				out = PacketFromHexLum(lum, n, seq)
			} else {
				continue
			}
			out.Seq = seq
			out.TimeMS = uint64(time.Now().UnixMilli())
			_ = m.client.SendGystPacket(out)
			// occasionally re-assert mark
			if seq%36 == 1 {
				_ = m.client.SendJSON(ln.mark.MeshJSON(m.nick))
			}
		}
	}
}

func (m *Model) publishViaMeshClient(ctx context.Context, opts StreamPubOpts) error {
	path := expandPath(opts.Src)
	isStream := IsStreamCodecPath(path) || DetectStreamFile(path) != "unknown"
	if isStream {
		return m.publishFileViaClient(ctx, opts)
	}
	if opts.Src == "sim" || opts.Src == "test" || opts.Src == "" {
		return m.publishSimViaClient(ctx, opts)
	}
	// ffmpeg/cam needs dedicated process — use RunStreamPub (own WS client)
	return RunStreamPub(opts)
}

func (m *Model) publishFileViaClient(ctx context.Context, opts StreamPubOpts) error {
	pkts, err := LoadStreamFile(expandPath(opts.Src))
	if err != nil {
		return err
	}
	video := make([]StreamPacket, 0, len(pkts))
	for _, p := range pkts {
		if p.Kind == KindRGB24 || p.Kind == KindJPEG || p.Kind == KindHexLum {
			video = append(video, p)
		}
	}
	if len(video) == 0 {
		video = pkts
	}
	useTS := packetTimelineUseful(video)
	fpsDelay := time.Second / time.Duration(max(1, opts.FPS))
	var seq uint32
	for {
		var prev uint64
		for i, p := range video {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			if useTS && i > 0 && p.TimeMS > prev {
				d := time.Duration(p.TimeMS-prev) * time.Millisecond
				if d > 2*time.Second {
					d = 2 * time.Second
				}
				if d > 0 {
					t := time.NewTimer(d)
					select {
					case <-ctx.Done():
						t.Stop()
						return nil
					case <-t.C:
					}
				}
			} else if i > 0 {
				t := time.NewTimer(fpsDelay)
				select {
				case <-ctx.Done():
					t.Stop()
					return nil
				case <-t.C:
				}
			}
			if p.TimeMS > 0 {
				prev = p.TimeMS
			}
			seq++
			out := transformPubPacket(p, opts, seq)
			if m.client != nil {
				_ = m.client.SendGystPacket(out)
			}
		}
		if !opts.Loop {
			return nil
		}
	}
}

func (m *Model) publishSimViaClient(ctx context.Context, opts StreamPubOpts) error {
	tick := time.NewTicker(time.Second / time.Duration(max(1, opts.FPS)))
	defer tick.Stop()
	var seq uint32
	for {
		select {
		case <-ctx.Done():
			return nil
		case tm := <-tick.C:
			seq++
			fp := genSimFrame(opts.W, opts.H, float64(tm.UnixMilli()), int(seq))
			var p StreamPacket
			if strings.HasPrefix(strings.ToLower(opts.Kind), "hex") {
				n := opts.HexN
				if n < 5 {
					n = 25
				}
				lum := RGBToHexLum(fp.RGB, fp.W, fp.H, n)
				p = PacketFromHexLum(lum, n, seq)
			} else {
				p = PacketFromFramePixels(fp, seq)
			}
			if m.client != nil {
				_ = m.client.SendGystPacket(p)
			}
			// local preview
			if m.prog != nil {
				m.prog.Send(frameReady{F: fp, Meta: "colossus:sim"})
			}
		}
	}
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
	socialHint := ""
	if q := ParseSocialQuery(src); q != nil {
		if q.Platform != "" {
			socialHint = q.Platform + "/@" + q.Handle
		} else {
			socialHint = "@" + q.Handle
		}
		m.pushSys("social " + socialHint + " · live first…")
	} else {
		m.pushSys("resolve " + truncate(src, 50))
	}

	// social handles need longer (multi-platform probe)
	timeout := 90 * time.Second
	if ParseSocialQuery(src) != nil {
		timeout = 120 * time.Second
	}
	r, err := ResolveMediaTimeout(src, timeout)
	if err != nil {
		m.pushSys("watch: " + err.Error())
		m.status = "resolve fail"
		return m, nil
	}

	// mobile / social → double-stack GrokGlyph sample size; else normal video scale
	w, h := m.videoCols(), m.videoPxH()
	m.watchMobile = r.Mobile
	m.watchLive = r.Live
	m.watchSocial = FormatSocialStatus(r)
	if r.Mobile {
		gn := m.glyphN
		if gn < 13 {
			gn = 25
		}
		mw, mh := MobileGlyphStackSize(gn)
		// don't exceed terminal budget but prefer portrait double-stack
		if mw > 0 {
			w = min(max(w, mw/2), max(mw, w))
			// portrait: height from double-stack half-rows
			half := MobilePortraitHalfRows(w, gn)
			h = half * 2
			if h < mh/2 {
				h = min(mh, half*2)
			}
		}
		// lab: stack layout suits portrait tiles
		if m.lab != nil && m.lab.On {
			m.lab.Layout = LayoutStack
			// slightly narrower tiles for mobile stack
			if m.lab.Scale > 80 {
				m.lab.Scale = 80
			}
		}
	}

	vp, err := OpenVideoPipeResolved(r, w, h, withAudio)
	if err != nil {
		m.pushSys("watch: " + err.Error())
		return m, nil
	}
	MetricIncr("watch_starts")
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
	if r.Live {
		via += "·live"
	}
	if r.Mobile {
		via += "·mobile"
	}
	m.status = fmt.Sprintf("▶ %s", truncate(label, 28))
	m.pushSys(fmt.Sprintf("▶ %s · %s · %dx%d", truncate(label, 40), via, w, h))
	if m.watchSocial != "" {
		m.pushSys("◈ " + m.watchSocial)
	}
	m.applyDepthToFrame()
	// multi-feed lab: drop primary into active/empty placeholder
	if m.lab != nil && m.lab.On {
		m.lab.FillWatchIntoActive(truncate(label, 14), r.Input, m.frame)
		m.pushSys("vid → slot " + fmt.Sprintf("%d", m.lab.Active+1))
	}

	// lazy-load secondary social content into remaining lab slots (staggered)
	var cmd tea.Cmd
	if len(r.Lazy) > 0 {
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
		m.lab.EnsurePlaceholders(min(MaxLabFeeds, 1+len(r.Lazy)))
		tag := m.watchSocial
		if tag == "" {
			tag = truncate(label, 20)
		}
		m.pushSys(fmt.Sprintf("lazy +%d more · staggered", len(r.Lazy)))
		items := append([]LazyMediaItem(nil), r.Lazy...)
		cmd = tea.Tick(SocialLazyStagger(), func(t time.Time) tea.Msg {
			return socialLazyTickMsg{Items: items, Index: 0, Tag: tag}
		})
	}
	return m, cmd
}

// applySocialLazy reserves one secondary item as a watch placeholder (no decode yet).
// Next items continue on a stagger so stream handling stays light.
func (m *Model) applySocialLazy(msg socialLazyTickMsg) (tea.Model, tea.Cmd) {
	if msg.Index < 0 || msg.Index >= len(msg.Items) {
		return m, nil
	}
	if m.lab == nil {
		m.lab = newLabState()
	}
	m.lab.On = true
	item := msg.Items[msg.Index]
	// find empty slot (skip active primary)
	m.lab.EnsurePlaceholders(min(MaxLabFeeds, msg.Index+2))
	slot := -1
	for i := range m.lab.Feeds {
		if m.lab.Feeds[i].IsEmpty() {
			slot = i
			break
		}
	}
	if slot < 0 && len(m.lab.Feeds) < MaxLabFeeds {
		m.lab.EnsurePlaceholders(len(m.lab.Feeds) + 1)
		for i := range m.lab.Feeds {
			if m.lab.Feeds[i].IsEmpty() {
				slot = i
				break
			}
		}
	}
	if slot >= 0 {
		label := item.Title
		if label == "" {
			label = item.Kind
		}
		if label == "" {
			label = "lazy"
		}
		// tiny placeholder poster (mobile double-stack proportions when flagged)
		var fr *FramePixels
		gn := m.glyphN
		if gn < 13 {
			gn = 25
		}
		if item.Mobile {
			w, h := MobileGlyphStackSize(min(gn, 25))
			// smaller poster for lab tile
			w, h = max(24, w/4), max(32, h/4)
			if h%2 != 0 {
				h++
			}
			fr = genSocialPoster(w, h, label, item.Kind, item.Platform)
		} else {
			fr = genSocialPoster(48, 28, label, item.Kind, item.Platform)
		}
		prev := m.lab.Active
		m.lab.Active = slot
		m.lab.FillWatchIntoActive(truncate(label, 14), item.URL, fr)
		m.lab.Active = prev
		kind := item.Kind
		if kind == "" {
			kind = "vod"
		}
		m.pushSys(fmt.Sprintf("lazy[%d] %s · %s", slot+1, kind, truncate(label, 28)))
	}
	// schedule next
	next := msg.Index + 1
	if next < len(msg.Items) {
		items := msg.Items
		tag := msg.Tag
		return m, tea.Tick(SocialLazyStagger(), func(t time.Time) tea.Msg {
			return socialLazyTickMsg{Items: items, Index: next, Tag: tag}
		})
	}
	m.pushSys("lazy done · n slot · /watch src to take")
	return m, nil
}

// genSocialPoster builds a labeled RGB poster for lazy-loaded social slots.
func genSocialPoster(w, h int, title, kind, platform string) *FramePixels {
	if w < 16 {
		w = 16
	}
	if h < 8 {
		h = 8
	}
	if h%2 != 0 {
		h++
	}
	rgb := make([]byte, w*h*3)
	// gradient by platform hue
	pr, pg, pb := byte(30), byte(40), byte(70)
	switch normalizeSocialPlatform(platform) {
	case SocialTwitch:
		pr, pg, pb = 100, 60, 180
	case SocialYouTube:
		pr, pg, pb = 180, 40, 40
	case SocialKick:
		pr, pg, pb = 40, 180, 80
	case SocialTikTok:
		pr, pg, pb = 20, 220, 200
	case SocialInstagram:
		pr, pg, pb = 200, 60, 140
	case SocialX:
		pr, pg, pb = 40, 40, 50
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			fade := float64(y) / float64(h)
			rgb[i] = byte(float64(pr) * (0.35 + 0.65*fade))
			rgb[i+1] = byte(float64(pg) * (0.35 + 0.65*fade))
			rgb[i+2] = byte(float64(pb) * (0.35 + 0.65*fade))
		}
	}
	// bright top bar for kind
	bar := h / 6
	if bar < 2 {
		bar = 2
	}
	for y := 0; y < bar; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			if kind == "live" {
				rgb[i], rgb[i+1], rgb[i+2] = 220, 40, 40
			} else {
				rgb[i] = byte(min(255, int(pr)+40))
				rgb[i+1] = byte(min(255, int(pg)+40))
				rgb[i+2] = byte(min(255, int(pb)+40))
			}
		}
	}
	_ = title
	return &FramePixels{W: w, H: h, RGB: rgb, Source: "social-lazy", Stamp: time.Now().UnixMilli()}
}

func (m *Model) stopWatch() {
	m.stopStreamPub()
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
	m.watchMobile = false
	m.watchLive = false
	m.watchSocial = ""
}

// startNewsWall opens lab grid of major broadcaster glyph tiles (GrokGlyph-style).
// arg: empty|all|us|eu|me|asia|world · optional count e.g. "eu 6"
func (m *Model) startNewsWall(arg string) (tea.Model, tea.Cmd) {
	region := "all"
	maxN := Media().NewsMax()
	if maxN < 2 {
		maxN = 4
	}
	fields := strings.Fields(strings.TrimSpace(arg))
	for _, f := range fields {
		switch strings.ToLower(f) {
		case "all", "us", "eu", "me", "asia", "world", "intl":
			region = strings.ToLower(f)
		case "stop", "off":
			m.stopNewsWall()
			m.pushSys("news wall stop")
			return m, nil
		default:
			if n, err := strconv.Atoi(f); err == nil && n > 0 {
				maxN = n
			}
		}
	}
	if maxN > MaxNewsWallFeeds {
		maxN = MaxNewsWallFeeds
	}
	if nmax := Media().NewsMax(); maxN > nmax {
		maxN = nmax
	}
	if maxN < 2 {
		maxN = 2
	}
	srcs := FilterNewsSources(region, maxN)
	if len(srcs) == 0 {
		m.pushSys("news wall: no sources")
		return m, nil
	}

	// stop previous wall pipes
	m.stopNewsWall()

	if m.lab == nil {
		m.lab = newLabState()
	}
	m.lab.On = true
	m.lab.Layout = LayoutGrid
	m.lab.ShowList = false
	m.lab.Scale = 48 // compact tiles for mosaic wall
	m.lab.FPS = 8
	m.lab.Style = PixelHalf // matrix default (GrokGlyph)
	m.burstMode = false
	m.compact = false
	m.promptMode = ModeLab
	m.videoOn = true

	// rebuild feeds as news posters
	m.lab.Feeds = nil
	m.lab.uid = 0
	m.lab.EnsurePlaceholders(len(srcs))
	// respect supervisor news budget
	if nmax := Media().NewsMax(); len(srcs) > nmax {
		srcs = srcs[:nmax]
		m.pushSys(fmt.Sprintf("news · capped to %d tiles (GY_NEWS_MAX)", nmax))
	}
	nw := &NewsWallState{
		On:          true,
		Sources:     srcs,
		StyleBase:   PixelHalf,
		Pipes:       make([]*NewsTilePipe, len(srcs)),
		loading:     true,
		AutoRecover: true,
	}
	m.lab.News = nw
	for i, s := range srcs {
		style := NewsWallStyleLadder[i%len(NewsWallStyleLadder)]
		poster := newsPoster(s.Label, s.Region, i+1)
		if i < len(m.lab.Feeds) {
			m.lab.Feeds[i] = FeedSlot{
				ID: fmt.Sprintf("news-%s", s.ID), Label: s.Label, Kind: "news",
				Frame: poster, Seed: i + 1, WatchSrc: s.URL, TileStyle: style,
			}
		}
	}
	m.lab.Active = 0
	m.status = fmt.Sprintf("news wall · %d · %s", len(srcs), region)
	m.pushSys(fmt.Sprintf("◈ news wall · %d agencies · GrokGlyph tiles · staggered live", len(srcs)))
	m.pushSys("  styles matrix·hex·braille·ascii·blocks · m cycle · L layout · /news stop")
	MetricIncr("news_starts")
	// kick first resolve
	return m, tea.Tick(400*time.Millisecond, func(t time.Time) tea.Msg {
		return newsWallLoadMsg{Index: 0, Region: region, MaxN: len(srcs)}
	})
}

// loadNewsWallIndex resolves + starts one broadcaster pipe (async-friendly).
func (m *Model) loadNewsWallIndex(msg newsWallLoadMsg) (tea.Model, tea.Cmd) {
	if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
		return m, nil
	}
	nw := m.lab.News
	if msg.Index < 0 || msg.Index >= len(nw.Sources) {
		nw.loading = false
		m.pushSys("news wall · all slots queued")
		return m, nil
	}
	src := nw.Sources[msg.Index]
	m.pushSys(fmt.Sprintf("news · resolve %s…", src.Label))
	idx := msg.Index
	label := src.Label
	page := src.URL
	style := NewsWallStyleLadder[idx%len(NewsWallStyleLadder)]
	// async resolve + start pipe
	return m, func() tea.Msg {
		r, err := ResolveMediaTimeout(page, 75*time.Second)
		if err != nil {
			return newsWallReadyMsg{Index: idx, Label: label, Err: err.Error()}
		}
		// prefer live HLS
		vid := r.Video
		if vid == "" {
			return newsWallReadyMsg{Index: idx, Label: label, Err: "no video url"}
		}
		tp, err := StartNewsTile(label, vid, style)
		if err != nil {
			return newsWallReadyMsg{Index: idx, Label: label, Err: err.Error()}
		}
		return newsWallReadyMsg{Index: idx, Label: label, Pipe: tp}
	}
}

func (m *Model) applyNewsWallReady(msg newsWallReadyMsg) (tea.Model, tea.Cmd) {
	if m.lab == nil || m.lab.News == nil {
		if msg.Pipe != nil {
			msg.Pipe.Stop()
		}
		return m, nil
	}
	nw := m.lab.News
	if !nw.On {
		if msg.Pipe != nil {
			msg.Pipe.Stop()
		}
		return m, nil
	}
	if msg.Err != "" {
		m.pushSys(fmt.Sprintf("news · %s fail: %s", msg.Label, truncate(msg.Err, 50)))
	} else if msg.Pipe != nil {
		// stop old pipe at index
		if msg.Index >= 0 && msg.Index < len(nw.Pipes) && nw.Pipes[msg.Index] != nil {
			nw.Pipes[msg.Index].Stop()
		}
		if msg.Index >= len(nw.Pipes) {
			// grow
			for len(nw.Pipes) <= msg.Index {
				nw.Pipes = append(nw.Pipes, nil)
			}
		}
		if msg.Index >= 0 && msg.Index < len(nw.Pipes) {
			nw.Pipes[msg.Index] = msg.Pipe
		}
		if msg.Index >= 0 && msg.Index < len(m.lab.Feeds) {
			m.lab.Feeds[msg.Index].Kind = "news"
			m.lab.Feeds[msg.Index].Label = msg.Label
			if fr := msg.Pipe.Snapshot(); fr != nil {
				m.lab.Feeds[msg.Index].Frame = fr
			}
		}
		m.pushSys(fmt.Sprintf("news · live %s · %s", msg.Label, NewsWallStyleName(msg.Pipe.Style)))
	}
	// schedule next
	next := msg.Index + 1
	if next < len(nw.Sources) {
		return m, tea.Tick(NewsWallStagger(), func(t time.Time) tea.Msg {
			return newsWallLoadMsg{Index: next}
		})
	}
	nw.loading = false
	m.status = fmt.Sprintf("news wall · %d live", len(nw.Sources))
	m.pushSys("news wall ready · GrokGlyph mosaic · m style · n next")
	return m, nil
}

// syncNewsWallFrames copies latest tile pipe frames into lab feeds.
func (m *Model) syncNewsWallFrames() {
	if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
		return
	}
	nw := m.lab.News
	for i, tp := range nw.Pipes {
		if tp == nil || i >= len(m.lab.Feeds) {
			continue
		}
		if fr := tp.Snapshot(); fr != nil {
			m.lab.Feeds[i].Frame = fr
			m.lab.Feeds[i].Kind = "news"
		}
	}
}

// recoverNewsWallTiles soft-restarts dead tiles (supervisor-aware, backoff).
func (m *Model) recoverNewsWallTiles() {
	if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
		return
	}
	nw := m.lab.News
	if !nw.AutoRecover {
		return
	}
	for i, tp := range nw.Pipes {
		if tp == nil || !tp.NeedsRestart() {
			continue
		}
		label := tp.Label
		nt, err := RestartNewsTile(tp)
		if err != nil {
			continue
		}
		nw.Pipes[i] = nt
		if i < len(m.lab.Feeds) {
			if fr := nt.Snapshot(); fr != nil {
				m.lab.Feeds[i].Frame = fr
			}
		}
		MetricIncr("recoveries")
		m.pushSys(fmt.Sprintf("media · restart %s (#%d)", label, nt.Restarts))
	}
}

// restartNewsTileActive restarts the active lab news tile (TUI R key).
func (m *Model) restartNewsTileActive() {
	if m.lab == nil || m.lab.News == nil || !m.lab.News.On {
		if m.vpipe != nil {
			// restart watch segment
			_ = m.vpipe.SeekRel(0) // may no-op; kill+reopen via stopWatch no
			m.pushSys("media · watch restart: /watch again or seek")
		}
		return
	}
	i := m.lab.Active
	if i < 0 || i >= len(m.lab.News.Pipes) {
		return
	}
	tp := m.lab.News.Pipes[i]
	if tp == nil {
		m.pushSys("media · empty tile")
		return
	}
	nt, err := RestartNewsTile(tp)
	if err != nil {
		m.pushSys("media · restart fail: " + err.Error())
		return
	}
	m.lab.News.Pipes[i] = nt
	if i < len(m.lab.Feeds) {
		if fr := nt.Snapshot(); fr != nil {
			m.lab.Feeds[i].Frame = fr
		}
	}
	m.status = "restart " + nt.Label
	m.pushSys("media · restarted " + nt.Label)
}

// killNewsTileActive drops the active news pipe (keeps poster).
func (m *Model) killNewsTileActive() {
	if m.lab == nil || m.lab.News == nil {
		return
	}
	i := m.lab.Active
	if i < 0 || i >= len(m.lab.News.Pipes) {
		return
	}
	tp := m.lab.News.Pipes[i]
	if tp == nil {
		return
	}
	label := tp.Label
	poster := tp.Poster
	tp.Stop()
	m.lab.News.Pipes[i] = nil
	if i < len(m.lab.Feeds) {
		m.lab.Feeds[i].Kind = "news"
		if poster != nil {
			m.lab.Feeds[i].Frame = poster.Clone()
		}
	}
	m.pushSys("media · killed " + label + " (poster held)")
	m.status = "kill " + label
}

// stopNewsWall tears down tile pipes and clears news state.
func (m *Model) stopNewsWall() {
	if m.lab == nil || m.lab.News == nil {
		return
	}
	for _, tp := range m.lab.News.Pipes {
		if tp != nil {
			tp.Stop()
		}
	}
	// belt: kill any leftover news kind in supervisor
	Media().KillKind(MediaKindNews)
	m.lab.News.On = false
	m.lab.News.Pipes = nil
	m.lab.News = nil
}

// popOutPlayer opens current (or arg) media in macOS QuickTime Player + PiP.
func (m *Model) popOutPlayer(arg string) (tea.Model, tea.Cmd) {
	if !PopOutSupported() {
		m.pushSys("PiP: " + PopOutPlayerName() + " only on macOS")
		return m, nil
	}
	src := strings.TrimSpace(arg)
	videoURL := ""
	title := ""
	if src == "" {
		// prefer active vpipe stream
		if m.vpipe != nil {
			videoURL = m.vpipe.VideoURL
			src = m.vpipe.Input
			if src == "" {
				src = m.vpipe.Src
			}
			title = m.vpipe.Src
		}
		if src == "" {
			src = m.watchPath
		}
		// lab active watch slot
		if src == "" && m.lab != nil && m.lab.On {
			if af := m.lab.ActiveFeed(); af != nil && af.WatchSrc != "" {
				src = af.WatchSrc
				title = af.Label
			}
		}
	}
	if src == "" && videoURL == "" {
		m.pushSys("PiP: nothing playing — /watch file|url first · O pop-out")
		return m, nil
	}
	m.status = "PiP…"
	m.pushSys("PiP → " + PopOutPlayerName() + " · " + truncate(firstNonEmptyStr(title, src), 40))
	// resolve can block on yt-dlp — run async
	s, v, t := src, videoURL, title
	return m, func() tea.Msg {
		msg, err := PopOutMacPlayer(s, v, t)
		if err != nil {
			return errMsg("PiP: " + err.Error())
		}
		return statusSysMsg(msg)
	}
}

// statusSysMsg pushes a system line + status (used by async PiP).
type statusSysMsg string

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
		// duplex: space toggles open mic off
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
	// duplex: duck RX so full-duplex walkie doesn't feedback as hard
	if m.duplexOn && m.player != nil {
		m.player.SetDuck(0.12)
	}
	sess, err := startPTT(func(chunk []byte) {
		// soft gate near-silence (signls/sektron-style clean triggers)
		// duplex keeps a lower gate so continuous talk still streams
		gate := 0.008
		if m.duplexOn {
			gate = 0.004
		}
		if SoftGate(chunk, gate) == nil {
			return
		}
		if m.client != nil {
			m.client.SendAudio(chunk)
		}
		lv := rmsLevel(chunk)
		if prog != nil {
			prog.Send(audioLvlMsg{Level: lv, Bands: bandLevels(chunk, 32), TX: true})
		}
		// mesh MIDI walkie VU as CC expression for jam peers
		if m.meshMIDI && m.client != nil {
			m.client.SendMIDI(BuildMeshMIDICC(m.nick, 0, 11, int(lv*127), "walkie"))
		}
	})
	if err != nil {
		m.pushSys("mic: " + err.Error())
		if m.player != nil {
			m.player.SetDuck(1)
		}
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
	} else if m.duplexOn {
		m.client.SendPTT(true)
		m.status = "DUPLEX"
		m.pushSys("duplex open-mic · RX ducked · space to stop")
	} else {
		m.client.SendPTT(true)
		m.status = "PTT"
	}
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(true, LevelToVelocity(0.5))
	}
	if m.meshMIDI && m.client != nil {
		m.client.SendMIDINote(MeshMIDINoteOn, 0, 48, 90, "walkie") // C3 PTT
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
	// restore full RX after TX
	if m.player != nil {
		m.player.SetDuck(1)
	}
	if m.client != nil {
		if burst {
			m.client.SendBurstEnd()
		}
		m.client.SendPTT(false)
	}
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(false, LevelToVelocity(m.peak))
	}
	if m.meshMIDI && m.client != nil {
		m.client.SendMIDINote(MeshMIDINoteOff, 0, 48, 0, "walkie")
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

// strudelHitToMIDI maps mini-notation hits to drum/mel notes for mesh MIDI.
func strudelHitToMIDI(ev strudel.Event) (note, ch, vel int) {
	vel = 90
	if ev.Vel > 0 {
		vel = int(ev.Vel)
		if vel > 127 {
			vel = 127
		}
	}
	if ev.MIDI > 0 {
		ch = 0
		if ev.Kind == "drum" {
			ch = 9
		}
		return ev.MIDI, ch, vel
	}
	snd := strings.ToLower(ev.Sound)
	// drums → ch 9 (GM)
	switch {
	case strings.Contains(snd, "bd") || snd == "kick":
		return 36, 9, vel
	case strings.Contains(snd, "sd") || snd == "snare":
		return 38, 9, vel
	case strings.Contains(snd, "hh") || strings.Contains(snd, "hat"):
		return 42, 9, vel
	case strings.Contains(snd, "cp") || strings.Contains(snd, "clap"):
		return 39, 9, vel
	case strings.Contains(snd, "oh"):
		return 46, 9, vel
	}
	if ev.Kind == "note" {
		return 60, 0, vel // C4 fallback
	}
	return -1, 0, 0
}

func (m *Model) shutdown() {
	if m.talking {
		_, _ = m.stopPTT()
	}
	if m.live != nil {
		m.live.Stop()
	}
	m.stopNewsWall()
	m.stopWatch()
	// kill every supervised ffmpeg/ffplay (no orphans)
	Media().Shutdown()
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
				dw, dh := m.styleDecodeWH(m.videoCols(), m.videoPxH())
				return m, decodeFrameCmd(jpeg, meta, dw, dh)
			}
		}
	}

	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		return m, nil
	}
	// plugin mesh inbound (may drop or mutate)
	msg = Plugins().ApplyMeshInbound(msg)
	if msg == nil {
		return m, nil
	}
	// X Spaces roster / levels / chat / captions (burst stage + RTMP producer)
	if t, _ := msg["type"].(string); strings.HasPrefix(t, "space-") {
		ApplySpaceMeshInbound(msg)
	}
	// vision-take mesh → theme bus (browser + peers)
	if t, _ := msg["type"].(string); t == "vision-take" {
		theme, _ := msg["theme"].(string)
		feed, _ := msg["feed"].(string)
		if theme != "" {
			Vision().mu.Lock()
			Vision().lastTheme = normalizeThemeToken(theme)
			if feed != "" {
				Vision().themes[feed] = Vision().lastTheme
			}
			Vision().mu.Unlock()
		}
	}
	switch msg["type"] {
	case "hello":
		// quiet — status already shows nick when connected
	case "roster":
		m.peers = nil
		if m.peerCaps == nil {
			m.peerCaps = make(map[string]CapProfile)
		}
		if arr, ok := msg["peers"].([]any); ok {
			for _, p := range arr {
				if pm, ok := p.(map[string]any); ok {
					nick, _ := pm["nick"].(string)
					talk, _ := pm["talking"].(bool)
					m.peers = append(m.peers, peerInfo{Nick: nick, Talking: talk})
					if cap, ok := ParseCapFromMesh(pm); ok && nick != "" {
						m.peerCaps[nick] = cap
					}
				}
			}
		}
		m.applyRoomGlyphN()
	case "join":
		if n, _ := msg["nick"].(string); n != "" {
			m.status = n + " +"
			if cap, ok := ParseCapFromMesh(msg); ok {
				if m.peerCaps == nil {
					m.peerCaps = make(map[string]CapProfile)
				}
				m.peerCaps[n] = cap
				m.applyRoomGlyphN()
			}
		}
	case "cap":
		from, _ := msg["from"].(string)
		if from == "" {
			from, _ = msg["nick"].(string)
		}
		if cap, ok := ParseCapFromMesh(msg); ok && from != "" && from != m.nick {
			if m.peerCaps == nil {
				m.peerCaps = make(map[string]CapProfile)
			}
			m.peerCaps[from] = cap
			m.applyRoomGlyphN()
		}
	case "program":
		// conductor program bus — venue sinks + dual follow + caption
		from, _ := msg["from"].(string)
		if bus, ok := ParseProgramBus(msg); ok {
			// ignore echo of our own higher/equal if we are conductor racing
			if from == m.nick && m.conductor && bus.Seq <= m.program.Seq {
				return m, nil
			}
			prevSeq := m.program.Seq
			prevMode := m.program.Mode
			prevSrc := m.program.Program.Mark
			m.applyProgramBus(bus, from)
			// status line on cut / mode / seq (caption already pushed in applyProgramBus)
			if bus.Seq != prevSeq || bus.Mode != prevMode || bus.Program.Mark != prevSrc {
				m.pushSys(FormatProgramLine(m.program))
				m.status = fmt.Sprintf("PGM %s", FormatProgramSource(m.program.Program))
				if eff := m.program.EffectiveCaption(); !eff.IsEmpty() {
					m.status = truncate(eff.Display(), 28)
				}
			}
		}
	case "caption":
		// informational caption (UI) — no program authority; Grok overlay + soft line
		from, _ := msg["from"].(string)
		if cap, ok := ParseCaptionFromMesh(msg); ok && !cap.IsEmpty() {
			if from != "" && from != m.nick {
				m.pushSys("◈ " + FormatCaptionLine(cap) + " · soft")
				m.status = truncate(cap.Display(), 28)
				if m.overlay != nil {
					m.overlay.Record(cap.Text, from)
				}
			}
		} else if text, _ := msg["text"].(string); text != "" && from != m.nick {
			m.pushSys("◈ " + from + ": " + truncate(text, 60))
			if m.overlay != nil {
				m.overlay.Record(text, from)
			}
		}
	case "leave":
		if n, _ := msg["nick"].(string); n != "" {
			m.status = n + " −"
			if m.peerCaps != nil {
				delete(m.peerCaps, n)
			}
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
	case "gyst", "gyst-frame":
		// live headless stream (DOJO/Colossus): rgb24|hexlum|jpeg over mesh
		// Cursor-Grok Forge: meta marks + stamped hexlum → dual Glyph peer pane
		from, _ := msg["from"].(string)
		if from == m.nick {
			return m, nil
		}
		// Cursor-Grok Forge NFT-style mark (meta or embedded on frame)
		if mark, ok := ParseForgeFromMesh(msg); ok {
			if m.acceptForgeRX(from, mark) {
				m.pushSys(FormatMarkLine(mark) + " · dual Glyph")
			}
			// meta-only mark packets may lack frame payload
			if b64, _ := msg["b64"].(string); b64 == "" && msg["kind"] == "meta" {
				return m, nil
			}
		}
		pkt, err := MeshToPacket(msg)
		if err != nil || pkt == nil {
			// still try meta payload
			if kind, _ := msg["kind"].(string); kind == "meta" {
				return m, nil
			}
			return m, nil
		}
		if pkt.Kind == KindMeta {
			if mark, ok := ParseForgeMark(pkt.Payload); ok {
				if m.acceptForgeRX(from, mark) {
					m.pushSys(FormatMarkLine(mark) + " · dual Glyph")
				}
			}
			return m, nil
		}
		// hexlum also feeds glyph consumers (Nothing / lastGlyph bridge)
		if pkt.Kind == KindHexLum && len(pkt.Payload) > 0 {
			ints := make([]int, len(pkt.Payload))
			for i, b := range pkt.Payload {
				ints[i] = int(b)
			}
			m.lastGlyph = ints
		}
		fp, err := FrameFromPacket(pkt)
		if err != nil || fp == nil {
			return m, nil
		}
		// prefer hex style for hexlum packets (aesthetic)
		if pkt.Kind == KindHexLum && m.pixelMode == PixelHalf {
			m.pixelMode = PixelHex
		}
		m.videoOn = true

		// Live dual Glyph receive of forge stream (stamped hexlum / frames)
		forgePeer := m.forgeRX != nil && m.forgeRXFrom == from
		meta := fmt.Sprintf("gyst:%s %s %dx%d", from, pkt.KindName(), fp.W, fp.H)
		if forgePeer {
			opened := m.ensureBurstForForgeRX(from)
			if opened {
				m.pushSys(fmt.Sprintf("◈ forge → dual Glyph · %s", BurstForgePeerLabel(from, m.forgeRX)))
			}
			// peer tile + burst meta so frameReady sticks dual right
			m.burstPeerFrame = fp
			m.burstRemote = from
			m.remoteTX = from
			meta = "burst:" + from
			// local dual left: show lab/active forge tile when we have one
			if m.forgeLocal != nil && m.burstLocalFrame == nil && m.frame != nil {
				m.burstLocalFrame = m.frame
			}
		}
		// optional auto Grok overlay on live mesh (throttled)
		var autoCmd tea.Cmd
		if m.overlay != nil && m.overlay.Auto {
			autoCmd = m.maybeAutoOverlay(from, pkt.KindName(), fp.W, fp.H)
		}
		return m, tea.Batch(func() tea.Msg {
			return frameReady{F: fp, Meta: meta}
		}, autoCmd)
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
			maxW, maxH = m.styleDecodeWH(m.videoCols(), m.videoPxH())
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
		// duplex: duck already applied on Player; half-duplex still plays full
		if m.player != nil {
			m.player.Write(pcm, sr, ch)
		}
		m.remoteTX = from
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
	case MeshMIDIType:
		// bidirectional MIDI peer sync (walkie + Strudel) — type "midi"
		mm, ok := ParseMeshMIDI(msg)
		if !ok || mm.From == m.nick {
			return m, nil
		}
		if m.midiOn && m.midiBridge != nil {
			if s := ApplyMeshMIDI(m.midiBridge, mm); s != "" {
				m.status = s
			}
		}
		// soft tempo lock from peer jam
		if mm.Kind == MeshMIDITempo && mm.CPS > 0 && m.live != nil {
			m.status = fmt.Sprintf("jam tempo · %.2f cps ←%s", mm.CPS, mm.From)
		}
	}
	return m, nil
}

// handleSpaceCmd: /space [status|id|key|caption|rtmp|rtmps|seat|listeners|push]
func (m *Model) handleSpaceCmd(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := "status"
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}
	rest := ""
	if len(fields) > 1 {
		rest = strings.Join(fields[1:], " ")
	}
	s := Spaces()
	switch sub {
	case "", "status", "show", "doctor":
		for _, ln := range strings.Split(strings.TrimRight(FormatSpaceDoctor(s), "\n"), "\n") {
			m.pushSys(ln)
		}
		m.status = "space"
		return m, nil
	case "id", "url", "open":
		if rest != "" {
			s.SetID(rest)
		}
		snap := s.Snapshot()
		m.pushSys("space → " + snap.URL)
		m.status = "space " + snap.ID
		return m, nil
	case "key", "stream-key":
		if rest == "" {
			m.pushSys("stream key: " + rtmpKeyStatus(s.Snapshot().RTMP) + " · /space key --pull|<key>")
			return m, nil
		}
		if rest == "--pull" || rest == "pull" || rest == "--auto" {
			src, key, err := PullStreamKey(PullKeyOpts{Clipboard: true})
			if err != nil {
				m.pushSys(err.Error())
				return m, nil
			}
			s.SetStreamKeyFrom(key, src)
			m.pushSys("stream key auto-pulled · " + src + " · " + rtmpKeyStatus(s.Snapshot().RTMP))
			m.status = "key " + src
			return m, nil
		}
		s.SetStreamKey(rest)
		m.pushSys("stream key set · " + rtmpKeyStatus(s.Snapshot().RTMP))
		m.status = "rtmp key"
		return m, nil
	case "mute":
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			m.pushSys("usage: /space mute host|cohost:N|speaker:N|all [off]")
			return m, nil
		}
		off := len(parts) > 1 && (parts[1] == "off" || parts[1] == "0" || parts[1] == "unmute")
		if parts[0] == "all" {
			s.SetMuteAll(!off, m.nick)
			if m.client != nil {
				_ = m.client.SendJSON(s.MeshMuteAllJSON(m.nick, !off))
				_ = m.client.SendJSON(s.MeshRosterJSON(m.nick))
			}
			m.pushSys(fmt.Sprintf("mute-all → %v", !off))
			m.status = "mute-all"
			return m, nil
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		if err := s.SetMute(role, idx, !off, m.nick); err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshMuteJSON(m.nick, role, idx, !off))
		}
		m.pushSys(fmt.Sprintf("mute %s[%d] → %v", role, idx, !off))
		m.status = "mute"
		return m, nil
	case "unmute":
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			m.pushSys("usage: /space unmute host|cohost:N|speaker:N|all")
			return m, nil
		}
		if parts[0] == "all" {
			s.SetMuteAll(false, m.nick)
			if m.client != nil {
				_ = m.client.SendJSON(s.MeshMuteAllJSON(m.nick, false))
			}
			m.pushSys("unmute all")
			return m, nil
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		_ = s.SetMute(role, idx, false, m.nick)
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshMuteJSON(m.nick, role, idx, false))
		}
		m.pushSys(fmt.Sprintf("unmute %s[%d]", role, idx))
		return m, nil
	case "offer", "asset":
		pub := strings.Contains(rest, "public")
		off := strings.Contains(rest, "off")
		s.SetAssetOffer(!off, m.nick, "", pub)
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshAssetJSON(m.nick))
		}
		m.pushSys(fmt.Sprintf("stream asset offer=%v public=%v", !off, pub))
		m.status = "asset"
		return m, nil
	case "guest":
		if rest == "" {
			m.pushSys("usage: /space guest <nick>")
			return m, nil
		}
		s.AllowGuest(rest)
		m.pushSys("guest allowed · " + rest)
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshAssetJSON(m.nick))
		}
		return m, nil
	case "rtmp":
		s.SetSecure(false)
		m.pushSys("ingest → " + XRTMPURL)
		return m, nil
	case "rtmps":
		s.SetSecure(true)
		m.pushSys("ingest → " + XRTMPSURL)
		return m, nil
	case "caption", "cap":
		s.SetCaption(rest)
		m.pushSys("space caption → " + emptyDash(rest))
		if m.client != nil && rest != "" {
			_ = m.client.SendJSON(map[string]any{
				"type": "space-caption", "from": m.nick, "text": rest, "caption": rest,
				"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
			})
		}
		m.status = "caption"
		return m, nil
	case "listeners", "n":
		parts := strings.Fields(rest)
		if len(parts) == 0 || parts[0] == "list" {
			snap := s.Snapshot()
			m.pushSys(fmt.Sprintf("listeners %d", snap.Listeners))
			for i, l := range snap.ListenerList {
				if i >= 20 {
					m.pushSys(fmt.Sprintf("  … +%d more", len(snap.ListenerList)-20))
					break
				}
				m.pushSys("  · " + l.Nick)
			}
			return m, nil
		}
		if parts[0] == "add" && len(parts) > 1 {
			nick := strings.Join(parts[1:], " ")
			s.AddListener(nick, "")
			if m.client != nil {
				_ = m.client.SendJSON(map[string]any{
					"type": "space-listener-join", "from": m.nick, "nick": nick,
					"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
				})
			}
			m.pushSys("listener +" + nick)
			return m, nil
		}
		if (parts[0] == "rm" || parts[0] == "remove" || parts[0] == "leave") && len(parts) > 1 {
			nick := strings.Join(parts[1:], " ")
			s.RemoveListener(nick)
			if m.client != nil {
				_ = m.client.SendJSON(map[string]any{
					"type": "space-listener-leave", "from": m.nick, "nick": nick,
					"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
				})
			}
			m.pushSys("listener -" + nick)
			return m, nil
		}
		if parts[0] == "join" {
			s.AddListener(m.nick, "")
			if m.client != nil {
				_ = m.client.SendJSON(map[string]any{
					"type": "space-listener-join", "from": m.nick, "nick": m.nick,
					"space": s.Snapshot().ID, "t": time.Now().UnixMilli(),
				})
			}
			m.pushSys("you joined as listener")
			return m, nil
		}
		n, _ := strconv.Atoi(parts[0])
		s.SetListeners(n)
		m.pushSys(fmt.Sprintf("listeners → %d", n))
		return m, nil
	case "seat":
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			m.pushSys("usage: /space seat host|cohost:N|speaker:N <nick>")
			return m, nil
		}
		role, idx, err := parseSeatSpec(parts[0])
		if err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		nick := strings.Join(parts[1:], " ")
		if err := s.Seat(role, idx, nick); err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		m.pushSys(fmt.Sprintf("seated %s[%d] → %s", role, idx, nick))
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshRosterJSON(m.nick))
		}
		return m, nil
	case "roster", "sync":
		if m.client != nil {
			_ = m.client.SendJSON(s.MeshRosterJSON(m.nick))
			m.pushSys("space roster → mesh")
		} else {
			m.pushSys("not connected")
		}
		return m, nil
	case "push", "rtmp-push":
		// /space push [input] — needs key + input
		key := s.Snapshot().RTMP.StreamKey
		if key == "" {
			m.pushSys("stream key available when ready · /space key <key> or GY_X_STREAM_KEY")
			return m, nil
		}
		in := strings.TrimSpace(rest)
		if in == "" {
			m.pushSys("usage: /space push <ffmpeg-input>  (file|url|avfoundation:0:0)")
			return m, nil
		}
		cfg := s.Snapshot().RTMP
		id, err := StartSpaceRTMPPush(in, cfg)
		if err != nil {
			m.pushSys("rtmp push: " + err.Error())
			return m, nil
		}
		s.mu.Lock()
		s.PushID = id
		s.mu.Unlock()
		m.pushSys("space-rtmp push id=" + id + " → " + cfg.BaseRTMPURL() + "/…")
		m.status = "rtmp live"
		return m, nil
	default:
		// bare id/url
		s.SetID(sub)
		m.pushSys("space → " + s.Snapshot().URL)
		return m, nil
	}
}

// handlePluginCmd: /plugin [list|on|off|reload|style] [name]
func (m *Model) handlePluginCmd(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := "list"
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
	}
	name := ""
	if len(fields) > 1 {
		name = fields[1]
	}
	switch sub {
	case "", "list", "ls", "status":
		for _, ln := range strings.Split(strings.TrimRight(Plugins().FormatPluginList(), "\n"), "\n") {
			m.pushSys(ln)
		}
		m.status = "plugins"
		return m, nil
	case "on", "enable":
		if name == "" {
			m.pushSys("usage: /plugin on <name>")
			return m, nil
		}
		if err := Plugins().SetEnabled(name, true); err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		m.pushSys("plugin " + name + " ON")
		m.status = "plugin on"
		return m, nil
	case "off", "disable":
		if name == "" {
			m.pushSys("usage: /plugin off <name>")
			return m, nil
		}
		if err := Plugins().SetEnabled(name, false); err != nil {
			m.pushSys(err.Error())
			return m, nil
		}
		// clear lab plugin style if it was this painter
		if m.lab != nil && strings.EqualFold(m.lab.PluginStyle, name) {
			m.lab.PluginStyle = ""
		}
		m.pushSys("plugin " + name + " OFF")
		m.status = "plugin off"
		return m, nil
	case "reload", "load":
		n, err := Plugins().Reload()
		if err != nil {
			m.pushSys("plugin reload: " + err.Error())
			return m, nil
		}
		m.pushSys(fmt.Sprintf("plugin reload · %d manifest(s) from %s", n, defaultPluginDir()))
		m.status = "plugins reloaded"
		return m, nil
	case "style", "paint":
		// /plugin style <name|off|clear> — set lab PluginStyle
		if m.lab == nil {
			m.lab = newLabState()
		}
		m.lab.On = true
		if name == "" || name == "off" || name == "clear" || name == "none" {
			m.lab.PluginStyle = ""
			m.pushSys("plugin style cleared · built-in " + m.lab.Style.String())
			m.status = "style " + m.lab.Style.String()
			return m, nil
		}
		if Plugins().FindStyle(name) == nil {
			// still allow setting if plugin exists but disabled
			if p := Plugins().Get(name); p == nil || p.Style() == nil {
				m.pushSys("no style plugin " + name + " · " + strings.Join(Plugins().StyleNames(), " "))
				return m, nil
			}
			_ = Plugins().SetEnabled(name, true)
		}
		m.lab.PluginStyle = strings.ToLower(name)
		// also stamp active feed
		if af := m.lab.ActiveFeed(); af != nil {
			af.PluginStyle = m.lab.PluginStyle
		}
		m.pushSys("plugin style → " + m.lab.PluginStyle)
		m.status = "style plugin:" + m.lab.PluginStyle
		return m, nil
	default:
		// bare name → toggle
		if p := Plugins().Get(sub); p != nil {
			on := !p.Enabled()
			p.SetEnabled(on)
			m.pushSys(fmt.Sprintf("plugin %s %s", sub, map[bool]string{true: "ON", false: "OFF"}[on]))
			return m, nil
		}
		m.pushSys("usage: /plugin list|on|off|reload|style <name>")
		return m, nil
	}
}

// handleOverlayCmd: /overlay [auto|off|caption|fx|prompt] [hint…]
// /grok-cap [hint] · /grok-fx [hint]
func (m *Model) handleOverlayCmd(cmd, arg string) (tea.Model, tea.Cmd) {
	if m.overlay == nil {
		m.overlay = newGrokOverlayState()
	}
	mode := OverlayCaption
	switch cmd {
	case "grok-fx", "grokfx":
		mode = OverlayEffect
	case "overlay", "grok-cap", "grokcap":
		// parse first token as mode if present
		fields := strings.Fields(arg)
		if len(fields) > 0 {
			switch strings.ToLower(fields[0]) {
			case "auto", "on":
				m.overlay.Auto = true
				m.overlay.Mode = OverlayCaption
				m.pushSys("overlay auto ON · caption on live gyst (throttled 8s)")
				return m, nil
			case "off", "stop":
				m.overlay.Auto = false
				m.pushSys("overlay auto OFF")
				return m, nil
			case "fx", "effect", "effects":
				mode = OverlayEffect
				arg = strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
			case "prompt", "ask", "jam":
				mode = OverlayPrompt
				arg = strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
			case "caption", "cap":
				mode = OverlayCaption
				arg = strings.TrimSpace(strings.TrimPrefix(arg, fields[0]))
			}
		}
	}
	m.overlay.Mode = mode
	hint := strings.TrimSpace(arg)
	peer, kind := "local", "glyph"
	w, h := m.glyphN, m.glyphN
	if m.frame != nil {
		w, h = m.frame.W, m.frame.H
		if m.frame.Source != "" {
			peer = m.frame.Source
		}
	}
	user := BuildOverlayUserPrompt(mode, hint, peer, kind, w, h)
	cfg := m.grokCfg
	m.overlay.MarkBusy(true)
	m.grokThinking = true
	m.status = "overlay…"
	m.pushSys(fmt.Sprintf("✦ overlay %s…", mode))
	return m, func() tea.Msg {
		reply, err := AskGrokOverlay(cfg, mode, user)
		if err != nil {
			return grokReplyMsg{Err: err.Error(), Overlay: true, Mode: mode}
		}
		return grokReplyMsg{Text: reply, Overlay: true, Mode: mode}
	}
}

// applyGrokOverlayReply routes caption/effect to program or soft mesh caption.
func (m *Model) applyGrokOverlayReply(text string, mode OverlayMode) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		if m.overlay != nil {
			m.overlay.MarkBusy(false)
		}
		return m, nil
	}
	if m.overlay != nil {
		m.overlay.Record(text, "grok")
	}
	m.chat = append(m.chat, chatLine{From: "grok", Text: text})
	m.trimChat()
	switch mode {
	case OverlayEffect:
		// soft caption + status — no PGM authority
		m.status = "fx · " + truncate(text, 40)
		m.pushSys("✦ fx " + truncate(text, 56))
		if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "caption", "from": m.nick, "text": text,
				"source": "grok-fx", "t": time.Now().UnixMilli(),
			})
		}
	case OverlayPrompt:
		m.pushSys("✦ " + truncate(text, 70))
		// try pattern extract same as normal grok
		if pat := extractPattern(text); pat != "" {
			return m.evalLive(pat, true)
		}
	default: // caption
		cap := OverlayReplyToCaption(text, "grok")
		// conductor → program bus; else soft mesh caption
		if m.conductor {
			m.program.SetCaptionRich(cap, m.nick)
			m.publishProgramBus()
			m.pushSys("◈ caption (grok) → ANC · " + FormatCaptionLine(cap))
		} else if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "caption", "from": m.nick, "text": cap.Text,
				"source": "grok-overlay", "t": time.Now().UnixMilli(),
			})
			m.pushSys("◈ soft caption · " + truncate(cap.Text, 50))
		} else {
			m.pushSys("◈ " + truncate(cap.Text, 60))
		}
	}
	return m, nil
}

// feedOrchestrateContext builds Grok take context from lab/news/watch/media.
func (m *Model) feedOrchestrateContext(hint string) FeedOrchestrateContext {
	ctx := FeedOrchestrateContext{
		Mode:     m.promptMode.String(),
		Style:    m.pixelMode.String(),
		GlyphN:   m.glyphN,
		GlyphAsp: m.glyphAspect.String(),
		Media:    FormatMediaHealthChrome(Media().Health()),
		Hint:     hint,
		Live:     Media().Health().Alive > 0 || m.vpipe != nil,
	}
	if m.lab != nil && m.lab.On {
		ctx.Mode = "lab"
		ctx.Style = m.lab.Style.String()
		if af := m.lab.ActiveFeed(); af != nil {
			ctx.Active = af.Label
			ctx.Kind = af.Kind
			if af.TileStyle > 0 || af.Kind == "news" {
				ctx.Style = af.TileStyle.String()
			}
		}
		if m.lab.News != nil && m.lab.News.On {
			ctx.Mode = "news"
			ctx.NewsCount = len(m.lab.News.Sources)
			ctx.Live = true
		}
	}
	if m.burstMode {
		ctx.Mode = "burst"
	}
	if m.vpipe != nil && m.watchPath != "" {
		ctx.Kind = "watch"
		if ctx.Active == "" {
			ctx.Active = m.watchPath
		}
	}
	return ctx
}

// startVisionTake runs focus-feed vision take (GY_VISION budgets · backpressure).
func (m *Model) startVisionTake(hint string) (tea.Model, tea.Cmd) {
	v := Vision()
	// /vision on|off|status
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "on", "enable", "1":
		v.SetEnabled(true)
		m.pushSys("vision ON · " + FormatVisionDoctor(v))
		m.status = "vision on"
		return m, m.visionLoopCmd()
	case "off", "disable", "0":
		v.SetEnabled(false)
		m.pushSys("vision OFF")
		m.status = "vision off"
		return m, nil
	case "status", "doctor", "show", "backbone":
		for _, ln := range strings.Split(strings.TrimRight(FormatVisionBackboneDoctor(v), "\n"), "\n") {
			m.pushSys(ln)
		}
		m.status = "vision"
		return m, nil
	case "":
		// one-shot take
	}
	// offline provider works without API key
	prov := v.Registry().PrimaryTakeProvider()
	if prov == nil || !prov.Available() {
		m.pushSys("vision: no provider available · set XAI_API_KEY or GY_VISION_OFFLINE=1")
		return m, nil
	}
	frame, label, _ := FocusFrameFromModel(m)
	if frame == nil {
		m.pushSys("vision: no focus frame · open lab/news/watch first")
		for _, ln := range strings.Split(strings.TrimRight(FormatVisionBackboneDoctor(v), "\n"), "\n") {
			m.pushSys(ln)
		}
		return m, nil
	}
	m.grokThinking = true
	m.status = "vision…"
	m.pushSys("✦ vision " + truncate(label, 28) + " · " + prov.Name())
	hintCopy := hint
	return m, func() tea.Msg {
		res, err := RunVisionPipeline(m, hintCopy)
		if err != nil {
			return grokReplyMsg{Err: err.Error(), Orchestrate: true, Vision: true}
		}
		// mesh emit backbone event
		if m.client != nil {
			ev := VisionEvent{
				Type: "vision-take", At: time.Now(),
				Feed: res.Frame.Feed, Kind: res.Frame.Kind, Provider: res.Provider,
				Take: res.Take, Theme: res.Take.Theme, MuteHint: res.Take.MuteHint,
				Caption: res.Take.Caption, Style: res.Take.Style,
				LatencyMs: res.Latency.Milliseconds(), JPEGBytes: res.Frame.JPEGBytes,
				Depth: res.Depth,
			}
			_ = m.client.SendJSON(ev.MeshJSON(m.nick))
		}
		return grokReplyMsg{Text: res.Take.Raw, Orchestrate: true, Vision: true, Feed: res.Frame.Feed}
	}
}

// visionLoopCmd schedules next auto vision take when GY_VISION enabled.
func (m *Model) visionLoopCmd() tea.Cmd {
	v := Vision()
	if !v.Enabled() {
		return nil
	}
	iv := v.Config().Interval
	if iv < time.Second {
		iv = 8 * time.Second
	}
	return tea.Tick(iv, func(t time.Time) tea.Msg {
		return visionTickMsg{At: t}
	})
}

type visionTickMsg struct{ At time.Time }

// startGrokOrchestrate asks Grok for a structured take and applies it.
func (m *Model) startGrokOrchestrate(hint string) (tea.Model, tea.Cmd) {
	if !m.grokCfg.Available() {
		m.pushSys("orch: set XAI_API_KEY or grok backend")
		return m, nil
	}
	if !MediaHealthyEnough() && Media().Health().Alive == 0 && m.vpipe == nil {
		// still allow pattern-only, but note
		m.pushSys("orch · no live media — style/pattern only")
	}
	ctx := m.feedOrchestrateContext(hint)
	cfg := m.grokCfg
	m.grokThinking = true
	m.status = "orch…"
	m.pushSys("✦ orchestrate " + ctx.Mode + " · " + truncate(ctx.Active, 24))
	return m, func() tea.Msg {
		take, err := AskGrokOrchestrate(cfg, ctx)
		if err != nil {
			return grokReplyMsg{Err: err.Error(), Orchestrate: true}
		}
		// pass raw so apply path re-parses consistently
		return grokReplyMsg{Text: take.Raw, Orchestrate: true}
	}
}

// note: MetricIncr("orch_takes") applied in applyGrokTake

// applyGrokTake applies STYLE/CAPTION/PATTERN/GLYPH/DEPTH/EFFECT/THEME/MUTE_HINT.
func (m *Model) applyGrokTake(take GrokTake) (tea.Model, tea.Cmd) {
	m.grokThinking = false
	if m.overlay != nil {
		m.overlay.MarkBusy(false)
	}
	if take.Raw != "" {
		from := "grok"
		if take.Vision {
			from = "vision"
		}
		m.chat = append(m.chat, chatLine{From: from, Text: truncate(take.Raw, 200)})
		m.trimChat()
	}
	var applied []string
	var cmd tea.Cmd

	if take.Theme != "" {
		feed := ""
		if af := m.lab; af != nil && af.On {
			if a := af.ActiveFeed(); a != nil {
				feed = a.Label
				// stamp plugin-style theme as lab plugin style name optional
			}
		}
		Vision().mu.Lock()
		Vision().lastTheme = take.Theme
		if feed != "" {
			Vision().themes[feed] = take.Theme
		}
		Vision().mu.Unlock()
		applied = append(applied, "theme="+take.Theme)
		// mesh for Live News browser cluster + chat-bridge
		if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "news-caption", "from": m.nick,
				"text": take.Caption, "theme": take.Theme,
				"feed": feed, "source": "vision",
				"t": time.Now().UnixMilli(),
			})
		}
	}

	if take.MuteHint != "" && take.MuteHint != "none" {
		applied = append(applied, "mute="+take.MuteHint)
		switch take.MuteHint {
		case "suggest-mute", "quiet":
			// soft: only auto-mute when vision auto loop + Spaces stage has cohosts
			if take.Vision && Vision().Enabled() && take.MuteHint == "suggest-mute" {
				// host-confirm style: push sys, don't force mute-all
				m.pushSys("vision mute_hint=suggest-mute · /space mute all to apply")
			}
			if take.MuteHint == "quiet" {
				m.status = "vision quiet"
			}
		case "talking":
			m.status = "vision talking"
		}
	}

	if take.Style != "" {
		if st, ok := ParsePixelStyleName(take.Style); ok {
			m.pixelMode = st
			if m.lab != nil && m.lab.On {
				m.lab.Style = st
				// news wall: paint all tiles with take style (unified wall look)
				if m.lab.News != nil && m.lab.News.On {
					for i := range m.lab.Feeds {
						if m.lab.Feeds[i].Kind == "news" {
							m.lab.Feeds[i].TileStyle = st
						}
					}
				}
			}
			applied = append(applied, "style="+st.String())
		}
	}

	if take.Glyph != "" {
		g := strings.ToLower(strings.TrimSpace(take.Glyph))
		switch g {
		case "square":
			m.glyphAspect = GlyphAspectSquare
			applied = append(applied, "glyph=square")
		case "phone-v", "phone", "vertical", "portrait":
			m.glyphAspect = GlyphAspectPhoneV
			if m.glyphN != GlyphPhone4a && m.glyphN != GlyphPhone3 {
				m.glyphN = GlyphPhone3
			}
			applied = append(applied, "glyph=phone-v")
		case "13":
			m.glyphN = GlyphPhone4a
			applied = append(applied, "glyph=13")
		case "25":
			m.glyphN = GlyphPhone3
			applied = append(applied, "glyph=25")
		case "37":
			m.glyphN = GlyphRes37
			m.glyphAspect = GlyphAspectSquare
			applied = append(applied, "glyph=37")
		case "49":
			m.glyphN = GlyphRes49
			m.glyphAspect = GlyphAspectSquare
			applied = append(applied, "glyph=49")
		}
	}

	if take.Depth != "" && m.depth != nil {
		switch strings.ToLower(strings.TrimSpace(take.Depth)) {
		case "off", "none":
			m.depth.SetMode(DepthOff)
			applied = append(applied, "depth=off")
		case "zip-lite", "ziplite", "lite":
			m.depth.SetMode(DepthZipLite)
			applied = append(applied, "depth=zip-lite")
		case "zipdepth", "zip":
			m.depth.SetMode(DepthZipDepth)
			applied = append(applied, "depth=zipdepth")
		case "gsplat", "splat":
			m.depth.SetMode(DepthGsplat)
			applied = append(applied, "depth=gsplat")
		}
	}

	if take.Caption != "" {
		src := "grok-orch"
		if take.Vision {
			src = "vision"
		}
		cap := OverlayReplyToCaption(take.Caption, src)
		if m.conductor {
			m.program.SetCaptionRich(cap, m.nick)
			m.publishProgramBus()
			applied = append(applied, "caption=pgm")
		} else if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "caption", "from": m.nick, "text": cap.Text,
				"source": src, "theme": take.Theme,
				"t": time.Now().UnixMilli(),
			})
			applied = append(applied, "caption=soft")
		} else {
			applied = append(applied, "caption")
		}
		if m.overlay != nil {
			m.overlay.Record(cap.Text, src)
		}
		// optional vision → overlay always when GY_VISION_OVERLAY
		if take.Vision && Vision().Config().Overlay && m.overlay != nil {
			applied = append(applied, "overlay")
		}
		m.pushSys("◈ " + truncate(cap.Text, 60))
	}

	if take.Effect != "" {
		m.pushSys("✦ fx " + truncate(take.Effect, 56))
		if m.client != nil {
			_ = m.client.SendJSON(map[string]any{
				"type": "caption", "from": m.nick, "text": take.Effect,
				"source": "grok-fx", "t": time.Now().UnixMilli(),
			})
		}
		applied = append(applied, "fx")
	}

	if take.Note != "" {
		m.pushSys("✦ note " + truncate(take.Note, 60))
	}

	if take.Pattern != "" {
		applied = append(applied, "pattern")
		m.status = take.TakeSummary()
		m.pushSys("✦ orch " + strings.Join(applied, " · "))
		return m.evalLive(take.Pattern, true)
	}

	if len(applied) > 0 {
		MetricIncr("orch_takes")
		m.status = take.TakeSummary()
		m.pushSys("✦ orch " + strings.Join(applied, " · "))
	} else {
		m.pushSys("✦ orch (no applicable lines)")
		m.status = "orch empty"
	}
	return m, cmd
}

// maybeAutoOverlay triggers throttled Grok caption on live gyst frames.
func (m *Model) maybeAutoOverlay(from, kind string, w, h int) tea.Cmd {
	if m.overlay == nil || !m.overlay.CanAuto() {
		return nil
	}
	if !m.grokCfg.Available() {
		return nil
	}
	mode := m.overlay.Mode
	if mode == "" {
		mode = OverlayCaption
	}
	user := BuildOverlayUserPrompt(mode, "", from, kind, w, h)
	cfg := m.grokCfg
	m.overlay.MarkBusy(true)
	return func() tea.Msg {
		reply, err := AskGrokOverlay(cfg, mode, user)
		if err != nil {
			return grokReplyMsg{Err: err.Error(), Overlay: true, Mode: mode}
		}
		return grokReplyMsg{Text: reply, Overlay: true, Mode: mode}
	}
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

// activePixelStyle is lab style when lab is on, else main pixel mode.
func (m *Model) activePixelStyle() PixelMode {
	if m.lab != nil && m.lab.On {
		return m.lab.Style
	}
	return m.pixelMode
}

// styleDecodeWH applies style-aware decode caps for stream/cam under filters.
func (m *Model) styleDecodeWH(baseW, baseH int) (int, int) {
	return StyleDecodeBudget(m.activePixelStyle(), baseW, baseH)
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
