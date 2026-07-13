/**
 * Phone / film camera + lighting controls (aligned with aito adjustments).
 * - Software grade: exposure, contrast, sat, temperature, tint, clarity, …
 * - Hardware intent: MediaTrackConstraints (iso, exposureCompensation, wb, torch, focus, zoom)
 * - Mesh: type:camera-controls { look }
 * - Apply to ImageData / canvas / luminance for glyph-cast
 */
(function (global) {
  "use strict";

  var DEFAULTS = {
    facing: "environment",
    focus_mode: "continuous",
    focus_distance: 0,
    exposure_mode: "continuous",
    ev: 0,
    shutter: "",
    iso: 0,
    wb_mode: "auto",
    color_temp_k: 0,
    torch: false,
    zoom: 1,
    fps: 30,
    exposure: 0,
    contrast: 0,
    saturation: 0,
    temperature: 0,
    tint: 0,
    clarity: 0,
    sharpen: 0,
    vignette: 0,
    brightness: 0,
    highlights: 0,
    shadows: 0,
    fill: 0,
    key: 0,
    night: false,
    grain: 0,
    preset: "neutral",
    lut: "",
    lut_intensity: 0,
  };

  function clone(o) {
    return Object.assign({}, DEFAULTS, o || {});
  }

  function clamp(v, lo, hi) {
    return Math.max(lo, Math.min(hi, v));
  }

  function applyPreset(look, name) {
    var l = clone(look);
    name = String(name || "neutral").toLowerCase();
    l.preset = name;
    function resetGrade() {
      l.exposure = l.contrast = l.saturation = 0;
      l.temperature = l.tint = l.clarity = l.sharpen = l.vignette = 0;
      l.brightness = l.highlights = l.shadows = l.fill = l.grain = 0;
      l.night = false;
    }
    switch (name) {
      case "neutral":
      case "reset":
        resetGrade();
        l.wb_mode = "auto";
        break;
      case "daylight":
      case "day":
        l.wb_mode = "daylight";
        l.color_temp_k = 5600;
        l.temperature = 0.05;
        l.contrast = 0.08;
        break;
      case "cloudy":
        l.wb_mode = "cloudy";
        l.color_temp_k = 6500;
        l.temperature = 0.2;
        l.tint = -0.05;
        break;
      case "tungsten":
      case "warm":
        l.wb_mode = "tungsten";
        l.color_temp_k = 3200;
        l.temperature = -0.35;
        l.tint = 0.05;
        break;
      case "fluorescent":
        l.wb_mode = "fluorescent";
        l.color_temp_k = 4000;
        l.temperature = -0.15;
        l.tint = -0.2;
        break;
      case "shade":
        l.color_temp_k = 7500;
        l.temperature = 0.3;
        l.shadows = 0.15;
        break;
      case "neon":
        l.temperature = -0.25;
        l.tint = 0.35;
        l.saturation = 0.25;
        l.contrast = 0.2;
        l.night = true;
        l.grain = 0.15;
        break;
      case "night":
      case "lowlight":
        l.exposure = 0.45;
        l.shadows = 0.35;
        l.fill = 0.4;
        l.grain = 0.25;
        l.night = true;
        l.iso = 1600;
        break;
      case "film":
      case "portra":
      case "cinematic":
        l.contrast = 0.12;
        l.saturation = -0.08;
        l.temperature = 0.12;
        l.tint = 0.04;
        l.vignette = 0.15;
        l.grain = 0.12;
        l.clarity = 0.1;
        break;
      case "punchy":
      case "vivid":
        l.contrast = 0.25;
        l.saturation = 0.2;
        l.clarity = 0.2;
        break;
      case "soft":
      case "skin":
        l.contrast = -0.12;
        l.clarity = -0.1;
        l.saturation = -0.05;
        l.highlights = -0.1;
        break;
      case "bleach":
        l.contrast = 0.35;
        l.saturation = -0.45;
        l.clarity = 0.15;
        break;
      default:
        break;
    }
    return l;
  }

  /** Grade RGBA ImageData in place. */
  function applyLookToImageData(img, look) {
    if (!img || !img.data) return;
    look = clone(look);
    var d = img.data;
    var w = img.width;
    var h = img.height;
    var exp = look.exposure + (look.ev || 0) * 0.35 + (look.night ? 0.25 : 0);
    var gain = Math.pow(2, exp);
    var lift = look.brightness * 40;
    var fill = look.fill * 35;
    var tr = look.temperature * 28;
    var tb = -look.temperature * 28;
    var tg = -look.tint * 22;
    var tr2 = look.tint * 12;
    var tb2 = look.tint * 12;
    var con = 1 + look.contrast;
    var sat = 1 + look.saturation;
    var hi = look.highlights;
    var sh = look.shadows;
    var vig = look.vignette;
    var grain = look.grain;
    var cx = (w - 1) / 2;
    var cy = (h - 1) / 2;
    var maxR = Math.hypot(cx, cy) || 1;
    var bak = null;
    if (Math.abs(look.clarity) > 0.02 || look.sharpen > 0.02) {
      bak = new Uint8ClampedArray(d);
    }
    for (var y = 0; y < h; y++) {
      for (var x = 0; x < w; x++) {
        var i = (y * w + x) * 4;
        var rf = d[i],
          gf = d[i + 1],
          bf = d[i + 2];
        if (bak) {
          rf = bak[i];
          gf = bak[i + 1];
          bf = bak[i + 2];
          var sr = 0,
            sg = 0,
            sb = 0,
            n = 0;
          var nbr = [
            [x - 1, y],
            [x + 1, y],
            [x, y - 1],
            [x, y + 1],
          ];
          for (var k = 0; k < 4; k++) {
            var xx = nbr[k][0],
              yy = nbr[k][1];
            if (xx < 0 || yy < 0 || xx >= w || yy >= h) continue;
            var j = (yy * w + xx) * 4;
            sr += bak[j];
            sg += bak[j + 1];
            sb += bak[j + 2];
            n++;
          }
          if (n) {
            sr /= n;
            sg /= n;
            sb /= n;
            var amt = look.clarity * 0.45 + look.sharpen * 0.35;
            rf += (rf - sr) * amt;
            gf += (gf - sg) * amt;
            bf += (bf - sb) * amt;
          }
        }
        rf = rf * gain + lift;
        gf = gf * gain + lift;
        bf = bf * gain + lift;
        var Y = 0.299 * rf + 0.587 * gf + 0.114 * bf;
        if (sh || fill) {
          var wsh = 1 - Y / 255;
          if (wsh < 0) wsh = 0;
          var add = (sh * 40 + fill) * wsh;
          rf += add;
          gf += add;
          bf += add;
        }
        if (hi) {
          var wh = Y / 255;
          rf += hi * -35 * wh;
          gf += hi * -35 * wh;
          bf += hi * -35 * wh;
        }
        rf += tr + tr2;
        gf += tg;
        bf += tb + tb2;
        rf = (rf - 128) * con + 128;
        gf = (gf - 128) * con + 128;
        bf = (bf - 128) * con + 128;
        var Y2 = 0.299 * rf + 0.587 * gf + 0.114 * bf;
        rf = Y2 + (rf - Y2) * sat;
        gf = Y2 + (gf - Y2) * sat;
        bf = Y2 + (bf - Y2) * sat;
        if (vig) {
          var dx = (x - cx) / maxR;
          var dy = (y - cy) / maxR;
          var factor = 1 - vig * (dx * dx + dy * dy) * 0.85;
          rf *= factor;
          gf *= factor;
          bf *= factor;
        }
        if (grain > 0) {
          var noise = ((((x * 374761393 + y * 668265263) ^ (x * y)) & 255) / 255 - 0.5) * grain * 40;
          rf += noise;
          gf += noise;
          bf += noise;
        }
        d[i] = clamp(rf, 0, 255) | 0;
        d[i + 1] = clamp(gf, 0, 255) | 0;
        d[i + 2] = clamp(bf, 0, 255) | 0;
      }
    }
  }

  /** Apply look when sampling video → luminance (float 0–1). */
  function applyLookToLum(lum, look) {
    if (!lum || !lum.length) return lum;
    look = clone(look);
    var exp = look.exposure + (look.ev || 0) * 0.35 + (look.night ? 0.25 : 0);
    var gain = Math.pow(2, exp);
    var lift = look.brightness * 0.15 + look.fill * 0.12;
    var con = 1 + look.contrast * 0.5;
    var out = lum instanceof Float32Array ? lum : Float32Array.from(lum);
    for (var i = 0; i < out.length; i++) {
      var v = out[i];
      if (v > 1) v = v / 255;
      v = v * gain + lift;
      v = (v - 0.5) * con + 0.5;
      if (look.shadows && v < 0.45) v += look.shadows * 0.12 * (1 - v / 0.45);
      if (look.highlights && v > 0.55) v += look.highlights * -0.1 * ((v - 0.55) / 0.45);
      if (look.grain) {
        var n = ((((i * 1103515245) & 0x7fffffff) / 0x7fffffff) - 0.5) * look.grain * 0.08;
        v += n;
      }
      out[i] = clamp(v, 0, 1);
    }
    return out;
  }

  /**
   * Apply MediaTrackConstraints for hardware controls (best-effort).
   * @returns {Promise<{applied:string[], skipped:string[]}>}
   */
  async function applyHardware(track, look) {
    var applied = [];
    var skipped = [];
    if (!track || !track.getCapabilities) {
      return { applied: applied, skipped: ["no getCapabilities"] };
    }
    look = clone(look);
    var caps = {};
    try {
      caps = track.getCapabilities() || {};
    } catch (_) {
      return { applied: applied, skipped: ["capabilities failed"] };
    }
    var adv = {};
    function trySet(key, val, constraintKey) {
      constraintKey = constraintKey || key;
      if (caps[constraintKey] === undefined && !(caps[constraintKey] && caps[constraintKey].max !== undefined)) {
        // some caps are objects with min/max
        if (typeof caps[constraintKey] !== "object" && caps[constraintKey] === undefined) {
          skipped.push(key);
          return;
        }
      }
      if (val === undefined || val === null || val === "") {
        skipped.push(key + ":empty");
        return;
      }
      adv[constraintKey] = val;
      applied.push(key);
    }

    if (look.facing) {
      // facing usually needs new getUserMedia — skip mid-stream
      skipped.push("facing:reopen");
    }
    if (caps.exposureMode && look.exposure_mode) trySet("exposureMode", look.exposure_mode);
    if (caps.exposureCompensation && look.ev != null) trySet("exposureCompensation", look.ev);
    if (caps.exposureTime && look.shutter) {
      // parse 1/125 → microseconds if needed — browsers vary; skip string shutters
      skipped.push("shutter:manual-only");
    }
    if (caps.iso && look.iso > 0) trySet("iso", look.iso);
    if (caps.whiteBalanceMode && look.wb_mode) {
      var wb = look.wb_mode;
      if (wb === "daylight" || wb === "cloudy" || wb === "tungsten" || wb === "fluorescent") {
        // map film WB to manual + colorTemperature when possible
        if (caps.colorTemperature && look.color_temp_k > 0) {
          trySet("whiteBalanceMode", "manual");
          trySet("colorTemperature", look.color_temp_k);
        } else {
          trySet("whiteBalanceMode", "continuous");
        }
      } else {
        trySet("whiteBalanceMode", wb === "auto" ? "continuous" : wb);
      }
    } else if (caps.colorTemperature && look.color_temp_k > 0) {
      trySet("colorTemperature", look.color_temp_k);
    }
    if (caps.focusMode && look.focus_mode) {
      var fm = look.focus_mode;
      if (fm === "continuous") trySet("focusMode", "continuous");
      else if (fm === "manual") trySet("focusMode", "manual");
      else if (fm === "single-shot") trySet("focusMode", "single-shot");
    }
    if (caps.focusDistance != null && look.focus_distance > 0) {
      trySet("focusDistance", look.focus_distance);
    }
    if (caps.torch != null && look.torch) trySet("torch", true);
    if (caps.torch != null && !look.torch) trySet("torch", false);
    if (caps.zoom && look.zoom >= 1) trySet("zoom", look.zoom);
    if (caps.brightness != null && look.brightness) {
      // some Android maps 0–100
      trySet("brightness", look.brightness);
    }
    if (caps.contrast != null && look.contrast) trySet("contrast", look.contrast);
    if (caps.saturation != null && look.saturation) trySet("saturation", look.saturation);
    if (caps.sharpness != null && look.sharpen) trySet("sharpness", look.sharpen);

    if (!Object.keys(adv).length) {
      return { applied: applied, skipped: skipped.concat(["nothing-applicable"]) };
    }
    try {
      await track.applyConstraints({ advanced: [adv] });
    } catch (e1) {
      // try flat constraints
      try {
        await track.applyConstraints(adv);
      } catch (e2) {
        skipped.push("applyConstraints:" + (e2 && e2.message ? e2.message : "fail"));
        return { applied: [], skipped: skipped };
      }
    }
    return { applied: applied, skipped: skipped };
  }

  /** Build UI panel into parent element. */
  function mountPanel(parent, look, onChange) {
    if (!parent) return null;
    look = clone(look);
    parent.classList.add("cam-panel");
    parent.innerHTML = "";
    var title = document.createElement("div");
    title.className = "cam-panel-title";
    title.textContent = "Camera · lighting";
    parent.appendChild(title);

    var presets = ["neutral", "daylight", "cloudy", "tungsten", "neon", "night", "film", "punchy", "soft", "bleach"];
    var preRow = document.createElement("div");
    preRow.className = "cam-presets";
    presets.forEach(function (p) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "cam-preset" + (look.preset === p ? " is-on" : "");
      b.textContent = p;
      b.addEventListener("click", function () {
        look = applyPreset(look, p);
        if (onChange) onChange(look, "preset");
        mountPanel(parent, look, onChange);
      });
      preRow.appendChild(b);
    });
    parent.appendChild(preRow);

    function slider(key, label, min, max, step) {
      var row = document.createElement("label");
      row.className = "cam-row";
      var span = document.createElement("span");
      span.textContent = label;
      var input = document.createElement("input");
      input.type = "range";
      input.min = String(min);
      input.max = String(max);
      input.step = String(step || 0.05);
      input.value = String(look[key] != null ? look[key] : 0);
      var val = document.createElement("em");
      val.textContent = Number(input.value).toFixed(2);
      input.addEventListener("input", function () {
        look[key] = parseFloat(input.value);
        val.textContent = look[key].toFixed(2);
        if (onChange) onChange(look, key);
      });
      row.appendChild(span);
      row.appendChild(input);
      row.appendChild(val);
      parent.appendChild(row);
    }

    slider("exposure", "Exposure", -2, 2, 0.05);
    slider("ev", "EV comp", -3, 3, 0.1);
    slider("contrast", "Contrast", -1, 1, 0.05);
    slider("saturation", "Saturation", -1, 1, 0.05);
    slider("temperature", "Temp (WB)", -1, 1, 0.05);
    slider("tint", "Tint", -1, 1, 0.05);
    slider("brightness", "Brightness", -1, 1, 0.05);
    slider("highlights", "Highlights", -1, 1, 0.05);
    slider("shadows", "Shadows", -1, 1, 0.05);
    slider("fill", "Fill light", 0, 1, 0.05);
    slider("clarity", "Clarity", -1, 1, 0.05);
    slider("sharpen", "Sharpen", 0, 2, 0.05);
    slider("vignette", "Vignette", -1, 1, 0.05);
    slider("grain", "Grain", 0, 1, 0.05);
    slider("zoom", "Zoom", 1, 8, 0.1);

    var row2 = document.createElement("div");
    row2.className = "cam-toggles";
    function toggle(key, label) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "cam-toggle" + (look[key] ? " is-on" : "");
      b.textContent = label;
      b.addEventListener("click", function () {
        look[key] = !look[key];
        b.classList.toggle("is-on", !!look[key]);
        if (onChange) onChange(look, key);
      });
      row2.appendChild(b);
    }
    toggle("night", "Night");
    toggle("torch", "Torch");
    parent.appendChild(row2);

    var facing = document.createElement("label");
    facing.className = "cam-row";
    facing.innerHTML = "<span>Facing</span>";
    var sel = document.createElement("select");
    ["environment", "user"].forEach(function (f) {
      var o = document.createElement("option");
      o.value = f;
      o.textContent = f === "environment" ? "back" : "front";
      if (look.facing === f) o.selected = true;
      sel.appendChild(o);
    });
    sel.addEventListener("change", function () {
      look.facing = sel.value;
      if (onChange) onChange(look, "facing");
    });
    facing.appendChild(sel);
    parent.appendChild(facing);

    return {
      getLook: function () {
        return clone(look);
      },
      setLook: function (l) {
        look = clone(l);
        mountPanel(parent, look, onChange);
      },
    };
  }

  function meshMessage(look, from) {
    return {
      type: "camera-controls",
      from: from || "browser",
      look: clone(look),
      t: Date.now(),
    };
  }

  function summary(look) {
    look = clone(look);
    var p = [];
    if (look.preset && look.preset !== "neutral") p.push(look.preset);
    if (look.exposure) p.push("exp=" + look.exposure.toFixed(2));
    if (look.fill) p.push("fill=" + look.fill.toFixed(2));
    if (look.night) p.push("night");
    if (look.torch) p.push("torch");
    return p.length ? p.join(" · ") : "neutral";
  }

  global.GY_CAMERA = {
    DEFAULTS: DEFAULTS,
    clone: clone,
    applyPreset: applyPreset,
    applyLookToImageData: applyLookToImageData,
    applyLookToLum: applyLookToLum,
    applyHardware: applyHardware,
    mountPanel: mountPanel,
    meshMessage: meshMessage,
    summary: summary,
  };
})(typeof window !== "undefined" ? window : globalThis);
