/**
 * Full-resolution Glyph Matrix screen cast player.
 * Receives multi-feed luminance streams (13/25/37/49) and paints integer LED scale
 * to fill the display — for Chromecast/Presentation, second window, or fullscreen TV.
 *
 * Wire: BroadcastChannel("gy-glyph-cast") · PresentationReceiver · postMessage
 */
(function () {
  "use strict";

  const CHANNEL = "gy-glyph-cast";
  const els = {
    stage: document.getElementById("gc-stage"),
    hud: document.getElementById("gc-hud"),
    meta: document.getElementById("gc-meta"),
    res: document.getElementById("gc-res"),
    led: document.getElementById("gc-led"),
    gap: document.getElementById("gc-gap"),
    layout: document.getElementById("gc-layout"),
    style: document.getElementById("gc-style"),
    fs: document.getElementById("gc-fs"),
    hide: document.getElementById("gc-hud-hide"),
  };

  /** @type {{id:string,nick:string,mode:string,lum:Uint8Array,glyphN?:number}[]} */
  let peers = [];
  let glyphN = 25;
  let renderStyle = "matrix";
  let layoutMode = "grid"; // grid|focus|dual
  let ledPxPref = "auto"; // auto | number
  let gapPref = "1";
  let lastMsgAt = 0;
  let raf = 0;
  let forceN = 0; // 0 = follow stream
  let hideHudTimer = 0;

  // ── decode helpers ───────────────────────────────────────
  function b64ToU8(b64) {
    try {
      const bin = atob(b64);
      const out = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
      return out;
    } catch {
      return null;
    }
  }

  function normalizePeer(p) {
    if (!p) return null;
    let lum = null;
    if (p.lumB64) lum = b64ToU8(p.lumB64);
    else if (Array.isArray(p.lum)) lum = Uint8Array.from(p.lum.map((v) => Math.max(0, Math.min(255, v | 0))));
    else if (p.lum && p.lum.length) lum = p.lum instanceof Uint8Array ? p.lum : Uint8Array.from(p.lum);
    if (!lum || !lum.length) return null;
    return {
      id: String(p.id || p.nick || Math.random().toString(36).slice(2)),
      nick: String(p.nick || p.id || "peer"),
      mode: p.mode || (p.self ? "self" : p.casting ? "cast" : "rx"),
      lum: lum,
      glyphN: p.glyphN || 0,
    };
  }

  function applyMessage(msg) {
    if (!msg || typeof msg !== "object") return;
    if (msg.type && msg.type !== "glyph-cast" && msg.type !== "frame" && msg.type !== "state") {
      // allow bare payload
      if (!msg.peers && !msg.glyphN) return;
    }
    if (msg.glyphN && !forceN) {
      glyphN = [13, 25, 37, 49].includes(msg.glyphN) ? msg.glyphN : glyphN;
      if (els.res && !forceN) els.res.value = String(glyphN);
    }
    if (msg.style && ["matrix", "blocks", "neon"].includes(msg.style)) {
      renderStyle = msg.style;
      if (els.style) els.style.value = renderStyle;
    }
    if (msg.layout && ["grid", "focus", "dual"].includes(msg.layout)) {
      layoutMode = msg.layout;
      if (els.layout) els.layout.value = layoutMode;
    }
    if (msg.ledPx != null && msg.ledPx !== "") {
      ledPxPref = String(msg.ledPx);
      if (els.led) els.led.value = ledPxPref === "auto" ? "auto" : ledPxPref;
    }
    if (Array.isArray(msg.peers)) {
      peers = msg.peers.map(normalizePeer).filter(Boolean);
    }
    lastMsgAt = performance.now();
    document.body.classList.toggle("is-waiting", peers.length === 0);
    paint();
  }

  // ── layout + paint ───────────────────────────────────────
  function visiblePeers() {
    if (!peers.length) return [];
    if (layoutMode === "focus") {
      const self = peers.find((p) => p.mode === "self" || p.mode === "cast") || peers[0];
      return [self];
    }
    if (layoutMode === "dual") {
      const self = peers.find((p) => p.mode === "self" || p.mode === "cast");
      const rx = peers.find((p) => p !== self);
      return [self, rx].filter(Boolean).slice(0, 2);
    }
    return peers.slice(0, 16);
  }

  function computeGrid(n, W, H) {
    let best = { cols: 1, rows: n, cell: 1 };
    for (let cols = 1; cols <= n; cols++) {
      const rows = Math.ceil(n / cols);
      const cell = Math.floor(Math.min((W - (cols - 1) * 2) / cols, (H - (rows - 1) * 2) / rows));
      if (cell > best.cell) best = { cols, rows, cell };
    }
    return best;
  }

  function ledSizeFor(cell, n) {
    // integer LED pitch that fills cell with optional gap
    let gap = 1;
    if (gapPref === "0") gap = 0;
    else if (gapPref === "2") gap = 2;
    else if (gapPref === "pct") gap = Math.max(1, Math.floor(cell / n / 10));

    if (ledPxPref !== "auto") {
      const led = Math.max(1, parseInt(ledPxPref, 10) || 8);
      return { led, gap, size: n * led + (n - 1) * gap };
    }
    // auto: max integer led so n*led+(n-1)*gap <= cell
    let led = Math.max(1, Math.floor((cell - (n - 1) * gap) / n));
    // prefer multiple that keeps LEDs square and crisp
    if (led >= 2 && led % 2 === 1) {
      /* keep odd ok */
    }
    const size = n * led + (n - 1) * gap;
    return { led, gap, size };
  }

  function paintMatrix(canvas, lum, n, led, gap, style, mode) {
    const pitch = led + gap;
    const css = n * led + (n - 1) * gap;
    const dpr = Math.min(window.devicePixelRatio || 1, 3);
    const W = Math.max(1, Math.floor(css * dpr));
    const H = W;
    if (canvas.width !== W) canvas.width = W;
    if (canvas.height !== H) canvas.height = H;
    canvas.style.width = css + "px";
    canvas.style.height = css + "px";
    const ctx = canvas.getContext("2d");
    ctx.imageSmoothingEnabled = false;
    ctx.fillStyle = "#050508";
    ctx.fillRect(0, 0, W, H);

    const scale = W / css;
    const ledS = Math.max(1, Math.floor(led * scale));
    const gapS = Math.max(0, Math.floor(gap * scale));
    const pitchS = ledS + gapS;

    for (let y = 0; y < n; y++) {
      for (let x = 0; x < n; x++) {
        let L = lum[y * n + x] | 0;
        if (L < 0) L = 0;
        if (L > 255) L = 255;
        let r, g, b;
        if (style === "neon") {
          r = Math.min(255, (L * 0.35) | 0);
          g = Math.min(255, (L * 1.1) | 0);
          b = Math.min(255, (L * 1.25) | 0);
        } else if (mode === "cast") {
          r = Math.min(255, L);
          g = (L * 0.55) | 0;
          b = (L * 0.5) | 0;
        } else if (mode === "rx") {
          r = (L * 0.4) | 0;
          g = (L * 0.85) | 0;
          b = Math.min(255, (L * 1.05) | 0);
        } else if (mode === "self") {
          r = (L * 0.55) | 0;
          g = (L * 0.95) | 0;
          b = Math.min(255, L);
        } else {
          r = g = b = L;
        }
        if (style === "blocks" && gapS === 0) {
          // fake gutter
        }
        ctx.fillStyle = "rgb(" + r + "," + g + "," + b + ")";
        const px = x * pitchS;
        const py = y * pitchS;
        if (style === "blocks") {
          const inset = Math.max(0, Math.floor(ledS * 0.08));
          ctx.fillRect(px + inset, py + inset, ledS - inset * 2, ledS - inset * 2);
        } else {
          ctx.fillRect(px, py, ledS, ledS);
        }
      }
    }
  }

  function paint() {
    if (!els.stage) return;
    const list = visiblePeers();
    const W = window.innerWidth;
    const H = window.innerHeight;
    const nTiles = Math.max(1, list.length);
    const grid = computeGrid(nTiles, W - 8, H - 8);

    els.stage.style.gridTemplateColumns = "repeat(" + grid.cols + ", auto)";
    els.stage.innerHTML = "";

    list.forEach((p, i) => {
      const n = forceN || p.glyphN || glyphN;
      // resample if lum length mismatches (pad/crop)
      let lum = p.lum;
      const need = n * n;
      if (lum.length !== need) {
        const out = new Uint8Array(need);
        const srcN = Math.floor(Math.sqrt(lum.length)) || 25;
        for (let y = 0; y < n; y++) {
          for (let x = 0; x < n; x++) {
            const sx = Math.min(srcN - 1, Math.floor((x / n) * srcN));
            const sy = Math.min(srcN - 1, Math.floor((y / n) * srcN));
            out[y * n + x] = lum[sy * srcN + sx] || 0;
          }
        }
        lum = out;
      }
      const geo = ledSizeFor(grid.cell, n);
      const tile = document.createElement("div");
      tile.className = "gc-tile is-" + (p.mode || "rx");
      tile.style.width = geo.size + "px";
      tile.style.height = geo.size + "px";
      const canvas = document.createElement("canvas");
      paintMatrix(canvas, lum, n, geo.led, geo.gap, renderStyle, p.mode);
      const lab = document.createElement("div");
      lab.className = "gc-tile-label";
      lab.textContent = p.nick + " · " + n + "² · " + geo.led + "px";
      tile.appendChild(canvas);
      tile.appendChild(lab);
      els.stage.appendChild(tile);
    });

    if (els.meta) {
      const n = forceN || glyphN;
      const age = lastMsgAt ? Math.round((performance.now() - lastMsgAt) / 100) / 10 + "s" : "—";
      els.meta.textContent =
        list.length +
        " feed" +
        (list.length === 1 ? "" : "s") +
        " · " +
        n +
        "×" +
        n +
        " · LED " +
        (ledPxPref === "auto" ? "auto" : ledPxPref + "px") +
        " · " +
        layoutMode +
        " · " +
        renderStyle +
        " · t+" +
        age;
    }
  }

  // ── transport ────────────────────────────────────────────
  let bc = null;
  try {
    bc = new BroadcastChannel(CHANNEL);
    bc.onmessage = (ev) => applyMessage(ev.data);
  } catch (_) {}

  window.addEventListener("message", (ev) => {
    if (ev.data && (ev.data.type === "glyph-cast" || ev.data.peers)) applyMessage(ev.data);
  });

  // Presentation API receiver (Chromecast / second screen)
  function setupPresentationReceiver() {
    try {
      if (!navigator.presentation || !navigator.presentation.receiver) return;
      navigator.presentation.receiver.connectionList.then((list) => {
        list.connections.forEach(wirePresConn);
        list.onconnectionavailable = (e) => wirePresConn(e.connection);
      });
    } catch (_) {}
  }
  function wirePresConn(conn) {
    if (!conn) return;
    conn.addEventListener("message", (e) => {
      try {
        const msg = typeof e.data === "string" ? JSON.parse(e.data) : e.data;
        applyMessage(msg);
      } catch (_) {}
    });
    try {
      conn.send(JSON.stringify({ type: "glyph-cast-ready" }));
    } catch (_) {}
  }
  setupPresentationReceiver();

  // announce ready on channel
  try {
    if (bc) bc.postMessage({ type: "glyph-cast-ready", t: Date.now() });
  } catch (_) {}

  // ── URL params (Live News / GrokGlyph handoff) ───────────
  const params = new URLSearchParams(location.search || "");
  const sourceTag = params.get("source") || "";
  if (params.get("layout") && ["grid", "focus", "dual"].includes(params.get("layout"))) {
    layoutMode = params.get("layout");
    if (els.layout) els.layout.value = layoutMode;
  }
  if (params.get("n") && [13, 25, 37, 49].includes(parseInt(params.get("n"), 10))) {
    forceN = parseInt(params.get("n"), 10);
    glyphN = forceN;
    if (els.res) els.res.value = String(forceN);
  }
  if (params.get("style") && ["matrix", "blocks", "neon"].includes(params.get("style"))) {
    renderStyle = params.get("style");
    if (els.style) els.style.value = renderStyle;
  }
  if (params.get("led")) {
    ledPxPref = params.get("led");
    if (els.led) els.led.value = ledPxPref;
  }

  // optional direct hub ingest (works even without parent BroadcastChannel)
  let hubWs = null;
  function connectHub(url, room) {
    if (!url) return;
    try {
      const u = new URL(url.replace(/^http/, "ws"));
      if (room && !u.searchParams.get("room")) u.searchParams.set("room", room);
      if (!u.searchParams.get("nick")) u.searchParams.set("nick", "glyph-cast");
      if (!u.searchParams.get("role")) u.searchParams.set("role", "cast");
      if (hubWs) try { hubWs.close(); } catch (_) {}
      hubWs = new WebSocket(u.toString());
      hubWs.onopen = () => {
        hubWs.send(JSON.stringify({ type: "join", nick: "glyph-cast", role: "cast", room: room || "news" }));
        if (els.meta) els.meta.textContent = "hub connected · waiting frames…";
        document.body.classList.remove("is-waiting");
      };
      hubWs.onmessage = (ev) => {
        let msg;
        try {
          msg = JSON.parse(ev.data);
        } catch (_) {
          return;
        }
        if (msg.type === "vburst-frame" || msg.type === "news-frame") {
          const label = msg.label || msg.feed || msg.from || msg.src || "live";
          let lum = null;
          if (Array.isArray(msg.glyph)) {
            lum = Uint8Array.from(msg.glyph.map((v) => (v > 1 ? v & 255 : Math.round(v * 255))));
          } else if (msg.b64) {
            lum = b64ToU8(msg.b64);
          }
          if (!lum || !lum.length) return;
          const n = msg.glyphN || Math.round(Math.sqrt(lum.length)) || 25;
          const id = String(label).toLowerCase().replace(/\s+/g, "-").slice(0, 24);
          const existing = peers.find((p) => p.id === id);
          const peer = {
            id: id,
            nick: String(label).slice(0, 18),
            mode: "cast",
            lum: lum,
            glyphN: n,
          };
          if (existing) {
            Object.assign(existing, peer);
          } else {
            peers.push(peer);
            if (peers.length > 16) peers.shift();
          }
          lastMsgAt = performance.now();
          document.body.classList.toggle("is-waiting", peers.length === 0);
          paint();
        }
      };
      hubWs.onclose = () => {
        if (els.meta) els.meta.textContent = (sourceTag ? sourceTag + " · " : "") + "hub closed";
      };
    } catch (_) {}
  }
  const hubParam = params.get("hub") || "";
  const roomParam = params.get("room") || "news";
  if (hubParam) connectHub(hubParam, roomParam);

  // ── controls ─────────────────────────────────────────────
  function goFullscreen() {
    const el = document.documentElement;
    if (!document.fullscreenElement) {
      (el.requestFullscreen || el.webkitRequestFullscreen || el.msRequestFullscreen || (() => {})).call(el);
    } else {
      (document.exitFullscreen || document.webkitExitFullscreen || (() => {})).call(document);
    }
  }

  function scheduleHudHide() {
    if (els.hud) els.hud.classList.remove("is-hidden");
    clearTimeout(hideHudTimer);
    hideHudTimer = setTimeout(() => {
      if (els.hud) els.hud.classList.add("is-hidden");
    }, 4000);
  }

  if (els.res) {
    els.res.addEventListener("change", () => {
      forceN = parseInt(els.res.value, 10) || 25;
      glyphN = forceN;
      paint();
    });
  }
  if (els.led) {
    els.led.addEventListener("change", () => {
      ledPxPref = els.led.value;
      paint();
    });
  }
  if (els.gap) {
    els.gap.addEventListener("change", () => {
      gapPref = els.gap.value;
      paint();
    });
  }
  if (els.layout) {
    els.layout.addEventListener("change", () => {
      layoutMode = els.layout.value;
      paint();
    });
  }
  if (els.style) {
    els.style.addEventListener("change", () => {
      renderStyle = els.style.value;
      paint();
    });
  }
  if (els.fs) els.fs.addEventListener("click", goFullscreen);
  if (els.hide) {
    els.hide.addEventListener("click", () => {
      if (els.hud) els.hud.classList.add("is-hidden");
    });
  }

  document.addEventListener("mousemove", scheduleHudHide);
  document.addEventListener("touchstart", scheduleHudHide, { passive: true });
  document.addEventListener("keydown", (e) => {
    if (e.key === "f" || e.key === "F") goFullscreen();
    if (e.key === "h" || e.key === "H") {
      if (els.hud) els.hud.classList.toggle("is-hidden");
    }
    if (e.key === "g" || e.key === "G") {
      layoutMode = layoutMode === "grid" ? "focus" : layoutMode === "focus" ? "dual" : "grid";
      if (els.layout) els.layout.value = layoutMode;
      paint();
    }
    if (e.key >= "1" && e.key <= "4") {
      forceN = [13, 25, 37, 49][parseInt(e.key, 10) - 1];
      glyphN = forceN;
      if (els.res) els.res.value = String(forceN);
      paint();
    }
    scheduleHudHide();
  });

  window.addEventListener("resize", () => {
    paint();
  });

  // idle sim when no feed
  function tick(t) {
    raf = requestAnimationFrame(tick);
    if (peers.length === 0 && performance.now() - lastMsgAt > 500) {
      // soft waiting pulse
      if (Math.floor(t / 200) % 2 === 0) {
        const n = forceN || glyphN;
        const lum = new Uint8Array(n * n);
        const cx = (n - 1) / 2;
        for (let y = 0; y < n; y++) {
          for (let x = 0; x < n; x++) {
            const d = Math.hypot(x - cx, y - cx) / (n * 0.55);
            lum[y * n + x] = Math.max(0, Math.min(255, ((1 - d) * 80 * (0.6 + 0.4 * Math.sin(t * 0.003))) | 0));
          }
        }
        peers = [{ id: "wait", nick: "waiting", mode: "rx", lum: lum, glyphN: n }];
        paint();
        peers = [];
      }
    }
  }

  document.body.classList.add("is-waiting");
  scheduleHudHide();
  raf = requestAnimationFrame(tick);

  // auto-fullscreen after short delay when opened as cast target
  if (location.search.includes("fs=1") || location.search.includes("cast=1")) {
    setTimeout(goFullscreen, 400);
  }

  if (sourceTag && els.meta) {
    els.meta.textContent = sourceTag + " · waiting for feed…";
  }
  // brand hint for Live News cast
  const brand = document.querySelector(".gc-brand");
  if (brand && sourceTag === "livenews") {
    brand.textContent = "◈ Live News · screen";
  }
})();
