/**
 * Venue Canvas — professional addressable cast layer (Sphere / stadium).
 *
 * CONCEPT BREAKDOWN (handle one layer at a time):
 *
 *   L0  BLUEPRINT   zones like a lidar/scan: stage, backstage, aisles,
 *                   openings/exits, parking, seats, screen
 *   L1  TARGETS     every cast address: id · kind · zone · (x,y,z)m · (px,py)@16K
 *   L2  INDEX       16K spatial hash: pixel → nearest target; zone/section/chunk → set
 *   L3  BULK        activate section / chunk / zone / pixel rect → hot target ids
 *   L4  CAST        phone/mesh pos binds to targetId | seat | px,py | bulk set
 *   L5  SCREEN      interior LED 16K×16K unwrap (az/el) — free LED spots are valid
 *
 * Depends on: GY_SPHERE (sphere-seating.js) for seat generation + seatToPixel.
 */
(function (root, factory) {
  const api = factory(root);
  if (typeof module === "object" && module.exports) module.exports = api;
  root.GY_VENUE = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function (root) {
  "use strict";

  const RES = 16000;
  const CELL = 64; // spatial hash cell size in pixels (16K/64 = 250 bins/side)
  const CHUNK_ROWS = 4;
  const CHUNK_COLS = 8;

  const ZONE = {
    seat: "seat",
    stage: "stage",
    backstage: "backstage",
    aisle: "aisle",
    opening: "opening",
    parking: "parking",
    screen: "screen",
    proscenium: "proscenium",
    vip: "vip",
  };

  const ZONE_META = {
    seat: { label: "Seats", color: [0.45, 0.7, 0.95], layer: 2 },
    stage: { label: "Stage", color: [1.0, 0.55, 0.25], layer: 3 },
    backstage: { label: "Backstage", color: [0.55, 0.4, 0.7], layer: 1 },
    aisle: { label: "Aisles", color: [0.55, 0.55, 0.55], layer: 2 },
    opening: { label: "Openings / exits", color: [0.95, 0.85, 0.25], layer: 2 },
    parking: { label: "Parking / exterior", color: [0.35, 0.55, 0.4], layer: 0 },
    screen: { label: "LED screen sample", color: [0.7, 0.3, 0.85], layer: 4 },
    proscenium: { label: "Proscenium", color: [0.95, 0.4, 0.4], layer: 3 },
    vip: { label: "VIP / suites", color: [0.75, 0.4, 0.9], layer: 2 },
  };

  function cfg() {
    const S = root.GY_SPHERE;
    return (S && S.SPHERE_VEGAS) || {
      res: RES,
      dome_radius_m: 78.6,
      height_m: 111.6,
      center_m: { x: 0, y: 55.8, z: 0 },
      screen: {
        radius_m: 75,
        az_start_deg: -100,
        az_end_deg: 100,
        el_start_m: 5,
        el_end_m: 105,
      },
    };
  }

  function seatToPixel(x_m, y_m, z_m) {
    if (root.GY_SPHERE && root.GY_SPHERE.seatToPixel) {
      return root.GY_SPHERE.seatToPixel(x_m, y_m, z_m, cfg());
    }
    const c = cfg();
    const az = Math.atan2(x_m, z_m);
    const az0 = (c.screen.az_start_deg * Math.PI) / 180;
    const az1 = (c.screen.az_end_deg * Math.PI) / 180;
    const azFrac = Math.max(0, Math.min(1, (az - az0) / (az1 - az0)));
    const elFrac = Math.max(
      0,
      Math.min(1, (y_m - c.screen.el_start_m) / (c.screen.el_end_m - c.screen.el_start_m))
    );
    return {
      px: Math.floor(azFrac * (c.res || RES)),
      py: Math.floor(elFrac * (c.res || RES)),
      azFrac,
      elFrac,
    };
  }

  /** Free LED spot: px,py → meters on interior screen surface + 3D unit. */
  function pixelToWorld(px, py, c) {
    c = c || cfg();
    const res = c.res || RES;
    const az0 = (c.screen.az_start_deg * Math.PI) / 180;
    const az1 = (c.screen.az_end_deg * Math.PI) / 180;
    const azFrac = Math.max(0, Math.min(1, px / res));
    const elFrac = Math.max(0, Math.min(1, py / res));
    const az = az0 + (az1 - az0) * azFrac;
    const y_m = c.screen.el_start_m + (c.screen.el_end_m - c.screen.el_start_m) * elFrac;
    const R = c.screen.radius_m || 75;
    const x_m = Math.sin(az) * R;
    const z_m = Math.cos(az) * R;
    const Rd = c.dome_radius_m || 78.6;
    return {
      x_m,
      y_m,
      z_m,
      x: x_m / Rd,
      y: (y_m / (c.height_m || 111.6) - 0.5) * 2,
      z: z_m / Rd,
      azFrac,
      elFrac,
      px: Math.floor(px),
      py: Math.floor(py),
    };
  }

  function clampPx(v) {
    return Math.max(0, Math.min(RES - 1, v | 0));
  }

  // ── target builders (blueprint scan) ──

  function makeTarget(o) {
    return {
      id: o.id,
      kind: o.kind,
      zone: o.zone,
      label: o.label || o.id,
      section: o.section || null,
      chunk: o.chunk || null,
      row: o.row != null ? o.row : null,
      col: o.col != null ? o.col : null,
      seatIdx: o.seatIdx != null ? o.seatIdx : null,
      x_m: o.x_m,
      y_m: o.y_m,
      z_m: o.z_m,
      x: o.x,
      y: o.y,
      z: o.z,
      px: clampPx(o.px),
      py: clampPx(o.py),
      res: RES,
      castable: o.castable !== false,
      bulk: o.bulk !== false,
    };
  }

  function chunkId(section, rChunk, cChunk) {
    return "chunk:" + section + ":R" + rChunk + "C" + cChunk;
  }

  function targetsFromSeats(seats) {
    const out = [];
    for (let i = 0; i < seats.length; i++) {
      const s = seats[i];
      const rCh = Math.floor(s.row / CHUNK_ROWS);
      const cCh = Math.floor(s.col / CHUNK_COLS);
      const ch = chunkId(s.section, rCh, cCh);
      out.push(
        makeTarget({
          id: "seat:" + s.id,
          kind: "seat",
          zone: ZONE.seat,
          label: s.id,
          section: String(s.section),
          chunk: ch,
          row: s.row,
          col: s.col,
          seatIdx: s.idx,
          x_m: s.x_m,
          y_m: s.y_m,
          z_m: s.z_m,
          x: s.x,
          y: s.y,
          z: s.z,
          px: s.px,
          py: s.py,
        })
      );
    }
    return out;
  }

  /** Procedural venue infra — blueprint / lidar-style sample points. */
  function generateInfraTargets() {
    const c = cfg();
    const Rd = c.dome_radius_m || 78.6;
    const out = [];
    let n = 0;

    function addGrid(zone, kind, label, ox, oy, oz, nx, ny, sx, sy, sz) {
      for (let i = 0; i < nx; i++) {
        for (let j = 0; j < ny; j++) {
          const x_m = ox + (i / Math.max(1, nx - 1) - 0.5) * sx;
          const z_m = oz + (j / Math.max(1, ny - 1) - 0.5) * sz;
          const y_m = oy + (ny > 1 ? (j / (ny - 1)) * sy : 0);
          const pix = seatToPixel(x_m, y_m, z_m);
          const id = kind + ":" + label.replace(/\s+/g, "_") + ":" + n++;
          out.push(
            makeTarget({
              id: id,
              kind: kind,
              zone: zone,
              label: label,
              section: zone,
              chunk: "chunk:" + zone + ":0",
              x_m: x_m,
              y_m: y_m,
              z_m: z_m,
              x: x_m / Rd,
              y: (y_m / (c.height_m || 111.6) - 0.5) * 2,
              z: z_m / Rd,
              px: pix.px,
              py: pix.py,
            })
          );
        }
      }
    }

    // Stage (front bowl)
    addGrid(ZONE.stage, "stage", "Stage", 0, 1.2, -18, 18, 10, 22, 0, 14);
    // Proscenium lip
    addGrid(ZONE.proscenium, "proscenium", "Proscenium", 0, 6.5, -22, 20, 1, 28, 0, 2);
    // Backstage / BOH under stage
    addGrid(ZONE.backstage, "backstage", "Backstage BOH", 0, -2.5, -12, 12, 6, 20, 0, 10);
    // VIP basement club
    addGrid(ZONE.vip, "vip", "VIP Club", 0, -4, -8, 8, 4, 12, 0, 8);

    // Aisles (radial)
    const aisleAngles = [-80, -60, -40, -20, 0, 20, 40, 60, 80];
    aisleAngles.forEach(function (deg) {
      const a = (deg * Math.PI) / 180;
      for (let r = 0; r < 14; r++) {
        const rad = 12 + r * 4.2;
        const x_m = Math.sin(a) * rad;
        const z_m = Math.cos(a) * rad;
        const y_m = 2 + r * 4.5;
        const pix = seatToPixel(x_m, y_m, z_m);
        out.push(
          makeTarget({
            id: "aisle:" + deg + ":" + r,
            kind: "aisle",
            zone: ZONE.aisle,
            label: "Aisle " + deg + "°",
            section: "aisle",
            chunk: "chunk:aisle:" + deg,
            x_m: x_m,
            y_m: y_m,
            z_m: z_m,
            x: x_m / Rd,
            y: (y_m / (c.height_m || 111.6) - 0.5) * 2,
            z: z_m / Rd,
            px: pix.px,
            py: pix.py,
          })
        );
      }
    });

    // Openings / exits / entries
    const openings = [
      { x: 0, y: 2, z: 55, label: "Main Entrance" },
      { x: -45, y: 2, z: 40, label: "West Entry" },
      { x: 45, y: 2, z: 40, label: "East Entry" },
      { x: -50, y: 8, z: 15, label: "Exit SW" },
      { x: 50, y: 8, z: 15, label: "Exit SE" },
      { x: -50, y: 8, z: -15, label: "Exit NW" },
      { x: 50, y: 8, z: -15, label: "Exit NE" },
      { x: 0, y: 12, z: 58, label: "Lobby Opening" },
      { x: -25, y: 25, z: 48, label: "Upper Exit West" },
      { x: 25, y: 25, z: 48, label: "Upper Exit East" },
    ];
    openings.forEach(function (o, i) {
      for (let dx = -1; dx <= 1; dx++) {
        for (let dz = -1; dz <= 1; dz++) {
          const x_m = o.x + dx * 1.2;
          const z_m = o.z + dz * 1.2;
          const y_m = o.y;
          const pix = seatToPixel(x_m, y_m, z_m);
          out.push(
            makeTarget({
              id: "opening:" + i + ":" + dx + ":" + dz,
              kind: "opening",
              zone: ZONE.opening,
              label: o.label,
              section: "opening",
              chunk: "chunk:opening:" + i,
              x_m: x_m,
              y_m: y_m,
              z_m: z_m,
              x: x_m / Rd,
              y: (y_m / (c.height_m || 111.6) - 0.5) * 2,
              z: z_m / Rd,
              px: pix.px,
              py: pix.py,
            })
          );
        }
      }
    });

    // Exterior parking lot (outside dome footprint — lidar ring)
    for (let ring = 0; ring < 4; ring++) {
      const rad = Rd + 25 + ring * 18;
      const nPts = 36 + ring * 12;
      for (let i = 0; i < nPts; i++) {
        const a = (i / nPts) * Math.PI * 2;
        const x_m = Math.sin(a) * rad;
        const z_m = Math.cos(a) * rad;
        const y_m = 0.5 + (i % 3) * 0.2;
        // parking maps to edge of LED unwrap (az still valid; el low)
        const pix = seatToPixel(x_m * 0.35, 8 + ring * 2, z_m * 0.35);
        out.push(
          makeTarget({
            id: "parking:R" + ring + ":" + i,
            kind: "parking",
            zone: ZONE.parking,
            label: "Parking R" + ring,
            section: "parking",
            chunk: "chunk:parking:R" + ring,
            x_m: x_m,
            y_m: y_m,
            z_m: z_m,
            x: (x_m / Rd) * 1.15,
            y: -0.95 + ring * 0.04,
            z: (z_m / Rd) * 1.15,
            px: pix.px,
            py: pix.py,
            castable: true,
          })
        );
      }
    }

    // Screen sample grid (addressable LED patches — not every pixel)
    const azSamples = 48;
    const elSamples = 28;
    for (let ei = 0; ei < elSamples; ei++) {
      for (let ai = 0; ai < azSamples; ai++) {
        const px = Math.floor((ai / (azSamples - 1)) * (RES - 1));
        const py = Math.floor((ei / (elSamples - 1)) * (RES - 1));
        const w = pixelToWorld(px, py, c);
        const tileX = Math.floor(ai / 6);
        const tileY = Math.floor(ei / 4);
        out.push(
          makeTarget({
            id: "screen:" + px + "x" + py,
            kind: "screen",
            zone: ZONE.screen,
            label: "LED " + px + "," + py,
            section: "screen",
            chunk: "chunk:screen:T" + tileX + "_" + tileY,
            x_m: w.x_m,
            y_m: w.y_m,
            z_m: w.z_m,
            x: w.x,
            y: w.y,
            z: w.z,
            px: px,
            py: py,
          })
        );
      }
    }

    return out;
  }

  // ── index ──

  function buildIndex(targets) {
    const byId = new Map();
    const byZone = new Map();
    const bySection = new Map();
    const byChunk = new Map();
    const hash = new Map(); // "hx,hy" → target ids

    function addList(map, key, id) {
      if (!key) return;
      let a = map.get(key);
      if (!a) {
        a = [];
        map.set(key, a);
      }
      a.push(id);
    }

    for (let i = 0; i < targets.length; i++) {
      const t = targets[i];
      byId.set(t.id, t);
      addList(byZone, t.zone, t.id);
      addList(bySection, t.section, t.id);
      addList(byChunk, t.chunk, t.id);
      const hx = Math.floor(t.px / CELL);
      const hy = Math.floor(t.py / CELL);
      const hk = hx + "," + hy;
      addList(hash, hk, t.id);
    }

    return { byId: byId, byZone: byZone, bySection: bySection, byChunk: byChunk, hash: hash };
  }

  function nearestInHash(index, px, py, maxDist) {
    maxDist = maxDist == null ? 400 : maxDist;
    const hx = Math.floor(px / CELL);
    const hy = Math.floor(py / CELL);
    let best = null;
    let bestD = maxDist * maxDist;
    for (let dx = -1; dx <= 1; dx++) {
      for (let dy = -1; dy <= 1; dy++) {
        const ids = index.hash.get(hx + dx + "," + (hy + dy));
        if (!ids) continue;
        for (let i = 0; i < ids.length; i++) {
          const t = index.byId.get(ids[i]);
          if (!t) continue;
          const d = (t.px - px) * (t.px - px) + (t.py - py) * (t.py - py);
          if (d < bestD) {
            bestD = d;
            best = t;
          }
        }
      }
    }
    // fallback: free LED pixel as virtual target
    if (!best) {
      const w = pixelToWorld(px, py);
      return makeTarget({
        id: "led:" + clampPx(px) + "x" + clampPx(py),
        kind: "led",
        zone: ZONE.screen,
        label: "LED free " + clampPx(px) + "," + clampPx(py),
        section: "screen",
        chunk: "chunk:screen:free",
        x_m: w.x_m,
        y_m: w.y_m,
        z_m: w.z_m,
        x: w.x,
        y: w.y,
        z: w.z,
        px: clampPx(px),
        py: clampPx(py),
      });
    }
    return best;
  }

  // ── venue build ──

  let _venue = null;

  function buildVenue(opts) {
    opts = opts || {};
    const S = root.GY_SPHERE;
    if (!S) throw new Error("GY_SPHERE required before GY_VENUE.buildVenue()");
    const seats = S.seatsCached();
    const seatTargets = targetsFromSeats(seats);
    const infra = generateInfraTargets();
    const targets = seatTargets.concat(infra);
    const index = buildIndex(targets);

    // section list + chunk list for bulk UI
    const sections = [];
    const sectionSet = new Set();
    const chunks = [];
    const chunkSet = new Set();
    for (let i = 0; i < targets.length; i++) {
      const t = targets[i];
      if (t.section && !sectionSet.has(t.section)) {
        sectionSet.add(t.section);
        sections.push(t.section);
      }
      if (t.chunk && !chunkSet.has(t.chunk)) {
        chunkSet.add(t.chunk);
        chunks.push(t.chunk);
      }
    }
    sections.sort();
    chunks.sort();

    _venue = {
      res: RES,
      cell: CELL,
      seats: seats.length,
      targets: targets,
      index: index,
      sections: sections,
      chunks: chunks,
      zones: Object.keys(ZONE_META),
      zoneMeta: ZONE_META,
      builtAt: Date.now(),
    };
    return _venue;
  }

  function venue() {
    if (!_venue) buildVenue();
    return _venue;
  }

  // ── bulk activate ──

  /**
   * Bulk activate targets for cast / highlight.
   * query examples:
   *   { section: "200" }
   *   { chunk: "chunk:200:R1C2" }
   *   { zone: "stage" }
   *   { zones: ["aisle","opening"] }
   *   { sections: ["100","200"] }
   *   { px: 8000, py: 4000 }           // single LED / nearest
   *   { rect: { x0,y0,x1,y1 } }        // pixel rectangle
   *   { seat: "200-R5-C12" }
   *   { all: true }
   *   { limit: 500 }
   */
  function bulkActivate(query) {
    query = query || {};
    const v = venue();
    const ids = new Set();
    const limit = query.limit > 0 ? query.limit : 5000;

    function addIds(list) {
      if (!list) return;
      for (let i = 0; i < list.length && ids.size < limit; i++) ids.add(list[i]);
    }

    if (query.all) {
      for (let i = 0; i < v.targets.length && ids.size < limit; i++) {
        if (v.targets[i].castable) ids.add(v.targets[i].id);
      }
    }
    if (query.section) addIds(v.index.bySection.get(String(query.section)));
    if (query.sections) {
      query.sections.forEach(function (s) {
        addIds(v.index.bySection.get(String(s)));
      });
    }
    if (query.chunk) addIds(v.index.byChunk.get(String(query.chunk)));
    if (query.chunks) {
      query.chunks.forEach(function (c) {
        addIds(v.index.byChunk.get(String(c)));
      });
    }
    if (query.zone) addIds(v.index.byZone.get(String(query.zone)));
    if (query.zones) {
      query.zones.forEach(function (z) {
        addIds(v.index.byZone.get(String(z)));
      });
    }
    if (query.seat || query.id) {
      const q = query.seat || query.id;
      const S = root.GY_SPHERE;
      if (String(q).indexOf("seat:") === 0 || String(q).indexOf(":") >= 0) {
        if (v.index.byId.has(String(q))) ids.add(String(q));
      }
      if (S) {
        const seat = S.findSeat(q);
        if (seat) ids.add("seat:" + seat.id);
      }
      if (v.index.byId.has(String(q))) ids.add(String(q));
    }
    if (query.px != null && query.py != null) {
      const t = nearestInHash(v.index, +query.px, +query.py, query.maxDist);
      if (t) {
        // register free LED if new
        if (!v.index.byId.has(t.id)) {
          v.targets.push(t);
          v.index.byId.set(t.id, t);
        }
        ids.add(t.id);
      }
    }
    if (query.rect) {
      const r = query.rect;
      const x0 = clampPx(Math.min(r.x0, r.x1));
      const x1 = clampPx(Math.max(r.x0, r.x1));
      const y0 = clampPx(Math.min(r.y0, r.y1));
      const y1 = clampPx(Math.max(r.y0, r.y1));
      const step = query.step > 0 ? query.step : 128;
      for (let py = y0; py <= y1 && ids.size < limit; py += step) {
        for (let px = x0; px <= x1 && ids.size < limit; px += step) {
          const t = nearestInHash(v.index, px, py, step * 2);
          if (t) {
            if (!v.index.byId.has(t.id)) {
              v.targets.push(t);
              v.index.byId.set(t.id, t);
            }
            ids.add(t.id);
          }
        }
      }
    }

    const list = [];
    ids.forEach(function (id) {
      const t = v.index.byId.get(id);
      if (t) list.push(t);
    });
    return {
      count: list.length,
      ids: list.map(function (t) {
        return t.id;
      }),
      targets: list,
      query: query,
    };
  }

  /** Resolve any cast query → mesh pos (for phone / sphere). */
  function resolvePos(query) {
    query = query || {};
    const v = venue();
    let t = null;

    if (query.target || query.id) {
      t = v.index.byId.get(String(query.target || query.id));
    }
    if (!t && query.seat) {
      const S = root.GY_SPHERE;
      const seat = S && S.findSeat(query.seat);
      if (seat) t = v.index.byId.get("seat:" + seat.id);
    }
    if (!t && query.px != null && query.py != null) {
      // Professional cast path: explicit px,py is an exact free LED address
      // (unless snap:true to nearest seat/infra for director convenience).
      if (query.snap) {
        t = nearestInHash(v.index, +query.px, +query.py, query.maxDist || 600);
      } else {
        const w = pixelToWorld(+query.px, +query.py);
        t = makeTarget({
          id: "led:" + clampPx(+query.px) + "x" + clampPx(+query.py),
          kind: "led",
          zone: ZONE.screen,
          label: "LED " + clampPx(+query.px) + "," + clampPx(+query.py),
          section: "screen",
          chunk: "chunk:screen:free",
          x_m: w.x_m,
          y_m: w.y_m,
          z_m: w.z_m,
          x: w.x,
          y: w.y,
          z: w.z,
          px: clampPx(+query.px),
          py: clampPx(+query.py),
        });
      }
      if (t && !v.index.byId.has(t.id)) {
        v.targets.push(t);
        v.index.byId.set(t.id, t);
      }
    }
    if (!t && query.section && query.row != null && query.col != null) {
      const S = root.GY_SPHERE;
      const seat = S && S.findSeat({ section: query.section, row: query.row, col: query.col });
      if (seat) t = v.index.byId.get("seat:" + seat.id);
    }
    if (!t) return null;

    return targetToMeshPos(t);
  }

  function targetToMeshPos(t) {
    if (!t) return null;
    return {
      map: "sphere-vegas-16k",
      addressable: true,
      target: t.id,
      kind: t.kind,
      zone: t.zone,
      section: t.section,
      chunk: t.chunk,
      id: t.label,
      seatId: t.kind === "seat" ? t.label : null,
      idx: t.seatIdx,
      row: t.row,
      col: t.col,
      x: t.x,
      y: t.y,
      z: t.z,
      x_m: t.x_m,
      y_m: t.y_m,
      z_m: t.z_m,
      px: t.px,
      py: t.py,
      res: RES,
    };
  }

  function findTarget(id) {
    return venue().index.byId.get(String(id)) || null;
  }

  function listChunks(section) {
    const v = venue();
    if (!section) return v.chunks.slice();
    const prefix = "chunk:" + section + ":";
    return v.chunks.filter(function (c) {
      return c.indexOf(prefix) === 0 || (section === "screen" && c.indexOf("chunk:screen:") === 0);
    });
  }

  function meta() {
    const v = venue();
    const byZone = {};
    v.zones.forEach(function (z) {
      byZone[z] = (v.index.byZone.get(z) || []).length;
    });
    return {
      res: RES,
      cell: CELL,
      targets: v.targets.length,
      seats: v.seats,
      sections: v.sections.length,
      chunks: v.chunks.length,
      zones: byZone,
      layers: [
        "L0 blueprint zones",
        "L1 targets",
        "L2 16K index",
        "L3 bulk activate",
        "L4 cast pos",
        "L5 screen LED",
      ],
    };
  }

  function summary() {
    const m = meta();
    return (
      "Venue 16K · " +
      m.targets.toLocaleString() +
      " targets · " +
      m.seats.toLocaleString() +
      " seats · " +
      Object.keys(m.zones).length +
      " zones"
    );
  }

  return {
    RES: RES,
    CELL: CELL,
    ZONE: ZONE,
    ZONE_META: ZONE_META,
    buildVenue: buildVenue,
    venue: venue,
    bulkActivate: bulkActivate,
    resolvePos: resolvePos,
    targetToMeshPos: targetToMeshPos,
    findTarget: findTarget,
    nearestPixel: function (px, py, maxDist) {
      return nearestInHash(venue().index, px, py, maxDist);
    },
    pixelToWorld: pixelToWorld,
    listChunks: listChunks,
    meta: meta,
    summary: summary,
  };
});
