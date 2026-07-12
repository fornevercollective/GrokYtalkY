# Live depth & gsplat (ZipDepth + aito lineage)

GrokYtalkY can run **live monocular depth** on camera / watch frames and render
**gsplat-style** depth stacks in the terminal — same vocabulary as:

| Source | What we took |
|--------|----------------|
| [ZipDepth](https://zipdepth.github.io) | Real-time zero-shot mono depth (optional sidecar) |
| `aito-mac/zipdepth-sidecar` | HTTP `/depth` + zip-lite fallback |
| `aito` / `aito-living-canvas` `spatial-depth.ts` | Depth modes, near-warm / far-cool grade |
| `overview` `videoLabEffects` | gsplat depth proxy + thermal stack |

This is **not** a full 3D Gaussian Splat viewer. “gsplat” here means the
**CPU depth-stack / bake-preview** path used in aito glass + overview VFL.

## Modes (`d` or `/depth`)

| Mode | Behavior |
|------|----------|
| `off` | passthrough RGB |
| `zip-lite` | multi-scale mono depth (no deps) → turbo false-color |
| `zipdepth` | POST frame to sidecar `:8766`; falls back to zip-lite |
| `gsplat` | zip-lite structure + gsplat thermal/rim stack |

```bash
gy                    # companion
# press c (cam) then d to cycle depth modes

/depth zipdepth       # prefer real ZipDepth sidecar
/depth gsplat
/depth lite
/depth off
```

Header flag shows active mode (`zip-lite` · `zipdepth` · `gsplat`).

## ZipDepth sidecar

```bash
# from GrokYtalkY
./scripts/zipdepth-sidecar.sh

# or aito-mac directly
python3 ~/dev/aito-mac/zipdepth-sidecar/booth_zipdepth.py --port 8766
```

| Env | Purpose |
|-----|---------|
| `ZIPDEPTH_URL` | override base URL (default `http://127.0.0.1:8766`) |
| `ZIPDEPTH_ROOT` | clone of fabiotosi92/ZipDepth |
| `ZIPDEPTH_CKPT` | `.pth` checkpoint |
| `ZIPDEPTH_ONNX` | exported ONNX |

```bash
gy doctor   # shows sidecar health + stream tools
```

Protocol (binary POST, same as aito-mac jax/zipdepth sidecars):

```
POST /depth
  u32le width | u32le height | RGB888 × w×h
→ JSON { w, h, depth: float[w*h], backend }
```

## Burst / Glyph Matrix

When depth is on, burst Glyph grids prefer **depth brightness** (near = bright LED)
via `DepthToGlyph` — pairs with Nothing Glyph Matrix 25×25.

## Related paths on this machine

```
~/dev/aito/src/lib/spatial-depth.ts
~/dev/aito-living-canvas/src/lib/spatial-depth.ts
~/dev/aito-mac/zipdepth-sidecar/
~/dev/overview/src/research/videoLabEffects.ts   # gsplat stack
```
