# Vision-first backbone (v1.68–1.69)

**Vision first.** Structured take path fits the existing orchestrator and media supervisor.

## Pipeline

```
capture → encode (JPEG budget) → infer (provider) → parse take → apply → emit
```

| Stage | Role |
|-------|------|
| capture | focus feed (lab active → main → burst) |
| encode | `GY_VISION_MAX_W/H` · JPEG q |
| infer | pluggable **VisionProvider** |
| apply | `applyGrokTake` STYLE/CAPTION/THEME/MUTE_HINT |
| emit | plugin **VisionHook** + mesh `type:vision-take` |

Backpressure: **max inflight 1**, min **interval**, drops counted.

## Providers (backbone)

| Name | Kind | When |
|------|------|------|
| `grok` | take | `XAI_API_KEY` · multimodal |
| `offline` | take | no key / `GY_VISION_OFFLINE=1` · deterministic |
| `aito-depth` | depth | `GY_VISION_AITO_URL` zipdepth sidecar (:8766) |
| `depth-proxy` | depth | local gsplat-style hint (always) |

```bash
export GY_VISION_PROVIDER=grok   # or offline
export GY_VISION_AITO_URL=http://127.0.0.1:8766
```

**Future slots (interfaces ready):** SAM segment, MediaPipe pose/IK, gsplat booth — as Aito sidecars implementing `VisionProvider`, no new stage primitives.

## Event stream (plugins)

```go
Vision().Registry().Subscribe(func(ev VisionEvent) { ... })
// or Plugin implementing VisionHook() VisionHook
```

Mesh: `type:vision-take` · `theme` · `caption` · `style` · `mute_hint` · `depth`

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
