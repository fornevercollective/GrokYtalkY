/**
 * Sphere Vegas — Bloch³ seating map
 * Source architecture: https://mueee.qbitos.ai/qpu-pointcloud.html
 * (SPHERE VEGAS — BLOCH³ → ~18,600 seats · 16K×16K LED · speaker beams)
 *
 * Placement authority for Stadium Glyph infinite canvas:
 *   seat → (x,y,z)m → Bloch (θ,φ) → screen px/py → speaker array
 *
 * No heavy deps. Browser + Node (via global or export).
 */
(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) {
    module.exports = api;
  }
  root.GY_SPHERE = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const SPHERE_VEGAS = {
    name: "Sphere Las Vegas",
    source: "mueee.qbitos.ai/qpu-pointcloud.html",
    height_ft: 366,
    width_ft: 516,
    height_m: 111.6,
    width_m: 157.3,
    dome_radius_m: 78.6,
    stage_height_m: 6.1,
    center_m: { x: 0, y: 55.8, z: 0 },
    // published venue targets (generator recomputes seats from sections)
    seats_target: 18600,
    haptic_target: 10000,
    floor_target: 1400,
    total_capacity: 20000,
    speakers: 167000,
    speaker_arrays: 1586,
    mobile_arrays: 300,
    proscenium_arrays: 464,
    led_interior_sqft: 160000,
    led_exterior_sqft: 580000,
    led_panels: 64000,
    res: 16000, // 16K×16K interior screen mapping
    pixels: 256000000,
    suites: 23,
    levels: 9,
    seat_width_m: 0.508,
    seat_depth_m: 0.762,
    row_spacing_m: 0.914,
    // Density tuned so generateSphereSeats() ≈ published 18,600 (procedural, not CAD).
    sections: {
      100: {
        rows: 16,
        cols_per_row: 148,
        start_angle: -70,
        end_angle: 70,
        radius_m: 22,
        y_base_m: 7.3,
        y_top_m: 16.5,
        rake_deg: 25,
        haptic: true,
        obstructed_rows: [0, 1],
      },
      200: {
        rows: 22,
        cols_per_row: 210,
        start_angle: -80,
        end_angle: 80,
        radius_m: 32,
        y_base_m: 16.8,
        y_top_m: 35.7,
        rake_deg: 30,
        haptic: true,
        obstructed_rows: [],
      },
      300: {
        rows: 26,
        cols_per_row: 248,
        start_angle: -85,
        end_angle: 85,
        radius_m: 42,
        y_base_m: 24,
        y_top_m: 48,
        rake_deg: 33,
        haptic: true,
        obstructed_rows: [],
      },
      400: {
        rows: 20,
        cols_per_row: 180,
        start_angle: -75,
        end_angle: 75,
        radius_m: 55,
        y_base_m: 38,
        y_top_m: 58,
        rake_deg: 35,
        haptic: false,
        obstructed_rows: [],
      },
      500: {
        rows: 16,
        cols_per_row: 148,
        start_angle: -60,
        end_angle: 60,
        radius_m: 65,
        y_base_m: 52,
        y_top_m: 68,
        rake_deg: 38,
        haptic: false,
        obstructed_rows: [],
      },
      floor: {
        rows: 12,
        cols_per_row: 56,
        start_angle: -30,
        end_angle: 30,
        radius_m: 10,
        y_base_m: 0.3,
        y_top_m: 6,
        rake_deg: 0,
        haptic: false,
        obstructed_rows: [],
      },
    },
    screen: {
      radius_m: 75,
      az_start_deg: -100,
      az_end_deg: 100,
      el_start_m: 5,
      el_end_m: 105,
      panel_tolerance_mm: 0.8,
    },
  };

  const R_DOME = SPHERE_VEGAS.dome_radius_m;
  const QWERTY_KEYS = "qwertyuiopasdfghjklzxcvbnm ".split("");

  /** Unit Bloch vector from seat meters (origin = dome center_m.y adjusted by caller). */
  function seatToBloch(x_m, y_m, z_m) {
    const r = Math.sqrt(x_m * x_m + y_m * y_m + z_m * z_m) || 1;
    const theta = Math.acos(Math.max(-1, Math.min(1, y_m / r)));
    const phi = Math.atan2(x_m, z_m);
    return {
      theta,
      phi,
      blochX: Math.sin(theta) * Math.cos(phi),
      blochY: Math.sin(theta) * Math.sin(phi),
      blochZ: Math.cos(theta),
    };
  }

  /** Map seat meters → interior 16K×16K LED pixel (az/el unwrap). */
  function seatToPixel(x_m, y_m, z_m, cfg) {
    cfg = cfg || SPHERE_VEGAS;
    const az = Math.atan2(x_m, z_m);
    const azStart = (cfg.screen.az_start_deg * Math.PI) / 180;
    const azEnd = (cfg.screen.az_end_deg * Math.PI) / 180;
    const azFrac = Math.max(0, Math.min(1, (az - azStart) / (azEnd - azStart)));
    const elFrac = Math.max(
      0,
      Math.min(
        1,
        (y_m - cfg.screen.el_start_m) / (cfg.screen.el_end_m - cfg.screen.el_start_m)
      )
    );
    return {
      px: Math.floor(azFrac * cfg.res),
      py: Math.floor(elFrac * cfg.res),
      azFrac,
      elFrac,
    };
  }

  /** Nearest speaker array + beam angles for seat. */
  function seatToSpeaker(x_m, y_m, z_m, cfg) {
    cfg = cfg || SPHERE_VEGAS;
    const az = Math.atan2(x_m, z_m);
    const el = Math.atan2(y_m, Math.sqrt(x_m * x_m + z_m * z_m));
    const azSlot = Math.floor(((az + Math.PI) / (2 * Math.PI)) * cfg.proscenium_arrays);
    const elSlot = Math.floor(
      ((el + Math.PI / 2) / Math.PI) * (cfg.speaker_arrays - cfg.proscenium_arrays)
    );
    const arrayIdx = Math.min(cfg.speaker_arrays - 1, azSlot + elSlot);
    const driversPerArray = Math.floor(cfg.speakers / cfg.speaker_arrays);
    return {
      arrayIdx,
      driverStart: arrayIdx * driversPerArray,
      driverEnd: (arrayIdx + 1) * driversPerArray - 1,
      beamAz: az,
      beamEl: el,
      distance_m: Math.sqrt(x_m * x_m + y_m * y_m + z_m * z_m),
    };
  }

  /**
   * Generate full seating chart (procedural — not official CAD).
   * ~nominal seats from section rows×cols (cols taper per row).
   */
  function generateSphereSeats(cfg) {
    cfg = cfg || SPHERE_VEGAS;
    const seats = [];
    const secs = cfg.sections;
    let globalIdx = 0;
    for (const secName of Object.keys(secs)) {
      const sec = secs[secName];
      const angRange = ((sec.end_angle - sec.start_angle) * Math.PI) / 180;
      const angStart = (sec.start_angle * Math.PI) / 180;
      const rakeRad = (sec.rake_deg * Math.PI) / 180;
      for (let r = 0; r < sec.rows; r++) {
        const rowFrac = r / (sec.rows - 1 || 1);
        const y_m = sec.y_base_m + (sec.y_top_m - sec.y_base_m) * rowFrac;
        const rakeOffset = Math.tan(rakeRad) * r * cfg.row_spacing_m;
        // mild bowl taper (edge rows slightly fewer seats)
        const nCols = Math.max(
          4,
          Math.round(sec.cols_per_row * (1 - Math.abs(rowFrac - 0.5) * 0.06))
        );
        for (let c = 0; c < nCols; c++) {
          const colFrac = c / (nCols - 1 || 1);
          const ang = angStart + angRange * colFrac;
          const x_m = Math.sin(ang) * sec.radius_m;
          const z_m = Math.cos(ang) * sec.radius_m;
          const norm_x = x_m / cfg.dome_radius_m;
          const norm_y = (y_m / cfg.height_m - 0.5) * 2;
          const norm_z = z_m / cfg.dome_radius_m;
          const bloch = seatToBloch(x_m, y_m - cfg.center_m.y, z_m);
          const pixel = seatToPixel(x_m, y_m, z_m, cfg);
          const speaker = seatToSpeaker(x_m, y_m - cfg.center_m.y, z_m, cfg);
          const obstructed = (sec.obstructed_rows || []).indexOf(r) >= 0;
          seats.push({
            idx: globalIdx,
            id: seatId(secName, r, c),
            section: secName,
            row: r,
            col: c,
            x_m,
            y_m,
            z_m,
            x: norm_x,
            y: norm_y,
            z: norm_z,
            theta: bloch.theta,
            phi: bloch.phi,
            blochX: bloch.blochX,
            blochY: bloch.blochY,
            blochZ: bloch.blochZ,
            px: pixel.px,
            py: pixel.py,
            azFrac: pixel.azFrac,
            elFrac: pixel.elFrac,
            speakerArray: speaker.arrayIdx,
            speakerDrivers: [speaker.driverStart, speaker.driverEnd],
            beamAz: speaker.beamAz,
            beamEl: speaker.beamEl,
            distance_m: speaker.distance_m,
            haptic: !!(sec.haptic && r < Math.floor(sec.rows * 0.7)),
            obstructed,
            rake_m: rakeOffset,
            kbOrigin: QWERTY_KEYS[globalIdx % QWERTY_KEYS.length],
          });
          globalIdx++;
        }
      }
    }
    return seats;
  }

  function seatId(section, row, col) {
    return String(section) + "-R" + row + "-C" + col;
  }

  /** Parse "200-R5-C12" | "floor-R0-C3" | "1234" (idx) | {section,row,col} */
  function parseSeatQuery(q) {
    if (q == null || q === "") return null;
    if (typeof q === "object") {
      if (q.section != null && q.row != null && q.col != null) {
        return { section: String(q.section), row: +q.row, col: +q.col };
      }
      if (q.idx != null) return { idx: +q.idx };
      return null;
    }
    const s = String(q).trim();
    if (/^\d+$/.test(s)) return { idx: parseInt(s, 10) };
    const m = s.match(/^([a-zA-Z0-9]+)-R(\d+)-C(\d+)$/i);
    if (m) return { section: m[1], row: parseInt(m[2], 10), col: parseInt(m[3], 10) };
    // section only
    if (/^(floor|100|200|300|400|500)$/i.test(s)) return { section: s.toLowerCase() === "floor" ? "floor" : s };
    return null;
  }

  let _cache = null;
  function seatsCached() {
    if (!_cache) _cache = generateSphereSeats();
    return _cache;
  }

  function findSeat(query) {
    const q = parseSeatQuery(query);
    if (!q) return null;
    const seats = seatsCached();
    if (q.idx != null) {
      if (q.idx < 0 || q.idx >= seats.length) return null;
      return seats[q.idx];
    }
    if (q.section != null && q.row != null && q.col != null) {
      for (let i = 0; i < seats.length; i++) {
        const s = seats[i];
        if (String(s.section) === String(q.section) && s.row === q.row && s.col === q.col) {
          return s;
        }
      }
      return null;
    }
    if (q.section != null) {
      // first seat in section
      for (let i = 0; i < seats.length; i++) {
        if (String(seats[i].section) === String(q.section)) return seats[i];
      }
    }
    return null;
  }

  /** Stadium Glyph mesh `pos` payload from a seat. */
  function seatToMeshPos(seat) {
    if (!seat) return null;
    return {
      map: "sphere-vegas-bloch3",
      idx: seat.idx,
      id: seat.id,
      section: seat.section,
      row: seat.row,
      col: seat.col,
      x: seat.x,
      y: seat.y,
      z: seat.z,
      x_m: seat.x_m,
      y_m: seat.y_m,
      z_m: seat.z_m,
      theta: seat.theta,
      phi: seat.phi,
      bloch: [seat.blochX, seat.blochY, seat.blochZ],
      px: seat.px,
      py: seat.py,
      res: SPHERE_VEGAS.res,
      speakerArray: seat.speakerArray,
      distance_m: seat.distance_m,
      haptic: seat.haptic,
      obstructed: seat.obstructed,
    };
  }

  /** Virtual canvas coords from seat (normalized 0..1 for az/el screen frac, or px). */
  function seatToCanvasUV(seat) {
    if (!seat) return null;
    return {
      u: seat.azFrac,
      v: seat.elFrac,
      px: seat.px,
      py: seat.py,
      res: SPHERE_VEGAS.res,
    };
  }

  function meta() {
    const seats = seatsCached();
    const bySec = {};
    let haptic = 0;
    for (let i = 0; i < seats.length; i++) {
      const s = seats[i];
      bySec[s.section] = (bySec[s.section] || 0) + 1;
      if (s.haptic) haptic++;
    }
    return {
      name: SPHERE_VEGAS.name,
      source: SPHERE_VEGAS.source,
      seats: seats.length,
      seats_target: SPHERE_VEGAS.seats_target,
      haptic,
      sections: bySec,
      res: SPHERE_VEGAS.res,
      pixels: SPHERE_VEGAS.pixels,
      dome_radius_m: SPHERE_VEGAS.dome_radius_m,
      speakers: SPHERE_VEGAS.speakers,
      speaker_arrays: SPHERE_VEGAS.speaker_arrays,
    };
  }

  function summary() {
    const m = meta();
    return (
      "Sphere Vegas · " +
      m.seats.toLocaleString() +
      " seats (target " +
      m.seats_target.toLocaleString() +
      ") · Bloch³ · " +
      m.res +
      "×" +
      m.res +
      " · haptic " +
      m.haptic.toLocaleString()
    );
  }

  return {
    SPHERE_VEGAS,
    R_DOME,
    seatToBloch,
    seatToPixel,
    seatToSpeaker,
    generateSphereSeats,
    seatId,
    parseSeatQuery,
    findSeat,
    seatToMeshPos,
    seatToCanvasUV,
    seatsCached,
    meta,
    summary,
  };
});
