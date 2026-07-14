/**
 * Browser ↔ native multi-cam bridge
 *
 * - getUserMedia permission gate (labels unlock after grant)
 * - enumerate built-in + external cameras
 * - open up to 3 concurrent streams (laptop C + L1 + R1)
 * - face MOCAP light tracker → preferred seat L1
 * - export lanes for HDRI stitch + media-queue push
 */
(function (global) {
  "use strict";

  var SCENE = ["L2", "L1", "C", "R1", "R2"];

  function isMobileUA() {
    try {
      return /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent || "");
    } catch (_) {
      return false;
    }
  }

  function classifyLabel(label, index) {
    var L = String(label || "").toLowerCase();
    if (/facetime|built-?in|integrated|macbook|webcam|hd camera \(built/i.test(L)) {
      return { kind: "laptop", slot: "C", brand: "Built-in" };
    }
    if (/continuity|iphone|ipad/i.test(L)) {
      return { kind: "front", slot: "L1", brand: "Continuity" };
    }
    if (/ultra|wide/i.test(L) && !/tele/i.test(L)) {
      return { kind: "uw", slot: "R2", brand: "External" };
    }
    if (/tele|zoom/i.test(L)) {
      return { kind: "tele", slot: "L2", brand: "External" };
    }
    if (/back|rear|environment/i.test(L)) {
      return { kind: "back", slot: "R1", brand: "External" };
    }
    if (/front|user|face|selfie/i.test(L)) {
      return { kind: "front", slot: "L1", brand: "External" };
    }
    if (/screen|capture screen|display/i.test(L)) {
      return { kind: "screen", slot: "R2", brand: "Screen" };
    }
    // external order
    if (index === 0) return { kind: "laptop", slot: "C", brand: "Built-in" };
    if (index === 1) return { kind: "front", slot: "L1", brand: "External" };
    return { kind: "back", slot: "R1", brand: "External" };
  }

  /**
   * Permission probe then list videoinputs with labels.
   * @returns {Promise<Array<{deviceId,label,kind,slot,brand}>>}
   */
  async function listCameras() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      throw new Error("getUserMedia unavailable");
    }
    // permission gate — unlocks device labels
    var probe = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: "user", width: { ideal: 640 }, height: { ideal: 480 } },
      audio: false,
    });
    probe.getTracks().forEach(function (t) {
      t.stop();
    });
    var devices = await navigator.mediaDevices.enumerateDevices();
    var cams = devices.filter(function (d) {
      return d.kind === "videoinput";
    });
    var out = [];
    var usedSlots = {};
    cams.forEach(function (d, i) {
      var cls = classifyLabel(d.label, i);
      if (cls.kind === "screen") return; // skip screen for three-cam default
      var slot = cls.slot;
      if (usedSlots[slot]) {
        slot = SCENE.find(function (s) {
          return !usedSlots[s];
        }) || slot;
      }
      usedSlots[slot] = true;
      out.push({
        deviceId: d.deviceId,
        label: d.label || "Camera " + (i + 1),
        kind: cls.kind,
        slot: slot,
        brand: cls.brand,
        groupId: d.groupId || "",
      });
    });
    return out;
  }

  /**
   * Open streams for preferred seats (default C, L1, R1 — three-cam).
   * @param {object} [opts]
   * @param {string[]} [opts.slots]
   * @param {number} [opts.max]
   */
  async function openThreeCam(opts) {
    opts = opts || {};
    var wantSlots = opts.slots || ["C", "L1", "R1"];
    var max = opts.max || 3;
    var listed = await listCameras();
    // prefer matching seat kinds
    var picked = [];
    var usedIds = {};

    function take(pred) {
      for (var i = 0; i < listed.length; i++) {
        var c = listed[i];
        if (usedIds[c.deviceId]) continue;
        if (pred(c)) {
          usedIds[c.deviceId] = true;
          picked.push(c);
          return true;
        }
      }
      return false;
    }

    wantSlots.forEach(function (slot) {
      if (picked.length >= max) return;
      take(function (c) {
        return c.slot === slot;
      });
    });
    // fill remaining
    listed.forEach(function (c) {
      if (picked.length >= max) return;
      if (usedIds[c.deviceId]) return;
      usedIds[c.deviceId] = true;
      picked.push(c);
    });

    var lanes = [];
    for (var i = 0; i < picked.length; i++) {
      var cam = picked[i];
      try {
        var stream = await navigator.mediaDevices.getUserMedia({
          video: {
            deviceId: { ideal: cam.deviceId },
            width: { ideal: 1280 },
            height: { ideal: 720 },
          },
          audio: false,
        });
        var video = document.createElement("video");
        video.playsInline = true;
        video.muted = true;
        video.autoplay = true;
        video.srcObject = stream;
        await video.play().catch(function () {});
        lanes.push({
          deviceId: cam.deviceId,
          label: cam.label,
          kind: cam.kind,
          slot: cam.slot,
          brand: cam.brand,
          stream: stream,
          video: video,
          short: cam.kind,
        });
      } catch (e) {
        console.warn("[cam-bridge] open skip", cam.label, e);
      }
    }
    return lanes;
  }

  /** Empty face sample (stable shape for multi-source / mesh). */
  function emptyFace() {
    return {
      x: 0.5,
      y: 0.4,
      confidence: 0,
      talking: false,
      variance: 0,
      varianceN: 0,
      muteHint: "none",
      slotHint: "L1",
    };
  }

  /**
   * Lightweight face MOCAP — brightness centroid + mouth-band variance.
   * Returns continuous variance for GrokGlyph seats / multi-source adapter:
   *   {x,y,confidence,talking,variance,varianceN,muteHint,slotHint}
   */
  function trackFace(videoOrCanvas) {
    var c = document.createElement("canvas");
    var w = 64,
      h = 64;
    c.width = w;
    c.height = h;
    var ctx = c.getContext("2d", { willReadFrequently: true });
    if (!ctx || !videoOrCanvas) return emptyFace();
    try {
      ctx.drawImage(videoOrCanvas, 0, 0, w, h);
      var img = ctx.getImageData(0, 0, w, h);
      var d = img.data;
      var sx = 0,
        sy = 0,
        sw = 0;
      var midY0 = Math.floor(h * 0.15);
      var midY1 = Math.floor(h * 0.75);
      for (var y = midY0; y < midY1; y++) {
        for (var x = 0; x < w; x++) {
          var i = (y * w + x) * 4;
          var L = 0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2];
          // skin-ish / bright face band
          if (L > 70 && L < 230 && d[i] > d[i + 2] * 0.85) {
            sx += x;
            sy += y;
            sw += 1;
          }
        }
      }
      if (sw < 20) return emptyFace();
      var cx = sx / sw / w;
      var cy = sy / sw / h;
      // crude talking: sample mouth band luminance variance
      var mouthY = Math.min(h - 2, Math.floor(cy * h + h * 0.12));
      var sum = 0,
        sum2 = 0,
        n = 0;
      for (var mx = Math.floor(w * 0.3); mx < Math.floor(w * 0.7); mx++) {
        var j = (mouthY * w + mx) * 4;
        var Lm = 0.299 * d[j] + 0.587 * d[j + 1] + 0.114 * d[j + 2];
        sum += Lm;
        sum2 += Lm * Lm;
        n++;
      }
      var mean = sum / Math.max(1, n);
      var varL = Math.max(0, sum2 / Math.max(1, n) - mean * mean);
      // continuous 0..1 flag for mesh / venue (threshold ~180 → talking)
      var varianceN = Math.min(1, varL / 400);
      var talking = varL > 180;
      var conf = Math.min(1, sw / 200);
      var muteHint = talking ? "talking" : conf > 0.25 ? "quiet" : "none";
      return {
        x: cx,
        y: cy,
        confidence: conf,
        talking: talking,
        variance: Math.round(varL * 10) / 10,
        varianceN: Math.round(varianceN * 1000) / 1000,
        muteHint: muteHint,
        slotHint: "L1",
      };
    } catch (_) {
      return emptyFace();
    }
  }

  /**
   * Re-sample face MOCAP on open lanes (no slot reshuffle unless reslot).
   * @param {object[]} lanes
   * @param {{reslot?:boolean}} [opts]
   */
  function refreshFaceMOCAP(lanes, opts) {
    opts = opts || {};
    if (!lanes || !lanes.length) return lanes;
    var best = null;
    var bestC = 0;
    lanes.forEach(function (lane) {
      var tr = trackFace(lane.video);
      lane.face = tr;
      lane.talking = !!tr.talking;
      lane.variance = tr.variance || 0;
      lane.varianceN = tr.varianceN || 0;
      lane.muteHint = tr.muteHint || "none";
      if (tr.confidence > bestC) {
        bestC = tr.confidence;
        best = lane;
      }
    });
    lanes.forEach(function (l) {
      l.mocap = !!(best && l === best && bestC > 0.25);
    });
    if (opts.reslot && best && bestC > 0.25) {
      applyFaceSlots(lanes, { alreadyTracked: true });
    }
    return lanes;
  }

  /**
   * Apply face track to lanes — boost front/L1 when face confidence high.
   * @param {object[]} lanes
   * @param {{alreadyTracked?:boolean}} [opts]
   */
  function applyFaceSlots(lanes, opts) {
    opts = opts || {};
    if (!lanes || !lanes.length) return lanes;
    var best = null;
    var bestC = 0;
    lanes.forEach(function (lane) {
      var tr = opts.alreadyTracked && lane.face ? lane.face : trackFace(lane.video);
      lane.face = tr;
      lane.talking = !!tr.talking;
      lane.variance = tr.variance || 0;
      lane.varianceN = tr.varianceN || 0;
      lane.muteHint = tr.muteHint || "none";
      if (tr.confidence > bestC) {
        bestC = tr.confidence;
        best = lane;
      }
    });
    lanes.forEach(function (l) {
      l.mocap = false;
    });
    if (best && bestC > 0.25) {
      // ensure face cam sits L1 for HDRI / mocap
      var taken = {};
      lanes.forEach(function (l) {
        if (l !== best) taken[l.slot] = true;
      });
      best.slot = "L1";
      best.kind = best.kind === "laptop" ? "laptop" : "front";
      best.mocap = true;
      lanes.forEach(function (l) {
        if (l === best) return;
        if (l.slot === "L1") {
          l.slot = taken["C"] ? "R1" : "C";
        }
      });
    }
    return lanes;
  }

  /**
   * Multi-source adapter snapshot — stable shape for queue, mesh, venue, HDRI.
   * Continuous face variance flag rides every source + rollup.
   */
  function toMultiSource(lanes) {
    var sources = (lanes || []).map(function (l) {
      var f = l.face || emptyFace();
      return {
        slot: l.slot || "",
        kind: l.kind || "",
        label: l.label || "",
        short: l.short || l.kind || "",
        deviceId: l.deviceId || "",
        mocap: !!l.mocap,
        talking: !!(l.talking || (f && f.talking)),
        variance: f.variance != null ? f.variance : l.variance || 0,
        varianceN: f.varianceN != null ? f.varianceN : l.varianceN || 0,
        muteHint: f.muteHint || l.muteHint || "none",
        confidence: f.confidence || 0,
        face: {
          x: f.x,
          y: f.y,
          confidence: f.confidence || 0,
          talking: !!f.talking,
          variance: f.variance || 0,
          varianceN: f.varianceN || 0,
          muteHint: f.muteHint || "none",
        },
      };
    });
    var faceSrc =
      sources.find(function (s) {
        return s.mocap;
      }) ||
      sources
        .slice()
        .sort(function (a, b) {
          return (b.confidence || 0) - (a.confidence || 0);
        })[0] ||
      null;
    var talkingSrc = sources.find(function (s) {
      return s.talking;
    });
    return {
      type: "multi-source",
      via: "cam-bridge",
      t: Date.now(),
      sources: sources,
      faceSlot: faceSrc ? faceSrc.slot : "",
      talking: !!(talkingSrc || (faceSrc && faceSrc.talking)),
      variance: faceSrc ? faceSrc.variance : 0,
      varianceN: faceSrc ? faceSrc.varianceN : 0,
      muteHint: talkingSrc
        ? "talking"
        : faceSrc && faceSrc.confidence > 0.25
          ? faceSrc.muteHint || "quiet"
          : "none",
      face: faceSrc ? faceSrc.face : emptyFace(),
    };
  }

  /** Stamp face MOCAP multi-source fields onto a queue item. */
  function stampQueueItem(item, lane, multi) {
    if (!item) return item;
    var f = (lane && lane.face) || emptyFace();
    item.slot = (lane && lane.slot) || item.slot || "";
    item.mocap = !!(lane && lane.mocap);
    item.talking = !!(lane && (lane.talking || f.talking));
    item.variance = f.variance || 0;
    item.varianceN = f.varianceN || 0;
    item.muteHint = f.muteHint || "none";
    item.face = f;
    if (multi) item.multiSource = multi;
    return item;
  }

  /**
   * Push opened browser lanes into a GY_MEDIA_QUEUE engine as titled items
   * using hub three-cam/device restream when possible; else mark browser-live.
   * Face MOCAP variance rides each item + multiSource rollup.
   */
  async function pushLanesToQueue(q, lanes, opts) {
    opts = opts || {};
    if (!q || !lanes || !lanes.length) return [];
    refreshFaceMOCAP(lanes, { reslot: false });
    var multi = toMultiSource(lanes);
    var bySlot = {};
    lanes.forEach(function (l) {
      if (l.slot) bySlot[l.slot] = l;
    });
    var added = [];
    // Prefer native three-cam pack for shared timeline (server HLS)
    if (opts.native !== false) {
      try {
        var base = q.hubHTTP ? q.hubHTTP() : location.origin;
        var r = await fetch(base + "/api/media/ingest/three-cam", {
          headers: { Accept: "application/json" },
        });
        var data = await r.json();
        if (data && data.ok && Array.isArray(data.items)) {
          data.items.forEach(function (it) {
            if (!it.video) return;
            var lane = bySlot[it.slot] || null;
            var item = q.addOne(it.src || it.video, {
              title: (it.slot || "") + " · " + (it.label || it.title || "cam"),
              video: it.video,
            });
            if (item) {
              item.video = it.video;
              item.status = "ready";
              item.live = true;
              item.via = it.via || "three-cam";
              stampQueueItem(item, lane, multi);
              added.push(item);
            }
          });
          if (added.length) {
            try {
              q._lastMultiSource = multi;
            } catch (_) {}
            return added;
          }
        }
      } catch (e) {
        console.warn("[cam-bridge] native three-cam", e);
      }
    }
    // Fallback: queue device: ids by seat order
    lanes.forEach(function (lane) {
      var id = "device:" + (lane.deviceId ? "id:" + lane.deviceId.slice(0, 12) : lane.slot);
      // browser deviceId isn't avfoundation index — use label-based native if we have index
      if (lane.avIndex != null) id = "device:avfoundation:" + lane.avIndex;
      var item = q.addOne(id, {
        title: (lane.slot || "") + " · " + (lane.label || lane.kind),
      });
      if (item) {
        stampQueueItem(item, lane, multi);
        added.push(item);
      }
    });
    try {
      q._lastMultiSource = multi;
    } catch (_) {}
    return added;
  }

  /** Lanes shape for GY_HDRI.runProbe (+ face variance for stitch bias). */
  function toHdriLanes(lanes) {
    return (lanes || []).map(function (l) {
      var f = l.face || null;
      return {
        slot: l.slot,
        video: l.video,
        short: l.short || l.kind,
        kind: l.kind,
        label: l.label,
        face: f,
        mocap: !!l.mocap,
        talking: !!(l.talking || (f && f.talking)),
        variance: f ? f.variance : 0,
        varianceN: f ? f.varianceN : 0,
        muteHint: f ? f.muteHint : "none",
      };
    });
  }

  function stopLanes(lanes) {
    (lanes || []).forEach(function (l) {
      try {
        if (l.stream) l.stream.getTracks().forEach(function (t) {
          t.stop();
        });
      } catch (_) {}
      try {
        if (l.video) {
          l.video.pause();
          l.video.srcObject = null;
        }
      } catch (_) {}
    });
  }

  global.GY_CAM_BRIDGE = {
    listCameras: listCameras,
    openThreeCam: openThreeCam,
    trackFace: trackFace,
    emptyFace: emptyFace,
    refreshFaceMOCAP: refreshFaceMOCAP,
    applyFaceSlots: applyFaceSlots,
    toMultiSource: toMultiSource,
    pushLanesToQueue: pushLanesToQueue,
    toHdriLanes: toHdriLanes,
    stopLanes: stopLanes,
    classifyLabel: classifyLabel,
  };
})(typeof window !== "undefined" ? window : globalThis);
