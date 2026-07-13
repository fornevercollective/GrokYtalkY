/**
 * GrokGlyph PWA — full-page multi-user 25×25 luminance video cast.
 * Side-by-side 1px stack · cam/file → glyph · cast via hub vburst-frame · RX peers.
 */
(function () {
  "use strict";

  const N = 25;
  const GAP = 1;
  const CAST_MS = 110; // ~9 fps cast (glyph grid is tiny)
  const RX_STALE_MS = 2500;
  const STORAGE_KEY = "grokglyph.v4";
  const DIR_ORDER = ["n", "e", "s", "w"];
  const DIR_DELTA = {
    n: { x: 0, y: -1 },
    e: { x: 1, y: 0 },
    s: { x: 0, y: 1 },
    w: { x: -1, y: 0 },
  };

  /**
   * @typedef {{
   *   id: string, nick: string, dir: string, on: boolean, seed: number,
   *   self?: boolean, nfc?: boolean, _capOff?: boolean,
   *   source?: 'sim'|'cam'|'file'|'rx',
   *   lum?: Uint8Array,
   *   lumAt?: number
   * }} Peer
   */

  const els = {
    stage: document.getElementById("gg-stage"),
    wrap: document.getElementById("gg-stage-wrap"),
    meshSvg: document.getElementById("gg-mesh-svg"),
    roster: document.getElementById("gg-roster"),
    rosterCount: document.getElementById("gg-roster-count"),
    rosterEmpty: document.getElementById("gg-roster-empty"),
    search: document.getElementById("gg-search"),
    searchClear: document.getElementById("gg-search-clear"),
    scaleLabel: document.getElementById("gg-scale-label"),
    countLabel: document.getElementById("gg-count-label"),
    castLabel: document.getElementById("gg-cast-label"),
    capHint: document.getElementById("gg-cap-hint"),
    nick: document.getElementById("gg-nick"),
    hubUrl: document.getElementById("gg-hub-url"),
    hubHint: document.getElementById("gg-hub-hint"),
    addBtn: document.getElementById("gg-add"),
    camBtn: document.getElementById("gg-cam"),
    castBtn: document.getElementById("gg-cast"),
    hubBtn: document.getElementById("gg-hub"),
    meshToggle: document.getElementById("gg-mesh-toggle"),
    nfcAdd: document.getElementById("gg-nfc-add"),
    nfcHint: document.getElementById("gg-nfc-hint"),
    installBtn: document.getElementById("gg-install"),
    videoFile: document.getElementById("gg-video-file"),
    localVideo: document.getElementById("gg-local-video"),
    fileVideo: document.getElementById("gg-file-video"),
    sample: document.getElementById("gg-sample"),
    drawer: document.getElementById("gg-drawer"),
    drawerToggle: document.getElementById("gg-drawer-toggle"),
    drawerHandle: document.getElementById("gg-drawer-handle"),
    drawerScrim: document.getElementById("gg-drawer-scrim"),
  };

  const sampleCtx = els.sample
    ? els.sample.getContext("2d", { willReadFrequently: true })
    : null;
  if (sampleCtx) sampleCtx.imageSmoothingEnabled = true;

  /** @type {Peer[]} */
  let peers = [];
  let meshOn = true;
  let drawerOpen = false;
  /** @type {string} */
  let rosterQuery = "";
  /** @type {string} */
  let rosterFilter = "all"; // all|live|on|off|rx|sim|nfc
  /** @type {string} */
  let rosterSort = "live"; // live|name|dir
  /** @type {string|null} */
  let focusPeerId = null;
  let gridCols = 1;
  let gridRows = 1;
  let cellPx = 100;
  let maxVisible = 4;
  let raf = 0;
  let t0 = performance.now();
  let lastCastAt = 0;
  let castSeq = 0;
  let camOn = false;
  let fileOn = false;
  let casting = false;
  let castSession = false; // vburst-start sent
  /** @type {MediaStream | null} */
  let mediaStream = null;
  /** @type {WebSocket | null} */
  let ws = null;
  /** @type {Map<string, HTMLCanvasElement>} */
  const canvasById = new Map();
  const lumBuf = new Map();
  /** @type {BeforeInstallPromptEvent | null} */
  let deferredInstall = null;
  // jpeg encode helper (reuse)
  const jpegCanvas = document.createElement("canvas");
  jpegCanvas.width = 100;
  jpegCanvas.height = 100;
  const jpegCtx = jpegCanvas.getContext("2d");
  if (jpegCtx) jpegCtx.imageSmoothingEnabled = false;

  // ── persistence ──────────────────────────────────────────
  function loadState() {
    try {
      const raw =
        localStorage.getItem(STORAGE_KEY) ||
        localStorage.getItem("grokglyph.v2") ||
        localStorage.getItem("grokglyph.v1");
      if (!raw) return null;
      return JSON.parse(raw);
    } catch {
      return null;
    }
  }

  function saveState() {
    try {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify({
          nick: myNick(),
          meshOn,
          hubUrl: els.hubUrl ? els.hubUrl.value.trim() : "",
          peers: peers.map((p) => ({
            id: p.id,
            nick: p.nick,
            dir: p.dir,
            on: p.on,
            seed: p.seed,
            self: !!p.self,
            nfc: !!p.nfc,
          })),
        })
      );
    } catch {
      /* ignore */
    }
  }

  function myNick() {
    return ((els.nick && els.nick.value) || "you").trim().slice(0, 16) || "you";
  }

  function uid(prefix) {
    return (prefix || "p") + "-" + Math.random().toString(36).slice(2, 8) + Date.now().toString(36).slice(-3);
  }

  function defaultSelf() {
    return {
      id: "self",
      nick: "you",
      dir: "c",
      on: true,
      seed: 42,
      self: true,
      source: "sim",
    };
  }

  function makePeer(dir, nickHint) {
    const d = DIR_ORDER.includes(dir) ? dir : "e";
    return {
      id: uid(d),
      nick: nickHint || "peer-" + d,
      dir: d,
      on: true,
      seed: (Math.random() * 1e9) | 0,
      nfc: false,
      source: "sim",
    };
  }

  function initPeers() {
    const st = loadState();
    if (st && Array.isArray(st.peers) && st.peers.length) {
      peers = st.peers.map((p) => ({ ...p, source: p.self ? "sim" : p.source || "sim" }));
      meshOn = st.meshOn !== false;
      if (st.nick && els.nick) els.nick.value = st.nick;
      if (st.hubUrl && els.hubUrl) els.hubUrl.value = st.hubUrl;
      if (!peers.some((p) => p.self || p.id === "self")) peers.unshift(defaultSelf());
    } else {
      peers = [defaultSelf()];
      if (els.hubUrl && !els.hubUrl.value) {
        els.hubUrl.value = defaultHubURL();
      }
    }
    applyNick();
    if (els.hubUrl && !els.hubUrl.value) els.hubUrl.value = defaultHubURL();
  }

  function applyNick() {
    const self = peers.find((p) => p.self || p.id === "self");
    if (self) self.nick = myNick();
  }

  function defaultHubURL() {
    const host = location.hostname || "127.0.0.1";
    const h =
      host === "localhost" || host === "127.0.0.1" || host === ""
        ? "127.0.0.1"
        : host;
    // Pages on github.io can't reach local hub without tunnel — still default local for gy serve
    if (location.protocol === "https:" && h.includes("github.io")) {
      return "wss://";
    }
    return "ws://" + h + ":9876/";
  }

  // ── video → 25×25 luminance ──────────────────────────────
  /**
   * Sample a video/image element into out (length N*N). Returns false if not ready.
   * @param {CanvasImageSource} src
   * @param {Uint8Array} out
   */
  function sampleSourceToLum(src, out) {
    if (!sampleCtx || !els.sample || !src) return false;
    try {
      // @ts-ignore
      if (src.readyState != null && src.readyState < 2) return false;
      // @ts-ignore
      if (src.videoWidth === 0 && src.naturalWidth === 0 && !(src.width > 0)) return false;
    } catch {
      /* continue */
    }
    sampleCtx.drawImage(src, 0, 0, N, N);
    let img;
    try {
      img = sampleCtx.getImageData(0, 0, N, N);
    } catch {
      return false; // tainted
    }
    const d = img.data;
    for (let i = 0, g = 0; i < d.length; i += 4, g++) {
      // Rec.601 luminance + slight gamma for LED readability
      const L = 0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2];
      out[g] = Math.max(0, Math.min(255, Math.pow(L / 255, 0.85) * 255)) | 0;
    }
    return true;
  }

  function ensureLum(id) {
    let buf = lumBuf.get(id);
    if (!buf) {
      buf = new Uint8Array(N * N);
      lumBuf.set(id, buf);
    }
    return buf;
  }

  function fillSimLuminance(out, seed, t, isSelf) {
    const cx = 12,
      cy = 12;
    const pulse = 0.55 + 0.45 * Math.sin(t * (isSelf ? 2.1 : 1.4) + (seed % 7) * 0.3);
    for (let y = 0; y < N; y++) {
      for (let x = 0; x < N; x++) {
        const dx = x - cx;
        const dy = y - cy;
        const r = Math.sqrt(dx * dx + dy * dy);
        let v = Math.max(0, 1 - r / 13.2);
        v = v * v;
        const h = hash2(x + seed * 3, y + seed * 7);
        v = v * (0.72 + 0.28 * h) * pulse;
        if (isSelf) {
          const ring = Math.abs(r - 8 - 2 * Math.sin(t * 1.5));
          if (ring < 1.2) v = Math.min(1, v + 0.35 * (1 - ring / 1.2));
        } else {
          v *= 0.55 + 0.45 * Math.sin((x + y) * 0.35 + t + seed * 0.01);
        }
        out[y * N + x] = Math.max(0, Math.min(255, (v * 255) | 0));
      }
    }
  }

  function hash2(x, y) {
    let n = x * 374761393 + y * 668265263;
    n = (n ^ (n >>> 13)) * 1274126177;
    n = n ^ (n >>> 16);
    return (n >>> 0) / 4294967295;
  }

  function drawGlyph(canvas, data, mode) {
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    if (canvas.width !== N) canvas.width = N;
    if (canvas.height !== N) canvas.height = N;
    const img = ctx.createImageData(N, N);
    const d = img.data;
    // mode: self | cast | rx | sim
    for (let i = 0; i < N * N; i++) {
      const L = data[i];
      let r, g, b;
      if (mode === "cast") {
        r = Math.min(255, (L * 1.0) | 0);
        g = Math.min(255, (L * 0.55) | 0);
        b = Math.min(255, (L * 0.5) | 0);
      } else if (mode === "rx") {
        r = (L * 0.4) | 0;
        g = (L * 0.85) | 0;
        b = Math.min(255, (L * 1.05) | 0);
      } else if (mode === "self") {
        r = (L * 0.55) | 0;
        g = (L * 0.95) | 0;
        b = Math.min(255, L);
      } else {
        r = (L * 0.35) | 0;
        g = (L * 0.85) | 0;
        b = (L * 0.95) | 0;
      }
      const o = i * 4;
      d[o] = r;
      d[o + 1] = g;
      d[o + 2] = b;
      d[o + 3] = 255;
    }
    ctx.putImageData(img, 0, 0);
  }

  // ── camera / file ────────────────────────────────────────
  async function enableCam() {
    if (camOn && mediaStream) return true;
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      setCastLabel("no cam API");
      return false;
    }
    try {
      mediaStream = await navigator.mediaDevices.getUserMedia({
        video: {
          facingMode: "user",
          width: { ideal: 320 },
          height: { ideal: 320 },
        },
        audio: false,
      });
      if (els.localVideo) {
        els.localVideo.srcObject = mediaStream;
        els.localVideo.muted = true;
        els.localVideo.playsInline = true;
        await els.localVideo.play();
      }
      camOn = true;
      fileOn = false;
      const self = peers.find((p) => p.self);
      if (self) self.source = "cam";
      if (els.camBtn) {
        els.camBtn.setAttribute("aria-pressed", "true");
        els.camBtn.textContent = "cam";
      }
      setCastLabel(casting ? "casting" : "cam live");
      return true;
    } catch (err) {
      camOn = false;
      setCastLabel("cam blocked");
      if (els.hubHint) {
        els.hubHint.textContent =
          "Camera blocked — allow permission or use a video file. " +
          (err && err.message ? err.message : "");
      }
      return false;
    }
  }

  function stopCam() {
    if (mediaStream) {
      mediaStream.getTracks().forEach((t) => t.stop());
      mediaStream = null;
    }
    if (els.localVideo) els.localVideo.srcObject = null;
    camOn = false;
    if (els.camBtn) {
      els.camBtn.setAttribute("aria-pressed", "false");
    }
    const self = peers.find((p) => p.self);
    if (self && self.source === "cam") self.source = fileOn ? "file" : "sim";
  }

  async function toggleCam() {
    if (camOn) {
      if (casting) await stopCast();
      stopCam();
      setCastLabel("idle");
      return;
    }
    await enableCam();
  }

  async function loadVideoFile(file) {
    if (!file || !els.fileVideo) return;
    const url = URL.createObjectURL(file);
    els.fileVideo.src = url;
    els.fileVideo.loop = true;
    els.fileVideo.muted = true;
    try {
      await els.fileVideo.play();
      fileOn = true;
      // prefer file over cam for self when chosen
      if (camOn) stopCam();
      const self = peers.find((p) => p.self);
      if (self) self.source = "file";
      setCastLabel(casting ? "casting file" : "file live");
      if (els.hubHint) els.hubHint.textContent = "Playing " + file.name + " → your glyph.";
    } catch (e) {
      setCastLabel("file error");
    }
  }

  // ── cast / hub ───────────────────────────────────────────
  function setCastLabel(t) {
    if (els.castLabel) els.castLabel.textContent = t;
  }

  function sendJSON(obj) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    try {
      ws.send(JSON.stringify(obj));
      return true;
    } catch {
      return false;
    }
  }

  function hubURL() {
    let url = (els.hubUrl && els.hubUrl.value.trim()) || defaultHubURL();
    if (url === "wss://" || url === "ws://") {
      return "";
    }
    const nick = myNick();
    if (!url.includes("nick=")) {
      url += (url.includes("?") ? "&" : "?") + "role=peer&nick=" + encodeURIComponent(nick);
    }
    return url;
  }

  function connectHub() {
    const url = hubURL();
    if (!url) {
      setCastLabel("set hub url");
      setDrawer(true);
      return;
    }
    if (ws) {
      try {
        ws.close();
      } catch {
        /* ignore */
      }
      ws = null;
    }
    setCastLabel("hub…");
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setCastLabel("hub error");
      return;
    }
    ws.onopen = () => {
      // advertise hex + gyst lanes so room can pick 25×25 hexlum
      sendJSON({
        type: "join",
        nick: myNick(),
        role: "grokglyph",
        cap: {
          class: "term-lean",
          role: "peer",
          lanes: ["glyph", "hex", "chat", "gyst"],
          glyph_n: N,
          forge: false,
        },
      });
      if (els.hubBtn) els.hubBtn.setAttribute("aria-pressed", "true");
      setCastLabel(casting ? "casting" : "hub on");
      saveState();
    };
    ws.onclose = () => {
      if (els.hubBtn) els.hubBtn.setAttribute("aria-pressed", "false");
      if (castSession) {
        castSession = false;
      }
      if (casting) {
        casting = false;
        if (els.castBtn) els.castBtn.setAttribute("aria-pressed", "false");
      }
      setCastLabel("hub off");
    };
    ws.onerror = () => setCastLabel("hub err");
    ws.onmessage = onHubMessage;
  }

  function disconnectHub() {
    if (casting) stopCast();
    if (ws) {
      try {
        ws.close();
      } catch {
        /* ignore */
      }
      ws = null;
    }
    if (els.hubBtn) els.hubBtn.setAttribute("aria-pressed", "false");
    setCastLabel(camOn || fileOn ? "local" : "idle");
  }

  function toggleHub() {
    if (ws && ws.readyState === WebSocket.OPEN) disconnectHub();
    else connectHub();
  }

  async function startCast() {
    // need a video source
    if (!camOn && !fileOn) {
      const ok = await enableCam();
      if (!ok && !fileOn) {
        setCastLabel("need cam/file");
        setDrawer(true);
        return;
      }
    }
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      connectHub();
      // wait briefly for open
      await new Promise((r) => setTimeout(r, 400));
      if (!ws || ws.readyState !== WebSocket.OPEN) {
        setCastLabel("hub first");
        setDrawer(true);
        return;
      }
    }
    casting = true;
    if (els.castBtn) {
      els.castBtn.setAttribute("aria-pressed", "true");
      els.castBtn.classList.add("is-live");
    }
    if (!castSession) {
      sendJSON({ type: "vburst-start", from: myNick(), t: Date.now() });
      castSession = true;
    }
    const self = peers.find((p) => p.self);
    if (self) self.source = camOn ? "cam" : fileOn ? "file" : self.source;
    setCastLabel("casting");
    layoutAndPaint();
  }

  function stopCast() {
    if (castSession) {
      sendJSON({ type: "vburst-end", from: myNick(), t: Date.now() });
      castSession = false;
    }
    casting = false;
    if (els.castBtn) {
      els.castBtn.setAttribute("aria-pressed", "false");
      els.castBtn.classList.remove("is-live");
    }
    setCastLabel(
      ws && ws.readyState === WebSocket.OPEN
        ? "hub on"
        : camOn || fileOn
          ? "local"
          : "idle"
    );
    layoutAndPaint();
  }

  async function toggleCast() {
    if (casting) stopCast();
    else await startCast();
  }

  function jpegB64FromLum(lum) {
    if (!jpegCtx || !sampleCtx || !els.sample) return "";
    const img = sampleCtx.createImageData(N, N);
    for (let i = 0; i < N * N; i++) {
      const L = lum[i];
      const o = i * 4;
      img.data[o] = L;
      img.data[o + 1] = L;
      img.data[o + 2] = L;
      img.data[o + 3] = 255;
    }
    sampleCtx.putImageData(img, 0, 0);
    jpegCtx.drawImage(els.sample, 0, 0, 100, 100);
    const dataUrl = jpegCanvas.toDataURL("image/jpeg", 0.5);
    return dataUrl.split(",")[1] || "";
  }

  function sendCastFrame(lum) {
    if (!casting || !ws || ws.readyState !== WebSocket.OPEN) return;
    castSeq = (castSeq + 1) >>> 0;
    const glyph = new Array(N * N);
    const raw = new Uint8Array(N * N);
    for (let i = 0; i < N * N; i++) {
      glyph[i] = lum[i];
      raw[i] = lum[i];
    }
    const t = Date.now();
    // 1) formal live hexlum lane (forge · SFU · agent · venue · peers)
    let b64raw = "";
    try {
      let s = "";
      for (let i = 0; i < raw.length; i++) s += String.fromCharCode(raw[i]);
      b64raw = btoa(s);
    } catch {
      b64raw = "";
    }
    sendJSON({
      type: "gyst",
      from: myNick(),
      kind: "hexlum",
      w: N,
      h: N,
      seq: castSeq,
      t: t,
      b64: b64raw,
      data: glyph,
      glyphN: N,
      lane: "hex",
      via: "grokglyph-cast",
    });
    // 2) walkie-compatible vburst (jpeg + glyph). hex_lane skips hub re-promote.
    const b64jpeg = jpegB64FromLum(lum);
    sendJSON({
      type: "vburst-frame",
      from: myNick(),
      fmt: "jpeg",
      b64: b64jpeg,
      w: 100,
      h: 100,
      glyph: glyph,
      glyphN: N,
      seq: castSeq,
      t: t,
      hex_lane: true,
    });
  }

  function onHubMessage(ev) {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return;
    }
    const typ = msg.type;
    const from = msg.from || msg.nick || "";
    if (!from || from === myNick()) return;

    if (typ === "vburst-start" || (typ === "ptt" && msg.state === "down")) {
      ensureRxPeer(from);
      setCastLabel("rx " + from);
    }
    if (typ === "vburst-end" || (typ === "ptt" && msg.state === "up")) {
      const p = findPeerByNick(from);
      if (p && p.source === "rx") {
        // keep last frame; mark stale via lumAt
        p.lumAt = 0;
      }
    }
    if (typ === "vburst-frame") {
      applyRxFrame(from, msg);
    }
    // live hexlum lane (gyst kind=hexlum · promoted vburst · stream-pub)
    if (typ === "gyst" || typ === "gyst-frame" || typ === "hexlum" || typ === "glyph") {
      const kind = msg.kind || "";
      if (
        typ === "hexlum" ||
        typ === "glyph" ||
        kind === "hexlum" ||
        kind === "hex" ||
        Array.isArray(msg.data) ||
        Array.isArray(msg.glyph)
      ) {
        applyHexLum(from, msg);
      }
    }
    if (typ === "join" || typ === "roster") {
      // optional: show roster peers as empty slots
      if (typ === "roster" && Array.isArray(msg.peers)) {
        msg.peers.forEach((pr) => {
          const n = pr.nick || pr.id;
          if (n && n !== myNick()) ensureRxPeer(n, false);
        });
      }
    }
  }

  function findPeerByNick(nick) {
    const low = String(nick).toLowerCase();
    return peers.find((p) => !p.self && p.nick.toLowerCase() === low);
  }

  function ensureRxPeer(nick, turnOn) {
    let p = findPeerByNick(nick);
    if (!p) {
      const dir = DIR_ORDER[peers.filter((x) => !x.self).length % 4];
      p = makePeer(dir, nick);
      p.source = "rx";
      p.on = turnOn !== false;
      peers.push(p);
      saveState();
      layoutAndPaint();
    } else {
      p.source = "rx";
      if (turnOn !== false) p.on = true;
    }
    return p;
  }

  function applyRxFrame(from, msg) {
    const p = ensureRxPeer(from, true);
    const buf = ensureLum(p.id);
    let ok = false;
    if (Array.isArray(msg.glyph) && msg.glyph.length >= N * N) {
      for (let i = 0; i < N * N; i++) buf[i] = Math.max(0, Math.min(255, Number(msg.glyph[i]) | 0));
      ok = true;
    } else if (Array.isArray(msg.data) && msg.data.length >= N * N) {
      for (let i = 0; i < N * N; i++) buf[i] = Math.max(0, Math.min(255, Number(msg.data[i]) | 0));
      ok = true;
    } else if (msg.b64) {
      // decode jpeg async into peer lum
      const im = new Image();
      im.onload = () => {
        const b = ensureLum(p.id);
        if (sampleSourceToLum(im, b)) {
          p.lum = b;
          p.lumAt = performance.now();
          p.source = "rx";
        }
      };
      im.src = "data:image/jpeg;base64," + msg.b64;
      return;
    }
    if (ok) {
      p.lum = buf;
      p.lumAt = performance.now();
      p.source = "rx";
    }
  }

  function applyHexLum(from, msg) {
    const data = msg.data || msg.glyph || msg.lum;
    if (!data) return;
    const p = ensureRxPeer(from || msg.mark || "stream", true);
    const buf = ensureLum(p.id);
    if (Array.isArray(data) && data.length >= N * N) {
      for (let i = 0; i < N * N; i++) buf[i] = Math.max(0, Math.min(255, Number(data[i]) | 0));
    } else if (typeof data === "string") {
      // base64 bytes
      try {
        const bin = atob(data);
        const n = Math.min(N * N, bin.length);
        for (let i = 0; i < n; i++) buf[i] = bin.charCodeAt(i);
      } catch {
        return;
      }
    } else return;
    p.lum = buf;
    p.lumAt = performance.now();
    p.source = "rx";
  }

  // ── layout (1px stack) ───────────────────────────────────
  function computeLayout() {
    const wrap = els.wrap;
    if (!wrap) return;
    const rect = wrap.getBoundingClientRect();
    const W = Math.max(1, Math.floor(rect.width));
    const H = Math.max(1, Math.floor(rect.height));
    const dpr = Math.min(window.devicePixelRatio || 1, 3);
    // capacity from filtered stage (search/filter shrinks crowded rooms)
    const want = stageOrder().filter((p) => p.on || p.self);
    const nWant = Math.max(1, want.length);
    const minCell = 25;
    let best = { cols: 1, rows: nWant, cell: 1 };

    for (let cols = 1; cols <= nWant; cols++) {
      const rows = Math.ceil(nWant / cols);
      const cellW = Math.floor((W - (cols - 1) * GAP) / cols);
      const cellH = Math.floor((H - (rows - 1) * GAP) / rows);
      const cell = Math.max(1, Math.min(cellW, cellH));
      if (
        cell > best.cell ||
        (cell === best.cell && Math.abs(cols - rows) < Math.abs(best.cols - best.rows)) ||
        (cell === best.cell && cols > best.cols)
      ) {
        best = { cols, rows, cell };
      }
    }

    let cell = best.cell;
    if (cell >= N) {
      const snapped = Math.floor(cell / N) * N;
      if (snapped >= N && snapped >= cell * 0.85) cell = snapped;
    }

    gridCols = best.cols;
    gridRows = best.rows;
    cellPx = cell;
    maxVisible = gridCols * gridRows;

    const ordered = stageOrder();
    ordered.forEach((p, i) => {
      if (p.self) {
        p.on = true;
        p._capOff = false;
        return;
      }
      p._capOff = i >= maxVisible;
    });
    // peers not in stage order stay cap-off when filtering
    const stageIds = new Set(ordered.map((p) => p.id));
    peers.forEach((p) => {
      if (!p.self && !stageIds.has(p.id) && (rosterFilter !== "all" || rosterQuery.trim())) {
        p._capOff = true;
      }
    });

    const root = document.documentElement;
    root.style.setProperty("--gg-gap", GAP + "px");
    root.style.setProperty("--gg-cell", cellPx + "px");
    root.style.setProperty("--gg-cols", String(gridCols));
    root.style.setProperty("--gg-rows", String(gridRows));

    const drawn = peers.filter((p) => isDrawn(p)).length;
    if (els.scaleLabel) {
      els.scaleLabel.textContent = cellPx + "px · " + gridCols + "×" + gridRows;
    }
    if (els.countLabel) els.countLabel.textContent = drawn + "/" + peers.length;
    if (els.capHint) {
      els.capHint.textContent =
        "(" + gridCols + "×" + gridRows + " · " + cellPx + "px · 1px · video cast)";
    }
    if (els.meshToggle) {
      els.meshToggle.setAttribute("aria-pressed", meshOn ? "true" : "false");
    }
    void dpr;
    void minCell;
  }

  function isLivePeer(p) {
    if (!p) return false;
    if (p.self) return !!(camOn || fileOn || casting);
    return (
      p.source === "rx" &&
      !!p.lumAt &&
      performance.now() - p.lumAt < RX_STALE_MS
    );
  }

  function peerSourceLabel(p) {
    if (p.self) {
      if (casting) return "cast";
      if (camOn) return "cam";
      if (fileOn) return "file";
      return "sim";
    }
    return p.source || "sim";
  }

  function peerMatchesFilter(p) {
    switch (rosterFilter) {
      case "live":
        return isLivePeer(p);
      case "on":
        return !!p.on;
      case "off":
        return !p.on && !p.self;
      case "rx":
        return p.source === "rx" || isLivePeer(p);
      case "sim":
        return peerSourceLabel(p) === "sim";
      case "nfc":
        return !!p.nfc;
      case "all":
      default:
        return true;
    }
  }

  function peerMatchesQuery(p) {
    const q = rosterQuery.trim().toLowerCase();
    if (!q) return true;
    const hay = [
      p.nick,
      p.id,
      p.dir,
      peerSourceLabel(p),
      p.nfc ? "nfc" : "",
      p.self ? "you self me" : "",
      isLivePeer(p) ? "live rx video" : "",
      p.on ? "on" : "off",
    ]
      .join(" ")
      .toLowerCase();
    // multi-token AND
    return q.split(/\s+/).every((tok) => hay.includes(tok));
  }

  function peerMatchesRoster(p) {
    return peerMatchesFilter(p) && peerMatchesQuery(p);
  }

  function sortPeers(list) {
    const arr = list.slice();
    arr.sort((a, b) => {
      if (focusPeerId) {
        if (a.id === focusPeerId) return -1;
        if (b.id === focusPeerId) return 1;
      }
      if (rosterSort === "name") {
        return a.nick.localeCompare(b.nick, undefined, { sensitivity: "base" });
      }
      if (rosterSort === "dir") {
        const ai = DIR_ORDER.indexOf(a.dir);
        const bi = DIR_ORDER.indexOf(b.dir);
        if (ai !== bi) return (ai < 0 ? 9 : ai) - (bi < 0 ? 9 : bi);
        return a.nick.localeCompare(b.nick);
      }
      // live (default)
      const al = isLivePeer(a) ? 1 : 0;
      const bl = isLivePeer(b) ? 1 : 0;
      if (al !== bl) return bl - al;
      if (a.on !== b.on) return a.on ? -1 : 1;
      const ai = DIR_ORDER.indexOf(a.dir);
      const bi = DIR_ORDER.indexOf(b.dir);
      if (ai !== bi) return (ai < 0 ? 9 : ai) - (bi < 0 ? 9 : bi);
      return a.nick.localeCompare(b.nick);
    });
    return arr;
  }

  function visibleOrder() {
    const self = peers.filter((p) => p.self);
    const rest = sortPeers(peers.filter((p) => !p.self));
    return self.concat(rest);
  }

  /** Stage order: self first, then roster-filtered matches (crowded-area search). */
  function stageOrder() {
    const filtering = rosterFilter !== "all" || !!rosterQuery.trim();
    let list = visibleOrder();
    if (filtering) {
      const matched = list.filter((p) => p.self || peerMatchesRoster(p));
      if (matched.length) list = matched;
    }
    // focus to front (after self)
    if (focusPeerId) {
      const self = list.filter((p) => p.self);
      const focus = list.filter((p) => !p.self && p.id === focusPeerId);
      const rest = list.filter((p) => !p.self && p.id !== focusPeerId);
      list = self.concat(focus, rest);
    }
    return list;
  }

  function isDrawn(p) {
    return !!(p.on && !p._capOff);
  }

  function setFocusPeer(id) {
    focusPeerId = id || null;
    layoutAndPaint();
    // pulse focus tile
    requestAnimationFrame(() => {
      const tile =
        els.stage &&
        els.stage.querySelector(
          focusPeerId ? '.gg-tile[data-id="' + focusPeerId + '"]' : ".gg-tile.is-you"
        );
      if (tile) {
        tile.classList.add("is-focus");
        try {
          tile.focus({ preventScroll: true });
        } catch {
          tile.focus();
        }
      }
    });
  }

  // ── tiles ────────────────────────────────────────────────
  function renderTiles() {
    if (!els.stage) return;
    // preserve canvases if same peer set? simpler rebuild
    const prevFocus = document.activeElement && document.activeElement.dataset
      ? document.activeElement.dataset.id
      : null;
    els.stage.innerHTML = "";
    canvasById.clear();

    for (const p of stageOrder().filter(isDrawn)) {
      const isVideo = isLivePeer(p);
      const tile = document.createElement("div");
      tile.className =
        "gg-tile" +
        (p.self ? " is-you" : "") +
        (p.self && casting ? " is-casting" : "") +
        (p.source === "rx" ? " is-rx" : "") +
        (isVideo ? " is-video" : "") +
        (p.on ? "" : " is-off") +
        (focusPeerId && p.id === focusPeerId ? " is-focus" : "");
      tile.dataset.id = p.id;
      tile.setAttribute("role", "listitem");
      tile.tabIndex = 0;
      tile.title = p.nick + (p.self ? " (you)" : "") + (isVideo ? " · video" : "");

      const badge = document.createElement("span");
      badge.className = "gg-dir-badge" + (isVideo ? " gg-live-badge" : "");
      if (p.self && casting) badge.textContent = "TX";
      else if (isVideo && p.source === "rx") badge.textContent = "RX";
      else if (p.self && (camOn || fileOn)) badge.textContent = "CAM";
      else if (p.dir && p.dir !== "c") badge.textContent = p.dir.toUpperCase();
      if (badge.textContent) tile.appendChild(badge);

      const c = document.createElement("canvas");
      c.width = N;
      c.height = N;
      c.setAttribute(
        "aria-label",
        "Glyph 25×25 video " + p.nick + (isVideo ? " live" : "")
      );
      tile.appendChild(c);
      canvasById.set(p.id, c);

      const meta = document.createElement("div");
      meta.className = "gg-tile-meta";
      const name = document.createElement("span");
      name.className = "gg-name";
      name.textContent = p.nick + (p.nfc ? "·nfc" : "");
      meta.appendChild(name);
      if (!p.self) {
        const del = document.createElement("button");
        del.type = "button";
        del.className = "gg-tile-del";
        del.title = "Delete";
        del.textContent = "×";
        del.addEventListener("click", (e) => {
          e.stopPropagation();
          removePeer(p.id);
        });
        meta.appendChild(del);
      }
      tile.appendChild(meta);

      let pressT = 0;
      tile.addEventListener("pointerdown", (e) => {
        if (p.self || e.button === 2) return;
        pressT = window.setTimeout(() => removePeer(p.id), 650);
      });
      const clearPress = () => {
        if (pressT) clearTimeout(pressT);
        pressT = 0;
      };
      tile.addEventListener("pointerup", clearPress);
      tile.addEventListener("pointerleave", clearPress);
      tile.addEventListener("pointercancel", clearPress);

      tile.addEventListener("click", () => {
        if (p.self) {
          setFocusPeer(p.id);
          return;
        }
        // single click: focus; double via second path uses toggle
        if (focusPeerId === p.id) {
          p.on = !p.on;
          saveState();
          layoutAndPaint();
        } else {
          setFocusPeer(p.id);
        }
      });

      els.stage.appendChild(tile);
      if (prevFocus && prevFocus === p.id) tile.focus();
    }
    renderRoster();
    requestAnimationFrame(() => drawMesh());
  }

  function renderRoster() {
    if (!els.roster) return;
    els.roster.innerHTML = "";
    const all = visibleOrder();
    const matched = all.filter(peerMatchesRoster);
    const list = sortPeers(matched.length ? matched : all.filter((p) => p.self));

    if (els.rosterCount) {
      const liveN = peers.filter(isLivePeer).length;
      const onN = peers.filter((p) => p.on).length;
      els.rosterCount.textContent =
        matched.length +
        "/" +
        peers.length +
        " · " +
        onN +
        " on · " +
        liveN +
        " live";
    }
    if (els.rosterEmpty) {
      els.rosterEmpty.hidden = matched.length > 0;
    }
    if (els.searchClear) {
      els.searchClear.hidden = !rosterQuery.trim();
    }

    for (const p of list) {
      const live = isLivePeer(p);
      const li = document.createElement("li");
      li.className =
        (isDrawn(p) ? "on" : "") +
        (live ? " is-live" : "") +
        (focusPeerId === p.id ? " is-focus" : "");
      li.dataset.id = p.id;
      li.title = "Focus " + p.nick;

      const dot = document.createElement("span");
      dot.className = "gg-dot";
      li.appendChild(dot);

      const label = document.createElement("span");
      label.className = "gg-rlabel";
      const src = peerSourceLabel(p);
      label.textContent =
        p.nick +
        (p.self ? " (you)" : "") +
        " · " +
        src +
        (p.nfc ? " ·nfc" : "") +
        (p._capOff ? " · overflow" : p.on ? "" : " · off");
      li.appendChild(label);

      const rid = document.createElement("span");
      rid.className = "gg-rid";
      rid.textContent = (p.dir || "·").toUpperCase();
      li.appendChild(rid);

      const actions = document.createElement("div");
      actions.className = "gg-ractions";
      if (!p.self) {
        const tog = document.createElement("button");
        tog.type = "button";
        tog.textContent = p.on ? "off" : "on";
        tog.title = "Toggle on stage";
        tog.addEventListener("click", (e) => {
          e.stopPropagation();
          p.on = !p.on;
          saveState();
          layoutAndPaint();
        });
        actions.appendChild(tog);
        const del = document.createElement("button");
        del.type = "button";
        del.className = "danger";
        del.textContent = "del";
        del.title = "Delete peer";
        del.addEventListener("click", (e) => {
          e.stopPropagation();
          if (focusPeerId === p.id) focusPeerId = null;
          removePeer(p.id);
        });
        actions.appendChild(del);
      } else {
        const focusBtn = document.createElement("button");
        focusBtn.type = "button";
        focusBtn.textContent = "you";
        focusBtn.addEventListener("click", (e) => {
          e.stopPropagation();
          setFocusPeer(p.id);
        });
        actions.appendChild(focusBtn);
      }
      li.appendChild(actions);

      li.addEventListener("click", () => {
        // ensure on when focusing from search
        if (!p.self && !p.on) {
          p.on = true;
          saveState();
        }
        setFocusPeer(p.id);
      });

      els.roster.appendChild(li);
    }
  }

  function drawMesh() {
    const svg = els.meshSvg;
    if (!svg || !els.wrap) return;
    while (svg.firstChild) svg.removeChild(svg.firstChild);
    if (!meshOn) return;
    const wrapRect = els.wrap.getBoundingClientRect();
    svg.setAttribute("viewBox", "0 0 " + wrapRect.width + " " + wrapRect.height);
    svg.setAttribute("width", String(wrapRect.width));
    svg.setAttribute("height", String(wrapRect.height));
    const tiles = [...els.stage.querySelectorAll(".gg-tile")];
    if (tiles.length < 2) return;
    const centers = tiles.map((t, idx) => {
      const r = t.getBoundingClientRect();
      return {
        col: idx % gridCols,
        row: Math.floor(idx / gridCols),
        x: r.left + r.width / 2 - wrapRect.left,
        y: r.top + r.height / 2 - wrapRect.top,
        you: t.classList.contains("is-you"),
        cast: t.classList.contains("is-casting"),
      };
    });
    const byPos = new Map();
    centers.forEach((c) => byPos.set(c.col + "," + c.row, c));
    function link(a, b) {
      const line = document.createElementNS("http://www.w3.org/2000/svg", "line");
      line.setAttribute("x1", String(a.x));
      line.setAttribute("y1", String(a.y));
      line.setAttribute("x2", String(b.x));
      line.setAttribute("y2", String(b.y));
      if (a.you || b.you || a.cast || b.cast) line.classList.add("gg-mesh-strong");
      svg.appendChild(line);
    }
    centers.forEach((c) => {
      const right = byPos.get(c.col + 1 + "," + c.row);
      const down = byPos.get(c.col + "," + (c.row + 1));
      if (right) link(c, right);
      if (down) link(c, down);
    });
  }

  // ── peers CRUD ───────────────────────────────────────────
  function addInDirection(dir) {
    if (!DIR_DELTA[dir]) return;
    const count = peers.filter((p) => p.dir === dir).length + 1;
    peers.push(makePeer(dir, dir.toUpperCase() + count));
    saveState();
    layoutAndPaint();
  }

  function subInDirection(dir) {
    for (let i = peers.length - 1; i >= 0; i--) {
      if (!peers[i].self && peers[i].dir === dir) {
        peers.splice(i, 1);
        saveState();
        layoutAndPaint();
        return;
      }
    }
  }

  function removePeer(id) {
    peers = peers.filter((p) => p.id !== id);
    if (!peers.some((p) => p.self)) peers.unshift(defaultSelf());
    lumBuf.delete(id);
    saveState();
    layoutAndPaint();
  }

  function addManualPeer() {
    const dir = DIR_ORDER[peers.filter((p) => !p.self).length % 4];
    peers.push(makePeer(dir, "peer" + peers.length));
    saveState();
    layoutAndPaint();
  }

  // ── NFC (unchanged spirit) ───────────────────────────────
  function nfcSupported() {
    return "NDEFReader" in window;
  }

  async function startNfcAdd() {
    if (!nfcSupported()) {
      if (els.nfcHint) {
        els.nfcHint.textContent =
          "Web NFC not available. Use + / compass. (Chrome Android + HTTPS.)";
      }
      setDrawer(true);
      return;
    }
    try {
      // @ts-ignore
      const reader = new NDEFReader();
      await reader.scan();
      if (els.nfcAdd) els.nfcAdd.textContent = "nfc…";
      setDrawer(true);
      reader.addEventListener("reading", ({ message, serialNumber }) => {
        const parsed = parseNdef(message, serialNumber);
        const existing = peers.find((p) => p.id === parsed.id);
        if (existing) {
          existing.on = true;
          existing.nfc = true;
        } else {
          const dir = DIR_ORDER[peers.filter((p) => !p.self).length % 4];
          peers.push({
            id: parsed.id,
            nick: parsed.nick,
            dir,
            on: true,
            seed: hashStr(parsed.id),
            nfc: true,
            source: "sim",
          });
        }
        saveState();
        layoutAndPaint();
      });
    } catch (err) {
      if (els.nfcHint) {
        els.nfcHint.textContent =
          "NFC: " + (err && err.message ? err.message : String(err));
      }
    }
  }

  function parseNdef(message, serialNumber) {
    let text = "";
    let url = "";
    try {
      for (const rec of message.records || []) {
        if (rec.recordType === "text") {
          text += new TextDecoder(rec.encoding || "utf-8")
            .decode(rec.data)
            .replace(/^[a-z]{2,3}\|?/, "") + " ";
        } else if (rec.recordType === "url" || rec.recordType === "absolute-url") {
          url = new TextDecoder().decode(rec.data);
        }
      }
    } catch {
      /* ignore */
    }
    const blob = (text + " " + url).trim();
    let id = "";
    let nick = "";
    const idm = blob.match(/(?:id|peer|gy)=([a-zA-Z0-9._-]{2,32})/i);
    if (idm) id = idm[1];
    const nm = blob.match(/(?:nick|name)=([a-zA-Z0-9._-]{1,16})/i);
    if (nm) nick = nm[1];
    if (!id && serialNumber) id = "nfc-" + String(serialNumber).replace(/\W/g, "").slice(-8);
    if (!id) id = uid("nfc");
    if (!nick) nick = "nfc-" + id.slice(-4);
    return { id, nick };
  }

  function hashStr(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
    return h >>> 0;
  }

  // ── drawer ───────────────────────────────────────────────
  function setDrawer(open) {
    drawerOpen = !!open;
    if (els.drawer) {
      els.drawer.classList.toggle("is-open", drawerOpen);
      els.drawer.setAttribute("aria-hidden", drawerOpen ? "false" : "true");
    }
    if (els.drawerToggle) els.drawerToggle.setAttribute("aria-expanded", drawerOpen ? "true" : "false");
    if (els.drawerScrim) els.drawerScrim.hidden = !drawerOpen;
  }

  function toggleDrawer() {
    setDrawer(!drawerOpen);
  }

  // ── animation: sample video every frame ──────────────────
  function tick(now) {
    const t = (now - t0) / 1000;
    const self = peers.find((p) => p.self);

    for (const p of peers) {
      if (!isDrawn(p)) continue;
      const c = canvasById.get(p.id);
      if (!c) continue;
      const buf = ensureLum(p.id);
      let mode = "sim";

      if (p.self) {
        let sampled = false;
        if (camOn && els.localVideo) {
          sampled = sampleSourceToLum(els.localVideo, buf);
          if (sampled) {
            p.source = "cam";
            mode = casting ? "cast" : "self";
          }
        }
        if (!sampled && fileOn && els.fileVideo) {
          sampled = sampleSourceToLum(els.fileVideo, buf);
          if (sampled) {
            p.source = "file";
            mode = casting ? "cast" : "self";
          }
        }
        if (!sampled) {
          fillSimLuminance(buf, p.seed, t, true);
          mode = "sim";
        } else {
          p.lum = buf;
          p.lumAt = now;
          // cast frames
          if (casting && now - lastCastAt >= CAST_MS) {
            lastCastAt = now;
            sendCastFrame(buf);
          }
        }
      } else if (
        p.source === "rx" &&
        p.lum &&
        p.lumAt &&
        now - p.lumAt < RX_STALE_MS
      ) {
        buf.set(p.lum);
        mode = "rx";
      } else if (p.lum && p.lumAt && now - p.lumAt < RX_STALE_MS) {
        buf.set(p.lum);
        mode = p.source === "rx" ? "rx" : "sim";
      } else {
        fillSimLuminance(buf, p.seed, t, false);
        mode = "sim";
      }

      drawGlyph(c, buf, mode);
    }

    // light roster badge refresh without full relayout (every ~1s)
    raf = requestAnimationFrame(tick);
  }

  function layoutAndPaint() {
    computeLayout();
    renderTiles();
  }

  // ── PWA ──────────────────────────────────────────────────
  function registerSW() {
    if (!("serviceWorker" in navigator)) return;
    navigator.serviceWorker
      .register(new URL("sw-grokglyph.js", window.location.href).href, { scope: "./" })
      .catch(() => {});
  }

  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferredInstall = e;
    if (els.installBtn) els.installBtn.hidden = false;
  });

  // ── wire ─────────────────────────────────────────────────
  function wire() {
    document.querySelectorAll(".gg-cbtn[data-dir]").forEach((btn) => {
      btn.addEventListener("click", () => addInDirection(btn.getAttribute("data-dir")));
    });
    document.querySelectorAll(".gg-sub[data-sub]").forEach((btn) => {
      btn.addEventListener("click", () => subInDirection(btn.getAttribute("data-sub")));
    });

    if (els.addBtn) els.addBtn.addEventListener("click", addManualPeer);
    if (els.camBtn) els.camBtn.addEventListener("click", () => toggleCam());
    if (els.castBtn) els.castBtn.addEventListener("click", () => toggleCast());
    if (els.hubBtn) els.hubBtn.addEventListener("click", () => toggleHub());
    if (els.meshToggle) {
      els.meshToggle.addEventListener("click", () => {
        meshOn = !meshOn;
        saveState();
        drawMesh();
        els.meshToggle.setAttribute("aria-pressed", meshOn ? "true" : "false");
      });
    }
    if (els.nfcAdd) els.nfcAdd.addEventListener("click", startNfcAdd);
    if (els.videoFile) {
      els.videoFile.addEventListener("change", () => {
        const f = els.videoFile.files && els.videoFile.files[0];
        if (f) loadVideoFile(f);
      });
    }

    if (els.drawerToggle) els.drawerToggle.addEventListener("click", toggleDrawer);
    if (els.drawerHandle) {
      els.drawerHandle.addEventListener("click", toggleDrawer);
      els.drawerHandle.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          toggleDrawer();
        }
      });
    }
    if (els.drawerScrim) els.drawerScrim.addEventListener("click", () => setDrawer(false));

    let touchY0 = null;
    if (els.drawerHandle) {
      els.drawerHandle.addEventListener(
        "touchstart",
        (e) => {
          touchY0 = e.changedTouches[0].clientY;
        },
        { passive: true }
      );
      els.drawerHandle.addEventListener(
        "touchend",
        (e) => {
          if (touchY0 == null) return;
          const dy = e.changedTouches[0].clientY - touchY0;
          touchY0 = null;
          if (dy > 40) setDrawer(false);
          else if (dy < -40) setDrawer(true);
        },
        { passive: true }
      );
    }

    // roster search + filter chips
    if (els.search) {
      els.search.addEventListener("input", () => {
        rosterQuery = els.search.value || "";
        layoutAndPaint();
      });
      els.search.addEventListener("keydown", (e) => {
        if (e.key === "Escape") {
          els.search.value = "";
          rosterQuery = "";
          layoutAndPaint();
          els.search.blur();
        }
        if (e.key === "Enter") {
          e.preventDefault();
          const first = stageOrder().find((p) => !p.self && peerMatchesRoster(p));
          if (first) setFocusPeer(first.id);
        }
      });
    }
    if (els.searchClear) {
      els.searchClear.addEventListener("click", () => {
        if (els.search) els.search.value = "";
        rosterQuery = "";
        layoutAndPaint();
        if (els.search) els.search.focus();
      });
    }
    document.querySelectorAll(".gg-chip[data-filter]").forEach((btn) => {
      btn.addEventListener("click", () => {
        rosterFilter = btn.getAttribute("data-filter") || "all";
        document.querySelectorAll(".gg-chip[data-filter]").forEach((b) => {
          b.classList.toggle("is-on", b === btn);
        });
        layoutAndPaint();
      });
    });
    document.querySelectorAll(".gg-chip[data-sort]").forEach((btn) => {
      btn.addEventListener("click", () => {
        rosterSort = btn.getAttribute("data-sort") || "live";
        document.querySelectorAll(".gg-chip[data-sort]").forEach((b) => {
          b.classList.toggle("is-on", b === btn);
        });
        layoutAndPaint();
      });
    });

    document.addEventListener("keydown", (e) => {
      const tag = e.target && e.target.tagName;
      const inField = tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";

      if (e.key === "Escape") {
        if (rosterQuery && els.search) {
          els.search.value = "";
          rosterQuery = "";
          layoutAndPaint();
          return;
        }
        if (focusPeerId) {
          focusPeerId = null;
          layoutAndPaint();
          return;
        }
        if (drawerOpen) setDrawer(false);
        return;
      }

      // / opens drawer + focuses search
      if (e.key === "/" && !inField) {
        e.preventDefault();
        setDrawer(true);
        if (els.search) {
          requestAnimationFrame(() => els.search.focus());
        }
        return;
      }

      // shortcuts: c cam, v cast, h hub
      if (inField) return;
      if (e.key === "c" || e.key === "C") toggleCam();
      if (e.key === "v" || e.key === "V") toggleCast();
      if (e.key === "h" || e.key === "H") toggleHub();
    });

    if (els.nick) {
      els.nick.addEventListener("change", () => {
        applyNick();
        saveState();
        layoutAndPaint();
      });
      els.nick.addEventListener("input", () => {
        applyNick();
        const selfTile = els.stage && els.stage.querySelector(".gg-tile.is-you .gg-name");
        if (selfTile) selfTile.textContent = myNick();
      });
    }
    if (els.hubUrl) {
      els.hubUrl.addEventListener("change", saveState);
    }

    if (els.installBtn) {
      els.installBtn.addEventListener("click", async () => {
        if (!deferredInstall) return;
        deferredInstall.prompt();
        await deferredInstall.userChoice;
        deferredInstall = null;
        els.installBtn.hidden = true;
      });
    }

    if (!nfcSupported() && els.nfcHint) {
      els.nfcHint.textContent =
        "NFC API not in this browser — use + / compass. Video: cam · cast · hub.";
    }

    let resizeT = 0;
    const onResize = () => {
      clearTimeout(resizeT);
      resizeT = setTimeout(layoutAndPaint, 60);
    };
    window.addEventListener("resize", onResize);
    if (window.visualViewport) window.visualViewport.addEventListener("resize", onResize);

    window.addEventListener("beforeunload", () => {
      if (castSession) sendJSON({ type: "vburst-end", from: myNick(), t: Date.now() });
      stopCam();
    });
  }

  // boot
  initPeers();
  wire();
  setDrawer(false);
  layoutAndPaint();
  raf = requestAnimationFrame(tick);
  registerSW();
})();
