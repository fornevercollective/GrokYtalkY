<img width="568" height="352" alt="Screenshot 2026-07-12 at 12 57 40 pm" src="https://github.com/user-attachments/assets/0cb48ac6-f97c-4c32-8249-c399f995b407" />
<img width="570" height="344" alt="Screenshot 2026-07-12 at 1 05 17 pm" src="https://github.com/user-attachments/assets/4b46a448-7078-4c72-b517-115193150952" />
<img width="570" height="344" alt="Screenshot 2026-07-12 at 1 01 58 pm" src="https://github.com/user-attachments/assets/6e9fd64f-6123-45ee-a383-9cf1a722b127" />
<img width="570" height="344" alt="Screenshot 2026-07-12 at 1 06 12 pm" src="https://github.com/user-attachments/assets/0d0d62e9-2fa5-4f41-8cfa-e34239f547db" />
<img width="568" height="352" alt="Screenshot 2026-07-12 at 12 57 19 pm" src="https://github.com/user-attachments/assets/f1185bc0-e08d-4918-a527-5eaf4917a7e6" />


# GrokYtalkY

**Grok terminal companion** — walkie talk / live Strudel patterns / hex video / MIDI, built with [Charm Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + Lip Gloss (same lineage as [cliamp](https://github.com/bjarneo/cliamp)).

Mesh audio + MIDI handling patterns from [signls](https://github.com/emprcl/signls) / [sektron](https://github.com/emprcl/sektron). Mini-notation inspired by [strudel.cc](https://strudel.cc/) and Qbpm jam bridge.

**Org:** [fornevercollective](https://github.com/fornevercollective)  
**Module:** `github.com/fornevercollective/grokytalky`

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)
[![Pages](https://img.shields.io/badge/Pages-live-00ff66)](https://fornevercollective.github.io/GrokYtalkY/)

**Site:** [fornevercollective.github.io/GrokYtalkY](https://fornevercollective.github.io/GrokYtalkY/) · inspired by [charm.land](https://charm.land)

---

## Quick Start

```bash
# terminal-wide short command (recommended)
git clone https://github.com/fornevercollective/GrokYtalkY.git
cd GrokYtalkY && make install    # → ~/.local/bin/gy

gy                 # companion dock
gy burst           # dual Glyph Matrix walkie (auto-fits 80×24 → 13×13)
gy serve           # mesh hub
gy watch clip.mp4
```

**Burst / Glyph:** dual circular LEDs (Nothing GDK layout). `[` `]` scale · `g` res · `space` PTT.
Site: [burst.html](https://fornevercollective.github.io/GrokYtalkY/burst.html) · [docs](https://fornevercollective.github.io/GrokYtalkY/docs.html#burst).

**Hybrid streams:** Cloudflare for 1k+ web viewers · DOJO SFU sidecar for private rooms + glyph/hex lanes · terminals stay 25²/half-block. See [`docs/streams-capacity.md`](docs/streams-capacity.md) · scaffold [`sfu/`](sfu/README.md).

**Space-style chat:** public 1k+ → CF Workers + Durable Objects (`chat/worker`) · DOJO 16–32 → `gy` hub / SFU `chat`. Same JSON envelope. Demo: [chat.html](https://fornevercollective.github.io/GrokYtalkY/chat.html). See [`docs/chat.md`](docs/chat.md).

```bash
gy serve
cd chat/worker && npm i && npx wrangler dev          # public Space :8787
gy chat-bridge --hosts YOUR_NICK                     # DOJO → Space captions
# open site/chat.html  (Public Space | DOJO hub)

make sfu && ./sfu/target/release/gy-sfu --bind 127.0.0.1:9880
make sfu-media   # webrtc-rs track fan-out + outbound glyph/chat DCs
# open site/dojo.html ×2 tabs → Join · glyph pulse · chat (e2e)
```

### Install (pick one)

| Method | Command | Binary on PATH |
|--------|---------|----------------|
| **Make** (user) | `make install` | `gy` → `~/.local/bin` |
| **System-wide** | `make install-system` | `/usr/local/bin/gy` + checks ffmpeg/yt-dlp |
| **Launch** | `make launch` | new Terminal window running `gy` |
| **Go** | `go install github.com/fornevercollective/grokytalky@latest` | `grokytalky` only |
| **Homebrew** (checkout) | `brew install --build-from-source ./Formula/grokytalky.rb` | `gy` + `grokytalky` |

**Streams:** `/watch` and `gy watch <url>` auto-resolve with **yt-dlp** (YouTube/Twitch/X/…) or pass raw `m3u8`/`rtsp`/files to ffmpeg.

**Depth / gsplat:** live mono depth (`d` cycles) — zip-lite offline, [ZipDepth](https://zipdepth.github.io) sidecar (`:8766` from aito-mac), or gsplat-style stack (aito / overview). See `docs/depth-gsplat.md`.

**Drag & drop:** drop image/video files onto the Terminal window (`gy` / `gy lab`) or the site feed wall — paths auto-open as watch tiles or stills.

**Scrub:** while a video is open — `k`/`space` pause, `j`/`l` ±5s, `J`/`L` ±30s, `0` restart, `<>` rate, `/seek`, `/rate`.

**Binary streams:** encode/decode RGB/PCM at packet level — `.gyst` / `.gyhex` / `.pcap` (see `docs/stream-binary.md`).

```bash
gy encode clip.mp4 out.gyst
gy decode out.pcap
gy watch out.gyst          # play packets; j/k scrub
# TUI: /rec → /export out.gyhex · /load out.gyst · /hexdump

# live headless (DOJO/Colossus) → hub → peers (no file)
gy serve
gy stream-pub sim --kind hexlum --hex 25 --nick colossus
gy colossus examples/dojo.pcap        # pcap loop (ts pace, default loop)
# other terminal: gy  (renders type:gyst frames)
# multi-pcap + Cursor-Grok Forge watermarks (lab):
#   /forge examples/dojo.pcap examples/dojo.pcap
```

**Video lab:** multi-feed wall next to chat with listed **FPS / scale / style / layout** controls:

```bash
gy lab                 # or V inside companion
# [ ] scale · , . fps · m style · L layout
# a +sim · c +cam · tab next feed · o toggle lists
# styles: half hex braille ascii blocks points halftone depth gsplat
# layouts: side | stack | grid | focus
```

```bash
gy watch 'https://www.youtube.com/watch?v=…'
gy watch 'https://cdn.example.com/live.m3u8'
# in TUI: paste URL + Enter (watch mode) or /watch URL
gy doctor   # ffmpeg · ffplay · yt-dlp
```

```bash
# ensure user bins are on PATH (zsh)
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc

# Go install path (if you used go install)
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc
```

**Not uv** — this is a Go binary (uv is for Python). Use `go install` / `make install` / Homebrew instead.

### Version & updates

```bash
gy --version          # one line
gy version            # commit, build date, install channel, binary path
gy update --check     # compare to GitHub latest (exit 2 if outdated)
gy update             # install latest via same channel (go / brew / local)
```

Builds embed version via ldflags (`make install` uses `git describe`).

**Grok prompt:** *"Clone fornevercollective/GrokYtalkY, run make install, then gy as a companion dock."*

---

## Companion, not takeover

Default UI is a **small Charm dock** (alt-screen, width-clamped) meant to sit beside Grok / Cursor / Dojo work — not replace them.

| Mode (`tab`) | Enter does |
|--------------|------------|
| `chat` | Mesh walkie chat + SPACE = PTT |
| `live` | Strudel mini-notation `s("bd*4")` |
| `grok` | ✦ Grok (xAI API or local backend) |
| `watch` | ffmpeg → terminal half-block video |

```bash
./bin/grokytalky              # companion dock
./bin/grokytalky burst        # Siri-sized video walkie orb (Glyph Matrix)
./bin/grokytalky --full       # larger layout
./bin/grokytalky serve        # Colossus/server: hub only, no TUI
./bin/grokytalky join host:9876
```

**Video burst:** hold **space** in burst mode for short face+voice PTT. Frames ship as JPEG + 25×25 glyph grid for [Nothing Glyph Matrix](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit). Web orb: `site/burst.html`. Android toy scaffold: `glyph/`.

---

## Stack

| Layer | Tech |
|-------|------|
| TUI | Bubble Tea v2, Lip Gloss v2 |
| Mesh | WebSocket hub (`serve`) |
| Patterns | Strudel-lite mini-notation + local synth |
| MIDI | Buffered outs + virtual port `GrokYtalkY` (signls/sektron-style) |
| Video | ffmpeg raw RGB24 → truecolor `▀` (clamped) |
| Audio | afplay/ffplay; ffplay for file watch |
| Grok | `XAI_API_KEY` → api.x.ai · or `GROK_CLI_URL` backend |

**Optional runtime:** `ffmpeg`, `ffplay`, `whisper-cli` (PTT translate), softsynth on MIDI port `GrokYtalkY`.

---

## Environment

```bash
# .env.example
export XAI_API_KEY=xai-...          # or GROK_API_KEY
export GROK_MODEL=grok-3-mini
export GROK_CLI_URL=http://127.0.0.1:3000   # local notes backend
export GROK_OFFLINE=0
```

Keys also load from `~/.grok/env` if present.

---

## Keys

| Key | Action |
|-----|--------|
| `tab` | Cycle chat · live · grok · watch |
| `enter` | Send (mode-dependent) |
| `g` | Grok mode |
| `p` | Pattern play/stop |
| `c` | Camera strip |
| `/watch path` | ffmpeg pixel video |
| `F` | Full ↔ companion |
| `?` | Help |
| `q` | Quit |

---

## Layout

```
GrokYtalkY/
├── site/                  # GitHub Pages (charm.land-style landing)
│   ├── index.html
│   └── styles.css
├── main.go model.go …     # companion TUI + hub
├── midi/                  # signls/sektron-style MIDI + clock
├── strudel/               # mini-notation engine + audio/MIDI sinks
├── scripts/install.sh
├── configs/ docs/ examples/
├── .github/workflows/     # ci.yml + pages.yml
└── AGENTS.md LLMS.md
```

---

## Related

- [grok-repo-template](https://github.com/fornevercollective/grok-repo-template) — Colossus/Dojo assembly line
- [cliamp](https://github.com/bjarneo/cliamp) — Charm music player reference
- [signls](https://github.com/emprcl/signls) / [sektron](https://github.com/emprcl/sektron) — MIDI sequencers
- [strudel.cc](https://strudel.cc/) — live coding patterns

---

## License

Apache-2.0 — see [LICENSE](LICENSE).
