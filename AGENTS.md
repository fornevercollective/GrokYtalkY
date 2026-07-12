# AGENTS.md — GrokYtalkY

Guidance for Grok Build, Cursor, and other agents working in this repo.

## Mission

GrokYtalkY is a **companion dock** for Grok terminal sessions: walkie mesh, Strudel-lite live patterns, ffmpeg terminal video, MIDI (signls/sektron-style), and Grok prompts. It must **not** replace the main Grok CLI — it sits beside it.

## Stack

- Go 1.26+, module `github.com/fornevercollective/grokytalky`
- Charm: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`
- WebSocket mesh hub (`serve`)
- Optional: ffmpeg/ffplay, whisper-cli, xAI API

## Rules

1. Keep default UI **compact companion** (width-clamped lines, alt-screen, cam off).
2. Never let half-block video exceed terminal cell width (use `layout.go` clamp helpers).
3. MIDI virtual port name: **`GrokYtalkY`**.
4. Brand: **GrokYtalkY** · org **fornevercollective** (not personal qbit paths).
5. Prefer one-command run: `go build -o bin/grokytalky . && ./bin/grokytalky`.
6. SpaceX capsule standard: minimal, readable, mission-critical comments (*why* only).

## Commands agents should use

```bash
go test ./...
go build -o bin/grokytalky .
./bin/grokytalky serve    # headless hub
./bin/grokytalky --help
```

## Related templates

- `fornevercollective/grok-repo-template` — Colossus/Dojo assembly line
- `fornevercollective` public tools style — `grok-public-folder`
