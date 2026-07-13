// GrokYtalkY — terminal walkie-talkie on the Charm stack (cliamp lineage)
// with MIDI audio handling from emprcl/signls + emprcl/sektron, and
// live speech translation via whisper-cli.
//
//	Bubble Tea v2 + Lip Gloss v2
//	MIDI: buffered outs + virtual "GrokYtalkY" port + clock (signls/sektron)
//	XL8:  whisper-cli -tr on PTT release
//
// Usage:
//
//	grokytalky
//	grokytalky --midi --translate
//	grokytalky join host:9876
//	grokytalky hub
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("grokytalky", flag.ContinueOnError)
	port := fs.Int("port", 9876, "hub / connect port")
	bind := fs.String("bind", "0.0.0.0", "hub bind address")
	host := fs.String("host", "", "remote hub host:port (join mode)")
	nick := fs.String("nick", "", "display name")
	midi := fs.Bool("midi", true, "enable signls/sektron-style MIDI out")
	midiDev := fs.String("midi-device", "", "MIDI out name substring (default: GrokYtalkY virtual)")
	translate := fs.Bool("translate", true, "live STT/translate on PTT via whisper-cli")
	lang := fs.String("lang", "auto", "source language for whisper")
	noTr := fs.Bool("no-tr", false, "disable whisper -tr (transcribe only, no EN translate)")
	model := fs.String("model", "", "whisper ggml model path")
	live := fs.Bool("live", false, "start in Strudel live-coding mode")
	full := fs.Bool("full", false, "full dashboard (default is compact companion dock)")
	cam := fs.Bool("cam", false, "enable camera on start")
	burst := fs.Bool("burst", false, "Siri-sized video burst orb (Glyph Matrix walkie)")
	lab := fs.Bool("lab", false, "multi-feed video lab (FPS/scale/style + chat)")
	glyphN := fs.Int("glyph", 25, "Glyph Matrix side (13|25 hardware, 37|49 terminal hi-res)")
	glyphScale := fs.Int("glyph-scale", 0, "cells per LED pitch (0=auto, 1–8 scale-up)")
	noAudio := fs.Bool("no-audio", false, "disable local pattern synth")
	noUpdate := fs.Bool("no-update", false, "skip TUI launch auto-update")
	_ = noAudio
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printHelp() }

	cmd := "term"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	// early commands / flags — before flag.Parse (unknown sub-flags like --check)
	switch cmd {
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "version", "ver", "-v", "-V", "--version":
		if cmd == "version" || cmd == "ver" {
			fmt.Print(versionDetail())
		} else {
			fmt.Println(versionLine())
		}
		return nil
	case "stream-pub", "publish", "stream-live", "colossus":
		// own flags (--kind, --hex, …) — before main flag.Parse
		return runStreamPubCmd(args)
	case "chat-bridge", "caption-bridge", "bridge-chat":
		return runChatBridgeCmd(args)
	case "sfu-bridge", "glyph-bridge", "bridge-sfu":
		return runSfuBridgeCmd(args)
	case "agent", "glyph-agent", "iot":
		return runGlyphAgentCmd(args)
	case "venue", "venue-adapter", "sink":
		return runVenueCmd(args)
	case "update", "upgrade":
		checkOnly := false
		for _, a := range args {
			switch a {
			case "--check", "-c":
				checkOnly = true
			case "-h", "--help":
				fmt.Println(`gy update [--check]
  --check, -c   report only; exit 2 if a newer release exists
  (default)     install latest via go install / brew / make channel

TUI launches auto-update by default (check GitHub → install → re-exec).
  GY_NO_AUTO_UPDATE=1   disable
  GY_AUTO_UPDATE=0       disable
  GY_AUTO_UPDATE=check   notify only, no install`)
				return nil
			}
		}
		err := runUpdate(checkOnly)
		if err == errUpdateAvailable {
			os.Exit(2)
		}
		return err
	}
	for _, a := range args {
		switch a {
		case "-h", "--help":
			printHelp()
			return nil
		case "-v", "-V", "--version":
			fmt.Println(versionLine())
			return nil
		}
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *noUpdate {
		_ = os.Setenv("GY_NO_AUTO_UPDATE", "1")
	}
	if *nick == "" {
		*nick = defaultNick()
	}

	xl8 := defaultTranslateConfig()
	if *model != "" {
		xl8.Model = *model
		xl8.Enabled = xl8.Model != "" && xl8.Bin != ""
	}
	xl8.Lang = *lang
	xl8.ToEN = !*noTr

	switch cmd {
	case "doctor":
		// gy doctor [st2110|cameras|sync]
		sub := ""
		if fs.NArg() > 0 {
			sub = strings.ToLower(fs.Arg(0))
		} else if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			// when doctor is the cmd, remaining may be in args before parse
			for _, a := range args {
				if !strings.HasPrefix(a, "-") {
					sub = strings.ToLower(a)
					break
				}
			}
		}
		switch sub {
		case "st2110", "2110", "smpte":
			fmt.Print(FormatST2110SuiteTable())
			fmt.Println()
			fmt.Print(FormatPTPDependencyBasis())
			fmt.Println()
			fmt.Print(FormatST20227Basis())
			fmt.Println()
			fmt.Print(FormatSyncClockReport(DefaultSyncClockReport()))
			return nil
		case "cameras", "camera", "tether":
			fmt.Print(FormatCameraTetherMatrix())
			return nil
		case "sync", "ptp":
			fmt.Print(FormatPTPDependencyBasis())
			fmt.Println()
			fmt.Print(FormatSyncClockReport(DefaultSyncClockReport()))
			return nil
		}
		fmt.Print(StreamDoctor())
		fmt.Println(DepthDoctorLine())
		fmt.Println(DepthModesList())
		fmt.Printf("gy binary: %s\n", versionLine())
		cap := DetectCapProfile(80, 24)
		fmt.Println(cap.SummaryLine())
		fmt.Println("doctor st2110 · doctor cameras · doctor sync")
		if p, err := os.Executable(); err == nil {
			fmt.Printf("path: %s\n", p)
		}
		return nil
	case "encode":
		// gy encode in.mp4 out.gyst|gyhex|pcap
		return runEncodeCLI(fs.Args())
	case "decode":
		// gy decode stream.gyst  (prints info; play via gy watch)
		return runDecodeCLI(fs.Args())
	case "hub", "receive", "serve":
		// server-level: headless mesh only — no TUI takeover
		return runHubOnly(*bind, *port)
	case "burst", "glyph", "orb":
		// Siri-sized popup: hold space = short video+audio walkie burst
		h := *host
		if h == "" {
			h = fmt.Sprintf("127.0.0.1:%d", *port)
		}
		return runTUI(Options{
			Nick: *nick, Host: h, MIDI: *midi, MIDIDev: *midiDev,
			Translate: *translate, XL8: xl8,
			Burst: true, Cam: true, GlyphN: *glyphN, GlyphScale: *glyphScale,
		}, true, *bind, *port)
	case "lab", "vwall", "feeds":
		h := *host
		if h == "" {
			h = fmt.Sprintf("127.0.0.1:%d", *port)
		}
		return runTUI(Options{
			Nick: *nick, Host: h, MIDI: *midi, MIDIDev: *midiDev,
			Translate: *translate, XL8: xl8,
			Lab: true, Full: true, GlyphN: *glyphN,
		}, true, *bind, *port)
	case "join", "talk":
		h := *host
		if h == "" && fs.NArg() > 0 {
			h = fs.Arg(0)
		}
		if h == "" {
			h = fmt.Sprintf("127.0.0.1:%d", *port)
		}
		if !strings.Contains(h, ":") {
			h = fmt.Sprintf("%s:%d", h, *port)
		}
		return runTUI(Options{
			Nick: *nick, Host: h, MIDI: *midi, MIDIDev: *midiDev,
			Translate: *translate, XL8: xl8, Live: *live, Full: *full, Cam: *cam,
			Burst: *burst, Lab: *lab, GlyphN: *glyphN, GlyphScale: *glyphScale,
		}, false, *bind, *port)
	case "term", "start", "live", "companion", "":
		h := *host
		if h == "" {
			h = fmt.Sprintf("127.0.0.1:%d", *port)
		}
		return runTUI(Options{
			Nick: *nick, Host: h, MIDI: *midi, MIDIDev: *midiDev,
			Translate: *translate, XL8: xl8,
			Live: *live || cmd == "live", Full: *full || *lab, Cam: *cam,
			Burst: *burst, Lab: *lab, GlyphN: *glyphN, GlyphScale: *glyphScale,
		}, true, *bind, *port)
	case "watch", "vplay":
		// grokytalky watch movie.mp4
		src := *host
		if src == "" && fs.NArg() > 0 {
			src = fs.Arg(0)
		}
		if src == "" && len(args) > 0 {
			// after parse, positional may be in fs.Args()
			for _, a := range fs.Args() {
				if !strings.HasPrefix(a, "-") {
					src = a
					break
				}
			}
		}
		if src == "" {
			return fmt.Errorf("usage: grokytalky watch file.mp4|mkv|mov|url")
		}
		return runWatchTUI(src, *nick, *port, *bind)
	default:
		// bare path: grokytalky ./clip.mp4
		if isVideoPath(cmd) || looksLikeVideoArg(cmd) {
			return runWatchTUI(cmd, *nick, *port, *bind)
		}
		return fmt.Errorf("unknown command %q (try grokytalky help)", cmd)
	}
}

func runWatchTUI(src, nick string, port int, bind string) error {
	if err := maybeAutoUpdateOnLaunch(); err != nil {
		return err
	}
	opts := Options{
		Nick: nick, Host: fmt.Sprintf("127.0.0.1:%d", port),
		MIDI: false, Translate: false, Live: false,
	}
	m := NewModel(opts)
	if os.Getenv("GY_JUST_UPDATED") == "1" {
		m.pushSys(fmt.Sprintf("◈ auto-updated · gy %s", Version))
		_ = os.Unsetenv("GY_JUST_UPDATED")
	}
	m.camOn = false
	m.videoOn = true
	m.compact = true
	prog := tea.NewProgram(m, tea.WithFPS(12))
	m.SetProgram(prog)
	go func() {
		time.Sleep(200 * time.Millisecond)
		prog.Send(autoWatchMsg{src: src})
	}()
	_, err := prog.Run()
	return err
}

func printHelp() {
	// argv0 so `gy --help` and `grokytalky --help` both read naturally
	cmd := "gy"
	if len(os.Args) > 0 {
		base := filepath.Base(os.Args[0])
		if base != "" && base != "." {
			cmd = base
		}
	}
	fmt.Printf(`GrokYtalkY %s — lean Charm companion + video burst

  %s                 companion dock (default)
  %s burst           Nothing Glyph dual circles (you | peer)
  %s lab             multi-feed lab (feeds | chat)
  %s encode in out   binary/hex/pcap encode stream
  %s decode file     inspect .gyst|.gyhex|.pcap
  %s stream-pub src  live headless GYST → hub (sim|file|pcap|cam)
  %s serve           headless hub
  %s chat-bridge     DOJO hub → public Space captions
  %s sfu-bridge      hub vburst|gyst hexlum → SFU glyph/hex
  %s agent           thin Glyph/IoT client (cap handshake, no TUI)
  %s venue           venue adapter stub (program bus + glyph)
  %s version         version + build info
  %s update          check & install latest
  %s update --check  check only (exit 2 if outdated)
  %s --full          larger layout
  %s watch URL|file  ffmpeg or binary stream
  %s doctor          check ffmpeg / yt-dlp / ffplay
  %s join HOST:PORT  remote hub

  tab   chat · live · grok · watch  (lab: next feed)
  V     video lab on/off     [ ] scale   , . fps
  L     layout side|stack|grid|focus
  m     style half·hex·…·depth·gsplat
  a     +sim feed            c  cam / +cam feed
  b     burst orb            F  full/companion
  ?     help                 q  quit

  install:  make install     →  ~/.local/bin/gy
            brew install --build-from-source ./Formula/grokytalky.rb
  burst = short video+audio walkie → Nothing Glyph Matrix dual circles
  flags: --burst --glyph 13|25|37|49 --glyph-scale 0..8 --midi --cam
  keys:  [ ] scale · g res · space PTT  (matches GlyphMatrix-Developer-Kit layout)
  env:   XAI_API_KEY · GROK_MODEL · GROK_CLI_URL · GY_CAP · GY_ROLE
         GY_NO_AUTO_UPDATE=1 · GY_AUTO_UPDATE=0|check  (TUI launch auto-update)
`, Version, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd, cmd)
}

func runHubOnly(bind string, port int) error {
	static := findStatic()
	addr := fmt.Sprintf("%s:%d", bind, port)
	h := NewHub(addr, false, static)
	fmt.Printf("GrokYtalkY hub %s\n  join: gy join 127.0.0.1:%d\n", Version, port)
	return h.ListenAndServe()
}

func runTUI(opts Options, startHub bool, bind string, port int) error {
	// every TUI launch: check GitHub + install + re-exec when newer
	// (GY_NO_AUTO_UPDATE=1 / GY_AUTO_UPDATE=0 to skip; soft-fail offline)
	if err := maybeAutoUpdateOnLaunch(); err != nil {
		return err
	}

	var hub *Hub
	if startHub {
		addr := fmt.Sprintf("%s:%d", bind, port)
		hub = NewHub(addr, true, findStatic())
		go func() { _ = hub.ListenAndServe() }()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModel(opts)
	m.cancel = cancel
	if os.Getenv("GY_JUST_UPDATED") == "1" {
		m.pushSys(fmt.Sprintf("◈ auto-updated · gy %s", Version))
		_ = os.Unsetenv("GY_JUST_UPDATED")
	}

	// Alt screen + FPS cap: stable redraw, no scrollback spool.
	// Lower FPS in companion mode to stay light next to Grok terminal.
	fps := 12
	if opts.Full {
		fps = 20
	}
	prog := tea.NewProgram(m, tea.WithFPS(fps))
	m.SetProgram(prog)
	m.AttachClient(ctx, prog)

	_, err := prog.Run()
	if hub != nil {
		_ = hub.Close()
	}
	return err
}

func defaultNick() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "anon"
	}
	host, _ := os.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		host = host[:i]
	}
	return u.Username + "@" + host
}

func findStatic() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd, filepath.Join(wd, ".."))
	}
	candidates = append(candidates, filepath.Join(os.Getenv("HOME"), "dev/mueee"))
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "walkie.html")); err == nil {
			return c
		}
		if _, err := os.Stat(filepath.Join(c, "hexcast-send.html")); err == nil {
			return c
		}
	}
	return ""
}
