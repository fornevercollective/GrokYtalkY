# GrokYtalkY Glyph Toy — video burst walkie

[Siri-sized](../docs/burst.md) short **video + voice** walkie, rendered on the
[Nothing Glyph Matrix](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit)
(Phone (3) = **25×25**, Phone (4a) Pro = **13×13**).

## Concept

| Gesture (Glyph Button) | Walkie meaning |
|------------------------|----------------|
| **Touch-down** (`action_down`) | Start burst — TX face + mic |
| **Touch-up** (`action_up`) | End burst |
| **Long press** (`change`) | Toggle hub connect / next peer |

While receiving a peer burst, the rear matrix shows their **glyph grid**
(brightness array from mesh `vburst-frame.glyph`).

While idle, a soft pulse ring indicates connection (AOD-friendly dim).

## Mesh protocol (same as terminal / web)

```json
{"type":"vburst-start","from":"nick"}
{"type":"vburst-frame","from":"nick","b64":"<jpeg>","w":120,"h":120,"glyph":[0..255×N²],"glyphN":25}
{"type":"audio","fmt":"pcm16","b64":"...","sr":16000,"ch":1,"from":"nick"}
{"type":"vburst-end","from":"nick"}
```

Connect to the GrokYtalkY hub WebSocket:

```
ws://HOST:9876/?role=peer&nick=android
```

## Project layout

```
glyph/
  README.md
  app/
    build.gradle.kts          # drop glyph-matrix-sdk-2.0.aar into app/libs/
    src/main/
      AndroidManifest.xml
      java/.../BurstToyService.kt
      java/.../MeshClient.kt
      java/.../GlyphBurstRenderer.kt
      res/values/strings.xml
      res/drawable/img_toy_preview.xml
```

## Setup (Android Studio)

1. Create an empty Android app (`minSdk 34` recommended; Nothing OS 14+).
2. Copy `glyph-matrix-sdk-2.0.aar` from the
   [GlyphMatrix-Developer-Kit](https://github.com/Nothing-Developer-Programme/GlyphMatrix-Developer-Kit)
   into `app/libs/`.
3. Copy sources from this folder into the app module.
4. Install on Phone (3) → Settings → Glyph Interface → manage toys → enable **GrokYtalkY Burst**.
5. Run a hub: `grokytalky serve` (or companion) on your LAN; set hub host in the toy intro activity.

## Permissions

```xml
<uses-permission android:name="com.nothing.ketchum.permission.ENABLE"/>
<uses-permission android:name="android.permission.CAMERA"/>
<uses-permission android:name="android.permission.RECORD_AUDIO"/>
<uses-permission android:name="android.permission.INTERNET"/>
```

## Rendering note

Mesh already sends `glyph: int[N*N]` brightness (0–255). Prefer that over re-decoding JPEG on device:

```kotlin
mGM.setAppMatrixFrame(glyphIntArray)  // or setMatrixFrame inside a Glyph Toy
```

Use `setAppMatrixFrame` when driving the matrix from a normal Activity;
use `setMatrixFrame` inside the Glyph Toy service (higher priority while selected).

## Terminal + web peers

```bash
# laptop
./bin/grokytalky burst

# browser
open site/burst.html  # Connect → hold orb

# phone
# select GrokYtalkY Burst toy → hold Glyph Button
```

All three share the same hub.
