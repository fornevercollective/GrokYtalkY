# Stadium Glyph — infinite canvas from phone atom → 112k feeds

**Status:** concept + architecture contract (v0)  
**Atom already shipped:** single-user phone cast → full-res player  
(`phone.html` · `glyph-cast.html` · `grokglyph.html` · `livenews.html`)  
**Molecule:** spatial LOD + virtual texture + hybrid SFU so the same Glyph can scale to stadium installs.

---

## Metaphor

| Install | Physical metaphor | Logical canvas |
|---------|-------------------|----------------|
| **Vegas Sphere–class** | Touchable interior LED (~16K×16K, multi-GPU) | Virtual infinite texture, page tiles 512–2048 |
| **Stadium boards** | SoFi / MBS wrap ribbon + end-zone walls | Multi-viewport sample of same world |
| **Drone / cloud** | Projection-mapped or LED swarm | Low native res, high sync (PTP / NMOS) |
| **nothing-to-watch energy** | Force / Voronoi WebGL of tens of thousands of items | Live video + Glyph tiles instead of posters |

Reference energy: [nothing-to-watch](https://nothing-to-watch.port80.ch) · [gnovotny/nothing-to-watch](https://github.com/gnovotny/nothing-to-watch)  
Attendance anchors (order-of-magnitude, not product SLAs): Zach Bryan Big House ~112k · Hyde Park ~70k · Yankee Stadium ~46k.

**You never run 112k × 1080p.** You run a hierarchical LOD. Aggregate “canvas size” is virtual; only hot tiles decode.

---

## What already exists (do not reinvent)

| Bone | Where | Role at scale |
|------|--------|----------------|
| Phone atom | `site/phone.html` + quick QR | One peer source (glyph / hexlum / mid JPEG) |
| Full-res cast player | `site/glyph-cast.html` + `glyph-cast-wire.js` | Becomes **one viewport** into the stadium world |
| Multi-feed wall | lab / `news_wall` / Live News | Section mosaic before seating map |
| Capability handshake | `cap_profile.go` | Edge class · lanes · max_fps · backpressure |
| Adaptive scale | `video_scale.go` · vision retune | Per-feed budget demote/promote |
| Mesh hub rooms | `hub.go` · `GY_ROOM_MAX` | Private / section rooms (not 100k) |
| DOJO SFU | `sfu/` webrtc-rs | Mid interactive band (~50–200 / node) |
| Public fan-out | mid-lane · CF Workers/DO · Calls | 1k+ viewers (extend for stadium room) |
| Venue timing | ST 2110 / NMOS / PTP scaffolding | Fixed cams + multi-output lock |
| AI interest | vision SAM / pose / theme-vision | **Promote** interesting tiles to higher LOD |
| Capacity contract | `docs/streams-capacity.md` | Lanes: glyph · hex · mid · full |

Hybrid diagram (today) already splits **hub / DOJO SFU / Cloudflare**. Stadium adds a **compositor + tile plane** between ingest and viewports.

---

## Resolution pyramid (strict)

| Zoom / distance | Per-feed resolution | Aggregate presentation | Notes |
|-----------------|---------------------|------------------------|--------|
| **Far** (stadium map / Sphere exterior) | 16×16 – 64×64, or pure Glyph hex / 25×25 luminance | Virtual 4K–8K base + infinite tiles | Theme clusters, motion blobs, pose heatmaps |
| **Medium** (section / aisle) | 160×160 – ~360p JPEG / mesh | Sphere-class ~16K×16K virtual | Cap handshake + downscale budgets |
| **Near** (one fan / one cam / one cluster) | 720p – 1080p (or native if promoted) | Local tile 2K–4K to interested clients only | SAM / pose / theme **boost** |
| **Extreme** (touch / focus) | Native phone or venue cam 4K | GPU texture swap | Full-res Glyph player path |

**Virtual canvas:** infinite or stadium-unwrapped 32K–64K *logical*. Physical outputs sample viewports only.

---

## Spatial model

```
  seating / GPS / beacon / QR seat code
           │
           ▼
  ┌─────────────────────┐
  │  Spatial index      │  quadtree · spatial hash · optional Voronoi
  │  feed_id → (x,y,z)  │  section · row · seat · free-form “field”
  └──────────┬──────────┘
             │
             ▼
  ┌─────────────────────┐
  │  Virtual texture    │  pages 512²–2048² · multi-res variants
  │  (tile server)      │  cold: JPEG/hex · hot: WebRTC / gyst mid
  └──────────┬──────────┘
             │
     ┌───────┴────────┐
     ▼                ▼
  Viewport A       Viewport B …
  (Sphere GPU)     (phone, board, drone)
```

### Placement sources (priority)

1. **Sphere Vegas · Bloch³ seating map** (canonical for Sphere-class installs)  
2. **Seat QR / ticket deep link** (`?seat=200-R5-C12` · room section)  
3. **Beacon / venue mesh** (Bluetooth / private 5G edge)  
4. **Phone GPS** (coarse; outdoor festivals)  
5. **Force-directed fallback** (nothing-to-watch style when geo unknown)

---

## Sphere Vegas — Bloch³ mapping

**Authority UI / prototype:** [qpu-pointcloud.html](https://mueee.qbitos.ai/qpu-pointcloud.html)  
**Module in gy:** [`site/sphere-seating.js`](../site/sphere-seating.js)

| Symbol | Meaning |
|--------|---------|
| **SPHERE_VEGAS** | Dome dims (~366′ × 516′), sections 100–500 + floor, 16K×16K LED, speaker arrays |
| **seat → (x,y,z)m** | Procedural rake + azimuth rings (not official CAD) |
| **Bloch³** | `θ, φ` + unit vector `(blochX, blochY, blochZ)` from seat vs dome center |
| **Screen px** | Az/el unwrap → `px, py` on virtual **16 000 × 16 000** interior |
| **Speaker** | Nearest array + beam az/el + distance_m |
| **Seat id** | `200-R5-C12` or global `idx` |

```text
phone ?seat=200-R5-C12
        │
        ▼
  GY_SPHERE.findSeat → seatToMeshPos
        │
        ▼
  vburst-frame.pos / gyst.pos = {
    map: "sphere-vegas-bloch3",
    id, section, row, col, idx,
    x,y,z, x_m,y_m,z_m,
    theta, phi, bloch:[…],
    px, py, res:16000,
    speakerArray, distance_m, haptic
  }
        │
        ▼
  Stadium canvas / glyph-cast viewport places feed at Bloch or screen UV
```

**BroadcastChannel (source prototype):** `sphere-vegas` · `hexcast-stream` · `iron-line` / `kbatch-training`  
Gy may later bridge the same payload on hub WS as `type:stadium-feed`.

**HUD labels (infra points, not seats):** Stage · Entries · Exits · VIP · Bridge · Aisles

---

## Wire / tile protocol (sketch)

### Room token

```http
GET /api/stadium?room=big-house-2027
→ {
  "room": "big-house-2027",
  "canvas": { "w": 32768, "h": 16384, "page": 1024 },
  "sfu": "wss://…",
  "tiles": "https://cdn…/tiles/{z}/{x}/{y}.jpg",
  "hot": "wss://…/hot?viewport=…",
  "glyph_n_far": 25
}
```

### Feed contribution (extend phone / venue)

```json
{
  "type": "stadium-feed",
  "from": "phone-nick",
  "room": "big-house-2027",
  "lod": "far|mid|near|extreme",
  "pos": { "x": 12040, "y": 8820, "section": "23", "seat": "A12" },
  "interest": 0.0,
  "glyph": [/* optional 25² */],
  "mid": { "fmt": "jpeg", "w": 160, "h": 160, "b64": "…" },
  "cap": { "class": "glyph-iot", "max_fps": 8, "bp": 4 }
}
```

### Viewport subscribe (viewer / board / drone)

```json
{
  "type": "stadium-viewport",
  "room": "big-house-2027",
  "rect": { "x": 8000, "y": 4000, "w": 4096, "h": 2160 },
  "zoom": 2.5,
  "max_hot": 48,
  "prefer": ["pose", "theme", "audio"]
}
```

Server returns: **cold page list** (CDN URLs) + **hot track IDs** (SFU / mesh) + **glyph lattice blob** for far field.

### LOD controller rules

| Signal | Action |
|--------|--------|
| `zoom` low / far | Demote all → glyph/hex; cluster by theme |
| Section in view | Promote section peers to mid JPEG |
| Touch / pin / high `interest` | Near or extreme for N hottest |
| Cap `bp` / battery / Wi‑Fi weak | Force demote; edge NPU stays on phone |
| Vision pose / SAM “sign” / motion | Raise `interest` → budget steal from cold |

---

## Handling architecture

```
 Phones (Snapdragon NPU)     Venue cams (2110/RTSP/WebRTC)
   · local downscale            · fixed high-res ingest
   · interest score             · NMOS identity
   · glyph/hex always-on
              \                /
               ▼              ▼
        ┌──────────────────────────┐
        │ Ingest + room fan-out    │
        │ hub (section) · SFU · CF │
        └────────────┬─────────────┘
                     ▼
        ┌──────────────────────────┐
        │ Compositor / tile plane  │
        │ spatial index + LOD + VT │
        └────────────┬─────────────┘
          ┌──────────┼──────────┐
          ▼          ▼          ▼
      Glyph cast  Stadium    Drone /
      viewport    boards     cloud map
```

### Bandwidth / power (order-of-magnitude)

| Strategy | Target intuition |
|----------|------------------|
| Aggressive far LOD + ROI | Most of 112k never leave **glyph/hex** |
| Edge encode on phone | Snapdragon free NPU + HW encoder (your cap budgets) |
| Peak aggregate (promoted only) | **50–200 Gbps** class with modern media edge — not on single `gy serve` |
| Stadium radio | Wi‑Fi 6E/7 + private 5G + mesh backhaul |

**Honest split:** `gy` hub remains **section / DOJO / terminal**. Public stadium room is **mid-lane + CF + multi-node SFU**. Compositor may be edge GPU cluster for Sphere install.

---

## Client surfaces (reuse)

| Surface | Today | Stadium role |
|---------|--------|--------------|
| [glyph-cast.html](https://fornevercollective.github.io/GrokYtalkY/glyph-cast.html) | Full-res cast player | Viewport into virtual canvas (stadium room token) |
| [grokglyph.html](https://fornevercollective.github.io/GrokYtalkY/grokglyph.html) | Glyph LED / dual | Far–mid lattice + personal pointer |
| [livenews.html](https://fornevercollective.github.io/GrokYtalkY/livenews.html) | Multi-feed wall | Section wall / theme cluster preview |
| phone.html | Single cast source | Source + optional **controller** (ray into canvas) |
| **sphere.html** | **Live Sphere Glyph viewer** | Seats + mesh `vburst`/`gyst` only (no qpu extras) |
| qr.html | Quick connect | Seat/section QR → room + pos |

### Sphere Glyph viewer (v1.80)

Focused page: [`site/sphere.html`](../site/sphere.html) + [`site/sphere.js`](../site/sphere.js)

- **In:** hub WS · `vburst-frame` / `gyst` hexlum · optional `pos` (Sphere Bloch³)
- **Out:** WebGL seat cloud + HUD Glyph tiles at live seats
- **Not included:** music, QPU patches, MOPA, VR Quest, kBatch, contrail language, laser export

```bash
gy serve
# laptop
open http://HOST:9876/sphere.html
# phones
open http://HOST:9876/phone.html?seat=200-R5-C12&quick=1
# hold Cast → Glyphs light seats on the Sphere
```

---

## PR-level plan (ship the molecule in slices)

### Phase 0 — Contract (this doc)
- [x] Architecture + pyramid + protocol sketch  
- [ ] `integrations/stadium-glyph.json` machine-readable contract (follow-up)

### Phase 1 — Stadium room atom (first code PR)
**Goal:** one phone contributes; cast player joins same room as **spatial mosaic of N≤32** (not 112k yet).

1. ~~`pos` on phone cast + Sphere seating map~~ (v1.79.2)  
2. ~~`sphere.html` live Glyph viewer on seats~~ (v1.80)  
3. `type:stadium-join` + room token on hub (reuse room tenancy).  
4. `glyph-cast` + `livenews`: layout feeds by `pos` or grid fallback.  
5. LOD demote: if peers > budget, force glyph-only for cold slots.  
6. Doctor: `gy doctor stadium` → room peers, lod histogram, bp.

### Phase 2 — Virtual texture pages
1. Tile server stub: multi-res JPEG pages from compositor snapshot.  
2. Viewport subscribe JSON; only hot WebRTC tracks.  
3. WebGL mosaic (extend Glyph / news wall) with pan/zoom + page fetch.

### Phase 3 — Interest promotion
1. Wire vision pose / theme scores → `interest`.  
2. LOD controller process (hub or sidecar).  
3. SAM/theme boost path reuses retarget budgets.

### Phase 4 — Hybrid 1k–10k
1. Public stadium room on CF mid-lane / multi SFU nodes.  
2. Section sharding (`room = event/section-23`).  
3. Metrics + backpressure already started on SFU.

### Phase 5 — Install outputs
1. Multi-viewport export (boards + Sphere tiles).  
2. Drone/projection map table (NMOS / PTP lock).  
3. Multi-GPU notes only — not in-process TensorFlow.

---

## Explicit non-goals (v0)

- Single-node `gy serve` for 112k full peers.  
- 112k × 1080p mesh.  
- In-process Sphere GPU cluster driver.  
- Perfect GPS seating without venue beacons / seat QR.

---

## Resolution budget (worked example)

Assume **100k phones**, far default **25×25 glyph @ 4 fps**, 1 byte/cell luminance:

| Band | Count | Per feed | Aggregate order |
|------|-------|----------|-----------------|
| Far glyph | 95 000 | 25² × 4 fps ≈ 2.5 KB/s | ~240 MB/s |
| Mid JPEG | 4 800 | 160² JPEG ~8 KB @ 6 fps ≈ 50 KB/s | ~240 MB/s |
| Near 720p | 180 | ~1–2 Mbps | ~0.2–0.4 Gbps |
| Extreme 1080p+ | 20 | ~4–8 Mbps | ~0.1 Gbps |
| **Total order** | | | **≪ 1–2 Gbps control plane + edge** if demotion holds |

If mid band explodes without LOD, you leave the design. **LOD is the product.**

---

## First PR acceptance criteria (Phase 1)

1. Seat or free `pos` on phone cast frames.  
2. `glyph-cast` stadium mode: mosaic of room peers by position.  
3. Cap budget: max hot mid-res slots (e.g. 16); rest glyph.  
4. Docs link from companion + streams-capacity.  
5. No new heavy Go deps; client WebGL optional behind flag.

---

## One-liner

**Single-user phone cast is the atom. Stadium Glyph is the infinite-canvas molecule: spatial LOD + virtual texture + hybrid SFU, sampling the same living Glyph Matrix from Sphere, boards, drones, or any phone viewport.**
