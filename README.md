# GrokYtalkY

**Grok terminal companion** — walkie talk / live Strudel patterns / hex video / MIDI, built with [Charm Bubble Tea v2](https://github.com/charmbracelet/bubbletea) + Lip Gloss (same lineage as [cliamp](https://github.com/bjarneo/cliamp)).

Mesh audio + MIDI handling patterns from [signls](https://github.com/emprcl/signls) / [sektron](https://github.com/emprcl/sektron). Mini-notation inspired by [strudel.cc](https://strudel.cc/) and Qbpm jam bridge.

**Org:** [fornevercollective](https://github.com/fornevercollective)  
**Module:** `github.com/fornevercollective/grokytalky`

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](https://go.dev/)

---

## Quick Start

```bash
git clone https://github.com/fornevercollective/grokytalky.git
cd grokytalky
go build -o bin/grokytalky .
./bin/grokytalky            # compact companion dock
./bin/grokytalky serve      # headless mesh hub (server)
./bin/grokytalky watch clip.mp4
```

**Install:**

```bash
go install github.com/fornevercollective/grokytalky@latest
# or
./scripts/install.sh
```

**Grok prompt:** *"Clone fornevercollective/grokytalky and run as a companion dock next to my terminal."*

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
./bin/grokytalky --full       # larger layout
./bin/grokytalky serve        # Colossus/server: hub only, no TUI
./bin/grokytalky join host:9876
```

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
├── main.go model.go …     # companion TUI + hub
├── midi/                  # signls/sektron-style MIDI + clock
├── strudel/               # mini-notation engine + audio/MIDI sinks
├── scripts/install.sh
├── configs/
├── docs/
├── examples/
├── bin/                   # build output (gitignored binary)
├── AGENTS.md
├── LLMS.md
└── metadata.yaml
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
