# Vision-first backbone + FFmpeg + Aito + closed-loop retarget (v1.72)

**Vision first. Full media control + Aito sides + SAM→crop retarget.**  
Structured take path drives the orchestrator, **FFmpeg control plane**, **Aito** SAM / pose / gsplat / depth, and **closed-loop retarget** (bbox → crop+retune focus encode).

## Pipeline

```
capture → encode (JPEG budget) → infer (provider) → parse take
       → apply (STYLE/CAPTION/… + MEDIA→FFmpeg) → emit
```

| Stage | Role |
|-------|------|
| capture | focus feed (lab active → main → burst) |
| encode | `GY_VISION_MAX_W/H` · JPEG q (vision API budget) |
| infer | pluggable **VisionProvider** |
| apply | `applyGrokTake` + **`ApplyVisionMediaControl`** |
| emit | plugin **VisionHook** + mesh `type:vision-take` |

Backpressure: **max inflight 1**, min **interval**, drops counted.  
Media ops: **`GY_VISION_MEDIA_MAX`** ops/minute.

## FFmpeg control plane

Vision is a first-class client of `Media()`:

| MEDIA op | FFmpeg action |
|----------|----------------|
| `restart` / `recover` | `RestartNewsTile` / `VideoPipe.Restart` via supervisor Kill+spawn |
| `kill` | `Media().Kill` / tile `Stop` (focus\|all\|news\|watch\|label) |
| `retune` | `RetuneNewsTile` — new `scale=W:H,fps=N` rawvideo pipe |
| `spawn` | resolve catalog/URL → `StartNewsTile` (budgeted) |
| `encode` | frame JPEG dump **or** one-shot `ffmpeg` under `MediaKindEncode` |
| `retarget` | **SAM bbox → ffmpeg crop+scale** on focus news tile (closed loop) |

```
MEDIA restart focus
MEDIA recover all
MEDIA kill news
MEDIA retune focus 96x54@5
MEDIA retune focus scale=128x72 fps=6
MEDIA retarget focus crop=0.2,0.1,0.5,0.7
MEDIA spawn aje
MEDIA encode focus jpeg
MEDIA encode jpeg /tmp/snap.jpg
```

Auto (when `GY_VISION_MEDIA_AUTO=1`): unhealthy focus news/watch after a vision take → `MEDIA recover focus`.

### Closed-loop retarget (SAM → crop)

```
capture → encode → infer take → SAM /segment
       → select person/face bbox (pad + 16:9 fit)
       → MEDIA retarget crop=x,y,w,h
       → apply: RetuneNewsTile with crop=…,scale=…,fps
       → focus encode now follows subject
```

| Env | Default | Role |
|-----|---------|------|
| `GY_VISION_RETARGET` | on | auto attach MEDIA retarget from SAM |
| `GY_VISION_RETARGET_PAD` | 0.08 | expand bbox |
| `GY_VISION_RETARGET_MIN` | 0.45 | min segment score |
| `GY_VISION_RETARGET_LABEL` | person,face,human,… | preferred labels |
| `GY_VISION_RETARGET_IOU` | 0.82 | skip if crop ≈ current |

FFmpeg filter shape: `crop=floor(iw*W/2)*2:…,scale=W:H,fps=N,format=rgb24`.

## Providers (backbone)

| Name | Kind | When |
|------|------|------|
| `grok` | take | `XAI_API_KEY` · multimodal |
| `offline` | take | no key / `GY_VISION_OFFLINE=1` · deterministic |
| `aito-sam` | segment | `POST /segment` · SAM regions (aito-living-canvas) |
| `aito-pose` | pose | `POST /pose` · MediaPipe joints/hands (aito-mac) |
| `aito-gsplat` | depth | `POST /gsplat` or `/booth` · gsplat stack |
| `aito-depth` | depth | `POST /depth` · ZipDepth RGB protocol |
| `depth-proxy` | depth | local gsplat-style hint (always, fallback) |

```bash
export GY_VISION_PROVIDER=grok   # or offline
export GY_VISION_AITO_URL=http://127.0.0.1:8766
export GY_VISION_AITO_MOCK=1     # local geometry mocks (no sidecar)
export GY_VISION_MEDIA=1         # FFmpeg control plane (default on)
export GY_VISION_MEDIA_MAX=4     # ops per minute
export GY_VISION_MEDIA_AUTO=1    # auto-recover dead focus
```

Side channels run **after** the primary take (best-effort). They enrich empty MUTE_HINT / DEPTH / CAPTION and appear on mesh `vision-take` (`segments`, `pose_hands`, `depth.backend`).

## Event stream (plugins)

```go
Vision().Registry().Subscribe(func(ev VisionEvent) { ... })
// or Plugin implementing VisionHook() VisionHook
```

Mesh: `type:vision-take` · `theme` · `caption` · `style` · `mute_hint` · `depth` · **`segment_top`** · **`pose_hands`** / `pose_joints`

### Live News browser (`site/livenews.html`)

Connect hub → consumes the same mesh:

| Field | UI |
|-------|-----|
| `theme` | theme cluster / sort |
| `caption` | tile chyron |
| `segment_top` | **SAM · label** badge on tile |
| `pose` / `pose_hands` | **pose ✋N** badge (hot when hands &gt; 0) |
| chips | vision bar under main column (pin on click) |

```bash
export GY_VISION=1 GY_VISION_AITO_MOCK=1
gy serve   # other terminal: gy lab · /news · /vision
# browser: livenews.html → Connect hub  (or Demo vision without hub)
```

### Builtin `theme-vision` VisionPlugin

| Piece | Role |
|-------|------|
| `VisionHook` | on `vision-take` stores THEME / feed / caption |
| `StylePainter` | `theme-vision` grades RGB by theme (scan/green/neon…) |
| Auto style | sets lab `PluginStyle=theme-vision` (`GY_VISION_THEME_STYLE`) |
| Auto pixel | maps theme → PixelMode when take has no STYLE (`GY_VISION_THEME_PIXEL`) |

```bash
/plugin list          # ✓ theme-vision  style vision
/plugin style theme-vision
/plugin off theme-vision
```

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
| `GY_VISION_MEDIA` | on | FFmpeg control plane |
| `GY_VISION_MEDIA_MAX` | 4 | media ops / minute |
| `GY_VISION_MEDIA_AUTO` | on | auto recover dead focus |
| `GY_VISION_AITO_URL` | `http://127.0.0.1:8766` | Aito sidecar base |
| `GY_VISION_AITO_MOCK` | off | mock SAM/pose/gsplat/depth |
| `GY_VISION_AITO_SEGMENT` | `/segment` | path override |
| `GY_VISION_AITO_POSE` | `/pose` | path override |
| `GY_VISION_AITO_GSPLAT` | `/gsplat` | path override (`/booth` fallback) |
| `GY_VISION_AITO_DEPTH` | `/depth` | ZipDepth path |
| `GY_VISION_NO_SAM` / `NO_POSE` / `NO_GSPLAT` | off | disable side providers |

Requires `XAI_API_KEY` for live grok (or `GY_VISION_OFFLINE=1`).

### Aito HTTP contract (sidecar)

```
GET  /health          → 2xx
POST /segment         JSON { image: dataURL } → { segments: [{id,label,score,bbox:[x,y,w,h]}] }
POST /pose            JSON { image } → { joints: {name:[x,y,c]}, hands: N }
POST /gsplat|/booth   JSON { image, mode } → { backend, mean, preview[] }
POST /depth           binary u32le w|h + RGB888 → { backend, mean, depth[], w, h }
```

Heavy ML stays in Aito. gy only POSTs focus frames and applies results.

## CLI / TUI

```bash
export XAI_API_KEY=…
export GY_VISION=1
gy lab   # or gy with /news wall

# in TUI:
/vision              # one-shot take on focus feed
/vision on           # enable auto loop
/vision off
/vision status       # backbone + media plane doctor
/vision media        # FFmpeg control plane only
/vision media-on|off
gy doctor vision
```

## Take lines (vision)

```
STYLE neon
CAPTION Markets board behind desk
THEME markets
MUTE_HINT quiet
MEDIA restart focus
```

## Aito relationship

| Aito surface | Role |
|--------------|------|
| `aito` | Grok vision for **photo LUTs/masks** (browser) |
| `aito-living-canvas` | **SAM** segmentation |
| `aito-mac` | **MediaPipe IK** + gsplat booth + zipdepth sidecar |

GrokYtalkY does **not** embed SAM/MediaPipe/gsplat booth. Those stay in Aito.  
Here: **lean xAI vision → orch take → stage apply + FFmpeg control plane**, supervisor-safe.

Plugins can hook `Vision().Snapshot()` / `VisionMedia().Snapshot()` / mesh `news-caption` theme as the event stream.
