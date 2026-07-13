# GrokYtalkY — LLM notes

## One-liner

Terminal companion for Grok: walkie mesh + Strudel patterns + ffmpeg pixel video + MIDI + xAI prompt modes.

## Install

```bash
git clone https://github.com/fornevercollective/grokytalky.git && cd grokytalky && go build -o bin/grokytalky .
```

## First prompt for Grok

> Run GrokYtalkY as a companion dock. Start `./bin/grokytalky serve` if I need a mesh hub. Do not replace my main terminal REPL — keep the UI compact.

## Key entry points

| Path | Role |
|------|------|
| `main.go` | CLI: serve, watch, join, companion TUI |
| `model.go` | Bubble Tea model, modes, Grok dispatch |
| `ui_view.go` / `layout.go` | Companion layout + wrap-safe rendering |
| `midi/` | Buffered MIDI + clock (signls/sektron lineage) |
| `strudel/` | Mini-notation engine + local audio |
| `vpipe.go` | ffmpeg RGB pipe for mp4/mkv/mov |
| `grok.go` | xAI / local backend chat |
| `sfu/` | DOJO WebRTC SFU sidecar (Tokio signaling; optional webrtc-rs) |
| `chat/` | Space-style chat: CF Worker+DO + protocol; DOJO uses hub/SFU |
| `docs/streams-capacity.md` | Hybrid CF + SFU + hub concurrency |
| `docs/chat.md` | Creator Studio / Spaces chat mapping |

## Env

- `XAI_API_KEY` or `GROK_API_KEY`
- `GROK_MODEL` (default `grok-3-mini`)
- `GROK_CLI_URL` (default `http://127.0.0.1:3000`)
