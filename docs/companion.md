# Companion mode

GrokYtalkY defaults to a **compact dock** for use next to Grok terminal / Cursor / Dojo sessions.

## Principles

1. **Do not take over** the primary REPL.
2. **Width-clamped** rendering — no half-block wrap spool on resize.
3. **Cam off** until `c` or `/watch`.
4. **Alt-screen** stable redraw (Bubble Tea v2 `View.AltScreen`).

## Server mode

```bash
grokytalky serve --port 9876 --bind 0.0.0.0
```

Headless WebSocket mesh for multi-machine / Colossus-side peers. No TUI.
