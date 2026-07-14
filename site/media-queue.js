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
    if (/^(www\.|youtu\.be\/|youtube\.com|twitch\.tv|tiktok\.com|vimeo\.com|x\.com|twitter\.com|t\.co\/)/i.test(s))
      return true;
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
    var quality = "best"; // best | 720
    var mediaTime = 0;
    var suppressMesh = false;
    var lastMeshSeek = 0;

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
        quality: quality,
        mediaTime: mediaTime,
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

    function setQuality(q) {
      quality = q === "720" ? "720" : "best";
      emit();
    }

    function setMediaTime(t) {
      mediaTime = Math.max(0, Number(t) || 0);
    }

    function absolutePlayURL(it) {
      if (!it || !it.video) return "";
      var v = it.video;
      if (/^https?:\/\//i.test(v)) return v;
      try {
        return new URL(v, hubHTTP(hubWs)).toString();
      } catch (_) {
        return hubHTTP(hubWs).replace(/\/$/, "") + (v.charAt(0) === "/" ? v : "/" + v);
      }
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
          var qParam = quality === "720" ? "720" : "best";
          var rm = await fetch(
            base +
              "/api/media/resolve?quality=" +
              encodeURIComponent(qParam) +
              "&url=" +
              encodeURIComponent(it.input),
            { headers: { Accept: "application/json" } }
          );
          data = await rm.json().catch(function () {
            return {};
          });
          if (!rm.ok || !data.ok) throw new Error((data && data.error) || "media resolve failed");
          it.video = data.video || "";
          it.audio = data.audio || "";
          it.title = data.title || it.title;
          it.live = !!data.live;
          it.via = data.via || "hub";
          it.platform = data.platform || data.platform || "";
          it.format = data.format || "";
          if (!it.video) throw new Error("no playable stream");
        }
        // also wrap raw video under hub if still external m3u8
        if (it.video && !/\/api\/media\/play\//.test(it.video) && /m3u8|hls/i.test(it.video)) {
          try {
            var wrap2 = await fetch(
              base +
                "/api/media/resolve?quality=best&url=" +
                encodeURIComponent(it.input),
              { headers: { Accept: "application/json" } }
            );
            var w2 = await wrap2.json().catch(function () {
              return null;
            });
            if (w2 && w2.ok && w2.video) it.video = w2.video;
          } catch (_) {}
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
      meshTimeline("playstate");
    }

    function next() {
      if (!items.length) return null;
      index = (index + 1) % items.length;
      mediaTime = 0;
      emit();
      meshTimeline("index");
      return items[index];
    }

    function prev() {
      if (!items.length) return null;
      index = (index - 1 + items.length) % items.length;
      mediaTime = 0;
      emit();
      meshTimeline("index");
      return items[index];
    }

    /** Seek relative seconds on current item (caller applies to <video>). */
    function seekRel(delta) {
      mediaTime = Math.max(0, mediaTime + (Number(delta) || 0));
      meshTimeline("seek");
      emit();
      return mediaTime;
    }

    function seekAbs(t) {
      mediaTime = Math.max(0, Number(t) || 0);
      meshTimeline("seek");
      emit();
      return mediaTime;
    }

    function meshTimeline(reason) {
      if (suppressMesh) return;
      if (typeof opts.sendMesh !== "function" && !(ws && ws.readyState === 1)) {
        meshSync();
        return;
      }
      var cur = items[index];
      var msg = {
        type: "media-queue",
        action: reason || "sync",
        mode: mode,
        index: index,
        playing: playing,
        mediaTime: mediaTime,
        quality: quality,
        video: cur ? absolutePlayURL(cur) : "",
        title: cur ? cur.title : "",
        input: cur ? cur.input : "",
        live: cur ? !!cur.live : false,
        t: Date.now(),
      };
      try {
        if (typeof opts.sendMesh === "function") opts.sendMesh(msg);
        else if (ws && ws.readyState === 1) ws.send(JSON.stringify(msg));
      } catch (_) {}
      meshSync();
    }

    /** Cast current resolved play URL into Sphere HDRI / dome layer */
    function castSphereDome() {
      var cur = items[index];
      if (!cur || !cur.video) return null;
      var video = absolutePlayURL(cur);
      var msg = {
        type: "media-dome",
        video: video,
        title: cur.title || cur.input,
        from: "queue",
        live: !!cur.live,
        mediaTime: mediaTime,
        playing: playing,
        t: Date.now(),
      };
      try {
        if (typeof opts.sendMesh === "function") opts.sendMesh(msg);
        else if (ws && ws.readyState === 1) ws.send(JSON.stringify(msg));
      } catch (_) {}
      return msg;
    }

    function applyRemoteTimeline(msg) {
      if (!msg || msg.type !== "media-queue") return;
      suppressMesh = true;
      try {
        if (typeof msg.index === "number" && msg.index >= 0 && msg.index < items.length) {
          index = msg.index;
        }
        if (typeof msg.playing === "boolean") {
          playing = msg.playing;
          items.forEach(function (it, i) {
            if (it.status === "playing") it.status = it.video ? "ready" : "queued";
            if (playing && i === index && it.video) it.status = "playing";
          });
        }
        if (typeof msg.mediaTime === "number") mediaTime = msg.mediaTime;
        if (msg.mode) mode = msg.mode;
        emit();
      } finally {
        suppressMesh = false;
      }
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
          mediaTime: mediaTime,
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

    function castTargets() {
      var base = hubHTTP(hubWs);
      var set = shareURL(base + "/queue.html");
      var sep = set.indexOf("?") >= 0 ? "&" : "?";
      var cur = items[index];
      var dome = cur && cur.video ? absolutePlayURL(cur) : "";
      return {
        queuePlayer: set + sep + "out=player&play=1",
        queueTV: set + sep + "out=tv&mode=seq&play=1&sync=1",
        glyphCast: base + "/glyph-cast.html?hub=" + encodeURIComponent(hubWs) + "&room=media",
        sphere:
          base +
          "/sphere.html?hdri=1" +
          (dome ? "&dome=" + encodeURIComponent(dome) : "") +
          "&hub=" +
          encodeURIComponent(hubWs),
        phone: base + "/phone.html?room=media&quick=1",
        share: set,
        domeVideo: dome,
      };
    }

    var dual = null;
    var meshNick = "queue";

    function handleInboundMedia(msg, via) {
      if (!msg) return;
      if (msg.type === "media-dome") {
        try {
          listeners.forEach(function (fn) {
            fn(Object.assign(snapshot(), { inboundDome: msg, via: via }));
          });
        } catch (_) {}
        return;
      }
      if (msg.type !== "media-queue") return;
      if (msg.action && msg.action !== "sync") {
        if (msg.from && meshNick && String(msg.from) === String(meshNick)) return;
        var now = Date.now();
        if (msg.action === "seek" && now - lastMeshSeek < 80) return;
        if (msg.action === "seek") lastMeshSeek = now;
        applyRemoteTimeline(msg);
      }
    }

    /**
     * Dual-path: LAN hub + gy-sfu (WebRTC DC or WS) for off-LAN phones.
     * Requires gy-sfu + gy sfu-bridge --room media.
     */
    function connectDual(cfg) {
      cfg = cfg || {};
      meshNick = cfg.nick || "queue-" + Math.random().toString(36).slice(2, 5);
      connectMesh(meshNick);
      if (typeof global.GY_MEDIA_SFU === "undefined") {
        return { hubOnly: true, dual: null };
      }
      try {
        if (dual) dual.disconnect();
      } catch (_) {}
      dual = global.GY_MEDIA_SFU.create({
        hubWs: hubWs,
        sfuWs: cfg.sfuWs,
        room: cfg.room || "media",
        nick: meshNick,
        token: cfg.token || "",
        webrtc: cfg.webrtc !== false,
        onMessage: function (msg, via) {
          handleInboundMedia(msg, via);
        },
        onStatus: cfg.onStatus || function () {},
      });
      opts.sendMesh = function (obj) {
        if (dual) return dual.send(obj);
        if (!ws || ws.readyState !== 1) return false;
        try {
          if (!obj.from) obj.from = meshNick;
          if (!obj.room) obj.room = "media";
          ws.send(JSON.stringify(obj));
          return true;
        } catch (_) {
          return false;
        }
      };
      dual.connect();
      return { hubOnly: false, dual: dual };
    }

    function connectMesh(nick) {
      meshNick = nick || meshNick || "queue";
      if (!hubWs || hubWs === "ws://" || hubWs === "wss://") return null;
      try {
        if (ws) ws.close();
      } catch (_) {}
      var url = hubWs;
      if (!/[?&]nick=/.test(url)) {
        url +=
          (url.includes("?") ? "&" : "?") +
          "nick=" +
          encodeURIComponent(meshNick) +
          "&role=queue&room=media";
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
              nick: meshNick,
              role: "queue",
              room: "media",
            })
          );
        } catch (_) {}
        meshTimeline("join");
      };
      ws.onmessage = function (ev) {
        var msg;
        try {
          msg = JSON.parse(ev.data);
        } catch (_) {
          return;
        }
        handleInboundMedia(msg, "hub");
      };
      opts.sendMesh = function (obj) {
        if (dual) return dual.send(obj);
        if (!ws || ws.readyState !== 1) return false;
        try {
          if (!obj.from) obj.from = meshNick;
          if (!obj.room) obj.room = "media";
          ws.send(JSON.stringify(obj));
          return true;
        } catch (_) {
          return false;
        }
      };
      return ws;
    }

    load();

    return {
      STORAGE: STORAGE,
      MAX_SIMUL: MAX_SIMUL,
      snapshot: snapshot,
      on: on,
      setHub: setHub,
      setMode: setMode,
      setQuality: setQuality,
      setMediaTime: setMediaTime,
      seekRel: seekRel,
      seekAbs: seekAbs,
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
      connectDual: connectDual,
      meshSync: meshSync,
      meshTimeline: meshTimeline,
      castSphereDome: castSphereDome,
      castTargets: castTargets,
      absolutePlayURL: absolutePlayURL,
      applyRemoteTimeline: applyRemoteTimeline,
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
    var scrubBound = false;

    function seekVideo(deltaOrAbs, absolute) {
      var v = els.videoMain;
      if (!v || !isFinite(v.duration) || v.duration <= 0) {
        if (absolute) q.seekAbs(deltaOrAbs);
        else q.seekRel(deltaOrAbs);
        return;
      }
      var t = absolute ? Number(deltaOrAbs) || 0 : (v.currentTime || 0) + (Number(deltaOrAbs) || 0);
      t = Math.max(0, Math.min(v.duration - 0.05, t));
      try {
        v.currentTime = t;
      } catch (_) {}
      q.setMediaTime(t);
      q.meshTimeline && q.meshTimeline("seek");
      if (els.scrub) {
        try {
          els.scrub.value = String(Math.floor(t));
          els.scrub.max = String(Math.floor(v.duration));
        } catch (_) {}
      }
      if (els.timeLab) {
        els.timeLab.textContent = fmtTime(t) + " / " + fmtTime(v.duration);
      }
    }

    function fmtTime(s) {
      s = Math.max(0, Math.floor(s || 0));
      var m = Math.floor(s / 60);
      var r = s % 60;
      return m + ":" + (r < 10 ? "0" : "") + r;
    }

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
      if (!scrubBound && els.videoMain) {
        scrubBound = true;
        els.videoMain.addEventListener("timeupdate", function () {
          var v = els.videoMain;
          if (!v) return;
          q.setMediaTime(v.currentTime || 0);
          if (els.scrub && isFinite(v.duration) && v.duration > 0) {
            els.scrub.max = String(Math.floor(v.duration));
            if (document.activeElement !== els.scrub) {
              els.scrub.value = String(Math.floor(v.currentTime || 0));
            }
          }
          if (els.timeLab && isFinite(v.duration)) {
            els.timeLab.textContent = fmtTime(v.currentTime) + " / " + fmtTime(v.duration);
          }
        });
        if (els.scrub) {
          els.scrub.addEventListener("input", function () {
            seekVideo(Number(els.scrub.value) || 0, true);
          });
        }
        // restore shared timeline
        var mt = q.snapshot().mediaTime;
        if (mt > 1) {
          try {
            els.videoMain.currentTime = mt;
          } catch (_) {}
        }
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
      seekRel: function (d) {
        seekVideo(d, false);
      },
      seekAbs: function (t) {
        seekVideo(t, true);
      },
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
