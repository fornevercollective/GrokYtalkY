# Video burst walkie (Siri-sized + Glyph Matrix)

Short **face + voice** transmissions — same mental model as PTT audio walkie, but each burst also ships a tiny video face (and a **Glyph Matrix** brightness grid for Nothing Phone).

Inspired by:

- GrokYtalkY walkie PTT (hold / release)
- Siri’s compact orb popup (not a full-screen call UI)
- [Nothing Glyph Matrix Developer Kit](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit) (25×25 / 13×13 LEDs)

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

- Phone (3): `glyphN = 25` → 625 LEDs  
- Phone (4a) Pro: `glyphN = 13` → 169 LEDs  

Android toy scaffold: [`glyph/`](../glyph/README.md)  
Use `GlyphMatrixManager.setMatrixFrame(int[])` with the `glyph` array directly — no need to decode JPEG on device for the rear display.

## Why “Siri-sized”

Full video calls are heavy and socially loud. Bursts are:

1. **Bounded** — hold to talk, release to stop  
2. **Small UI** — orb / matrix / 11-line terminal popup  
3. **Face-readable at 25×25** — enough for expression, not surveillance stream  

Same mesh as walkie chat; optional whisper translate still hooks PTT release on the audio path.
