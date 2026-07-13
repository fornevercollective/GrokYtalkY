# How much live feed data can you pipe?

GrokYtalkY uses **ffmpeg** (capture / decode / scale) and **ffplay** (audio, optional preview). Capacity depends on **codec**, **resolution**, **FPS**, and **number of feeds** — not a single hard limit.

## Hybrid delivery (recommended)

**Cloudflare** for global web viewers + DDoS/TLS simplicity.  
**Custom webrtc-rs + Tokio SFU** (`sfu/`) for DOJO ownership, private rooms, and custom low-res **glyph/hex** lanes.  
**JAX / FFmpeg** stay **off the hot path** (process → publish ladder).  
**Terminals** stay on the tight **25² / half-block** aesthetic — that *is* the product.

```
                    cameras · gy · browser · Glyph Toy
                                   │
                    ┌──────────────┴──────────────┐
                    │   ingest + process (async)  │
                    │   FFmpeg ladder · JAX depth │
                    └──────────────┬──────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
              ▼                    ▼                    ▼
     ┌─────────────────┐  ┌─────────────────┐  ┌──────────────────┐
     │ GrokYtalkY hub  │  │ DOJO SFU sidecar│  │ Cloudflare Calls │
     │ (hexcast WS)    │  │ webrtc-rs+Tokio │  │ / media SFU      │
     │ burst·glyph·PCM │  │ private rooms   │  │ global viewers   │
     └────────┬────────┘  └────────┬────────┘  └────────┬─────────┘
              │                    │                    │
              ▼                    ▼                    ▼
        gy · Glyph · hex     DOJO peers (WebRTC)   1k+ web watchers
        16–32 interactive     + glyph/hex DataCh    (downsampled lane)
```

| Layer | Role | Concurrency sweet spot |
|-------|------|------------------------|
| **GrokYtalkY hub** (`gy serve`) | Walkie bursts, lab tiles, Glyph ints, hex/binary | **8–32** hot peers / room |
| **DOJO SFU** (`sfu/`) | Private rooms, WebRTC + custom lanes | **~50–200** peers / node |
| **Cloudflare** | Public broadcast, TLS, edge fan-out | **1k+** viewers / room |
| **FFmpeg / JAX** | Transcode, ZipDepth, style — offline/worker | Not per-packet fan-out |

### Lanes (do not push 1080p into ▀)

| Lane | Content | Consumers |
|------|---------|-----------|
| `glyph` | 13² / 25² brightness or RGB LED grid | Nothing Matrix, terminal dual ◎ |
| `hex` | Low-res mosaic / .gyhex packets | Terminal hex style, hexcast |
| `chat` | Space-style text (not media) | terminal · web · CF DO |
| `mid` | ~160–320 wide H.264/VP8 | Compact web tiles |
| `full` | Optional 720p+ for web only | Cloudflare / web player |

### Chat flows (Space / Creator Studio)

Chat is a **separate plane** from media — same hybrid split, tiny payloads.

| Flow | Where | Scale | Notes |
|------|--------|-------|-------|
| **Public Space chat** | CF Workers + **Durable Objects** | **1k+** | Edge WS fanout, roster, host pin, persistence |
| **DOJO jam chat** | `gy serve` hub WS and/or `gy-sfu` | **16–32** | Native terminal; same `{type:chat}` JSON |
| **Glyph ticker** | `glyph` / `hex` lane (optional) | jam | LED captions; not full chat history |
| **Bridge** | worker or sidecar | one-way or moderated | DOJO → CF captions; CF → stage Qs only |

```
  Space viewers (1k+) ──WS──► CF Worker ──► Durable Object room
  DOJO peers (16–32)  ──WS──► gy hub / gy-sfu  (chat · glyph · hex)
  Media               ──► CF Calls / SFU mid|full   (never carries chat)
```

Detail: [`docs/chat.md`](chat.md) · scaffold: [`chat/`](../chat/README.md)

**Jam target:** 16–32 interactive peers on hub/SFU.  
**Broadcast target:** 1k+ via CF, terminals on `glyph`/`hex` downsampled lanes.

Scaffold: [`sfu/`](../sfu/README.md) · [`chat/`](../chat/README.md) · site: [docs.html#streams-scale](https://fornevercollective.github.io/GrokYtalkY/docs.html#streams-scale)

---

## Paths in this app

| Path | Format | Typical use |
|------|--------|-------------|
| Terminal lab tiles | **RGB24** resampled → half-blocks | Local display only |
| Cam snap | JPEG stills @ lab FPS | Cam → tile |
| `/watch` + vpipe | ffmpeg **raw RGB24** pipe + **ffplay** audio | File / URL / yt-dlp |
| Mesh burst | JPEG ~120² + PCM16 16 kHz + `glyph[]` | Short video walkie |
| ZipDepth sidecar | RGB stills POST | Depth map |
| DOJO SFU (sidecar) | WebRTC + DataChannel lanes | Private multi-peer rooms |
| CF (optional) | WebRTC / HLS edge | Public simulcast |

## Order-of-magnitude budgets

### A) Terminal multi-feed lab (RGB tiles, no network encode)

Rough: `feeds × width × height × 3 × fps × 8 / 1e6` Mbps of **CPU memory bandwidth**.

| Scale×H (approx) | FPS | 1 feed | 4 feeds | 6 feeds |
|------------------|-----|--------|---------|---------|
| 64×40 | 12 | ~0.7 Mbps | ~2.9 | **~4.4** |
| 96×60 | 15 | ~2.1 | ~8.4 | ~12.6 |
| 160×96 | 24 | ~8.8 | ~35 | ~53 |
| 320×180 | 30 | ~41 | ~166 | heavy |

**Practical lab default:** scale **64**, fps **12**, ≤**6** feeds → fine on a laptop.

The lab status line shows a live estimate:  
`budget ~X Mbps RGB tiles (WxH@fps ×N) · mesh JPEG ~Y Mbps`

### B) ffmpeg decode → raw pipe (watch)

| Stage | 720p30 | 1080p30 |
|-------|--------|---------|
| yuv420p raw | ~332 Mbps | ~746 Mbps |
| RGB24 raw | ~664 Mbps | ~1.5 Gbps |

So we **never** keep full HD in the TUI — we scale to tile size first:

```bash
ffmpeg -i SRC -vf scale=W:H,format=rgb24 -f rawvideo pipe:1
```

### C) Compressed live (network / disk)

With **libx264** / **h264_videotoolbox** / **libx265** / **libvpx-vp9** / **libsvtav1**:

| Target | Rough bitrate |
|--------|----------------|
| 640×360@30 H.264 | ~0.5–1.5 Mbps |
| 1280×720@30 H.264 | ~2–5 Mbps |
| 1920×1080@30 H.264 | ~5–10 Mbps |
| 720p60 | ~4–8 Mbps |

**LAN multi-viewer rule of thumb:**  
`viewers × stream_bitrate + 20%` under link capacity.

### D) Mesh burst (JPEG + PCM)

| Stream | Rate |
|--------|------|
| JPEG 120×120 @ 6 fps ≈ 10–20 KB | ~0.5–1 Mbps each |
| PCM16 mono 16 kHz | **0.256 Mbps** fixed |
| 6 peers bursting | ~4–6 Mbps + audio |

### E) SFU / CF fan-out

| Path | Rough cost |
|------|------------|
| SFU 1 publisher → N viewers | ~`N × lane_bitrate` egress (server) |
| Glyph lane 25² @ 8 fps | tiny (≪ 0.1 Mbps / peer) |
| Mid 320p H.264 @ 15 | ~0.3–0.8 Mbps / viewer |
| CF edge | billed / free tier; hides TURN pain |

## ffplay / ffmpeg roles

| Tool | Role | Limit notes |
|------|------|-------------|
| **ffmpeg** | cam grab, yt-dlp URL open, scale, RGB pipe | CPU-bound decode; prefer `-hwaccel videotoolbox` on macOS for HD |
| **ffplay** | audio from watch / peer PCM | One player process; overlapping bursts mix in OS mixer |
| **yt-dlp** | resolve site → progressive/HLS URL | Network + CDN; not a bandwidth cap itself |

## Recommended ceilings (GrokYtalkY)

| Use | Feeds | Scale | FPS | Notes |
|-----|-------|-------|-----|-------|
| Lab next to chat | 2–4 | 48–80 | 8–15 | Default sweet spot |
| Max lab | 6 | ≤64 | ≤12 | Raise scale only if CPU idle |
| Single watch | 1 | full term | 12–24 | Auto-scales to terminal |
| Burst mesh | 1 TX | 120² | 4–8 | Keep bursts short |
| DOJO jam (interactive) | 16–32 peers | glyph/hex | 4–12 | Hub + optional SFU |
| Public showcase | 1k+ viewers | mid via CF | 15–30 | Terminals stay glyph/hex |

## Quick fill placeholders

```text
gy lab
1–6     select empty slot
c       drop camera into slot
a       drop sim into slot
/watch URL|file   drop video (yt-dlp auto)
r       clear slot back to placeholder
```

## Headroom checklist

1. Prefer **yuv420 / H.264** on the wire; RGB only after scale for TUI.  
2. Cap **FPS** before **scale** if CPU spikes.  
3. Don’t open 6× 1080p ffplay previews — tiles only.  
4. Mesh: JPEG thumbs + glyph ints, not raw RGB.  
5. SFU: publish **lanes**; never one 1080p into every terminal.  
6. `gy doctor` + lab **budget** line for live estimate.
