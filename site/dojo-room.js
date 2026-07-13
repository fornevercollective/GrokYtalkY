/**
 * E2E DOJO room client — gy-sfu media mode.
 * - RTCPeerConnection: offer (no `to`) → SFU answer
 * - Tracks: cam → SFU → remote video
 * - DataChannels: SFU outbound glyph|hex|chat (+ optional client DCs)
 */
(function () {
  const $ = (id) => document.getElementById(id);

  const state = {
    ws: null,
    pc: null,
    peerId: null,
    dcs: {}, // label -> RTCDataChannel
    localStream: null,
  };

  if (!$("nick").value) {
    $("nick").value = "peer-" + Math.random().toString(36).slice(2, 6);
  }

  function setStatus(t, cls) {
    const el = $("status");
    el.textContent = t;
    el.className = "dojo-status" + (cls ? " " + cls : "");
  }

  function chatLine(from, text) {
    const log = $("chat-log");
    const d = document.createElement("div");
    d.innerHTML = `<span class="who">${escapeHtml(from)}</span> · ${escapeHtml(text)}`;
    log.appendChild(d);
    log.scrollTop = log.scrollHeight;
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function paintGlyph(canvas, data, n) {
    const ctx = canvas.getContext("2d");
    n = n || 25;
    canvas.width = n;
    canvas.height = n;
    const img = ctx.createImageData(n, n);
    for (let i = 0; i < n * n; i++) {
      const v = data && data[i] != null ? data[i] & 255 : 20;
      const o = i * 4;
      img.data[o] = v;
      img.data[o + 1] = Math.min(255, v + 30);
      img.data[o + 2] = Math.min(255, v + 60);
      img.data[o + 3] = 255;
    }
    ctx.putImageData(img, 0, 0);
  }

  function pulseGlyphLocal() {
    const n = 25;
    const data = new Array(n * n);
    const t = Date.now() / 200;
    for (let y = 0; y < n; y++) {
      for (let x = 0; x < n; x++) {
        const cx = x - (n - 1) / 2;
        const cy = y - (n - 1) / 2;
        const d = Math.hypot(cx, cy);
        const on = d <= n / 2 + 0.35;
        data[y * n + x] = on
          ? Math.floor(80 + 140 * (0.5 + 0.5 * Math.sin(t + d * 0.4)))
          : 0;
      }
    }
    paintGlyph($("glyph-tx"), data, n);
    return { n, data };
  }

  function sendGlyph() {
    const g = pulseGlyphLocal();
    const payload = JSON.stringify({ type: "glyph", n: g.n, data: g.data });
    const dc = state.dcs.glyph;
    if (dc && dc.readyState === "open") {
      dc.send(payload);
      setStatus("glyph sent via DC", "live");
    } else if (state.ws && state.ws.readyState === WebSocket.OPEN) {
      state.ws.send(payload);
      setStatus("glyph sent via WS fallback", "live");
    } else {
      setStatus("not connected", "err");
    }
  }

  async function ensureMedia() {
    if (state.localStream) return state.localStream;
    const stream = await navigator.mediaDevices.getUserMedia({
      video: { width: 320, height: 240, frameRate: 15 },
      audio: true,
    });
    state.localStream = stream;
    $("local-video").srcObject = stream;
    return stream;
  }

  function wireDc(dc) {
    const label = (dc.label || "").toLowerCase();
    state.dcs[label] = dc;
    dc.onopen = () => setStatus(`DC ${label} open`, "live");
    dc.onmessage = (ev) => {
      let msg = ev.data;
      if (typeof msg !== "string") {
        // binary glyph
        const arr = new Uint8Array(msg);
        const n = arr.length === 169 ? 13 : 25;
        paintGlyph($("glyph-rx"), arr, n);
        return;
      }
      try {
        const j = JSON.parse(msg);
        if (j.type === "glyph" || (j.data && j.n)) {
          paintGlyph($("glyph-rx"), j.data, j.n || 25);
        } else if (j.type === "chat" || label === "chat") {
          chatLine(j.from || j.nick || "peer", j.text || msg);
        }
      } catch {
        if (label === "chat") chatLine("peer", msg);
      }
    };
  }

  async function createPc() {
    const pc = new RTCPeerConnection({
      iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
    });
    state.pc = pc;

    pc.onicecandidate = (ev) => {
      if (!ev.candidate || !state.ws) return;
      state.ws.send(
        JSON.stringify({
          type: "ice",
          candidate: ev.candidate.toJSON(),
        }),
      );
    };

    pc.ontrack = (ev) => {
      const v = $("remote-video");
      if (ev.streams && ev.streams[0]) {
        v.srcObject = ev.streams[0];
      } else {
        const ms = v.srcObject || new MediaStream();
        ms.addTrack(ev.track);
        v.srcObject = ms;
      }
      setStatus("remote track", "live");
    };

    // SFU-created outbound DCs land here
    pc.ondatachannel = (ev) => wireDc(ev.channel);

    // Also create client-side DCs (redundant ok; SFU registers both)
    ["glyph", "chat", "hex"].forEach((label) => {
      try {
        wireDc(pc.createDataChannel(label));
      } catch (_) {}
    });

    const stream = await ensureMedia();
    stream.getTracks().forEach((t) => pc.addTrack(t, stream));
    return pc;
  }

  async function negotiateOffer() {
    const pc = state.pc;
    const offer = await pc.createOffer();
    await pc.setLocalDescription(offer);
    state.ws.send(JSON.stringify({ type: "offer", sdp: offer.sdp }));
  }

  async function handleSignal(msg) {
    if (msg.type === "welcome") {
      state.peerId = msg.peer_id;
      setStatus(
        `joined ${msg.room} · peer ${String(msg.peer_id).slice(0, 8)} · media=${msg.media}` +
          (msg.auth ? " · auth" : ""),
        msg.media ? "live" : "err",
      );
      if (!msg.media) {
        setStatus("SFU media=false — rebuild with make sfu-media", "err");
        return;
      }
      await createPc();
      await negotiateOffer();
      return;
    }

    if (msg.type === "answer" && state.pc) {
      // SFU answer (from correlates to our peer id in media mode)
      await state.pc.setRemoteDescription({ type: "answer", sdp: msg.sdp });
      setStatus("SFU answer applied", "live");
      return;
    }

    if (msg.type === "offer" && state.pc) {
      // SFU renegotiation (new forwarded track)
      await state.pc.setRemoteDescription({ type: "offer", sdp: msg.sdp });
      const answer = await state.pc.createAnswer();
      await state.pc.setLocalDescription(answer);
      state.ws.send(JSON.stringify({ type: "answer", sdp: answer.sdp }));
      setStatus("renegotiate answer sent", "live");
      return;
    }

    if (msg.type === "ice" && state.pc && msg.candidate) {
      try {
        await state.pc.addIceCandidate(msg.candidate);
      } catch (e) {
        /* ignore */
      }
      return;
    }

    if (msg.type === "glyph") {
      paintGlyph($("glyph-rx"), msg.data, msg.n || 25);
      return;
    }

    if (msg.type === "chat") {
      chatLine(msg.nick || msg.from || "peer", msg.text || "");
      return;
    }

    if (msg.type === "error") {
      setStatus(msg.message || "error", "err");
    }
  }

  function join() {
    leave();
    const base = $("sfu-url").value.trim();
    const room = $("room").value.trim() || "dojo";
    const nick = $("nick").value.trim() || "anon";
    const token = ($("token") && $("token").value.trim()) || localStorage.getItem("gy-sfu-token") || "";
    const u = new URL(base.replace(/^http/, "ws"));
    u.searchParams.set("room", room);
    u.searchParams.set("nick", nick);
    if (token) {
      u.searchParams.set("token", token);
      localStorage.setItem("gy-sfu-token", token);
    }
    setStatus("connecting " + u);
    const ws = new WebSocket(u.toString());
    state.ws = ws;
    ws.onopen = () => {
      setStatus("ws open · join" + (token ? " +token" : ""), "live");
      // join.token mirrors ?token= (SFU accepts either)
      ws.send(
        JSON.stringify({
          type: "join",
          room: room,
          nick: nick,
          lanes: ["glyph", "hex", "chat"],
          ...(token ? { token } : {}),
        }),
      );
    };
    ws.onerror = () => setStatus("ws error", "err");
    ws.onclose = () => setStatus("ws closed", "err");
    ws.onmessage = async (ev) => {
      try {
        await handleSignal(JSON.parse(ev.data));
      } catch (e) {
        setStatus(String(e), "err");
      }
    };
  }

  function leave() {
    if (state.ws) {
      try {
        state.ws.send(JSON.stringify({ type: "leave" }));
        state.ws.close();
      } catch (_) {}
    }
    if (state.pc) {
      try {
        state.pc.close();
      } catch (_) {}
    }
    state.ws = null;
    state.pc = null;
    state.dcs = {};
    state.peerId = null;
    if (state.localStream) {
      state.localStream.getTracks().forEach((t) => t.stop());
      state.localStream = null;
    }
    $("local-video").srcObject = null;
    $("remote-video").srcObject = null;
    setStatus("left");
  }

  $("btn-join").onclick = () => join();
  $("btn-leave").onclick = () => leave();
  $("btn-glyph").onclick = () => sendGlyph();
  $("chat-form").onsubmit = (e) => {
    e.preventDefault();
    const text = $("chat-input").value.trim();
    if (!text) return;
    const payload = JSON.stringify({
      type: "chat",
      text,
      from: $("nick").value,
      t: Date.now(),
    });
    const dc = state.dcs.chat;
    if (dc && dc.readyState === "open") dc.send(payload);
    else if (state.ws) state.ws.send(payload);
    chatLine($("nick").value, text);
    $("chat-input").value = "";
  };

  // idle pulse on TX canvas
  setInterval(() => {
    if (!$("btn-glyph")) return;
    pulseGlyphLocal();
  }, 80);
})();
