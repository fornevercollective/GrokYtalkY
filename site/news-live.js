/**
 * Live News real-stream sampler — YouTube live → HLS → 25² glyph.
 * Resolve: hub /api/media/resolve (blank/yt-dlp) or blank /api/ingest/resolve.
 * Playback: native HLS (Safari) or hls.js (Chrome).
 */
(function (global) {
  "use strict";

  var MAX_LIVE = 6; // concurrent video decodes (browser budget)
  var GLYPH_N = 25;
  var SAMPLE_MS = 200;

  function blankBase() {
    if (global.GY_BLANK_URL) return String(global.GY_BLANK_URL).replace(/\/$/, "");
    try {
      if (location.port === "5173") return location.origin;
    } catch (_) {}
    return "http://127.0.0.1:5173";
  }

  function hubBase() {
    try {
      if (location.protocol === "http:" || location.protocol === "https:") {
        return location.origin;
      }
    } catch (_) {}
    return "http://127.0.0.1:9876";
  }

  async function resolveStream(pageURL) {
    pageURL = String(pageURL || "").trim();
    if (!pageURL) throw new Error("empty url");

    // 1) blank ingest (CORS + HLS proxy — best for YouTube live)
    try {
      const res = await fetch(blankBase() + "/api/ingest/resolve", {
        method: "POST",
        headers: { "Content-Type": "application/json", Accept: "application/json" },
        body: JSON.stringify({ url: pageURL }),
      });
      const j = await res.json();
      if (j && j.ok && j.streamUrl) {
        var play = j.playPath ? blankBase() + j.playPath : j.streamUrl;
        return {
          video: play,
          raw: j.streamUrl,
          title: j.title || "",
          via: j.playPath ? "blank-proxy" : "blank",
          live: true,
          streamKind: j.streamKind || "",
        };
      }
    } catch (_) {
      /* blank down */
    }

    // 2) gy hub resolve (yt-dlp / blank server-side)
    const res2 = await fetch(
      hubBase() + "/api/media/resolve?url=" + encodeURIComponent(pageURL),
      { headers: { Accept: "application/json" } }
    );
    const j2 = await res2.json();
    if (!j2 || !j2.ok || !j2.video) {
      throw new Error((j2 && j2.error) || "resolve failed");
    }
    return {
      video: j2.video,
      title: j2.title || "",
      via: j2.via || "hub",
      live: !!j2.live,
    };
  }

  function loadHlsScript() {
    return new Promise(function (resolve, reject) {
      if (global.Hls) {
        resolve(global.Hls);
        return;
      }
      var s = document.createElement("script");
      s.src = "https://cdn.jsdelivr.net/npm/hls.js@1.5.17/dist/hls.min.js";
      s.async = true;
      s.onload = function () {
        resolve(global.Hls);
      };
      s.onerror = function () {
        reject(new Error("hls.js load failed"));
      };
      document.head.appendChild(s);
    });
  }

  function sampleVideo(video, n) {
    n = n || GLYPH_N;
    if (!video || video.readyState < 2) return null;
    var c = document.createElement("canvas");
    c.width = n;
    c.height = n;
    var ctx = c.getContext("2d", { willReadFrequently: true });
    if (!ctx) return null;
    var vw = video.videoWidth || 1;
    var vh = video.videoHeight || 1;
    var side = Math.min(vw, vh);
    var sx = Math.floor((vw - side) / 2);
    var sy = Math.floor((vh - side) / 2);
    try {
      ctx.drawImage(video, sx, sy, side, side, 0, 0, n, n);
      var img = ctx.getImageData(0, 0, n, n);
    } catch (e) {
      return null; // CORS taint
    }
    var d = img.data;
    var lum = new Float32Array(n * n);
    for (var i = 0, g = 0; i < d.length; i += 4, g++) {
      lum[g] = (0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2]) / 255;
    }
    return lum;
  }

  /**
   * Attach live sampler to a tile record.
   * rec: { src, video?, hls?, timer?, live, hubFrame, lum, onLiveError? }
   */
  async function startTileLive(rec, opts) {
    opts = opts || {};
    if (!rec || !rec.src || !rec.src.url) throw new Error("no source url");
    stopTileLive(rec);

    var resolved = await resolveStream(rec.src.url);
    rec.resolved = resolved;
    rec.liveError = "";

    var video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.autoplay = true;
    video.crossOrigin = "anonymous";
    video.setAttribute("playsinline", "");
    video.style.cssText = "position:fixed;left:-9999px;width:1px;height:1px;opacity:0;pointer-events:none";
    document.body.appendChild(video);
    rec.video = video;

    var url = resolved.video;
    var isHls = /\.m3u8(\?|$)/i.test(url) || resolved.streamKind === "hls" || /\/api\/ingest\/play\//.test(url);

    if (isHls && video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
    } else if (isHls) {
      var Hls = await loadHlsScript();
      if (Hls && Hls.isSupported()) {
        var hls = new Hls({
          enableWorker: true,
          lowLatencyMode: true,
          maxBufferLength: 8,
        });
        hls.loadSource(url);
        hls.attachMedia(video);
        rec.hls = hls;
        hls.on(Hls.Events.ERROR, function (_e, data) {
          if (data && data.fatal) {
            rec.liveError = "hls " + (data.type || "error");
            if (typeof opts.onError === "function") opts.onError(rec, rec.liveError);
          }
        });
      } else {
        video.src = url;
      }
    } else {
      video.src = url;
    }

    try {
      await video.play();
    } catch (e) {
      rec.liveError = "play: " + (e && e.message ? e.message : e);
    }

    rec.live = true;
    rec.hubFrame = true; // don't let simLum overwrite
    rec.timer = global.setInterval(function () {
      var lum = sampleVideo(video, opts.glyphN || GLYPH_N);
      if (lum) {
        rec.lum = lum;
        rec.live = true;
        rec.hubFrame = true;
        rec.lastSample = Date.now();
        if (typeof opts.onSample === "function") opts.onSample(rec);
      }
    }, opts.sampleMs || SAMPLE_MS);

    return rec;
  }

  function stopTileLive(rec) {
    if (!rec) return;
    if (rec.timer) {
      clearInterval(rec.timer);
      rec.timer = 0;
    }
    if (rec.hls) {
      try {
        rec.hls.destroy();
      } catch (_) {}
      rec.hls = null;
    }
    if (rec.video) {
      try {
        rec.video.pause();
        rec.video.removeAttribute("src");
        rec.video.load();
        if (rec.video.parentNode) rec.video.parentNode.removeChild(rec.video);
      } catch (_) {}
      rec.video = null;
    }
  }

  /**
   * Start live for up to MAX_LIVE main ids (staggered resolve).
   */
  async function startMainLive(tileMap, mainIds, opts) {
    opts = opts || {};
    var max = opts.max != null ? opts.max : MAX_LIVE;
    var ids = (mainIds || []).slice(0, max);
    var results = { ok: 0, fail: 0, errors: [] };
    for (var i = 0; i < ids.length; i++) {
      var id = ids[i];
      var rec = tileMap.get(id);
      if (!rec) continue;
      try {
        if (opts.onStatus) opts.onStatus("resolving " + (rec.src.label || id) + "…");
        await startTileLive(rec, opts);
        results.ok++;
        if (opts.onStatus) opts.onStatus("live · " + (rec.src.label || id) + " · " + (rec.resolved && rec.resolved.via));
      } catch (e) {
        results.fail++;
        rec.liveError = e && e.message ? e.message : String(e);
        results.errors.push(id + ": " + rec.liveError);
        if (opts.onStatus) opts.onStatus("fail · " + id + " · " + rec.liveError);
      }
      // stagger so we don't hammer yt-dlp
      if (i + 1 < ids.length) await new Promise(function (r) { setTimeout(r, 600); });
    }
    return results;
  }

  function stopAllLive(tileMap) {
    if (!tileMap) return;
    tileMap.forEach(function (rec) {
      stopTileLive(rec);
      if (rec) {
        rec.hubFrame = false;
        // keep last lum; sim resumes if hubFrame false
      }
    });
  }

  global.GY_NEWS_LIVE = {
    resolveStream: resolveStream,
    startTileLive: startTileLive,
    stopTileLive: stopTileLive,
    startMainLive: startMainLive,
    stopAllLive: stopAllLive,
    sampleVideo: sampleVideo,
    MAX_LIVE: MAX_LIVE,
    blankBase: blankBase,
    hubBase: hubBase,
  };
})(typeof window !== "undefined" ? window : globalThis);
