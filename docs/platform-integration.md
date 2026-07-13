# Grok streaming platform integration (v1.76)

**Handoff surface** for FFmpeg operators, Grok multimodal stage control, hybrid web/terminal streaming stacks, and partner demos.

GrokYtalkY is the **terminal + mesh authority**: vision takes drive style, theme, captions, **and** supervised FFmpeg pipelines. Heavy ML stays in **Aito**. Public 1k+ stays on **Cloudflare**. Private jam on **gy-sfu**.

```
 cameras / URL / news
        │
        ▼
   FFmpeg (Media supervisor)  ◄── vision MEDIA · retarget crop
        │
        ▼
   hub mesh ── vision-take · gyst · program · chat
        │
   ┌────┼────────────┬──────────────┐
   ▼    ▼            ▼              ▼
  gy   Live News   gy-sfu        CF mid-lane
 TUI   segment_top  glyph DC      1k+ web
       + pose       + token
```

## Status

| | |
|--|--|
| Contract | [`integrations/grok-stream-platform.json`](../integrations/grok-stream-platform.json) |
| Doctor | `gy doctor platform` · `gy platform` |
| Export | `gy platform export` · `gy platform export -o handoff.json` |
| Related | [`powerhouse-protocol.json`](../integrations/powerhouse-protocol.json) · [`vision.md`](vision.md) · [`streams-capacity.md`](streams-capacity.md) |

## Victory recap (export-ready)

**Pipeline:**  
`capture → encode → infer (Grok/offline + Aito) → apply (+FFmpeg/retarget/theme-vision) → mesh → Live News / glyph-cast / stage`

| Version | Win |
|---------|-----|
| **1.70** | Vision → **FFmpeg control plane** (`MEDIA` restart/kill/spawn/retune/encode/recover) |
| **1.71** | **Aito** SAM / pose / gsplat / real `/depth` (no TensorFlow in gy) |
| **1.72** | Closed-loop **SAM bbox → crop+retune** focus encode |
| **1.73** | **theme-vision** plugin (theme-reactive StylePainter on `vision-take`) |
| **1.74** | **SFU token** + hub↔**glyph DC** bridge (bidi) |
| **1.75** | **Live News** browser consumes `segment_top` + pose |
| **1.75.1** | **Docs** site header matches all pages |
| **1.76** | **Platform readiness** doctor + JSON export + contract |
| **1.77** | **Cast wiring** — Live News → glyph-cast full-res (BroadcastChannel + hub) |
| **1.78** | **Camera / lighting** — phone/film controls (aito-aligned) + mesh `camera-controls` |
| **1.79** | **Phone quick connect** — QR scan page + one-tap hub+camera on `phone.html` |
| **1.79.1** | **QR platform bar** — drop Go QR dep; client MIT encoder + optional system `qrencode` |
| **concept** | **Stadium Glyph** — phone atom → infinite canvas LOD (Sphere / 112k / drones) · [`stadium-glyph.md`](stadium-glyph.md) |
| **1.79.2** | **Sphere Vegas Bloch³ seating** — `site/sphere-seating.js` + phone `?seat=` → cast `pos` |

Three bullets for external recaps:

1. **Vision-first FFmpeg plane** — multimodal takes spawn/restart/retune/encode supervised pipes; SAM bbox closed-loop retarget.  
2. **Aito sides, not TensorFlow** — SAM / pose / gsplat-depth sidecars; theme-vision paints the stage; Live News shows `segment_top` + pose.  
3. **Demo-ready cast + platform handoff** — pin mosaic → **Cast screen** → full LED `glyph-cast`; `gy platform export` for partner stacks.

## Explicit non-goals (honest)

- Full auto-cycling screen cast of **every** Live News feed (main mosaic: pin / shuffle / cycle). Full-res cast: **GrokGlyph / glyph-cast.html**.
- In-process TensorFlow / full 3D Gaussian splat viewer — Aito sidecars only; gsplat is **proxy + booth depth**.
- 1k+ interactive WebRTC on gy-sfu — use Cloudflare mid-lane / Calls.

## Planes

### 1. FFmpeg

- All children via **`Media()`** (`PrepMediaCmd`, budgets `GY_MEDIA_MAX` / `GY_NEWS_MAX`).
- Vision control plane: `GY_VISION_MEDIA=1` · take lines `MEDIA retarget crop=…`.
- Publish: `stream-pub` · `stream-x` · `space-rtmp` · `venue` ST2110/NDI.

### 2. Grok vision

```bash
export XAI_API_KEY=…
export GY_VISION=1
export GY_VISION_MEDIA=1
export GY_VISION_RETARGET=1
export GY_VISION_THEME_STYLE=1
# optional mock sides without Aito box:
export GY_VISION_AITO_MOCK=1
gy serve
gy lab   # /news · /vision
```

Mesh: `type:vision-take` with `theme`, `caption`, `segment_top`, `pose_hands`, `depth`, `media_ops`.

### 3. Aito (optional ML)

| Endpoint | Provider |
|----------|----------|
| `POST /segment` | `aito-sam` |
| `POST /pose` | `aito-pose` |
| `POST /gsplat` · `/booth` | `aito-gsplat` |
| `POST /depth` | `aito-depth` (ZipDepth RGB) |

`GY_VISION_AITO_URL=http://127.0.0.1:8766`

### 4. SFU + browser + cast

```bash
export GY_SFU_TOKEN=$(gy sfu-token)
make sfu-media && ./sfu/target/release/gy-sfu --token "$GY_SFU_TOKEN"
gy sfu-bridge --token "$GY_SFU_TOKEN"
# site/dojo.html · site/livenews.html · site/glyph-cast.html
```

**Live News → full-res cast (demo punch):**

1. Open `site/livenews.html` → **Connect hub** (optional)  
2. Pin feeds → **Main** (or **Fill from sort**)  
3. **Cast screen** → `glyph-cast.html` popup streams main mosaic at integer LED scale  
4. **Cast TV** → Presentation API when available  
5. Direct: `glyph-cast.html?hub=ws://127.0.0.1:9876&room=news&source=livenews&cast=1`

Wire: `site/glyph-cast-wire.js` · `BroadcastChannel("gy-glyph-cast")` · optional hub ingest on the cast page.

### 5. X / Spaces (optional)

`GY_X_STREAM_KEY` · `gy stream-x` · `gy space-rtmp`

## Partner checklist

```bash
gy doctor platform          # score + required/optional rows
gy platform export -o handoff.json
gy doctor vision
gy doctor sfu
gy doctor reliability
```

**Ready** = required checks green (ffmpeg, Media supervisor, vision providers, media control plane).  
**Partial** = live Grok key missing or optional Aito/SFU/X not up.  
**Blocked** = missing required tool (e.g. no ffmpeg).

## Integration contract

Machine-readable: [`integrations/grok-stream-platform.json`](../integrations/grok-stream-platform.json)

- mesh field map for `vision-take`
- env matrix
- launch recipes
- authority rules (who owns PGM, media, ML, 1k+)

Powerhouse stack (overview / blank / Qbpm) remains in [`powerhouse-protocol.json`](../integrations/powerhouse-protocol.json).

## One-liner pitch

> GrokYtalkY is a vision-first FFmpeg control plane and glyph mesh for Grok streaming: multimodal takes retarget encode, Aito supplies SAM/pose/depth, browsers consume `vision-take`, SFU carries glyph DCs under a shared token — ready for hybrid platform integration.
