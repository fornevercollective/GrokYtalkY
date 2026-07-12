# How much live feed data can you pipe?

GrokYtalkY uses **ffmpeg** (capture / decode / scale) and **ffplay** (audio, optional preview). Capacity depends on **codec**, **resolution**, **FPS**, and **number of feeds** — not a single hard limit.

## Paths in this app

| Path | Format | Typical use |
|------|--------|-------------|
| Terminal lab tiles | **RGB24** resampled → half-blocks | Local display only |
| Cam snap | JPEG stills @ lab FPS | Cam → tile |
| `/watch` + vpipe | ffmpeg **raw RGB24** pipe + **ffplay** audio | File / URL / yt-dlp |
| Mesh burst | JPEG ~120² + PCM16 16 kHz | Short video walkie |
| ZipDepth sidecar | RGB stills POST | Depth map |

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

With **libx264** / **h264_videotoolbox** / **libx265** / **libvpx-vp9** / **libsvtav1** (present on this machine):

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
4. Mesh: JPEG thumbs, not raw RGB.  
5. `gy doctor` + lab **budget** line for live estimate.
