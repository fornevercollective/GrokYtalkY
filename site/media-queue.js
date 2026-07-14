/**
 * Multi-link media queue — web links · social handles · play · cast out.
 *
 * Resolve: hub GET /api/media/resolve · /api/social
 * Persist: localStorage gy.media.queue.v1
 * Share:   ?set=… (base64url JSON) or #add=url
 * Mesh:    type:media-queue  (optional fan-out to other devices)
 */
(function (global) {
  "use strict";

  var STORAGE = "gy.media.queue.v1";
  var MAX_ITEMS = 48;
  var MAX_SIMUL = 4;

  function uid() {
    return "q" + Date.now().toString(36) + Math.random().toString(36).slice(2, 7);
  }

  function hubHTTP(hubWs) {
    try {
      if (hubWs) {
        var u = new URL(String(hubWs).replace(/^ws/i, function (m) {
          return m.toLowerCase() === "wss" ? "https" : "http";
        }));
        return u.origin;
      }
    } catch (_) {}
    try {
      if (location.protocol === "http:" || location.protocol === "https:") {
        if (!(location.hostname || "").includes("github.io")) return location.origin;
      }
    } catch (_) {}
    return "http://127.0.0.1:9876";
  }

  function defaultHubWS() {
    try {
      if ((location.hostname || "").includes("github.io")) return "ws://";
      if (location.protocol === "file:") return "ws://127.0.0.1:9876/";
      if (location.port === "9876") {
        return (location.protocol === "https:" ? "wss:" : "ws:") + "//" + location.host + "/";
      }
      return "ws://" + (location.hostname || "127.0.0.1") + ":9876/";
    } catch (_) {
      return "ws://127.0.0.1:9876/";
    }
  }

  function looksSocial(s) {
    s = String(s || "").trim();
    if (!s) return false;
    if (/^https?:\/\//i.test(s)) return false;
    if (/^@[\w.-]+/.test(s)) return true;
    if (/^(yt|youtube|twitch|tt|tiktok|ig|instagram|x|twitter|kick):/i.test(s)) return true;
    if (/^social:/i.test(s)) return true;
    return false;
  }

  function looksURL(s) {
    s = String(s || "").trim();
    if (/^https?:\/\//i.test(s)) return true;
    if (/^(www\.|youtu\.be\/|youtube\.com|twitch\.tv|tiktok\.com|vimeo\.com)/i.test(s)) return true;
    return false;
  }

  function normalizeInput(s) {
    s = String(s || "").trim();
    if (!s) return "";
    if (looksURL(s) && !/^https?:\/\//i.test(s)) s = "https://" + s;
    return s;
  }

  function splitInputs(raw) {
    raw = String(raw || "");
    // newlines, commas, or multiple spaces between urls
    var parts = raw.split(/[\n\r,;]+|\s{2,}/);
    var out = [];
    var seen = {};
    parts.forEach(function (p) {
      p = normalizeInput(p);
      if (!p || seen[p]) return;
      // single-space lists of urls
      if (/\s/.test(p) && /https?:\/\//i.test(p)) {
        p.split(/\s+/).forEach(function (q) {
          q = normalizeInput(q);
          if (q && !seen[q]) {
            seen[q] = 1;
            out.push(q);
          }
        });
        return;
      }
      seen[p] = 1;
      out.push(p);
    });
    return out;
  }

  function b64urlEncode(str) {
    try {
      var b = btoa(unescape(encodeURIComponent(str)));
      return b.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
    } catch (_) {
      return "";
    }
  }

  function b64urlDecode(s) {
    try {
      s = String(s || "").replace(/-/g, "+").replace(/_/g, "/");
      while (s.length % 4) s += "=";
      return decodeURIComponent(escape(atob(s)));
    } catch (_) {
      return "";
    }
  }

  /**
   * @param {object} [opts]
   * @param {string} [opts.hubWs]
   * @param {function} [opts.onChange]
   * @param {function} [opts.sendMesh] (obj) => bool
   */
  function create(opts) {
    opts = opts || {};
    var hubWs = opts.hubWs || defaultHubWS();
    var items = [];
    var index = 0; // current play index
    var mode = "seq"; // seq | multi | audio
    var playing = false;
    var ws = null;
    var listeners = [];

    function emit() {
      var snap = snapshot();
      listeners.forEach(function (fn) {
        try {
          fn(snap);
        } catch (_) {}
      });
      if (typeof opts.onChange === "function") {
        try {
          opts.onChange(snap);
        } catch (_) {}
      }
      save();
    }

    function snapshot() {
      return {
        items: items.slice(),
        index: index,
        mode: mode,
        playing: playing,
        current: items[index] || null,
        hubWs: hubWs,
        hubHttp: hubHTTP(hubWs),
      };
    }

    function save() {
      try {
        localStorage.setItem(
          STORAGE,
          JSON.stringify({
            items: items.map(function (it) {
              return {
                id: it.id,
                input: it.input,
                title: it.title,
                video: it.video,
                audio: it.audio,
                live: it.live,
                via: it.via,
                platform: it.platform,
                status: it.status === "playing" ? "ready" : it.status,
                error: it.error || "",
                t: it.t,
              };
            }),
            index: index,
            mode: mode,
            hubWs: hubWs,
          })
        );
      } catch (_) {}
    }

    function load() {
      try {
        var raw = localStorage.getItem(STORAGE);
        if (!raw) return;
        var st = JSON.parse(raw);
        if (Array.isArray(st.items)) items = st.items;
        if (typeof st.index === "number") index = Math.max(0, Math.min(st.index, Math.max(0, items.length - 1)));
        if (st.mode) mode = st.mode;
        if (st.hubWs) hubWs = st.hubWs;
      } catch (_) {}
    }

    function on(fn) {
      if (typeof fn === "function") listeners.push(fn);
      return function off() {
        listeners = listeners.filter(function (f) {
          return f !== fn;
        });
      };
    }

    function setHub(wsUrl) {
      hubWs = wsUrl || defaultHubWS();
      emit();
    }

    function setMode(m) {
      if (m === "multi" || m === "audio" || m === "seq") mode = m;
      emit();
    }

    function find(id) {
      for (var i = 0; i < items.length; i++) if (items[i].id === id) return items[i];
      return null;
    }

    function addOne(input, meta) {
      meta = meta || {};
      input = normalizeInput(input);
      if (!input) return null;
      // de-dupe by input
      for (var i = 0; i < items.length; i++) {
        if (items[i].input === input) return items[i];
      }
      if (items.length >= MAX_ITEMS) items.shift();
      var it = {
        id: uid(),
        input: input,
        title: meta.title || input.slice(0, 64),
        video: meta.video || "",
        audio: meta.audio || "",
        live: !!meta.live,
        via: meta.via || "",
        platform: meta.platform || "",
        status: meta.video ? "ready" : "queued",
        error: "",
        t: Date.now(),
      };
      items.push(it);
      emit();
      meshSync();
      return it;
    }

    function addMany(raw) {
      var list = Array.isArray(raw) ? raw : splitInputs(raw);
      var added = [];
      list.forEach(function (s) {
        var it = addOne(s);
        if (it) added.push(it);
      });
      return added;
    }

    function remove(id) {
      var was = items[index] && items[index].id;
      items = items.filter(function (it) {
        return it.id !== id;
      });
      if (index >= items.length) index = Math.max(0, items.length - 1);
      if (was && (!items[index] || items[index].id !== was)) {
        /* index adjusted */
      }
      emit();
      meshSync();
    }

    function clear() {
      items = [];
      index = 0;
      playing = false;
      emit();
      meshSync();
    }

    function move(id, dir) {
      var i = -1;
      for (var n = 0; n < items.length; n++) if (items[n].id === id) i = n;
      if (i < 0) return;
      var j = i + (dir < 0 ? -1 : 1);
      if (j < 0 || j >= items.length) return;
      var tmp = items[i];
      items[i] = items[j];
      items[j] = tmp;
      if (index === i) index = j;
      else if (index === j) index = i;
      emit();
    }

    function select(idOrIndex) {
      if (typeof idOrIndex === "number") {
        index = Math.max(0, Math.min(idOrIndex, items.length - 1));
      } else {
        for (var i = 0; i < items.length; i++) {
          if (items[i].id === idOrIndex) {
            index = i;
            break;
          }
        }
      }
      emit();
    }

    async function resolveItem(it) {
      if (!it) return it;
      it.status = "resolving";
      it.error = "";
      emit();
      var base = hubHTTP(hubWs);
      try {
        var data = null;
        if (looksSocial(it.input)) {
          var rs = await fetch(base + "/api/social?q=" + encodeURIComponent(it.input), {
            headers: { Accept: "application/json" },
          });
          data = await rs.json().catch(function () {
            return {};
          });
          if (!rs.ok) throw new Error(data.error || "social resolve failed");
          // social returns video/title; wrap via media resolve if needed for CORS
          it.title = data.title || it.title;
          it.live = !!data.live;
          it.platform = data.platform || "";
          it.via = data.via || "social";
          if (data.video) {
            // Prefer hub-wrapped play URL when raw CDN would CORS-block
            if (/m3u8|hls|googlevideo|manifest/i.test(data.video) && !/\/api\/media\/play\//.test(data.video)) {
              try {
                var wrap = await fetch(
                  base + "/api/media/resolve?url=" + encodeURIComponent(it.input),
                  { headers: { Accept: "application/json" } }
                );
                var wj = await wrap.json().catch(function () {
                  return null;
                });
                if (wj && wj.ok && wj.video) {
                  it.video = wj.video;
                  it.audio = wj.audio || data.audio || "";
                  it.via = wj.via || it.via;
                  it.live = wj.live != null ? !!wj.live : it.live;
                } else {
                  it.video = data.video;
                  it.audio = data.audio || "";
                }
              } catch (_) {
                it.video = data.video;
                it.audio = data.audio || "";
              }
            } else {
              it.video = data.video;
              it.audio = data.audio || "";
            }
          } else {
            throw new Error("no stream for " + it.input);
          }
        } else {
          var rm = await fetch(base + "/api/media/resolve?url=" + encodeURIComponent(it.input), {
            headers: { Accept: "application/json" },
          });
          data = await rm.json().catch(function () {
            return {};
          });
          if (!rm.ok || !data.ok) throw new Error((data && data.error) || "media resolve failed");
          it.video = data.video || "";
          it.audio = data.audio || "";
          it.title = data.title || it.title;
          it.live = !!data.live;
          it.via = data.via || "hub";
          it.platform = data.platform || "";
          if (!it.video) throw new Error("no playable stream");
        }
        it.status = "ready";
      } catch (e) {
        it.status = "error";
        it.error = e && e.message ? e.message : String(e);
      }
      emit();
      return it;
    }

    async function resolveAll(onlyQueued) {
      var list = items.slice();
      for (var i = 0; i < list.length; i++) {
        var it = list[i];
        if (onlyQueued && it.status === "ready" && it.video) continue;
        if (it.status === "playing" && it.video) continue;
        await resolveItem(it);
      }
      return snapshot();
    }

    function setPlaying(on) {
      playing = !!on;
      items.forEach(function (it, i) {
        if (it.status === "playing") it.status = it.video ? "ready" : "queued";
        if (playing && i === index && it.video) it.status = "playing";
      });
      emit();
      meshSync();
    }

    function next() {
      if (!items.length) return null;
      index = (index + 1) % items.length;
      emit();
      return items[index];
    }

    function prev() {
      if (!items.length) return null;
      index = (index - 1 + items.length) % items.length;
      emit();
      return items[index];
    }

    /** Items to play for current mode (seq: 1, multi: up to MAX_SIMUL ready). */
    function playSet() {
      if (!items.length) return [];
      if (mode === "seq" || mode === "audio") {
        var cur = items[index];
        return cur ? [cur] : [];
      }
      // multi: from index, ready/playing items
      var out = [];
      for (var i = 0; i < items.length && out.length < MAX_SIMUL; i++) {
        var j = (index + i) % items.length;
        var it = items[j];
        if (it && (it.video || it.status === "ready" || it.status === "playing")) out.push(it);
        else if (it && it.status !== "error") out.push(it);
      }
      return out;
    }

    function exportSet() {
      return {
        v: 1,
        t: Date.now(),
        mode: mode,
        index: index,
        items: items.map(function (it) {
          return { input: it.input, title: it.title };
        }),
      };
    }

    function importSet(obj) {
      if (!obj) return;
      if (typeof obj === "string") {
        try {
          obj = JSON.parse(obj);
        } catch (_) {
          // treat as raw multi-line inputs
          addMany(obj);
          return;
        }
      }
      if (obj.mode) mode = obj.mode;
      if (Array.isArray(obj.items)) {
        obj.items.forEach(function (row) {
          if (!row) return;
          if (typeof row === "string") addOne(row);
          else addOne(row.input || row.url || "", { title: row.title });
        });
      }
      if (typeof obj.index === "number") index = Math.max(0, Math.min(obj.index, items.length - 1));
      emit();
    }

    function shareURL(pageBase) {
      var payload = b64urlEncode(JSON.stringify(exportSet()));
      var base = pageBase || "";
      try {
        if (!base) base = location.href.split("?")[0].split("#")[0];
      } catch (_) {
        base = "queue.html";
      }
      return base + "?set=" + payload;
    }

    function loadFromLocation() {
      try {
        var q = new URLSearchParams(location.search || "");
        var set = q.get("set") || q.get("links");
        if (set) {
          var json = b64urlDecode(set);
          if (json) importSet(json);
        }
        var add = q.get("add") || q.get("url");
        if (add) addMany(add);
        var hash = (location.hash || "").replace(/^#/, "");
        if (hash.indexOf("add=") === 0) {
          addMany(decodeURIComponent(hash.slice(4)));
        }
      } catch (_) {}
    }

    function meshSync() {
      if (typeof opts.sendMesh !== "function") return;
      try {
        opts.sendMesh({
          type: "media-queue",
          action: "sync",
          mode: mode,
          index: index,
          playing: playing,
          items: items.map(function (it) {
            return {
              id: it.id,
              input: it.input,
              title: it.title,
              status: it.status,
              live: it.live,
              video: it.video ? true : false,
            };
          }),
          t: Date.now(),
        });
      } catch (_) {}
    }

    function connectMesh(nick) {
      if (!hubWs || hubWs === "ws://" || hubWs === "wss://") return null;
      try {
        if (ws) ws.close();
      } catch (_) {}
      var url = hubWs;
      if (!/[?&]nick=/.test(url)) {
        url += (url.includes("?") ? "&" : "?") + "nick=" + encodeURIComponent(nick || "queue") + "&role=queue&room=media";
      }
      try {
        ws = new WebSocket(url);
      } catch (_) {
        return null;
      }
      ws.onopen = function () {
        try {
          ws.send(
            JSON.stringify({
              type: "join",
              nick: nick || "queue",
              role: "queue",
              room: "media",
            })
          );
        } catch (_) {}
        meshSync();
      };
      return ws;
    }

    function castTargets() {
      var base = hubHTTP(hubWs);
      var set = shareURL(base + "/queue.html");
      return {
        queuePlayer: set + (set.indexOf("?") >= 0 ? "&" : "?") + "out=player",
        queueTV: set + (set.indexOf("?") >= 0 ? "&" : "?") + "out=tv&mode=seq",
        glyphCast: base + "/glyph-cast.html?hub=" + encodeURIComponent(hubWs) + "&room=media",
        sphere: base + "/sphere.html",
        phone: base + "/phone.html?room=media&quick=1",
        share: set,
      };
    }

    load();

    return {
      STORAGE: STORAGE,
      MAX_SIMUL: MAX_SIMUL,
      snapshot: snapshot,
      on: on,
      setHub: setHub,
      setMode: setMode,
      addOne: addOne,
      addMany: addMany,
      remove: remove,
      clear: clear,
      move: move,
      select: select,
      find: find,
      resolveItem: resolveItem,
      resolveAll: resolveAll,
      setPlaying: setPlaying,
      next: next,
      prev: prev,
      playSet: playSet,
      exportSet: exportSet,
      importSet: importSet,
      shareURL: shareURL,
      loadFromLocation: loadFromLocation,
      connectMesh: connectMesh,
      meshSync: meshSync,
      castTargets: castTargets,
      hubHTTP: function () {
        return hubHTTP(hubWs);
      },
      defaultHubWS: defaultHubWS,
      splitInputs: splitInputs,
      looksSocial: looksSocial,
    };
  }

  /**
   * Attach HTML media elements to a queue snapshot / engine.
   * @param {ReturnType<create>} q
   * @param {object} els { stage, videoMain, audioOnly, multiWrap, status }
   */
  function bindPlayer(q, els) {
    els = els || {};
    var multiVideos = [];
    var hlsInstances = []; // optional hls.js if present
    var endedBound = false;

    function destroyMulti() {
      multiVideos.forEach(function (v) {
        try {
          v.pause();
          v.removeAttribute("src");
          v.load();
          if (v.parentNode) v.parentNode.removeChild(v);
        } catch (_) {}
      });
      multiVideos = [];
      hlsInstances.forEach(function (h) {
        try {
          h.destroy && h.destroy();
        } catch (_) {}
      });
      hlsInstances = [];
      if (els.multiWrap) els.multiWrap.innerHTML = "";
    }

    function setStatus(t) {
      if (els.status) els.status.textContent = t || "";
    }

    function attachSrc(videoEl, url) {
      if (!videoEl || !url) return Promise.resolve(false);
      return new Promise(function (resolve) {
        videoEl.crossOrigin = "anonymous";
        // native HLS (Safari) or progressive
        if (/\.m3u8(\?|$)/i.test(url) || /\/api\/media\/play\//i.test(url)) {
          if (videoEl.canPlayType("application/vnd.apple.mpegurl")) {
            videoEl.src = url;
          } else if (global.Hls && global.Hls.isSupported && global.Hls.isSupported()) {
            var hls = new global.Hls({ enableWorker: true, lowLatencyMode: true });
            hls.loadSource(url);
            hls.attachMedia(videoEl);
            hlsInstances.push(hls);
          } else {
            videoEl.src = url;
          }
        } else {
          videoEl.src = url;
        }
        var done = function (ok) {
          videoEl.onloadeddata = null;
          videoEl.onerror = null;
          resolve(ok);
        };
        videoEl.onloadeddata = function () {
          done(true);
        };
        videoEl.onerror = function () {
          done(false);
        };
        videoEl.load();
        setTimeout(function () {
          done(videoEl.readyState >= 2);
        }, 8000);
      });
    }

    async function apply() {
      var snap = q.snapshot();
      var set = q.playSet();
      destroyMulti();

      if (els.videoMain) {
        try {
          els.videoMain.pause();
        } catch (_) {}
      }
      if (els.audioOnly) {
        try {
          els.audioOnly.pause();
        } catch (_) {}
      }

      if (!set.length) {
        setStatus("queue empty — paste links or @handles");
        return;
      }

      if (snap.mode === "audio") {
        var a = set[0];
        if (!a.video && !a.audio) {
          setStatus("resolve first…");
          await q.resolveItem(a);
          a = q.snapshot().current;
        }
        var srcA = (a && (a.audio || a.video)) || "";
        if (els.audioOnly && srcA) {
          els.audioOnly.src = srcA;
          if (snap.playing) {
            try {
              await els.audioOnly.play();
            } catch (_) {}
          }
          setStatus("speakers · " + (a.title || a.input));
        }
        if (els.videoMain) els.videoMain.removeAttribute("src");
        return;
      }

      if (snap.mode === "multi") {
        if (els.videoMain) {
          els.videoMain.hidden = true;
        }
        if (els.multiWrap) {
          els.multiWrap.hidden = false;
          for (var i = 0; i < set.length; i++) {
            var it = set[i];
            if (!it.video) {
              await q.resolveItem(it);
              it = q.find(it.id) || it;
            }
            if (!it.video) continue;
            var cell = document.createElement("div");
            cell.className = "mq-multi-cell";
            var lab = document.createElement("span");
            lab.className = "mq-multi-lab";
            lab.textContent = (it.live ? "LIVE · " : "") + (it.title || it.input).slice(0, 40);
            var v = document.createElement("video");
            v.playsInline = true;
            v.muted = true; // multi: mute to allow autoplay; main can unmute
            v.autoplay = true;
            v.controls = true;
            cell.appendChild(v);
            cell.appendChild(lab);
            els.multiWrap.appendChild(cell);
            multiVideos.push(v);
            await attachSrc(v, it.video);
            if (snap.playing) {
              try {
                await v.play();
              } catch (_) {}
            }
          }
          setStatus("multi · " + multiVideos.length + " streams");
        }
        return;
      }

      // sequential
      if (els.multiWrap) els.multiWrap.hidden = true;
      if (els.videoMain) els.videoMain.hidden = false;
      var cur = set[0];
      if (!cur.video) {
        setStatus("resolving…");
        await q.resolveItem(cur);
        cur = q.snapshot().current || cur;
      }
      if (!cur || !cur.video) {
        setStatus((cur && cur.error) || "no stream");
        return;
      }
      await attachSrc(els.videoMain, cur.video);
      if (!endedBound && els.videoMain) {
        endedBound = true;
        els.videoMain.addEventListener("ended", function () {
          if (q.snapshot().mode !== "seq") return;
          q.next();
          q.setPlaying(true);
          apply();
        });
      }
      if (snap.playing) {
        try {
          els.videoMain.muted = false;
          await els.videoMain.play();
        } catch (_) {
          try {
            els.videoMain.muted = true;
            await els.videoMain.play();
            setStatus("playing muted · tap unmute · " + (cur.title || "").slice(0, 40));
            return;
          } catch (e2) {
            setStatus("play blocked · tap play");
            return;
          }
        }
      }
      setStatus(
        (snap.playing ? "playing" : "ready") +
          " · " +
          (cur.live ? "LIVE · " : "") +
          (cur.title || cur.input).slice(0, 48)
      );
    }

    q.on(function () {
      /* external will call apply when needed */
    });

    return {
      apply: apply,
      destroy: destroyMulti,
      setStatus: setStatus,
    };
  }

  global.GY_MEDIA_QUEUE = {
    create: create,
    bindPlayer: bindPlayer,
    hubHTTP: hubHTTP,
    defaultHubWS: defaultHubWS,
    splitInputs: splitInputs,
    b64urlEncode: b64urlEncode,
    b64urlDecode: b64urlDecode,
    STORAGE: STORAGE,
  };
})(typeof window !== "undefined" ? window : globalThis);
