/**
 * Shared Live News / GrokGlyph → glyph-cast.html wire.
 * BroadcastChannel("gy-glyph-cast") · window.open · Presentation API.
 *
 * Message shape (type:glyph-cast):
 *   { type, glyphN, style, layout, ledPx, peers:[{id,nick,mode,lum|lumB64,glyphN}], t }
 */
(function (global) {
  "use strict";

  var CHANNEL = "gy-glyph-cast";

  function lumToB64(lum) {
    if (!lum || !lum.length) return "";
    try {
      var n = lum.length;
      var u8 = lum instanceof Uint8Array ? lum : null;
      if (!u8) {
        u8 = new Uint8Array(n);
        for (var i = 0; i < n; i++) {
          var v = lum[i];
          if (v <= 1) v = Math.round(v * 255);
          u8[i] = Math.max(0, Math.min(255, v | 0));
        }
      } else if (lum[0] <= 1 && lum[0] >= 0 && typeof lum[0] === "number") {
        // Float32 0–1
        var u = new Uint8Array(n);
        for (var j = 0; j < n; j++) u[j] = Math.max(0, Math.min(255, Math.round(lum[j] * 255)));
        u8 = u;
      }
      var s = "";
      var chunk = 0x8000;
      for (var o = 0; o < u8.length; o += chunk) {
        s += String.fromCharCode.apply(null, u8.subarray(o, o + chunk));
      }
      return btoa(s);
    } catch (e) {
      return "";
    }
  }

  function floatLumToU8(lum) {
    if (!lum || !lum.length) return new Uint8Array(0);
    if (lum instanceof Uint8Array) {
      // detect 0–1 float stored wrong — if max <= 1 treat as float
      var max = 0;
      for (var i = 0; i < Math.min(lum.length, 64); i++) if (lum[i] > max) max = lum[i];
      if (max <= 1) {
        var out = new Uint8Array(lum.length);
        for (var k = 0; k < lum.length; k++) out[k] = Math.round(lum[k] * 255);
        return out;
      }
      return lum;
    }
    var u = new Uint8Array(lum.length);
    for (var j = 0; j < lum.length; j++) {
      var v = lum[j];
      if (v <= 1) v = Math.round(v * 255);
      u[j] = Math.max(0, Math.min(255, v | 0));
    }
    return u;
  }

  /**
   * @param {object} opts
   * @param {Array<{id:string,nick:string,mode?:string,lum:any,glyphN?:number}>} opts.peers
   * @param {number} [opts.glyphN=25]
   * @param {string} [opts.style=matrix]
   * @param {string} [opts.layout=grid]
   * @param {string} [opts.ledPx=auto]
   * @param {string} [opts.room]
   * @param {string} [opts.source]
   */
  function buildPayload(opts) {
    opts = opts || {};
    var glyphN = opts.glyphN || 25;
    var peers = (opts.peers || []).map(function (p) {
      var lum = floatLumToU8(p.lum);
      return {
        id: String(p.id || p.nick || "feed"),
        nick: String(p.nick || p.id || "feed"),
        mode: p.mode || "cast",
        glyphN: p.glyphN || glyphN,
        lumB64: lumToB64(lum),
      };
    });
    return {
      type: "glyph-cast",
      glyphN: glyphN,
      style: opts.style || "matrix",
      layout: opts.layout || "grid",
      ledPx: opts.ledPx || "auto",
      peers: peers,
      room: opts.room || "",
      source: opts.source || "",
      t: Date.now(),
    };
  }

  function playerURL(qs) {
    var u = new URL("glyph-cast.html", global.location.href);
    qs = qs || {};
    Object.keys(qs).forEach(function (k) {
      if (qs[k] != null && qs[k] !== "") u.searchParams.set(k, String(qs[k]));
    });
    return u.href;
  }

  /**
   * Cast session controller.
   */
  function createSession(defaults) {
    defaults = defaults || {};
    var bc = null;
    var win = null;
    var pres = null;
    var on = false;
    var lastPush = 0;
    var listeners = [];

    function emit(ev, data) {
      listeners.forEach(function (fn) {
        try {
          fn(ev, data);
        } catch (_) {}
      });
    }

    function ensureBC() {
      if (bc) return bc;
      try {
        bc = new BroadcastChannel(CHANNEL);
        bc.onmessage = function (ev) {
          if (ev.data && ev.data.type === "glyph-cast-ready") {
            emit("ready", ev.data);
            push(true);
          }
        };
      } catch (_) {
        bc = null;
      }
      return bc;
    }

    function openPopup(fullscreen) {
      ensureBC();
      var url = playerURL({
        fs: fullscreen ? "1" : "",
        cast: "1",
        source: defaults.source || "",
        layout: defaults.layout || "grid",
        n: defaults.glyphN || 25,
        hub: defaults.hub || "",
        room: defaults.room || "",
      });
      if (win && !win.closed) {
        try {
          win.focus();
        } catch (_) {}
      } else {
        win = global.open(
          url,
          "gy-glyph-cast",
          "popup=yes,width=1280,height=800,menubar=no,toolbar=no,location=no,status=no"
        );
      }
      on = true;
      emit("open", { url: url, mode: "popup" });
      setTimeout(function () {
        push(true);
      }, 280);
      setTimeout(function () {
        push(true);
      }, 700);
    }

    function openPresentation() {
      if (!("PresentationRequest" in global)) {
        openPopup(true);
        return;
      }
      ensureBC();
      try {
        var req = new PresentationRequest([
          playerURL({
            fs: "1",
            cast: "1",
            source: defaults.source || "livenews",
            layout: defaults.layout || "grid",
            n: defaults.glyphN || 25,
            hub: defaults.hub || "",
            room: defaults.room || "",
          }),
        ]);
        req
          .start()
          .then(function (conn) {
            pres = conn;
            on = true;
            emit("open", { mode: "presentation" });
            conn.addEventListener("close", function () {
              pres = null;
              if (!win || win.closed) {
                on = false;
                emit("close");
              }
            });
            conn.addEventListener("message", function (e) {
              try {
                var m = typeof e.data === "string" ? JSON.parse(e.data) : e.data;
                if (m && m.type === "glyph-cast-ready") push(true);
              } catch (_) {}
            });
            push(true);
          })
          .catch(function () {
            openPopup(true);
          });
      } catch (_) {
        openPopup(true);
      }
    }

    /**
     * @param {object|function} payloadOrBuilder  buildPayload opts or fn → opts
     * @param {boolean} [force]
     */
    function push(payloadOrBuilder, force) {
      if (typeof payloadOrBuilder === "boolean") {
        force = payloadOrBuilder;
        payloadOrBuilder = null;
      }
      if (!on && !force) return false;
      if (win && win.closed && !pres) {
        on = false;
        emit("close");
        return false;
      }
      var now = performance.now();
      if (!force && now - lastPush < 90) return false;
      lastPush = now;

      var opts = defaults;
      if (typeof payloadOrBuilder === "function") {
        opts = Object.assign({}, defaults, payloadOrBuilder() || {});
      } else if (payloadOrBuilder && typeof payloadOrBuilder === "object") {
        opts = Object.assign({}, defaults, payloadOrBuilder);
      }
      var msg = buildPayload(opts);

      try {
        if (bc) bc.postMessage(msg);
      } catch (_) {}
      try {
        if (win && !win.closed && win.postMessage) win.postMessage(msg, "*");
      } catch (_) {}
      try {
        if (pres && pres.send) {
          pres.send(JSON.stringify(msg));
        }
      } catch (_) {}
      emit("push", msg);
      return true;
    }

    function close() {
      on = false;
      if (win && !win.closed) {
        try {
          win.close();
        } catch (_) {}
      }
      win = null;
      if (pres) {
        try {
          if (pres.terminate) pres.terminate();
          if (pres.close) pres.close();
        } catch (_) {}
        pres = null;
      }
      emit("close");
    }

    function isOn() {
      if (win && win.closed && !pres) on = false;
      return on;
    }

    return {
      open: function (opts) {
        opts = opts || {};
        if (opts.presentation) openPresentation();
        else openPopup(!!opts.fullscreen);
      },
      openPopup: openPopup,
      openPresentation: openPresentation,
      push: push,
      close: close,
      isOn: isOn,
      on: function (fn) {
        if (typeof fn === "function") listeners.push(fn);
      },
      setDefaults: function (d) {
        defaults = Object.assign({}, defaults, d || {});
      },
      CHANNEL: CHANNEL,
      buildPayload: buildPayload,
      playerURL: playerURL,
    };
  }

  global.GY_GLYPH_CAST_WIRE = {
    CHANNEL: CHANNEL,
    lumToB64: lumToB64,
    floatLumToU8: floatLumToU8,
    buildPayload: buildPayload,
    playerURL: playerURL,
    createSession: createSession,
  };
})(typeof window !== "undefined" ? window : globalThis);
