# Vision bus (v1.68) — focus-feed Grok takes

**Vision first.** Structured take path fits the existing orchestrator and media supervisor — no new mesh primitives beyond `THEME` / `MUTE_HINT` lines and `news-caption` theme stamps.

## What it does

1. Picks **one focus frame** (lab active tile → main watch frame → burst local)
2. Downsamples to **JPEG budget** (`GY_VISION_MAX_W/H`)
3. Calls **xAI vision** (`grok-2-vision-latest` by default)
4. Parses `STYLE` / `CAPTION` / `THEME` / `MUTE_HINT` / …
5. Applies via **`applyGrokTake`** (style wall, captions, optional overlay)
6. Stamps theme for news clustering; mesh `news-caption` for Live News browser

Backpressure: **max inflight 1**, min **interval**, drops counted when saturated.

## Env

| Variable | Default | Role |
|----------|---------|------|
| `GY_VISION` / `GY_VISION_ON` | off | enable auto loop on TUI start |
| `GY_VISION_INTERVAL_MS` | 8000 | min ms between takes |
| `GY_VISION_MAX_W` / `MAX_H` | 320 / 180 | JPEG decode budget |
| `GY_VISION_JPEG_Q` | 72 | JPEG quality |
| `GY_VISION_MODEL` | `grok-2-vision-latest` | multimodal model |
| `GY_VISION_OVERLAY` | off | prefer overlay record path |
| `GY_VISION_MAX_INFLIGHT` | 1 | concurrent takes |

Requires `XAI_API_KEY` (multimodal). Text-only backend is not enough.

## CLI / TUI

```bash
export XAI_API_KEY=…
export GY_VISION=1
gy lab   # or gy with /news wall

# in TUI:
/vision              # one-shot take on focus feed
/vision on           # enable auto loop
/vision off
/vision status
gy doctor vision
```

## Take lines (vision)

```
STYLE neon
CAPTION Markets board behind desk
THEME markets
MUTE_HINT quiet
```

## Aito relationship

| Aito surface | Role |
|--------------|------|
| `aito` | Grok vision for **photo LUTs/masks** (browser) |
| `aito-living-canvas` | **SAM** segmentation |
| `aito-mac` | **MediaPipe IK** + gsplat booth + zipdepth sidecar |

GrokYtalkY does **not** embed SAM/MediaPipe/gsplat booth. Those stay in Aito.  
Here: **lean xAI vision → orch take → stage apply**, supervisor-safe.

Plugins can later hook `Vision().Snapshot()` / mesh `news-caption` theme as the event stream.
