# Camera & lighting controls (v1.78)

Standard **phone / film** image controls aligned with [aito](https://fornevercollective.github.io/aito/) adjustments, applied on GrokYtalkY phone cast, glyph mesh, and TUI.

## Two layers

| Layer | What | Where |
|-------|------|--------|
| **Hardware** | MediaTrackConstraints: EV, ISO, WB mode, color temp, focus, torch, zoom | Browser `camera-controls.js` → `applyConstraints` |
| **Software grade** | exposure, contrast, sat, temperature, tint, clarity, sharpen, vignette, fill, shadows, highlights, grain, night | Always — Go `ApplyCameraLook` + JS `applyLookToImageData` |

Aito reference fields: `exposure · contrast · saturation · temperature · tint · clarity · sharpen · vignette` (`store.adjustments`).

## Presets

`neutral` · `daylight` · `cloudy` · `tungsten` · `fluorescent` · `shade` · `neon` · `night` · `film` · `punchy` · `soft` · `bleach`

```bash
# TUI
/camera status
/look film
/look night
/camera exposure=0.2 fill=0.35 wb=daylight iso=400
gy doctor camera

# env seed
export GY_CAMERA_PRESET=film
```

Take lines (orch/vision):

```
CAMERA exposure=0.15 contrast=0.1 temperature=0.1 fill=0.2
LOOK film
```

## Mesh

```json
{
  "type": "camera-controls",
  "from": "phone",
  "look": {
    "exposure": 0.2,
    "contrast": 0.1,
    "saturation": 0,
    "temperature": 0.12,
    "tint": 0.04,
    "fill": 0.3,
    "night": false,
    "iso": 400,
    "wb_mode": "daylight",
    "preset": "film"
  }
}
```

`vburst-frame` from phone may embed `look` for cast receivers.

## Browser

| Page | UI |
|------|-----|
| `phone.html` | **Look** button → full panel; grades glyph + JPEG cast |
| `camera-controls.js` | Shared module (presets, grade, hardware, mesh) |
| `glyph-cast.html` | **Look** panel · grades LED paint · mesh `camera-controls` · `?look=film` |

### glyph-cast

```text
https://fornevercollective.github.io/GrokYtalkY/glyph-cast.html?look=film&cast=1
https://…/glyph-cast.html?hub=ws://127.0.0.1:9876&room=news&look=night
```

- **Look** / key `l` — aito-aligned sliders + presets  
- Inbound `type:camera-controls` or `vburst-frame.look` from phone  
- Per-peer look when present; else global look grades all tiles

## Phone / film map

| Control | Phone | Film metaphor |
|---------|-------|----------------|
| Exposure / EV | exposureCompensation | stops |
| ISO | iso constraint | film speed |
| Shutter | exposureTime (when manual) | 1/125 … |
| WB | whiteBalanceMode + colorTemperature | daylight 5600K / tungsten 3200K |
| Focus | focusMode / focusDistance | AF continuous |
| Torch | torch | on-camera light |
| Zoom | zoom | crop / digital |
| Fill / shadows | software | bounce / fill light |
| Grain / vignette | software | stock / falloff |

## Relation to Aito

| Aito | GrokYtalkY |
|------|------------|
| `adjustments.exposure…` | `CameraLook` grade |
| Film LUTs / presets | `preset` + optional `lut` slug |
| Tether ISO/shutter UI | phone panel + mesh |
| SAM / Grok look prompts | vision take `CAMERA` / `LOOK` lines |

Heavy retouch stays in Aito; gy applies **live grade** on cast path for stage/demo.

## Demo

```bash
gy serve --bind 0.0.0.0
# phone: open hub /phone.html → Camera → Look → film/night → Cast
# laptop: gy  · see graded glyph
```
