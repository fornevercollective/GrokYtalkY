# Companion mode

GrokYtalkY defaults to a **compact dock** for use next to Grok terminal / Cursor / Dojo sessions.

## Principles

1. **Do not take over** the primary REPL.
2. **Width-clamped** rendering — no half-block wrap spool on resize.
3. **Cam off** until `c` or `/watch`.
4. **Alt-screen** stable redraw (Bubble Tea v2 `View.AltScreen`).

## Server mode

```bash
gy serve --port 9876 --bind 0.0.0.0
```

Headless WebSocket mesh for multi-machine / Colossus-side peers. No TUI.

## Same Wi‑Fi · phone → terminal

`gy serve` (and companion hub) bind `0.0.0.0` by default and print a **phone cast** URL:

```text
same Wi‑Fi · phone → terminal
  phone cast  http://192.168.x.x:9876/phone.html
  mesh WS     ws://192.168.x.x:9876/?role=phone&nick=phone
  discover    UDP …:9877  (probe GYWHO1)
```

| Device | Action |
|--------|--------|
| Phone browser | Open `phone.html` → Camera → hold **Cast** |
| Nothing Glyph | Intro → **Discover on Wi‑Fi** → hold Glyph Button |
| Terminal | `gy` or `gy join 192.168.x.x:9876` · `/lan` reprints banner |

APIs: `GET /api/lan` · UDP `GYWHO1` → `GYHUB1`+JSON. Phone TX uses `vburst-frame` + `gyst` hexlum.
