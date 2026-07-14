# Sphere · 16K addressable venue canvas

Professional cast addressing for multi-phone / multi-spot testing.  
Implementation: `site/venue-canvas.js` · viewer `site/sphere.html` · phone `site/phone.html`.

## Concept breakdown (handle one layer)

| Layer | Name | What you manage |
|-------|------|-----------------|
| **L0** | **Blueprint** | Zones like a lidar/scan: `seat` · `stage` · `backstage` · `aisle` · `opening` · `parking` · `screen` · `proscenium` · `vip` |
| **L1** | **Targets** | Every cast address: `id`, `kind`, `zone`, meters, **(px,py) @ 16 000²** |
| **L2** | **Index** | Spatial hash on 16K: pixel → nearest target; zone / section / chunk → id set |
| **L3** | **Bulk activate** | Turn on section, chunk, zone, or LED rect → hot target list |
| **L4** | **Cast** | Phone/mesh `pos` binds to `target` \| `seat` \| `px,py` |
| **L5** | **Screen** | Interior LED unwrap — **any free LED spot is a valid cast target** |
| **L6** | **Cameras** | Fixed viewing positions (FOH, wings, balcony, parking, overhead…) |
| **L7** | **Lighting** | Venue wash (ambient/key/fill/stage) + **phone flashlights** on mesh |

```
phone / bulk demo
      │
      ▼
 resolvePos({ seat | px,py | target | bulk ids })
      │
      ▼
 vburst-frame.pos = {
   map: "sphere-vegas-16k",
   addressable: true,
   target, zone, section, chunk,
   px, py, res: 16000,
   x,y,z …
 }
      │
      ▼
 sphere.html lights that LED / seat / zone point + Glyph HUD
```

## Zones (blueprint)

| Zone | Role |
|------|------|
| **seat** | Bowl seating (section 100–500 + floor), chunked for bulk |
| **stage** | Performance deck |
| **proscenium** | Lip / arrays |
| **backstage** | BOH under stage |
| **aisle** | Radial circulation |
| **opening** | Entries / exits / lobby openings |
| **parking** | Exterior lot ring (outside dome) |
| **screen** | LED sample patches + free-pixel fallback |
| **vip** | Suites / club |

## Chunks

Seats are grouped: `chunk:{section}:R{rowChunk}C{colChunk}`  
(default **4 rows × 8 cols** per chunk).  
Bulk activate a chunk ≈ section slice for multi-cast stress tests.

## Phone cast URLs

```text
# exact seat
/phone.html?seat=200-R5-C12&quick=1

# any 16K LED spot
/phone.html?px=8000&py=4000&quick=1

# named target id (from click-pick on sphere)
/phone.html?target=seat:200-R5-C12&quick=1
/phone.html?target=stage:Stage:0&quick=1
```

Field accepts: `200-R5-C12` · `8000,4000` · full `target:` id.

## Sphere director

1. Open `/sphere.html` (wave + full dome).  
2. **Click** any seat / infra / free LED → pick panel (copy phone URL · demo cast).  
3. **Bulk:** choose `section` | `chunk` | `zone` | `LED rect` → **Bulk activate** (highlight).  
4. **Bulk cast demo** sprays Glyphs across activated targets.  
5. Real phones: each with its own `?seat=` / `?px=&py=` → concurrent multi-cast.

## Bulk API (JS)

```js
GY_VENUE.buildVenue();
GY_VENUE.bulkActivate({ section: "200" });
GY_VENUE.bulkActivate({ chunk: "chunk:200:R1C2" });
GY_VENUE.bulkActivate({ zone: "stage" });
GY_VENUE.bulkActivate({ zones: ["aisle", "opening"] });
GY_VENUE.bulkActivate({ px: 8000, py: 4000 });
GY_VENUE.bulkActivate({
  rect: { x0: 0, y0: 0, x1: 15999, y1: 8000 },
  step: 128,
});
GY_VENUE.resolvePos({ px: 1200, py: 9000 });
```

## What “pixel precision” means here

| Capability | Status |
|------------|--------|
| Cast to **any seat** by id | Yes |
| Cast to **any 16K (px,py)** (nearest target or free LED) | Yes |
| Bulk section / chunk / zone / rect | Yes |
| Click-pick on sphere → URL | Yes |
| True one-LED-per-seat unique CAD | Procedural map (not official Sphere CAD) |
| Physical lidar import | Blueprint is **procedural scan-like**; swap-in real CSV later via same target schema |

## Camera viewing positions (L6)

Director dropdown on `sphere.html` → free-cam lookAt:

| id | View |
|----|------|
| `foh` | Front of house |
| `stage` | Stage apron |
| `wing_l` / `wing_r` | Stage wings |
| `balcony` / `sec200_*` / `sec400` | House bowl |
| `backstage` / `entry` / `parking` / `overhead` / `led_close` | Ops & exterior |

Drag orbit exits free-cam back to free orbit. Cameras are also zone=`camera` targets (bulk-activatable).

## Lighting + phone flashlights (L7)

`site/venue-lighting.js` · panel **Lights** on sphere.

| Control | Mesh / source |
|---------|----------------|
| ambient · key · fill · stageWash · exposure | Local panel or `type:venue-light` kind=`wash` |
| Concert / Dim house / Flat presets | Sphere panel |
| **Phone 🔦 Flash** | Hardware torch when allowed + mesh `type:venue-light` kind=`flashlight` |
| Torch on Look panel | `type:camera-controls` look.torch + cast `look.torch` |

Flashlight position follows cast `pos` (seat / LED). Sphere shades points with inverse-square falloff so multi-phone flashlights light nearby seats/aisles.

```text
/phone.html?seat=200-R5-C12&quick=1
# tap 🔦 Flash → torch + venue light at that seat on sphere.html
```

## Related

- [`docs/stadium-glyph.md`](stadium-glyph.md) — infinite canvas LOD strategy  
- [`docs/camera-controls.md`](camera-controls.md) — look / torch hardware  
- `site/sphere-seating.js` — seat generation + Bloch³  
- `site/venue-canvas.js` — L0–L6 addressable layer  
- `site/venue-lighting.js` — L7 lighting + flashlights  
