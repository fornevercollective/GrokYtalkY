/**
 * Filmmaker hurdle hop — quick multi-cam HDRI-style probe + subject strip.
 *
 * Not photogrammetry-grade. Uses scene slots L2·L1·C·R1·R2 from GrokGlyph:
 *  - subject strip: horizontal multi-angle face/scene board
 *  - equirect probe: wedge-mapped lat-long env (lighting vibe / dome map)
 *  - export PNG + optional hub cast for sphere shell
 */
(function (global) {
  "use strict";

  var SLOT_ORDER = ["L2", "L1", "C", "R1", "R2"];
  // Longitude coverage for each slot (degrees, -180..180), outward env assumption
  var SLOT_LON = {
    L2: { min: -162, max: -90 },
    L1: { min: -90, max: -30 },
    C: { min: -30, max: 30 },
    R1: { min: 30, max: 90 },
    R2: { min: 90, max: 162 },
  };

  function hubHTTP() {
    try {
      if (location.protocol === "http:" || location.protocol === "https:") {
        return location.origin;
      }
    } catch (_) {}
    return "http://127.0.0.1:9876";
  }

  function slotOf(meta) {
    if (!meta) return "";
    if (meta.slot) return String(meta.slot).toUpperCase();
    if (meta.sceneSlot) return String(meta.sceneSlot).toUpperCase();
    var m = String(meta.nick || meta.from || meta.id || "").match(
      /-(L2|L1|C|R1|R2)$/i
    );
    return m ? m[1].toUpperCase() : "";
  }

  /**
   * @param {HTMLVideoElement|HTMLCanvasElement|HTMLImageElement} src
   * @param {number} maxSide
   */
  function grabFrame(src, maxSide) {
    maxSide = maxSide || 640;
    if (!src) return null;
    var w = src.videoWidth || src.naturalWidth || src.width || 0;
    var h = src.videoHeight || src.naturalHeight || src.height || 0;
    if (!w || !h) return null;
    var scale = Math.min(1, maxSide / Math.max(w, h));
    var cw = Math.max(1, Math.round(w * scale));
    var ch = Math.max(1, Math.round(h * scale));
    var c = document.createElement("canvas");
    c.width = cw;
    c.height = ch;
    var ctx = c.getContext("2d");
    if (!ctx) return null;
    try {
      ctx.drawImage(src, 0, 0, cw, ch);
    } catch (e) {
      return null;
    }
    return c;
  }

  /**
   * Simple “HDR-ish” local contrast + lift shadows (single-EV quick path).
   * Not multi-bracket Debevec — hurdle hop for filmmakers on set.
   */
  function quickTonemap(canvas) {
    var ctx = canvas.getContext("2d", { willReadFrequently: true });
    if (!ctx) return canvas;
    var img = ctx.getImageData(0, 0, canvas.width, canvas.height);
    var d = img.data;
    for (var i = 0; i < d.length; i += 4) {
      var r = d[i] / 255,
        g = d[i + 1] / 255,
        b = d[i + 2] / 255;
      // lift shadows, soft shoulder
      r = Math.pow(r, 0.85);
      g = Math.pow(g, 0.85);
      b = Math.pow(b, 0.85);
      var l = 0.2126 * r + 0.7152 * g + 0.0722 * b;
      var boost = 0.12 * (1 - l);
      r = Math.min(1, r + boost);
      g = Math.min(1, g + boost);
      b = Math.min(1, b + boost);
      d[i] = (r * 255) | 0;
      d[i + 1] = (g * 255) | 0;
      d[i + 2] = (b * 255) | 0;
    }
    ctx.putImageData(img, 0, 0);
    return canvas;
  }

  /**
   * Subject strip — L2|L1|C|R1|R2 contact board for face/scene multi-angle.
   * @param {Array<{slot:string, canvas:HTMLCanvasElement, label?:string}>} views
   */
  function stitchSubjectStrip(views, opts) {
    opts = opts || {};
    var tileH = opts.tileH || 360;
    var gap = opts.gap != null ? opts.gap : 4;
    var ordered = SLOT_ORDER.map(function (s) {
      return views.find(function (v) {
        return String(v.slot).toUpperCase() === s;
      });
    }).filter(Boolean);
    if (!ordered.length) return null;

    var widths = ordered.map(function (v) {
      var c = v.canvas;
      return Math.round((c.width / c.height) * tileH);
    });
    var totalW = widths.reduce(function (a, b) {
      return a + b;
    }, 0) + gap * (ordered.length - 1) + 16;
    var totalH = tileH + 36;
    var out = document.createElement("canvas");
    out.width = totalW;
    out.height = totalH;
    var ctx = out.getContext("2d");
    ctx.fillStyle = "#0a0a0e";
    ctx.fillRect(0, 0, totalW, totalH);
    var x = 8;
    ordered.forEach(function (v, i) {
      var tw = widths[i];
      ctx.drawImage(v.canvas, x, 8, tw, tileH);
      ctx.fillStyle = "rgba(0,0,0,0.65)";
      ctx.fillRect(x, tileH - 4, tw, 28);
      ctx.fillStyle = v.slot === "C" ? "#6ee7b7" : "#7dd3fc";
      ctx.font = "600 13px ui-monospace, monospace";
      ctx.fillText(
        v.slot + (v.label ? " · " + v.label : ""),
        x + 6,
        tileH + 16
      );
      x += tw + gap;
    });
    return out;
  }

  /**
   * Equirect wedge probe — map each slot image into a longitude band.
   * Vertical: center band of source (face/horizon-ish) stretched in lat.
   */
  function stitchEquirect(views, opts) {
    opts = opts || {};
    var W = opts.width || 2048;
    var H = opts.height || 1024;
    var out = document.createElement("canvas");
    out.width = W;
    out.height = H;
    var ctx = out.getContext("2d");
    // gradient sky/ground placeholder
    var g = ctx.createLinearGradient(0, 0, 0, H);
    g.addColorStop(0, "#1a2744");
    g.addColorStop(0.45, "#2a2a32");
    g.addColorStop(0.55, "#2a2a32");
    g.addColorStop(1, "#1a1510");
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, W, H);

    var bySlot = {};
    views.forEach(function (v) {
      bySlot[String(v.slot).toUpperCase()] = v;
    });

    SLOT_ORDER.forEach(function (slot) {
      var v = bySlot[slot];
      if (!v || !v.canvas) return;
      var lon = SLOT_LON[slot];
      if (!lon) return;
      var x0 = Math.floor(((lon.min + 180) / 360) * W);
      var x1 = Math.ceil(((lon.max + 180) / 360) * W);
      if (x1 <= x0) x1 = x0 + 1;
      var bandW = x1 - x0;
      // use middle 70% of source height (less floor/ceiling)
      var src = v.canvas;
      var sy = Math.floor(src.height * 0.12);
      var sh = Math.floor(src.height * 0.76);
      var y0 = Math.floor(H * 0.22);
      var y1 = Math.floor(H * 0.78);
      ctx.drawImage(src, 0, sy, src.width, sh, x0, y0, bandW, y1 - y0);
      // soft edge blend
      var fade = Math.min(24, (bandW / 4) | 0);
      if (fade > 2) {
        var grdL = ctx.createLinearGradient(x0, 0, x0 + fade, 0);
        grdL.addColorStop(0, "rgba(10,10,14,0.85)");
        grdL.addColorStop(1, "rgba(10,10,14,0)");
        ctx.fillStyle = grdL;
        ctx.fillRect(x0, y0, fade, y1 - y0);
        var grdR = ctx.createLinearGradient(x1 - fade, 0, x1, 0);
        grdR.addColorStop(0, "rgba(10,10,14,0)");
        grdR.addColorStop(1, "rgba(10,10,14,0.85)");
        ctx.fillStyle = grdR;
        ctx.fillRect(x1 - fade, y0, fade, y1 - y0);
      }
    });

    // label
    ctx.fillStyle = "rgba(0,0,0,0.5)";
    ctx.fillRect(8, H - 28, 320, 20);
    ctx.fillStyle = "#a7f3d0";
    ctx.font = "11px ui-monospace, monospace";
    ctx.fillText("GrokYtalkY HDRI probe · slot wedges · quick tonemap", 12, H - 14);
    return out;
  }

  /**
   * Sample equirect canvas → 25² glyph for sphere dome cast fallback.
   */
  function equirectToGlyph(canvas, n) {
    n = n || 25;
    var c = document.createElement("canvas");
    c.width = n;
    c.height = n;
    var ctx = c.getContext("2d");
    ctx.drawImage(canvas, 0, 0, n, n);
    var img = ctx.getImageData(0, 0, n, n);
    var glyph = new Array(n * n);
    for (var i = 0, g = 0; i < img.data.length; i += 4, g++) {
      glyph[g] = Math.round(
        0.299 * img.data[i] + 0.587 * img.data[i + 1] + 0.114 * img.data[i + 2]
      );
    }
    return glyph;
  }

  function downloadCanvas(canvas, filename) {
    if (!canvas) return;
    var a = document.createElement("a");
    a.download = filename || "gy-hdri-probe.png";
    a.href = canvas.toDataURL("image/png");
    a.click();
  }

  /**
   * Build views from GrokGlyph camLanes + optional peer snapshots.
   * @param {Array} lanes - {slot, video, short, label, kind}
   */
  function captureFromLanes(lanes, opts) {
    opts = opts || {};
    var views = [];
    (lanes || []).forEach(function (lane) {
      var slot = String(lane.slot || slotOf(lane) || "").toUpperCase();
      if (!slot) return;
      var src = lane.video || lane.canvas || lane.el;
      var frame = grabFrame(src, opts.maxSide || 720);
      if (!frame) return;
      if (opts.tonemap !== false) quickTonemap(frame);
      views.push({
        slot: slot,
        canvas: frame,
        label: lane.short || lane.kind || lane.label || slot,
        kind: lane.kind || "",
      });
    });
    return views;
  }

  /**
   * Full hurdle hop: capture → strip + equirect → export + optional mesh cast.
   */
  function runProbe(lanes, opts) {
    opts = opts || {};
    var views = captureFromLanes(lanes, opts);
    if (!views.length) {
      return { ok: false, error: "no camera frames — enable cam first" };
    }
    var strip = stitchSubjectStrip(views, opts);
    var equirect = stitchEquirect(views, {
      width: opts.eqW || 2048,
      height: opts.eqH || 1024,
    });
    if (opts.tonemap !== false && equirect) quickTonemap(equirect);

    var glyph = equirect ? equirectToGlyph(equirect, opts.glyphN || 25) : null;
    var result = {
      ok: true,
      views: views,
      strip: strip,
      equirect: equirect,
      glyph: glyph,
      slots: views.map(function (v) {
        return v.slot;
      }),
      t: Date.now(),
    };

    if (opts.download) {
      if (strip) downloadCanvas(strip, "gy-subject-strip.png");
      if (equirect) downloadCanvas(equirect, "gy-hdri-probe.png");
    }

    if (opts.sendMesh && typeof opts.sendMesh === "function" && equirect) {
      var jpeg = equirect.toDataURL("image/jpeg", 0.72);
      var b64 = jpeg.split(",")[1] || "";
      opts.sendMesh({
        type: "hdri-probe",
        from: opts.from || "hdri",
        slots: result.slots,
        w: equirect.width,
        h: equirect.height,
        b64: b64,
        fmt: "jpeg",
        glyph: glyph,
        glyphN: opts.glyphN || 25,
        cast: "sphere",
        project: true,
        t: result.t,
      });
      // also glyph dome cast for peers without image decode
      if (glyph) {
        opts.sendMesh({
          type: "vburst-frame",
          from: (opts.from || "hdri") + "-probe",
          glyph: glyph,
          glyphN: opts.glyphN || 25,
          cast: "sphere",
          project: true,
          t: result.t,
          cam: { kind: "hdri", slot: "EQ" },
        });
      }
    }

    // hub archive (optional)
    if (opts.postHub && equirect) {
      fetch(hubHTTP() + "/api/hdri/probe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          slots: result.slots,
          w: equirect.width,
          h: equirect.height,
          jpeg: equirect.toDataURL("image/jpeg", 0.7),
          strip: strip ? strip.toDataURL("image/jpeg", 0.7) : "",
          t: result.t,
        }),
      }).catch(function () {});
    }

    return result;
  }

  global.GY_HDRI = {
    SLOT_ORDER: SLOT_ORDER,
    grabFrame: grabFrame,
    quickTonemap: quickTonemap,
    stitchSubjectStrip: stitchSubjectStrip,
    stitchEquirect: stitchEquirect,
    equirectToGlyph: equirectToGlyph,
    captureFromLanes: captureFromLanes,
    runProbe: runProbe,
    downloadCanvas: downloadCanvas,
    hubHTTP: hubHTTP,
  };
})(typeof window !== "undefined" ? window : globalThis);
