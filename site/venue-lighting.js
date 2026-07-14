/**
 * Venue lighting — global wash + key/fill + phone flashlights.
 *
 * Mesh:
 *   type:venue-light { kind:"flashlight"|"wash", from, on, intensity, pos, color, t }
 *   type:camera-controls { look:{ torch:true, … }, from }  → flashlight on
 *   vburst-frame.look.torch + pos → flashlight at cast seat
 *
 * Integrates with sphere point shading (sampleAt) and phone torch hardware.
 */
(function (root, factory) {
  const api = factory(root);
  if (typeof module === "object" && module.exports) module.exports = api;
  root.GY_LIGHT = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  const DEFAULTS = {
    ambient: 0.28,
    key: 0.72,
    keyAz: -0.35, // radians-ish offset around bowl
    keyEl: 0.55,
    fill: 0.22,
    stageWash: 0.55,
    stageZ: -12, // meters-ish center stage in bowl coords
    stageRadius: 28,
    exposure: 1.05,
    flashIntensity: 1.35,
    flashRadius: 22, // world units (normalized*3 space ≈ meters/scale)
    flashFalloff: 1.6,
    flashColor: [1.0, 0.94, 0.82],
  };

  function cloneLight(o) {
    return Object.assign({}, DEFAULTS, o || {});
  }

  function createState(opts) {
    return {
      params: cloneLight(opts),
      /** nick → { on, intensity, x,y,z, color, t, from } in render space */
      flashlights: new Map(),
      TTL: 5000,
    };
  }

  function setParams(state, patch) {
    if (!state) return;
    Object.keys(patch || {}).forEach(function (k) {
      if (DEFAULTS[k] !== undefined) state.params[k] = patch[k];
    });
  }

  function upsertFlashlight(state, msg) {
    if (!state || !msg) return;
    const from = String(msg.from || msg.nick || "phone").slice(0, 24);
    const on =
      msg.on !== false &&
      (msg.on === true ||
        msg.kind === "flashlight" ||
        (msg.look && msg.look.torch) ||
        msg.torch === true);
    if (!on && msg.on === false) {
      state.flashlights.delete(from);
      return;
    }
    // position: prefer pos meters/normalized, else 0
    let x = 0,
      y = 0.5,
      z = 0;
    const p = msg.pos || msg.eye || null;
    if (p) {
      if (p.x != null && Math.abs(p.x) <= 3) {
        x = p.x * 3;
        y = (p.y != null ? p.y : 0) * 1.5;
        z = (p.z != null ? p.z : 0) * 3;
      } else if (p.x_m != null) {
        // crude: same scale as sphere seats (x_m / dome * 3)
        const Rd = 78.6;
        x = (p.x_m / Rd) * 3;
        y = ((p.y_m / 111.6 - 0.5) * 2) * 1.5;
        z = (p.z_m / Rd) * 3;
      }
    }
    const intensity =
      msg.intensity != null
        ? +msg.intensity
        : msg.look && msg.look.fill
          ? 0.8 + msg.look.fill
          : state.params.flashIntensity;
    const color = msg.color || state.params.flashColor;
    if (!on) {
      state.flashlights.delete(from);
      return;
    }
    state.flashlights.set(from, {
      from: from,
      on: true,
      intensity: intensity,
      x: x,
      y: y,
      z: z,
      color: color,
      t: Date.now(),
    });
  }

  function prune(state, now) {
    if (!state) return;
    now = now || Date.now();
    state.flashlights.forEach(function (f, k) {
      if (now - f.t > state.TTL) state.flashlights.delete(k);
    });
  }

  /**
   * Sample lighting at a render-space point.
   * @returns { r,g,b, gain }  gain multiplies base brightness
   */
  function sampleAt(state, px, py, pz, opts) {
    opts = opts || {};
    const p = state.params;
    const nearStage = opts.nearStage || Math.hypot(px, pz - p.stageZ * 0.04) < p.stageRadius * 0.05;

    // key direction (world-ish)
    const kx = Math.sin(p.keyAz) * Math.cos(p.keyEl);
    const ky = Math.sin(p.keyEl);
    const kz = Math.cos(p.keyAz) * Math.cos(p.keyEl);
    // approximate normal: outward from origin (dome)
    const len = Math.hypot(px, py, pz) || 1;
    const nx = px / len;
    const ny = py / len;
    const nz = pz / len;
    const ndotl = Math.max(0, nx * kx + ny * ky + nz * kz);

    let gain = p.ambient + p.key * ndotl + p.fill * (0.5 + 0.5 * (1 - ndotl));
    if (nearStage || (opts.zone === "stage" || opts.zone === "proscenium")) {
      gain += p.stageWash * 0.45;
    }
    gain *= p.exposure;

    let r = 1,
      g = 1,
      b = 1;
    // flashlights
    state.flashlights.forEach(function (f) {
      if (!f.on) return;
      const dx = px - f.x;
      const dy = py - f.y;
      const dz = pz - f.z;
      const d2 = dx * dx + dy * dy + dz * dz;
      const rad = p.flashRadius * 0.12; // scale to render units
      const att = f.intensity / (1 + Math.pow(d2 / (rad * rad + 0.001), p.flashFalloff * 0.5));
      if (att < 0.01) return;
      const c = f.color || p.flashColor;
      r += c[0] * att * 0.85;
      g += c[1] * att * 0.85;
      b += c[2] * att * 0.85;
      gain += att * 0.55;
    });

    return {
      r: Math.min(2.2, r),
      g: Math.min(2.2, g),
      b: Math.min(2.2, b),
      gain: Math.min(2.5, Math.max(0.05, gain)),
    };
  }

  function meshFlashlight(from, on, pos, intensity) {
    return {
      type: "venue-light",
      kind: "flashlight",
      from: from || "phone",
      on: !!on,
      intensity: intensity != null ? intensity : 1.2,
      pos: pos || null,
      color: DEFAULTS.flashColor.slice(),
      t: Date.now(),
    };
  }

  function meshWash(params, from) {
    return {
      type: "venue-light",
      kind: "wash",
      from: from || "sphere",
      params: cloneLight(params),
      t: Date.now(),
    };
  }

  function applyMeshMessage(state, msg) {
    if (!msg || !state) return false;
    if (msg.type === "venue-light") {
      if (msg.kind === "wash" && msg.params) {
        setParams(state, msg.params);
        return true;
      }
      if (msg.kind === "flashlight" || msg.on != null || msg.torch != null) {
        upsertFlashlight(state, msg);
        return true;
      }
    }
    if (msg.type === "camera-controls" && msg.look) {
      upsertFlashlight(state, {
        from: msg.from,
        on: !!msg.look.torch,
        look: msg.look,
        pos: msg.pos || null,
        intensity: msg.look.torch ? 1.2 + (msg.look.fill || 0) : 0,
      });
      return !!msg.look.torch || msg.look.torch === false;
    }
    if ((msg.type === "vburst-frame" || msg.type === "gyst") && msg.look && msg.look.torch) {
      upsertFlashlight(state, {
        from: msg.from,
        on: true,
        look: msg.look,
        pos: msg.pos,
        intensity: 1.15 + (msg.look.fill || 0) * 0.5,
      });
      return true;
    }
    return false;
  }

  function listFlashlights(state) {
    const out = [];
    if (!state) return out;
    state.flashlights.forEach(function (f) {
      out.push(f);
    });
    return out;
  }

  function summary(state) {
    if (!state) return "lights off";
    const p = state.params;
    return (
      "amb=" +
      p.ambient.toFixed(2) +
      " key=" +
      p.key.toFixed(2) +
      " fill=" +
      p.fill.toFixed(2) +
      " stage=" +
      p.stageWash.toFixed(2) +
      " · 🔦" +
      state.flashlights.size
    );
  }

  return {
    DEFAULTS: DEFAULTS,
    cloneLight: cloneLight,
    createState: createState,
    setParams: setParams,
    upsertFlashlight: upsertFlashlight,
    prune: prune,
    sampleAt: sampleAt,
    meshFlashlight: meshFlashlight,
    meshWash: meshWash,
    applyMeshMessage: applyMeshMessage,
    listFlashlights: listFlashlights,
    summary: summary,
  };
});
