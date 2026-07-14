# Filmmaker hurdle hop · HDRI quick probe

Jump the multi-cam → environment/lighting reference gap without Hugin or multi-bracket lab workflows.

## What you get

| Output | Purpose |
|--------|---------|
| **Subject strip** | L2 · L1 · C · R1 · R2 contact board (face/scene multi-angle) |
| **Equirect probe** | Slot-wedge lat-long map (lighting vibe / dome preview) |
| **Sphere map** | Shell samples the probe as env wash |
| **Export** | PNG equirect + strip download |

**Not** multi-EV Debevec, ghost removal, or photogrammetry. Single-EV quick tonemap + known scene slots.

## Flow

```
Laptop (C webcam) ──┐
Phone L1 (front)  ──┼─► GrokGlyph cam · scene order
Phone R1 (back)   ──┘
         │
         ▼  HDRI button
   subject strip + equirect
         │
    ┌────┴────┐
 download   cast → sphere shell
```

### URLs

- Laptop: https://fornevercollective.github.io/GrokYtalkY/grokglyph.html  
- Phone left: `…/grokglyph.html?slot=L1`  
- Phone right: `…/grokglyph.html?slot=R1`  
- Sphere: https://fornevercollective.github.io/GrokYtalkY/sphere.html  

### Steps

1. **Laptop** — open GrokGlyph · **cam** (webcam = **C**) · **hub** if casting  
2. **Phones** — `?slot=L1` / `?slot=R1` · **cam** · **cast**  
3. Laptop — **HDRI** → freeze lanes · strip + equirect panel  
4. **↓ equirect** / **↓ strip** or **cast sphere**  

## CLI / API

```bash
gy hdri doctor
gy serve   # optional hub archive + mesh fan-out
```

| API | Role |
|-----|------|
| `GET /api/hdri` | Status |
| `POST /api/hdri/probe` | Store last probe (+ mesh if hub attached) |

## Files

| Path | Role |
|------|------|
| `site/hdri-stitch.js` | Capture · tonemap · wedge stitch · glyph |
| `site/grokglyph.js` | HDRI button · panel · cast |
| `site/sphere.js` | `hdri-probe` → dome env map |
| `hdri.go` · `hdri_cmd.go` | Hub store · `gy hdri` |

## Limits

- iPhone often one lens at a time → use **two phones** for front+back  
- Not genlocked — lighting reference, not VFX plate  
- Upgrade path: OpenCV/ffmpeg sidecar for multi-band stitch later  
