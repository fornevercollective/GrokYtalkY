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
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	hwmidi "github.com/fornevercollective/grokytalky/midi"
	"github.com/fornevercollective/grokytalky/strudel"
)

const version = "1.7.0-dock"

// Model is the Bubble Tea v2 app state (cliamp-style Elm architecture).
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
}

func NewModel(opts Options) *Model {
	m := &Model{
		nick:      opts.Nick,
		host:      opts.Host,
		width:     80,
		height:    24,
		status:    "starting…",
		bands:     make([]float64, 32),
		pixelMode:  PixelHalf,
		videoOn:    opts.Cam || opts.Full, // companion: video off until c or /watch
		camOn:      opts.Cam,
		compact:    !opts.Full,
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
			{Sys: true, Text: fmt.Sprintf("GrokYtalkY %s · companion dock (Grok-side)", version)},
			{Sys: true, Text: "tab modes · g grok · /watch mp4 · c cam · F full · ? help"},
		},
	}
	if opts.Live {
		m.promptMode = ModeLive
		m.liveMode = true
	}

	// MIDI first so live sink can use it
	var mid hwmidi.Midi
	var dev int
	if opts.MIDI {
		mid = hwmidi.NewOptional()
		dev = hwmidi.FindDevice(mid.DeviceNames(), opts.MIDIDev)
		m.midiBridge = hwmidi.NewBridge(mid, dev)
		names := mid.DeviceNames()
		if len(names) > 0 {
			m.pushSys(fmt.Sprintf("midi: %s — signls/sektron + strudel hits", names[dev]))
		} else {
			m.pushSys("midi: no devices")
			m.midiOn = false
		}
	}

	// Live engine (Strudel REPL-like)
	// IMPORTANT: MIDI virtual port alone is silent — always attach local audio synth.
	var sinks []strudel.Sink
	audio := strudel.NewAudioSink()
	if audio.Enabled() {
		sinks = append(sinks, audio)
		m.pushSys("audio: local synth (afplay/ffplay) — you should hear bd/sd/hh")
	} else {
		m.pushSys("audio: NONE — install afplay or ffplay")
	}
	if mid != nil {
		ms := strudel.NewMIDISink(mid, dev)
		ms.OnHit(func(ev strudel.Event) {
			if m.prog != nil {
				m.prog.Send(liveHitMsg{Ev: ev})
			}
		})
		sinks = append(sinks, ms)
		m.pushSys("midi out also on (needs a softsynth on GrokYtalkY port for MIDI audio)")
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

	if opts.Translate {
		if m.xl8.Enabled {
			m.pushSys("translate: whisper on PTT release")
		} else {
			m.xl8On = false
		}
	}
	if m.liveMode {
		m.pushSys("mode live · Enter evaluates mini-notation")
	}
	if m.grokCfg.APIKey != "" {
		m.pushSys("grok: " + m.grokCfg.ModeLabel() + " ready · tab → grok")
	} else {
		m.pushSys("grok: set XAI_API_KEY or start backend at " + m.grokCfg.BackendURL)
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
		// debounce thrash: only reflow when size actually changes
		nw, nh := msg.Width, msg.Height
		if nw < 40 {
			nw = 40
		}
		if nh < 10 {
			nh = 10
		}
		if nw == m.layoutW && nh == m.layoutH {
			return m, nil
		}
		m.width, m.height = nw, nh
		m.layoutW, m.layoutH = nw, nh
		// drop oversized frame so next paint resamples to new cols
		if m.frame != nil && m.frame.W > m.videoCols()+8 {
			m.frame = nil
		}
		return m, nil

	case tickMsg:
		m.spin++
		var cmds []tea.Cmd
		// pull ffmpeg video pipe frames (~12 fps)
		if m.vpipe != nil && m.vpipe.Running() {
			if rgb, w, h, seq, ok := m.vpipe.Snapshot(); ok && seq != m.vpipeSeq {
				m.vpipeSeq = seq
				m.frame = RGBToFramePixels(rgb, w, h, m.watchPath)
				m.frameMeta = fmt.Sprintf("file %dx%d", w, h)
				m.pixelMode = PixelHalf
				m.videoOn = true
			}
		} else if m.camOn && (m.vpipe == nil || !m.vpipe.Running()) {
			// camera only when not watching a file/stream
			m.camTick++
			if m.camTick%8 == 0 { // ~2.5 fps
				cmds = append(cmds, m.captureCamCmd())
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
		m.status = string(msg)
		m.connected = strings.Contains(string(msg), "connected")
		if m.connected {
			m.pushSys("connected as " + m.nick + " → " + m.host)
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
		if m.midiOn && m.midiBridge != nil {
			m.midiBridge.Frame()
		}
		return m, nil

	case camSnapMsg:
		if m.client != nil && len(msg) > 0 {
			m.client.SendFrame("term:"+m.nick, 320, 200, msg)
			// local preview
			return m, decodeFrameCmd(msg, "local", m.videoCols(), m.videoPxH())
		}
		return m, nil

	case autoWatchMsg:
		return m.startWatch(msg.src, true)

	case liveHitMsg:
		m.liveHit = fmt.Sprintf("%s %s", msg.Ev.Kind, msg.Ev.Sound)
		m.liveCycle = msg.Cycle
		// flash spectrum on hits
		m.level = 0.7
		m.peak = PeakHold(m.peak, 0.7, 0.8, 0.2)
		return m, nil

	case liveCycleMsg:
		m.liveCycle = msg.Cycle
		m.status = fmt.Sprintf("live cyc=%d cps=%.2f", msg.Cycle, msg.CPS)
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

	case tea.KeyPressMsg:
		return m.handleKey(msg)
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
		m.promptMode = (m.promptMode + 1) % ModeCount
		m.liveMode = m.promptMode == ModeLive
		m.pushSys("mode " + m.promptMode.String())
		return m, nil
	case "shift+tab":
		m.promptMode = (m.promptMode + ModeCount - 1) % ModeCount
		m.liveMode = m.promptMode == ModeLive
		m.pushSys("mode " + m.promptMode.String())
		return m, nil
	}

	if m.input == "" {
		switch k {
		case " ":
			if m.promptMode == ModeChat {
				return m.togglePTT()
			}
			m.input += " "
			return m, nil
		case "c":
			if m.vpipe != nil {
				m.vpipe.Stop()
				m.vpipe = nil
				m.watchPath = ""
			}
			m.camOn = !m.camOn
			if m.camOn {
				m.pixelMode = PixelHalf
				m.videoOn = true
				m.pushSys("cam on · Half truecolor")
			} else {
				m.pushSys("cam off")
			}
			return m, nil
		case "v":
			m.videoOn = !m.videoOn
			return m, nil
		case "m":
			m.pixelMode = (m.pixelMode + 1) % PixelCount
			m.pushSys("pixel: " + m.pixelMode.String())
			return m, nil
		case "t":
			m.xl8On = !m.xl8On
			if m.xl8On && !m.xl8.Enabled {
				m.xl8 = defaultTranslateConfig()
				m.xl8On = m.xl8.Enabled
			}
			m.pushSys(fmt.Sprintf("translate %v", m.xl8On))
			return m, nil
		case "i":
			m.midiOn = !m.midiOn
			if m.midiOn && m.midiBridge == nil {
				mid := hwmidi.NewOptional()
				m.midiBridge = hwmidi.NewBridge(mid, 0)
			}
			m.pushSys(fmt.Sprintf("midi %v", m.midiOn))
			return m, nil
		case "l":
			m.promptMode = ModeLive
			m.liveMode = true
			m.pushSys("mode live")
			return m, nil
		case "g":
			m.promptMode = ModeGrok
			m.pushSys("mode grok · " + m.grokCfg.ModeLabel())
			return m, nil
		case "p":
			_ = m.toggleLive()
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7":
			idx := int(k[0] - '1')
			if idx >= 0 && idx < len(m.presets) {
				return m.evalLive(m.presets[idx], true)
			}
			return m, nil
		case "?", "f1":
			m.showHelp = !m.showHelp
			return m, nil
		case "f", "F":
			m.compact = !m.compact
			if m.compact {
				m.pushSys("companion dock")
			} else {
				m.pushSys("full layout")
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
	if isVideoPath(line) || looksLikeVideoArg(line) {
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
	case "watch", "vplay", "movie", "video":
		src := arg
		if src == "" {
			m.pushSys("usage: /watch file.mp4|mkv|mov  or  /watch https://…")
			return m, nil
		}
		return m.startWatch(src, true)
	case "vstop", "watchstop":
		m.stopWatch()
		m.pushSys("■ video pipe stopped")
		return m, nil
	case "vpipe", "vinfo":
		if m.vpipe != nil && m.vpipe.Running() {
			m.pushSys(fmt.Sprintf("vpipe %s %dx%d audio=%v", m.watchPath, m.vpipe.W, m.vpipe.H, m.vpipe.HasAudio))
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

func (m *Model) startWatch(src string, withAudio bool) (tea.Model, tea.Cmd) {
	src = strings.Trim(src, `"'`)
	// stop camera while watching file
	m.camOn = false
	if m.vpipe != nil {
		m.vpipe.Stop()
		m.vpipe = nil
	}
	w := m.videoCols()
	h := m.videoPxH()
	vp, err := OpenVideoPipe(src, w, h, withAudio)
	if err != nil {
		m.pushSys("watch: " + err.Error())
		return m, nil
	}
	m.vpipe = vp
	m.watchPath = src
	m.videoOn = true
	m.pixelMode = PixelHalf
	m.vpipeSeq = 0
	base := filepath.Base(src)
	m.pushSys(fmt.Sprintf("▶ ffmpeg pipe %s → %dx%d half-blocks + audio", base, w, h))
	m.pushSys("/vstop to stop · m cycles pixel mode")
	return m, nil
}

func (m *Model) stopWatch() {
	if m.vpipe != nil {
		m.vpipe.Stop()
		m.vpipe = nil
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
	if isURL(line) {
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
	m.pushSys("◎ eval " + truncate(strings.ReplaceAll(code, "\n", " "), 60))
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
			m.pushSys("▶ live")
		}
	}
	return m, nil
}

func (m *Model) toggleLive() tea.Cmd {
	// run sync so model state is immediate; return nil cmd
	if m.live == nil {
		m.pushSys("no live engine")
		return nil
	}
	if m.live.Playing() {
		m.live.Stop()
		m.pushSys("■ stop")
		return nil
	}
	if m.live.Code() == "" {
		_ = m.live.Eval(m.liveCode)
	}
	m.live.Play()
	m.pushSys("▶ play")
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
		m.pushSys("not connected")
		return m, nil
	}
	prog := m.prog
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
		m.pushSys("mic/ffmpeg: " + err.Error())
		return m, nil
	}
	m.ptt = sess
	m.talking = true
	m.client.SendPTT(true)
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(true, LevelToVelocity(0.5))
	}
	m.pushSys("🎤 PTT")
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
	m.talking = false
	m.level = 0
	if m.client != nil {
		m.client.SendPTT(false)
	}
	if m.midiOn && m.midiBridge != nil {
		m.midiBridge.PTT(false, LevelToVelocity(m.peak))
	}
	m.pushSys("⏹ clear")

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
		m.pushSys(fmt.Sprintf("hub hello id=%v", msg["id"]))
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
			m.pushSys(n + " joined")
		}
	case "leave":
		if n, _ := msg["nick"].(string); n != "" {
			m.pushSys(n + " left")
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
			m.pushSys("🎤 " + from + " TX")
		} else {
			m.remoteTX = ""
			m.pushSys("⏹ " + from + " clear")
		}
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

func (m *Model) captureCamCmd() tea.Cmd {
	return func() tea.Msg {
		path := os.TempDir() + "/grokytalky-cam.jpg"
		// Higher res + correct AVFoundation pixel format (uyvy422/nv12).
		// q:v 5 ≈ readable faces in truecolor half-blocks.
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
				"-vf", "scale=480:300:flags=bicubic",
				"-q:v", "5",
				path,
			}
		} else {
			args = []string{
				"-hide_banner", "-loglevel", "error", "-y",
				"-f", "v4l2", "-i", "/dev/video0",
				"-frames:v", "1",
				"-vf", "scale=480:300:flags=bicubic",
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
				"-frames:v", "1", "-vf", "scale=320:200", "-q:v", "8", path,
			}
			if runtime.GOOS != "darwin" {
				args2 = []string{
					"-hide_banner", "-loglevel", "error", "-y",
					"-f", "v4l2", "-i", "/dev/video0",
					"-frames:v", "1", "-vf", "scale=320:200", "-q:v", "8", path,
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
	// companion: small fixed-ish width to avoid wrap
	if m.compact {
		return max(24, min(56, m.width-2))
	}
	return max(32, min(72, m.width-4))
}

func (m *Model) videoPxH() int {
	// companion: few half-block rows only
	if m.compact {
		return 12 // 6 visual rows
	}
	rows := max(6, min(14, m.height/4))
	return rows * 2
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.renderCharm())
	// v2: alt screen on the View (prevents scrollback spool)
	v.AltScreen = true
	return v
}
