/**
 * Live News real-stream sampler — YouTube live → HLS → 25² glyph.
 *
 * Prefer hub /api/media/resolve (serial server-side blank/yt-dlp).
 * Fallback: blank /api/ingest/resolve.
 * Playback: hls.js or native Safari HLS. Sample with crossOrigin + blank proxy CORS.
 */
(function (global) {
  "use strict";

  var MAX_LIVE = 4; // fewer concurrent decodes = more reliable
  var GLYPH_N = 25;
  var SAMPLE_MS = 250;
  var RESOLVE_GAP_MS = 900;

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

  function fetchTimeout(url, opts, ms) {
    opts = opts || {};
    ms = ms || 45000;
    var ctrl = typeof AbortController !== "undefined" ? new AbortController() : null;
    var t = setTimeout(function () {
      if (ctrl) ctrl.abort();
    }, ms);
    var o = Object.assign({}, opts);
    if (ctrl) o.signal = ctrl.signal;
    return fetch(url, o).finally(function () {
      clearTimeout(t);
    });
  }

  /**
   * Quick health: prefer built-in hub media tools (yt-dlp + CORS play proxy).
   * blank is optional fallback only.
   */
  async function preflight() {
    var blank = blankBase();
    var hub = hubBase();
    // 1) Hub built-in tools (gy serve) — sufficient for Live News
    try {
      var hr = await fetchTimeout(hub + "/api/lan", { headers: { Accept: "application/json" } }, 2500);
      if (hr.ok) {
        return {
          ok: true,
          via: "hub",
          blank: blank,
          hub: hub,
          note: "built-in resolve+play proxy",
        };
      }
    } catch (_) {}
    // 2) blank optional
    try {
      var br = await fetchTimeout(blank + "/", {}, 2500);
      if (br.status === 503) {
        return {
          ok: false,
          error:
            "hub down and blank concurrent cap (503) — start gy serve, or restart blank",
          via: "blank",
        };
      }
      if (br.ok || (br.status > 0 && br.status < 500)) {
        return { ok: true, via: "blank", blank: blank, hub: hub };
      }
    } catch (_) {}
    return {
      ok: false,
      error: "hub not reachable at " + hub + " — run: gy serve --bind 0.0.0.0 --port 9876",
      via: "none",
    };
  }

  async function resolveStream(pageURL) {
    pageURL = String(pageURL || "").trim();
    if (!pageURL) throw new Error("empty url");
    var lastErr = "resolve failed";

    // 1) Hub first (server blank/yt-dlp → blank-proxy play URL)
    try {
      var res2 = await fetchTimeout(
        hubBase() + "/api/media/resolve?url=" + encodeURIComponent(pageURL),
        { headers: { Accept: "application/json" } },
        45000
      );
      if (res2.status === 503) {
        lastErr = "hub/blank concurrent cap (503) — restart blank";
      } else {
        var j2 = await res2.json().catch(function () {
          return null;
        });
        if (j2 && j2.ok && j2.video) {
          return {
            video: j2.video,
            title: j2.title || "",
            via: j2.via || "hub",
            live: !!j2.live,
            streamKind: /m3u8|hls|play\//i.test(j2.video) ? "hls" : "",
          };
        }
        if (j2 && j2.error) lastErr = j2.error;
        else if (!res2.ok) lastErr = "hub resolve HTTP " + res2.status;
      }
    } catch (e) {
      lastErr = e && e.name === "AbortError" ? "hub resolve timeout" : e && e.message ? e.message : String(e);
    }

    // 2) Direct blank
    try {
      var res = await fetchTimeout(
        blankBase() + "/api/ingest/resolve",
        {
          method: "POST",
          headers: { "Content-Type": "application/json", Accept: "application/json" },
          body: JSON.stringify({ url: pageURL }),
        },
        45000
      );
      if (res.status === 503) {
        lastErr = "blank concurrent cap (503) — restart blank: cd ~/dev/blank && ./start.sh";
      } else if (!res.ok) {
        lastErr = "blank HTTP " + res.status;
      } else {
        var j = await res.json();
        if (j && j.ok && j.streamUrl) {
          var play = j.playPath ? blankBase() + j.playPath : j.streamUrl;
          return {
            video: play,
            raw: j.streamUrl,
            title: j.title || "",
            via: j.playPath ? "blank-proxy" : "blank",
            live: true,
            streamKind: j.streamKind || "hls",
          };
        }
        if (j && j.error) lastErr = j.error;
      }
    } catch (e2) {
      lastErr =
        e2 && e2.name === "AbortError" ? "blank resolve timeout" : e2 && e2.message ? e2.message : String(e2);
    }

    throw new Error(lastErr);
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
        reject(new Error("hls.js CDN failed — check network"));
      };
      document.head.appendChild(s);
    });
  }

  var _sampleCanvas = null;
  function sampleVideo(video, n) {
    n = n || GLYPH_N;
    if (!video || video.readyState < 2) return null;
    if (!_sampleCanvas) _sampleCanvas = document.createElement("canvas");
    var c = _sampleCanvas;
    c.width = n;
    c.height = n;
    var ctx = c.getContext("2d", { willReadFrequently: true });
    if (!ctx) return null;
    var vw = video.videoWidth || 1;
    var vh = video.videoHeight || 1;
    var side = Math.min(vw, vh);
    var sx = Math.floor((vw - side) / 2);
    var sy = Math.floor((vh - side) * 0.15);
    try {
      ctx.drawImage(video, sx, sy, side, side, 0, 0, n, n);
      var img = ctx.getImageData(0, 0, n, n);
    } catch (e) {
      return null;
    }
    var d = img.data;
    var lum = new Float32Array(n * n);
    for (var i = 0, g = 0; i < d.length; i += 4, g++) {
      lum[g] = (0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2]) / 255;
    }
    return lum;
  }

  function waitPlaying(video, ms) {
    ms = ms || 12000;
    return new Promise(function (resolve) {
      if (video.readyState >= 2 && !video.paused) {
        resolve(true);
        return;
      }
      var done = false;
      function fin(ok) {
        if (done) return;
        done = true;
        video.removeEventListener("playing", onPlay);
        video.removeEventListener("loadeddata", onPlay);
        video.removeEventListener("error", onErr);
        resolve(ok);
      }
      function onPlay() {
        fin(true);
      }
      function onErr() {
        fin(false);
      }
      video.addEventListener("playing", onPlay);
      video.addEventListener("loadeddata", onPlay);
      video.addEventListener("error", onErr);
      setTimeout(function () {
        fin(video.readyState >= 2);
      }, ms);
    });
  }

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
    video.preload = "auto";
    video.setAttribute("playsinline", "");
    video.setAttribute("muted", "");
    video.style.cssText =
      "position:fixed;left:-9999px;top:0;width:160px;height:90px;opacity:0;pointer-events:none";
    document.body.appendChild(video);
    rec.video = video;

    var url = resolved.video;
    var isHls =
      /\.m3u8(\?|$)/i.test(url) ||
      resolved.streamKind === "hls" ||
      /\/api\/ingest\/play\//.test(url) ||
      /manifest\.googlevideo|playlist_type\/DVR/i.test(url);

    if (isHls && video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
    } else if (isHls) {
      var Hls = await loadHlsScript();
      if (Hls && Hls.isSupported()) {
        var hls = new Hls({
          enableWorker: true,
          lowLatencyMode: true,
          maxBufferLength: 12,
          maxMaxBufferLength: 20,
        });
        hls.loadSource(url);
        hls.attachMedia(video);
        rec.hls = hls;
        hls.on(Hls.Events.MANIFEST_PARSED, function () {
          video.play().catch(function () {});
        });
        hls.on(Hls.Events.ERROR, function (_e, data) {
          if (data && data.fatal) {
            rec.liveError = "hls " + (data.details || data.type || "error");
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
    } catch (_) {
      /* autoplay policies — muted should allow */
    }
    var ok = await waitPlaying(video, 14000);
    if (!ok && video.readyState < 2) {
      throw new Error(rec.liveError || "video never started (" + (resolved.via || "") + ")");
    }

    rec.live = true;
    rec.hubFrame = true;
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

    // first sample ASAP
    setTimeout(function () {
      var lum = sampleVideo(video, opts.glyphN || GLYPH_N);
      if (lum) {
        rec.lum = lum;
        if (typeof opts.onSample === "function") opts.onSample(rec);
      }
    }, 400);

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

  async function startMainLive(tileMap, mainIds, opts) {
    opts = opts || {};
    var max = opts.max != null ? opts.max : MAX_LIVE;
    var ids = (mainIds || []).slice(0, max);
    var results = { ok: 0, fail: 0, errors: [] };
    var consecutiveHardFail = 0;
    for (var i = 0; i < ids.length; i++) {
      if (typeof opts.isCancelled === "function" && opts.isCancelled()) {
        results.errors.push("cancelled");
        break;
      }
      var id = ids[i];
      var rec = tileMap.get(id);
      if (!rec) continue;
      try {
        if (opts.onStatus) opts.onStatus("resolving " + (rec.src.label || id) + "…");
        await startTileLive(rec, opts);
        results.ok++;
        consecutiveHardFail = 0;
        if (opts.onStatus)
          opts.onStatus(
            "live " +
              results.ok +
              "/" +
              ids.length +
              " · " +
              (rec.src.label || id) +
              " · " +
              (rec.resolved && rec.resolved.via)
          );
      } catch (e) {
        results.fail++;
        rec.liveError = e && e.message ? e.message : String(e);
        results.errors.push(id + ": " + rec.liveError);
        if (opts.onStatus) opts.onStatus("fail · " + id + " · " + rec.liveError);
        // stop early on blank/hub systemic failures
        if (/concurrent cap|not reachable|503|blank disabled/i.test(rec.liveError)) {
          consecutiveHardFail++;
          if (consecutiveHardFail >= 1) {
            results.errors.push("aborted remaining: " + rec.liveError);
            break;
          }
        }
      }
      if (i + 1 < ids.length) {
        await new Promise(function (r) {
          setTimeout(r, RESOLVE_GAP_MS);
        });
      }
    }
    return results;
  }

  function stopAllLive(tileMap) {
    if (!tileMap) return;
    tileMap.forEach(function (rec) {
      stopTileLive(rec);
      if (rec) rec.hubFrame = false;
    });
  }

  function countLive(tileMap) {
    var n = 0;
    if (!tileMap) return 0;
    tileMap.forEach(function (rec) {
      if (rec && (rec.live || rec.hubFrame) && rec.video) n++;
    });
    return n;
  }

  global.GY_NEWS_LIVE = {
    resolveStream: resolveStream,
    preflight: preflight,
    startTileLive: startTileLive,
    stopTileLive: stopTileLive,
    startMainLive: startMainLive,
    stopAllLive: stopAllLive,
    sampleVideo: sampleVideo,
    countLive: countLive,
    MAX_LIVE: MAX_LIVE,
    blankBase: blankBase,
    hubBase: hubBase,
  };
})(typeof window !== "undefined" ? window : globalThis);
