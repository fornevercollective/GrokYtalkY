/**
 * BitChat dual-path helper for GrokYtalkY site (sphere · GrokGlyph · chat).
 *
 * Native BitChat apps: https://github.com/permissionlesstech/bitchat
 * Hub bridge:          GET/POST /api/bitchat/*
 *
 * Browser cannot open BLE mesh; this module:
 *  - polls /api/bitchat status
 *  - sends dual-path chat (hub WS + optional /api/bitchat/send for egress queue)
 *  - tags UI for via:bitchat peers and control actions
 */
(function (global) {
  "use strict";

  function hubHTTP() {
    try {
      if (location.protocol === "http:" || location.protocol === "https:") {
        return location.origin;
      }
    } catch (_) {}
    return "http://127.0.0.1:9876";
  }

  /**
   * @param {object} opts
   * @param {function} [opts.sendMesh] (obj) → bool  hub WebSocket send
   * @param {function} [opts.getNick]
   * @param {function} [opts.getRoom]
   * @param {function} [opts.onStatus] (text, kind?)
   * @param {function} [opts.onPeer] (peer)
   * @param {function} [opts.onChat] ({from,text,via,transport})
   * @param {function} [opts.onControl] ({action,from,text})
   */
  function create(opts) {
    opts = opts || {};
    var state = {
      enabled: true,
      bridges: 0,
      peers: [],
      dualSend: true, // also queue hub chat for BLE egress
      lastSnap: null,
      pollTimer: 0,
    };

    function nick() {
      if (typeof opts.getNick === "function") {
        try {
          return opts.getNick() || "web";
        } catch (_) {}
      }
      return "web";
    }
    function room() {
      if (typeof opts.getRoom === "function") {
        try {
          return opts.getRoom() || "global";
        } catch (_) {}
      }
      return "global";
    }
    function status(t, kind) {
      if (typeof opts.onStatus === "function") opts.onStatus(t, kind);
    }

    async function refresh() {
      try {
        var r = await fetch(hubHTTP() + "/api/bitchat", {
          headers: { Accept: "application/json" },
        });
        var j = await r.json();
        state.lastSnap = j;
        state.enabled = !!j.enabled;
        state.bridges = j.bridges || 0;
        state.peers = Array.isArray(j.peers) ? j.peers : [];
        if (typeof opts.onPeer === "function") {
          state.peers.forEach(function (p) {
            opts.onPeer(p);
          });
        }
        return j;
      } catch (e) {
        state.enabled = false;
        return { ok: false, error: String(e) };
      }
    }

    /**
     * Send chat on Wi‑Fi hub and optionally queue for BitChat BLE egress.
     */
    function sendChat(text, extra) {
      text = String(text || "").trim();
      if (!text) return false;
      extra = extra || {};
      var from = extra.from || nick();
      var meshOk = false;
      if (typeof opts.sendMesh === "function") {
        meshOk = !!opts.sendMesh({
          type: "chat",
          text: text,
          from: from,
          room: extra.room || room(),
          t: Date.now(),
          meta: {
            via: "wifi",
            dual: state.dualSend,
            bitchat_egress: state.dualSend,
          },
        });
      }
      // also push send API so egress queue fills even if WS not open
      if (state.dualSend) {
        fetch(hubHTTP() + "/api/bitchat/send", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            text: text,
            from: from,
            room: extra.room || room(),
            dual: true,
          }),
        }).catch(function () {});
      }
      return meshOk;
    }

    /** Handle inbound mesh message (from hub WS). */
    function onMesh(msg) {
      if (!msg || !msg.type) return;
      var via =
        (msg.meta && msg.meta.via) ||
        msg.via ||
        (msg.type && String(msg.type).indexOf("bitchat") === 0 ? "bitchat" : "");
      if (msg.type === "bitchat-chat" || (msg.type === "chat" && via === "bitchat")) {
        if (typeof opts.onChat === "function") {
          opts.onChat({
            from: msg.from,
            text: msg.text,
            via: "bitchat",
            transport: (msg.meta && msg.meta.transport) || "ble",
            room: msg.room,
          });
        }
        return;
      }
      if (msg.type === "bitchat-presence") {
        if (typeof opts.onPeer === "function") {
          opts.onPeer({
            nick: msg.from,
            transport: (msg.meta && msg.meta.transport) || "ble",
            last_seen: msg.t || Date.now(),
          });
        }
        return;
      }
      if (msg.type === "bitchat-control") {
        if (typeof opts.onControl === "function") {
          opts.onControl({
            action: msg.action,
            from: msg.from,
            text: msg.text,
          });
        }
      }
    }

    function startPoll(ms) {
      stopPoll();
      ms = ms || 8000;
      refresh();
      state.pollTimer = setInterval(refresh, ms);
    }
    function stopPoll() {
      if (state.pollTimer) clearInterval(state.pollTimer);
      state.pollTimer = 0;
    }

    function setDual(on) {
      state.dualSend = !!on;
    }

    function summaryLine() {
      if (!state.enabled) return "bitchat off";
      var p = state.peers.length;
      var b = state.bridges;
      return "bitchat · bridges " + b + " · peers " + p;
    }

    return {
      refresh: refresh,
      sendChat: sendChat,
      onMesh: onMesh,
      startPoll: startPoll,
      stopPoll: stopPoll,
      setDual: setDual,
      summaryLine: summaryLine,
      getState: function () {
        return {
          enabled: state.enabled,
          bridges: state.bridges,
          peers: state.peers.slice(),
          dualSend: state.dualSend,
          snap: state.lastSnap,
        };
      },
    };
  }

  /** One-shot sim for demos without native app */
  async function sim(text, from) {
    var r = await fetch(hubHTTP() + "/api/bitchat/sim", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        text: text || "hello from BLE sim",
        from: from || "alice",
      }),
    });
    return r.json();
  }

  global.GY_BITCHAT = {
    create: create,
    sim: sim,
    hubHTTP: hubHTTP,
  };
})(typeof window !== "undefined" ? window : globalThis);
