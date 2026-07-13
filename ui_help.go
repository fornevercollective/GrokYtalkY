package main

import (
	"fmt"
	"strings"
)

// Multi-page in-app help (? / F1). Tab / shift-tab cycle while open.

const HelpPageCount = 6

// Help page indices.
const (
	HelpPageKeys = iota
	HelpPageStream
	HelpPageForge
	HelpPageVenue
	HelpPageCLI
	HelpPageDocs
)

// HelpPageTitle short tab label for status / header pills.
func HelpPageTitle(page int) string {
	switch page % HelpPageCount {
	case HelpPageKeys:
		return "keys"
	case HelpPageStream:
		return "stream"
	case HelpPageForge:
		return "forge"
	case HelpPageVenue:
		return "venue"
	case HelpPageCLI:
		return "cli"
	case HelpPageDocs:
		return "docs"
	default:
		return "?"
	}
}

func helpPagePills(active int) string {
	var parts []string
	for i := 0; i < HelpPageCount; i++ {
		t := HelpPageTitle(i)
		if i == active%HelpPageCount {
			parts = append(parts, styTitle().Reverse(true).Render(" "+t+" "))
		} else {
			parts = append(parts, styDim().Render(t))
		}
	}
	return strings.Join(parts, styDim().Render(" · "))
}

// helpOverlay multi-page help body (tab while ? is open).
func helpOverlay(width, height, page int) string {
	page = page % HelpPageCount
	if page < 0 {
		page += HelpPageCount
	}
	head := styDim().Render("help ") + helpPagePills(page) +
		styDim().Render("  tab/⇧tab · esc/? close")
	body := helpPageBody(page)
	// fit height: header + body lines
	maxBody := height - 2
	if maxBody < 8 {
		maxBody = 8
	}
	lines := strings.Split(body, "\n")
	if len(lines) > maxBody {
		lines = lines[:maxBody]
		lines[len(lines)-1] = truncate(lines[len(lines)-1], max(10, width-8)) + "…"
	}
	text := styText().Render(strings.Join(lines, "\n"))
	return panel("◈ "+HelpPageTitle(page), head+"\n"+text, width)
}

func helpPageBody(page int) string {
	switch page % HelpPageCount {
	case HelpPageKeys:
		return helpBodyKeys()
	case HelpPageStream:
		return helpBodyStream()
	case HelpPageForge:
		return helpBodyForge()
	case HelpPageVenue:
		return helpBodyVenue()
	case HelpPageCLI:
		return helpBodyCLI()
	case HelpPageDocs:
		return helpBodyDocs()
	default:
		return helpBodyKeys()
	}
}

func helpBodyKeys() string {
	return `keys · navigation
  tab / ⇧tab     cycle tabs
  1–7            chat live grok watch lab burst phone
  V lab · b burst · P phone · g glyph N · G phone-v
  /news wall     AlJazeera·DW·Sky·ABC… GrokGlyph tiles
  media          H health · R restart · K kill · GY_MEDIA_MAX
                 · in help: next/prev help page
                 · in lab: next feed
  enter          send chat · eval live · watch path · grok ask
  space          PTT (chat, empty line) · scrub pause
  ? / F1         this help (multi-page) · esc close
  q / ctrl+c     quit
  F              full ↔ companion dock
  b              dual Glyph burst orb (you | peer) + Spaces stage
  V              video lab on/off
  L              lab layout side|stack|grid|focus
  m              pixel style half·hex·braille·…
  a              +sim feed · c cam · g grok mode
  p              pattern play/stop
  1–7            pattern presets
  [ ]            glyph LED scale · g res ladder (burst)

modes (prompt bar)
  › chat   mesh walkie + slash commands
  ◎ live   Strudel mini-notation
  ✦ grok   xAI / local prompt
  ▶ watch  path or URL → ffmpeg pipe`
}

func helpBodyStream() string {
	return `stream · binary · colossus
  /watch url|file|@u ffmpeg (+ yt-dlp · live first)
  /pip · /popout · O  macOS QuickTime PiP pop-out
  /news [us|eu|weather|all N]  glyph news wall (world catalog)
  site/livenews.html   world news+weather+public · main swap · theme clump
  /media · H           ffmpeg supervisor health
  R restart tile · K kill pipe (lab/news)
  /orch [hint] · *     Grok take: STYLE/CAPTION/PATTERN/GLYPH
  /vision [on|off|status|media]  vision + FFmpeg + Aito sides
  MEDIA restart|kill|spawn|retune|encode|recover  (take lines)
  GY_VISION=1 · MEDIA=1 · AITO_URL · AITO_MOCK  vision budgets
  /plugin list|on|off|reload|style   in-process style + mesh hooks
  /space mute|listeners|key --pull   X Spaces host controls + key pull
  /space offer · guest <nick>        gy as X.com stream asset
  gy stream-x init|start|offer       reusable Media Studio publisher
  gy space · gy space-rtmp           ca.pscp.tv RTMP/RTMPS
  gy doctor reliability · metrics   process health / prom text
  gy doctor plugins · space · vision registry + Spaces + vision bus
  /social @user      social handle · lazy lab fill
  /duplex            open-mic full duplex (RX duck)
  /meshmidi          Strudel/walkie → hub type:midi
  /overlay auto|fx|… Grok caption/effect on streams
  /grok-cap · /grok-fx  overlay prompts
  /lan · /phone      same-WiFi phone cast URL banner
  /vstop             stop video pipe
  /pause · /seek     +10|-30|90 · /rate 1.5
  /rec · /export f   .gyst|.gyhex|.pcap
  /load stream.gyst  binary-level play
  /hexdump           current frame as gyhex line
  /colossus pcap|sim live TUI ingest + hub gyst
  /colossus multi a.pcap b.pcap  → multi + forge
  /colossus stop

  scrub: k/space pause · j/l ±5s · J/L ±30s · 0 start · <> rate

formats
  .gyst   GYST packets (rgb24/pcm16/jpeg/hexlum/meta)
  .gyhex  text hex lines
  .pcap   Wireshark USER0 wrapping GYST
  gy encode clip.mp4 out.gyst
  gy decode out.pcap
  gy stream-pub src   headless gyst → hub

styles
  half hex braille ascii blocks points
  halftone depth gsplat edges poster
  scan dither neon  (heavy styles auto-throttle)`
}

func helpBodyForge() string {
	return `forge · dual Glyph · caps
  /forge a.pcap b…     multi-pcap lab + cgf: marks
  /forge status|stop
  /forge next|prev     dual-left slot step (hold)
  /forge hold|rotate   freeze / resume left rotate

  dual Glyph
    left  ↻ forge slots (local multi-pcap)
    right peer RX (forgeRX + stamped hexlum)
    auto-open dual on forge RX · lattice 4×4 corner

  /conductor claim|release|status
  /take [slot] · /preview [slot]|clear
  /caption text|clear   on-air → ANC 0x05
  /caption lang=en role=lower speaker=x Hello
  /caption en: Hello    rich caption_meta (compat plain UDW)
  gy chat-bridge --program-caption   host chat → bus caption (no take)
  /hold · /black · /program
    program bus → hub type:program (venue on-air)
    ANC: mark·tally·bus·preview·caption (0xC1+JSON when rich)

  caps (join handshake)
    GY_CAP=term-full|term-lean|term-mono|glyph-iot|bridge
    GY_ROLE=term|agent|bridge
    RoomGlyphN soft-fits peer glyph N`
}

func helpBodyVenue() string {
	return `venue · ST 2110 · NDI · 2022-7 · ANC
  gy venue --sink log|ndi|st2110|st2110-30|st2110-40
  gy venue --sink st2110 --profile 2110-20
  gy venue --tp 2110TPN --fps-exact 30000/1001
  gy venue --depth 10 --sampling YCbCr-4:2:2
  gy venue --rtp A --rtp-b B     ST 2022-7 dual-dest tee
  gy venue --audio-rtp …         ST 2110-30 L24
  gy venue --anc-rtp …           ST 2110-40 mark/tally/bus ANC
  gy venue --sink ndi,st2110,log multi-sink

  ANC capture point = program bus
    /take · /hold · /black → DID 0x5F SDID 01 mark · 02 tally · 03 bus
    OnANC on every VenueSink · jsonl sidecar

  doctor
    gy doctor st2110 · sync · cameras

  bridges
    gy sfu-bridge · gy chat-bridge · gy agent

  honest limits
    PTP free-run until facility GM
    2022-7 dual-dest tee (not multi-NIC clone)
    CEA-708 / full VANC = facility`
}

func helpBodyCLI() string {
	return `cli · env · update · install
  gy                 companion dock
  gy burst · gy lab  Glyph dual · multi-feed lab
  gy serve · gy join HOST:PORT
  gy watch file|url|@user|twitch:name
  gy stream-pub sim|cam|- --kind hexlum --room dojo --dual
  gy serve               phone cast URL · GET /api/lan
  gy stream-pub|colossus src
  gy agent · gy venue · gy sfu-bridge · gy chat-bridge
  gy mid-lane --room R --edge URL   edge mid-lane hook
  gy encode · gy decode · gy doctor
  gy update | upgrade [--check]
  gy install | uninstall | clean install
  gy install dependencies [--yes]
  gy version

env
  XAI_API_KEY · GROK_MODEL · GROK_CLI_URL
  GY_CAP · GY_ROLE · GY_ROOM · GY_ROOM_MAX · PREFIX
  GY_EDGE_URL · GY_EDGE_TOKEN
  GY_NO_AUTO_UPDATE=1 · GY_AUTO_UPDATE=0|check
  ZIPDEPTH_URL

flags
  --full · --burst · --lab · --cam · --midi
  --glyph 13|25|37|49 · --glyph-scale 0..8
  --no-update   skip TUI launch auto-update

auto-update
  every TUI launch checks GitHub → install → re-exec
  opt-out: GY_NO_AUTO_UPDATE · --no-update`
}

func helpBodyDocs() string {
	ver := Version
	return fmt.Sprintf(`docs · pages · powerhouse  (gy %s)

  site  https://fornevercollective.github.io/GrokYtalkY/
  docs  …/docs.html · dojo · chat · burst · livenews · grokglyph (PWA)
  hexlum lane  vburst glyph → gyst kind=hexlum (hub promote)
               GrokGlyph cast dual-pubs gyst + vburst
  GrokGlyph    roster search / filters · / key · focus peer
  hub rooms    ?room= · program-per-room · /api/rooms · GY_ROOM_MAX
  mid-lane     gy mid-lane → edge/mid-lane worker (public audience)
               site/mid-lane.html · wrangler dev --port 8788
  sfu          make sfu-media · GY_SFU_TOKEN · GY_SFU_TURN_URLS
               GET :9880/health · /metrics · outbox backpressure
  facility     gy doctor nmos · GY_PTP_LOCKED · GY_NMOS_REGISTRY
  HD ladder    gy mid-lane --whip --playback (CF Calls URLs)


  powerhouse stack (overview · blank · grok-cli · Qbpm)
    docs/powerhouse-stack.md
    integrations/powerhouse-protocol.json
    scripts/powerhouse-up.sh
    blank: STAGEFORGE_GY=1 Launch-StageForge
    grok-cli: scripts/powerhouse-stage.sh
    Qbpm: window.qbpmGy · gy-program-bridge.js

  repo docs/
    streams-capacity.md · st2110-sync-cameras.md
    stream-binary.md · burst.md · chat.md · pages.md

  scaffold  sfu/ · chat/
  module    github.com/fornevercollective/grokytalky
  help      tab pages · ? close`, ver)
}
