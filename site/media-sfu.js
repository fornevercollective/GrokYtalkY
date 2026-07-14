/**
 * Dual-path media timeline relay: hub WS + gy-sfu (WebRTC DC or WS).
 *
 * Off-LAN phones join SFU (TURN/public host) while director stays on LAN hub;
 * sfu-bridge mirrors media-queue / media-dome both ways.
 *
 * Usage:
 *   var dual = GY_MEDIA_SFU.create({
 *     hubWs, sfuWs, room, nick, token,
 *     onMessage: function(msg, via) {},
 *   });
 *   dual.send(obj);  // fan both paths
 *   dual.connect();
 */
(function (global) {
  "use strict";

  function defaultSfuWS() {
    try {
      if (location.port === "9876" || location.port === "9880") {
        var host = location.hostname || "127.0.0.1";
        return "ws://" + host + ":9880/ws";
      }
    } catch (_) {}
    return "ws://127.0.0.1:9880/ws";
  }

  /**
   * @param {object} opts
   */
  function create(opts) {
    opts = opts || {};
    var hubWsUrl = opts.hubWs || "";
    var sfuBase = opts.sfuWs || defaultSfuWS();
    var room = opts.room || "media";
    var nick = opts.nick || "queue-" + Math.random().toString(36).slice(2, 5);
    var token = opts.token || "";
    var onMessage = typeof opts.onMessage === "function" ? opts.onMessage : function () {};
    var onStatus = typeof opts.onStatus === "function" ? opts.onStatus : function () {};

    var hub = null;
    var sfu = null;
    var pc = null;
    var dcs = {};
    var peerId = null;
    var wantMedia = opts.webrtc !== false; // try WebRTC DC when SFU media=true
    var connected = { hub: false, sfu: false, dc: false };

    function status() {
      var parts = [];
      if (connected.hub) parts.push("hub");
      if (connected.sfu) parts.push("sfu");
      if (connected.dc) parts.push("dc");
      onStatus(parts.length ? "dual · " + parts.join("+") : "dual off", connected);
      return connected;
    }

    function emitIn(msg, via) {
      if (!msg || typeof msg !== "object") return;
      // unwrap SFU chat dual-path
      if (msg.type === "chat" && msg.meta && msg.meta.mq) {
        var body = msg.meta.mq;
        if (typeof body === "object") {
          if (!body.from) body.from = msg.from || msg.nick;
          onMessage(body, via || "sfu");
          return;
        }
      }
      if (msg.type === "media-queue" || msg.type === "media-dome") {
        onMessage(msg, via || "hub");
      }
    }

    function wrapForSfu(obj) {
      var typ = obj.type || "media-queue";
      var title = obj.title || obj.action || typ;
      return {
        type: "chat",
        from: obj.from || nick,
        text: "◈ " + typ + " · " + String(title).slice(0, 48),
        role: "media",
        meta: {
          lane: typ,
          mq: obj,
        },
      };
    }

    function send(obj) {
      if (!obj || typeof obj !== "object") return false;
      var ok = false;
      var payload = Object.assign({}, obj);
      if (!payload.from) payload.from = nick;
      if (!payload.room) payload.room = room;
      if (!payload.t) payload.t = Date.now();

      // Hub path (LAN)
      if (hub && hub.readyState === 1) {
        try {
          hub.send(JSON.stringify(payload));
          ok = true;
        } catch (_) {}
      }

      // SFU path — prefer DC chat, fallback WS
      var sfuMsg = wrapForSfu(payload);
      var dc = dcs.chat;
      if (dc && dc.readyState === "open") {
        try {
          dc.send(JSON.stringify(sfuMsg));
          ok = true;
        } catch (_) {}
      } else if (sfu && sfu.readyState === 1) {
        try {
          sfu.send(JSON.stringify(sfuMsg));
          ok = true;
        } catch (_) {}
      }
      return ok;
    }

    function wireDc(dc) {
      if (!dc) return;
      var label = (dc.label || "chat").toLowerCase();
      dcs[label] = dc;
      dc.onopen = function () {
        connected.dc = true;
        status();
      };
      dc.onclose = function () {
        if (dcs[label] === dc) delete dcs[label];
        connected.dc = Object.keys(dcs).some(function (k) {
          return dcs[k] && dcs[k].readyState === "open";
        });
        status();
      };
      dc.onmessage = function (ev) {
        var raw = ev.data;
        if (typeof raw !== "string") return;
        try {
          emitIn(JSON.parse(raw), "sfu-dc");
        } catch (_) {}
      };
    }

    async function ensurePc() {
      if (pc) return pc;
      pc = new RTCPeerConnection({
        iceServers: [
          { urls: "stun:stun.l.google.com:19302" },
        ].concat(
          (opts.turnUrls || []).map(function (u) {
            return typeof u === "string" ? { urls: u } : u;
          })
        ),
      });
      pc.onicecandidate = function (ev) {
        if (!ev.candidate || !sfu || sfu.readyState !== 1) return;
        try {
          sfu.send(
            JSON.stringify({
              type: "ice",
              candidate: ev.candidate.toJSON(),
            })
          );
        } catch (_) {}
      };
      pc.ondatachannel = function (ev) {
        wireDc(ev.channel);
      };
      // Client DCs — timeline only, no camera
      ["chat", "glyph"].forEach(function (label) {
        try {
          wireDc(pc.createDataChannel(label));
        } catch (_) {}
      });
      // recv-only transceiver keeps negotiation happy without getUserMedia
      try {
        pc.addTransceiver("video", { direction: "recvonly" });
        pc.addTransceiver("audio", { direction: "recvonly" });
      } catch (_) {}
      return pc;
    }

    async function negotiate() {
      if (!pc || !sfu || sfu.readyState !== 1) return;
      var offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      sfu.send(JSON.stringify({ type: "offer", sdp: offer.sdp }));
    }

    async function onSfuMsg(msg) {
      if (msg.type === "welcome") {
        peerId = msg.peer_id;
        connected.sfu = true;
        status();
        if (wantMedia && msg.media) {
          try {
            await ensurePc();
            await negotiate();
          } catch (e) {
            onStatus("webrtc fail · WS-only dual", connected);
          }
        }
        return;
      }
      if (msg.type === "answer" && pc) {
        try {
          await pc.setRemoteDescription({ type: "answer", sdp: msg.sdp });
        } catch (_) {}
        return;
      }
      if (msg.type === "offer" && pc) {
        try {
          await pc.setRemoteDescription({ type: "offer", sdp: msg.sdp });
          var answer = await pc.createAnswer();
          await pc.setLocalDescription(answer);
          sfu.send(JSON.stringify({ type: "answer", sdp: answer.sdp }));
        } catch (_) {}
        return;
      }
      if (msg.type === "ice" && pc && msg.candidate) {
        try {
          await pc.addIceCandidate(msg.candidate);
        } catch (_) {}
        return;
      }
      if (msg.type === "chat" || msg.type === "media-queue" || msg.type === "media-dome") {
        emitIn(msg, "sfu");
      }
    }

    function connectHub() {
      if (!hubWsUrl || hubWsUrl === "ws://" || hubWsUrl === "wss://") return;
      try {
        if (hub) hub.close();
      } catch (_) {}
      var u = hubWsUrl;
      if (!/[?&]nick=/.test(u)) {
        u +=
          (u.indexOf("?") >= 0 ? "&" : "?") +
          "nick=" +
          encodeURIComponent(nick) +
          "&role=queue&room=" +
          encodeURIComponent(room);
      }
      try {
        hub = new WebSocket(u);
      } catch (_) {
        return;
      }
      hub.onopen = function () {
        connected.hub = true;
        status();
        try {
          hub.send(
            JSON.stringify({
              type: "join",
              nick: nick,
              role: "queue",
              room: room,
            })
          );
        } catch (_) {}
      };
      hub.onclose = function () {
        connected.hub = false;
        status();
      };
      hub.onerror = function () {
        connected.hub = false;
        status();
      };
      hub.onmessage = function (ev) {
        try {
          emitIn(JSON.parse(ev.data), "hub");
        } catch (_) {}
      };
    }

    function connectSfu() {
      var base = sfuBase;
      try {
        var u = new URL(base.replace(/^http/, "ws"));
        if (!/\/ws/.test(u.pathname)) {
          u.pathname = (u.pathname.replace(/\/$/, "") || "") + "/ws";
        }
        u.searchParams.set("room", room);
        u.searchParams.set("nick", nick);
        if (token) u.searchParams.set("token", token);
        sfu = new WebSocket(u.toString());
      } catch (e) {
        onStatus("sfu url bad", connected);
        return;
      }
      sfu.onopen = function () {
        try {
          sfu.send(
            JSON.stringify({
              type: "join",
              room: room,
              nick: nick,
              lanes: ["chat", "glyph", "hex"],
              token: token || undefined,
            })
          );
        } catch (_) {}
      };
      sfu.onclose = function () {
        connected.sfu = false;
        connected.dc = false;
        status();
      };
      sfu.onerror = function () {
        onStatus("sfu error", connected);
      };
      sfu.onmessage = function (ev) {
        try {
          onSfuMsg(JSON.parse(ev.data));
        } catch (_) {}
      };
    }

    function connect() {
      connectHub();
      connectSfu();
      return status();
    }

    function disconnect() {
      try {
        if (hub) hub.close();
      } catch (_) {}
      try {
        if (sfu) {
          sfu.send(JSON.stringify({ type: "leave" }));
          sfu.close();
        }
      } catch (_) {}
      try {
        if (pc) pc.close();
      } catch (_) {}
      hub = null;
      sfu = null;
      pc = null;
      dcs = {};
      connected = { hub: false, sfu: false, dc: false };
      status();
    }

    function setConfig(cfg) {
      cfg = cfg || {};
      if (cfg.hubWs != null) hubWsUrl = cfg.hubWs;
      if (cfg.sfuWs != null) sfuBase = cfg.sfuWs;
      if (cfg.room != null) room = cfg.room;
      if (cfg.nick != null) nick = cfg.nick;
      if (cfg.token != null) token = cfg.token;
    }

    return {
      connect: connect,
      disconnect: disconnect,
      send: send,
      status: status,
      setConfig: setConfig,
      get connected() {
        return Object.assign({}, connected);
      },
      get nick() {
        return nick;
      },
    };
  }

  global.GY_MEDIA_SFU = {
    create: create,
    defaultSfuWS: defaultSfuWS,
  };
})(typeof window !== "undefined" ? window : globalThis);
