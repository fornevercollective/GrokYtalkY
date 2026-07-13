# Video burst walkie (Siri-sized + Glyph Matrix)

Short **face + voice** transmissions — same mental model as PTT audio walkie, but each burst also ships a tiny video face (and a **Glyph Matrix** brightness grid for Nothing Phone).

Inspired by:

- GrokYtalkY walkie PTT (hold / release)
- Siri’s compact orb popup (not a full-screen call UI)
- [Nothing Glyph Matrix Developer Kit](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit)
  — circular LED allocation matching [25111_spec](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit/blob/main/image/25111_spec.svg)
  / [23112_spec](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit/blob/main/image/23112_spec.svg)
  (13×13 / 25×25 hardware; terminal can scale cells/LED and raise resolution to 37/49)

## Run

```bash
# Siri-sized terminal orb
./bin/grokytalky burst

# from companion dock: press `b`
./bin/grokytalky

# browser orb
# serve site/ then open burst.html — Connect + hold
python3 -m http.server 8765 -d site

# hub only (phones/peers join)
./bin/grokytalky serve
```

| Client | Gesture |
|--------|---------|
| Terminal orb | **Space** hold = TX burst |
| Web orb | Press-and-hold the circle |
| Glyph Toy | Glyph Button **down/up** |

## Wire protocol

| type | role |
|------|------|
| `vburst-start` | peer began TX |
| `vburst-frame` | JPEG thumb + optional `glyph: int[N²]` (0–255) |
| `audio` | existing PCM16 chunks (16 kHz mono) |
| `vburst-end` | peer released |
| `ptt` | also sent for RX indicator parity |

Example frame:

```json
{
  "type": "vburst-frame",
  "from": "qbit",
  "b64": "<jpeg base64>",
  "w": 120, "h": 120,
  "glyph": [0, 12, 40, ...],
  "glyphN": 25,
  "t": 1710000000000
}
```

Hub broadcasts to all peers except sender (same as chat/audio).

## Glyph Matrix

Layout follows the official GDK circular LED disk (not a filled square):

| Mode | Matrix N | Active LEDs | Role |
|------|----------|-------------|------|
| Phone (4a) Pro | 13 | **137** | hardware `DEVICE_25111p` |
| Phone (3) | 25 | **489** | hardware `DEVICE_23112` |
| Terminal hi-res | 37 / 49 | circular denser | display only |

**Scale / resolution increase** (terminal):

| Control | Action |
|---------|--------|
| `]` / `[` | LED pitch scale ×1…×8 (cells per LED; gaps like SVG fill ~0.73) |
| `g` | cycle matrix res 13 → 25 → 37 → 49 |
| `--glyph N` | preferred resolution |
| `--glyph-scale S` | start scale (`0` = auto-fit terminal) |

**Window fit (full circles, never half-clipped):**

| Terminal | Dual fit |
|----------|----------|
| **80×24** (default) | auto **13×13** full disks (25 does not fit) |
| ~54×31+ | full **25×25** dual |
| larger | scale-up and/or 37/49 |

Prefer 25 on a small window still **displays** the largest full dual that fits (usually 13); mesh still ships device N.

Mesh / Android always receive **device N** (25 or 13) brightness `int[]`, even when the terminal is showing a fitted size.

Android toy scaffold: [`glyph/`](../glyph/README.md)  
Use `GlyphMatrixManager.setMatrixFrame(int[])` with the `glyph` array directly — no need to decode JPEG on device for the rear display.

## Why “Siri-sized”

Full video calls are heavy and socially loud. Bursts are:

1. **Bounded** — hold to talk, release to stop  
2. **Small UI** — orb / matrix / 11-line terminal popup  
3. **Face-readable at 25×25** — enough for expression, not surveillance stream  

Same mesh as walkie chat; optional whisper translate still hooks PTT release on the audio path.

## Live News wall

Separate page: [`site/livenews.html`](../site/livenews.html) — **global** news, weather, and
public-access / PEG / government streams as **25×25 Glyph Matrix** tiles.

| Feature | Detail |
|---------|--------|
| Catalog | `site/news-sources.js` (world regions + weather + public) |
| Sections | Collapsible rows · **↻ refresh** each · **→ main** swaps section into mosaic |
| Main column | Speakers-style Glyph grid · pin / shuffle / cycle / fill from sort |
| Theme AI | Captions/transcripts → `news-theme.js` clusters (breaking, politics, markets, weather…) |
| Hub | `news-caption` · `news-transcript` · `vburst-frame` |

Demo: **Demo themes** injects sample captions → sort **by theme** → **Cluster now**.

## X Spaces stage (burst page)

[`site/burst.html`](../site/burst.html) — collapsible sections (GrokGlyph-style):

| Slot | Count | UI |
|------|-------|-----|
| Host | 1 | **video circle** (face + ring + level) |
| Co-hosts | **2** | **video circles** |
| Speakers | **10** | **25×25 Glyph Matrix** tiles |
| Listeners | N (grid) | **GrokGlyph-style join grid** · Stress +12 |

Collapsible: You · Space · Hosts · Speakers · Listeners · Chat · RTMP.

Also on the page:

- **Space URL** — default [1AJEmmANrPeJL](https://x.com/i/spaces/1AJEmmANrPeJL?s=20)
- **Chat details** — stage Q / pins / system placeholders
- **Captions** — lower-third bar + mesh `space-caption`
- **RTMP / RTMPS** Media Studio ingest (Canada):
  - `rtmps://ca.pscp.tv:443/x`
  - `rtmp://ca.pscp.tv:80/x`
  - **Stream key** field: *available when ready* (paste from studio.x.com → Sources → RTMP)

### Mesh types

| type | role |
|------|------|
| `space-roster` | host / cohosts[] / speakers[] / listeners / caption |
| `space-level` | per-seat audio level for waveforms |
| `space-chat` | Space chat line |
| `space-caption` | lower-third |

### CLI

```bash
gy space                          # status + RTMP readiness
gy space id https://x.com/i/spaces/1AJEmmANrPeJL
gy space key "$STREAM_KEY"        # when Media Studio is ready
gy space rtmps                    # or: gy space rtmp
gy space seat host qbit
gy space seat cohost:0 alice
gy space seat speaker:3 bob
gy space caption "GrokYtalkY live"
gy space-rtmp --key "$KEY" --in clip.mp4
gy space-rtmp --rtmp --key "$KEY" --in "avfoundation:0:0"   # mac cam+mic
# env: GY_X_STREAM_KEY · GY_X_RTMP_URL
```

TUI: `/space` · `/space key …` · `/space push <input>` · `/space seat …`
