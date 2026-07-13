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

`gy serve` (and companion hub) bind `0.0.0.0` by default and print a **phone cast** URL + **quick QR**:

```text
same Wi‑Fi · phone → terminal
  phone cast  http://192.168.x.x:9876/phone.html
  quick QR    http://192.168.x.x:9876/api/lan/qr
  scan tip    open QR on laptop · phone scans → Quick connect
  mesh WS     ws://192.168.x.x:9876/?role=phone&nick=phone
  discover    UDP …:9877  (probe GYWHO1)
```

### Quick connect (v1.79+)

| Device | Action |
|--------|--------|
| Laptop | `gy serve` · open `/qr.html` or `/api/lan/qr` (or `/phone.html` shows QR on desktop) |
| Phone | Scan QR → page opens → tap **⚡ Quick connect** (hub + camera) → hold **Cast** |
| Phone (deep link) | `phone.html?quick=1` auto-runs hub + camera |
| Terminal | `gy` · `/lan` / `/phone` / `/qr` reprints banner |
| Nothing Glyph | Intro → **Discover on Wi‑Fi** → hold Glyph Button |

APIs:

- `GET /api/lan` → `{ws, phone, qr, ips, …}`
- `GET /api/lan/qr` → **HTML** scan page (QR drawn client-side via vendored MIT `site/qrcode-generator.js`)
- `GET /api/lan/qr?format=png` → PNG only if system **`qrencode`** is installed (optional)
- `GET /qr.html` → static scan UI (same client encoder)
- UDP `GYWHO1` → `GYHUB1`+JSON

**No Go QR dependency** (platform bar: no stale `skip2/go-qrcode`). Phone TX uses `vburst-frame` + `gyst` hexlum.
