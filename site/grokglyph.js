/**
 * GrokGlyph PWA — multi-user 25×25 luminance Glyph Matrix room.
 * Side-by-side tiles scale to device; mesh joins visible peers;
 * compass N/E/S/W add & subtract; NFC tap add + delete.
 */
(function () {
  "use strict";

  const N = 25; // Glyph Matrix side (hardware luminance)
  const STORAGE_KEY = "grokglyph.v1";
  const DIR_ORDER = ["n", "e", "s", "w"];
  const DIR_DELTA = {
    n: { x: 0, y: -1 },
    e: { x: 1, y: 0 },
    s: { x: 0, y: 1 },
    w: { x: -1, y: 0 },
  };

  /** @typedef {{ id: string, nick: string, dir: string, on: boolean, seed: number, self?: boolean, nfc?: boolean }} Peer */

  const els = {
    stage: document.getElementById("gg-stage"),
    meshSvg: document.getElementById("gg-mesh-svg"),
    roster: document.getElementById("gg-roster"),
    scaleLabel: document.getElementById("gg-scale-label"),
    countLabel: document.getElementById("gg-count-label"),
    meshLabel: document.getElementById("gg-mesh-label"),
    capHint: document.getElementById("gg-cap-hint"),
    nick: document.getElementById("gg-nick"),
    addBtn: document.getElementById("gg-add"),
    meshToggle: document.getElementById("gg-mesh-toggle"),
    nfcAdd: document.getElementById("gg-nfc-add"),
    nfcHint: document.getElementById("gg-nfc-hint"),
    installBtn: document.getElementById("gg-install"),
    wrap: document.querySelector(".gg-stage-wrap"),
  };

  /** @type {Peer[]} */
  let peers = [];
  let meshOn = true;
  let cellPx = 100;
  let maxVisible = 4;
  let raf = 0;
  let t0 = performance.now();
  /** @type {Map<string, HTMLCanvasElement>} */
  const canvasById = new Map();
  /** @type {BeforeInstallPromptEvent | null} */
  let deferredInstall = null;

  // ── persistence ──────────────────────────────────────────
  function loadState() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
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
          nick: els.nick.value.trim() || "you",
          meshOn,
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
      /* ignore quota */
    }
  }

  function uid(prefix) {
    return (
      (prefix || "p") +
      "-" +
      Math.random().toString(36).slice(2, 8) +
      Date.now().toString(36).slice(-3)
    );
  }

  function defaultSelf() {
    return {
      id: "self",
      nick: "you",
      dir: "c",
      on: true,
      seed: 42,
      self: true,
    };
  }

  function initPeers() {
    const st = loadState();
    if (st && Array.isArray(st.peers) && st.peers.length) {
      peers = st.peers;
      meshOn = st.meshOn !== false;
      if (st.nick) els.nick.value = st.nick;
      // ensure self exists
      if (!peers.some((p) => p.self || p.id === "self")) {
        peers.unshift(defaultSelf());
      }
    } else {
      peers = [
        defaultSelf(),
        makePeer("n", "north"),
        makePeer("e", "east"),
      ];
    }
    applyNick();
  }

  function applyNick() {
    const n = (els.nick.value || "you").trim().slice(0, 16) || "you";
    const self = peers.find((p) => p.self || p.id === "self");
    if (self) self.nick = n;
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
    };
  }

  // ── scale / capacity from device ─────────────────────────
  /**
   * Fit as many 25×25 glyphs as the viewport allows.
   * Cell size adapts; overflow peers flip off (still listed).
   */
  function computeLayout() {
    const wrap = els.wrap;
    if (!wrap) return;
    const rect = wrap.getBoundingClientRect();
    const w = Math.max(200, rect.width - 24);
    const h = Math.max(160, Math.min(window.innerHeight * 0.48, rect.height || 400));
    // ideal cell: leave room for label (~18px) + gap
    const dpr = Math.min(window.devicePixelRatio || 1, 2.5);
    const minCell = 56;
    const maxCell = Math.min(168, Math.floor(w / 2));
    // prefer cells that map ~4–6 CSS px per LED for crisp pixelation
    let cell = Math.floor(Math.min(maxCell, Math.max(minCell, w / 3 - 12)));
    // snap so 25 LEDs divide reasonably
    cell = Math.max(minCell, Math.floor(cell / 25) * 25 || cell);
    if (cell < minCell) cell = minCell;

    const gap = Math.max(6, Math.round(cell * 0.08));
    const cols = Math.max(1, Math.floor((w + gap) / (cell + gap)));
    // height budget: tile = cell + ~22 meta
    const tileH = cell + 28;
    const rows = Math.max(1, Math.floor((h + gap) / (tileH + gap)));
    maxVisible = Math.max(1, cols * rows);

    cellPx = cell;
    document.documentElement.style.setProperty("--gg-cell", cell + "px");
    document.documentElement.style.setProperty("--gg-gap", gap + "px");

    const short = window.matchMedia("(max-width: 520px)").matches;
    els.scaleLabel.textContent =
      cell +
      "px · " +
      cols +
      "×" +
      rows +
      " · dpr " +
      dpr.toFixed(1) +
      (short ? " · phone" : "");

    // auto on/off by capacity (self always prefers on)
    const ordered = visibleOrder();
    ordered.forEach((p, i) => {
      if (p.self) {
        p.on = true;
        return;
      }
      // keep manual off sticky only if user toggled — we use on as capacity flag;
      // capacity: first maxVisible stay candidates; excess force off for display
      if (i >= maxVisible) {
        p._capOff = true;
      } else {
        p._capOff = false;
      }
    });

    const onCount = peers.filter((p) => p.on && !p._capOff).length;
    els.countLabel.textContent =
      onCount + " on / " + peers.length + " · cap " + maxVisible;
    els.capHint.textContent =
      "(device shows up to " + maxVisible + " matrices)";
    els.meshLabel.textContent = meshOn ? "mesh on" : "mesh off";
  }

  /** Prefer self, then compass N E S W, then rest. */
  function visibleOrder() {
    const self = peers.filter((p) => p.self);
    const rest = peers.filter((p) => !p.self);
    rest.sort((a, b) => {
      const ai = DIR_ORDER.indexOf(a.dir);
      const bi = DIR_ORDER.indexOf(b.dir);
      if (ai !== bi) return (ai < 0 ? 9 : ai) - (bi < 0 ? 9 : bi);
      return a.nick.localeCompare(b.nick);
    });
    return self.concat(rest);
  }

  function isDrawn(p) {
    return p.on && !p._capOff;
  }

  // ── luminance pattern (25×25) ────────────────────────────
  /**
   * Procedural Glyph-like luminance for demo (no cam required).
   * Mirrors terminal hexlum spirit: circular falloff + soft noise + pulse.
   * @param {Uint8Array} out length 625
   * @param {number} seed
   * @param {number} t seconds
   * @param {boolean} isSelf
   */
  function fillLuminance(out, seed, t, isSelf) {
    const cx = 12,
      cy = 12;
    const pulse = 0.55 + 0.45 * Math.sin(t * (isSelf ? 2.1 : 1.4) + (seed % 7) * 0.3);
    for (let y = 0; y < N; y++) {
      for (let x = 0; x < N; x++) {
        const dx = x - cx;
        const dy = y - cy;
        const r = Math.sqrt(dx * dx + dy * dy);
        // soft disk like Nothing Glyph Matrix
        let v = Math.max(0, 1 - r / 13.2);
        v = v * v;
        // hash noise
        const h = hash2(x + seed * 3, y + seed * 7);
        v = v * (0.72 + 0.28 * h) * pulse;
        // directional accent stripe
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

  function drawGlyph(canvas, data, accent) {
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    // keep buffer at 25×25; CSS scales
    if (canvas.width !== N) canvas.width = N;
    if (canvas.height !== N) canvas.height = N;
    const img = ctx.createImageData(N, N);
    const d = img.data;
    for (let i = 0; i < N * N; i++) {
      const L = data[i];
      // cyan-tinted luminance (hardware white LEDs on dark)
      const r = (L * (accent ? 0.55 : 0.35)) | 0;
      const g = (L * (accent ? 0.95 : 0.85)) | 0;
      const b = (L * (accent ? 1.0 : 0.95)) | 0;
      const o = i * 4;
      d[o] = Math.min(255, r + (L > 200 ? 20 : 0));
      d[o + 1] = Math.min(255, g);
      d[o + 2] = Math.min(255, b);
      d[o + 3] = 255;
    }
    ctx.putImageData(img, 0, 0);
  }

  // ── DOM tiles ────────────────────────────────────────────
  function renderTiles() {
    const order = visibleOrder();
    const keep = new Set();
    els.stage.innerHTML = "";
    canvasById.clear();

    for (const p of order) {
      if (!isDrawn(p)) continue;
      keep.add(p.id);
      const tile = document.createElement("div");
      tile.className =
        "gg-tile" +
        (p.self ? " is-you" : "") +
        (p.on ? "" : " is-off");
      tile.dataset.id = p.id;
      tile.setAttribute("role", "listitem");

      if (p.dir && p.dir !== "c") {
        const badge = document.createElement("span");
        badge.className = "gg-dir-badge";
        badge.textContent = p.dir.toUpperCase();
        tile.appendChild(badge);
      }

      const c = document.createElement("canvas");
      c.width = N;
      c.height = N;
      c.setAttribute("aria-label", "Glyph Matrix 25×25 " + p.nick);
      tile.appendChild(c);
      canvasById.set(p.id, c);

      const meta = document.createElement("div");
      meta.className = "gg-tile-meta";
      const name = document.createElement("span");
      name.className = "gg-name";
      name.textContent = p.nick + (p.nfc ? " ·nfc" : "");
      meta.appendChild(name);
      if (!p.self) {
        const del = document.createElement("button");
        del.type = "button";
        del.className = "gg-tile-del";
        del.title = "Delete peer";
        del.setAttribute("aria-label", "Delete " + p.nick);
        del.textContent = "×";
        del.addEventListener("click", (e) => {
          e.stopPropagation();
          removePeer(p.id);
        });
        meta.appendChild(del);
      }
      tile.appendChild(meta);

      tile.addEventListener("click", () => {
        if (p.self) return;
        p.on = !p.on;
        saveState();
        layoutAndPaint();
      });

      els.stage.appendChild(tile);
    }

    renderRoster();
    requestAnimationFrame(() => drawMesh());
  }

  function renderRoster() {
    els.roster.innerHTML = "";
    for (const p of visibleOrder()) {
      const li = document.createElement("li");
      li.className = isDrawn(p) ? "on" : "";
      const dot = document.createElement("span");
      dot.className = "gg-dot";
      li.appendChild(dot);
      const label = document.createElement("span");
      label.textContent =
        p.nick +
        (p.self ? " (you)" : "") +
        (p._capOff ? " · overflow" : p.on ? "" : " · off");
      li.appendChild(label);
      const rid = document.createElement("span");
      rid.className = "gg-rid";
      rid.textContent = (p.dir || "·").toUpperCase() + " · " + p.id.slice(0, 10);
      li.appendChild(rid);

      if (!p.self) {
        const tog = document.createElement("button");
        tog.type = "button";
        tog.textContent = p.on ? "off" : "on";
        tog.title = "Toggle on/off";
        tog.addEventListener("click", () => {
          p.on = !p.on;
          saveState();
          layoutAndPaint();
        });
        li.appendChild(tog);

        const del = document.createElement("button");
        del.type = "button";
        del.className = "danger";
        del.textContent = "delete";
        del.addEventListener("click", () => removePeer(p.id));
        li.appendChild(del);
      }
      els.roster.appendChild(li);
    }
  }

  // ── mesh joining lines ───────────────────────────────────
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

    const centers = tiles.map((t) => {
      const r = t.getBoundingClientRect();
      return {
        id: t.dataset.id,
        x: r.left + r.width / 2 - wrapRect.left,
        y: r.top + r.height / 2 - wrapRect.top,
      };
    });

    // join each tile to nearest neighbors (Delaunay-lite: k=2 nearest)
    const drawn = new Set();
    for (let i = 0; i < centers.length; i++) {
      const dists = centers
        .map((c, j) => ({ j, d: (c.x - centers[i].x) ** 2 + (c.y - centers[i].y) ** 2 }))
        .filter((x) => x.j !== i)
        .sort((a, b) => a.d - b.d)
        .slice(0, 2);
      for (const n of dists) {
        const a = Math.min(i, n.j);
        const b = Math.max(i, n.j);
        const key = a + "-" + b;
        if (drawn.has(key)) continue;
        drawn.add(key);
        const line = document.createElementNS("http://www.w3.org/2000/svg", "line");
        line.setAttribute("x1", String(centers[i].x));
        line.setAttribute("y1", String(centers[i].y));
        line.setAttribute("x2", String(centers[n.j].x));
        line.setAttribute("y2", String(centers[n.j].y));
        if (tiles[i].classList.contains("is-you") || tiles[n.j].classList.contains("is-you")) {
          line.classList.add("gg-mesh-strong");
        }
        svg.appendChild(line);
      }
    }
  }

  // ── compass add / subtract ───────────────────────────────
  function addInDirection(dir) {
    const d = DIR_DELTA[dir];
    if (!d) return;
    const count = peers.filter((p) => p.dir === dir).length + 1;
    const nick = dir.toUpperCase() + count;
    const p = makePeer(dir, nick);
    peers.push(p);
    // if over capacity, turn off oldest non-self in that dir or global overflow
    enforceCapacity();
    saveState();
    layoutAndPaint();
  }

  function subInDirection(dir) {
    // remove last peer in that direction (not self)
    for (let i = peers.length - 1; i >= 0; i--) {
      const p = peers[i];
      if (!p.self && p.dir === dir) {
        peers.splice(i, 1);
        saveState();
        layoutAndPaint();
        return;
      }
    }
  }

  function enforceCapacity() {
    const order = visibleOrder();
    let onIdx = 0;
    for (const p of order) {
      if (p.self) continue;
      if (!p.on) continue;
      onIdx++;
      // soft: leave on flags; layout _capOff handles display
    }
    void onIdx;
  }

  function removePeer(id) {
    peers = peers.filter((p) => p.id !== id && !(p.self && id === "self"));
    // never remove last self
    if (!peers.some((p) => p.self)) peers.unshift(defaultSelf());
    saveState();
    layoutAndPaint();
  }

  function addManualPeer() {
    const dirs = DIR_ORDER;
    const dir = dirs[peers.filter((p) => !p.self).length % 4];
    peers.push(makePeer(dir, "peer" + peers.length));
    saveState();
    layoutAndPaint();
  }

  // ── NFC (Web NFC — Chromium Android) ─────────────────────
  let nfcListening = false;

  function nfcSupported() {
    return "NDEFReader" in window;
  }

  async function startNfcAdd() {
    if (!nfcSupported()) {
      els.nfcHint.textContent =
        "Web NFC not available here. Use + Peer or compass. (Chrome on Android + HTTPS.)";
      return;
    }
    try {
      // @ts-ignore
      const reader = new NDEFReader();
      await reader.scan();
      nfcListening = true;
      els.nfcAdd.textContent = "NFC listening…";
      els.nfcAdd.classList.add("primary");
      els.nfcHint.textContent = "Hold a tag / peer phone near the device…";
      reader.addEventListener("reading", ({ message, serialNumber }) => {
        const parsed = parseNdef(message, serialNumber);
        addFromNfc(parsed);
      });
      reader.addEventListener("readingerror", () => {
        els.nfcHint.textContent = "NFC read error — try again.";
      });
    } catch (err) {
      els.nfcHint.textContent =
        "NFC permission denied or unavailable: " + (err && err.message ? err.message : String(err));
      nfcListening = false;
      els.nfcAdd.textContent = "NFC tap add";
    }
  }

  function parseNdef(message, serialNumber) {
    let text = "";
    let url = "";
    try {
      for (const rec of message.records || []) {
        if (rec.recordType === "text") {
          const dec = new TextDecoder(rec.encoding || "utf-8");
          // skip language code prefix if present
          const full = dec.decode(rec.data);
          text += full.replace(/^[a-z]{2,3}\|?/, "") + " ";
        } else if (rec.recordType === "url") {
          const dec = new TextDecoder();
          url = dec.decode(rec.data);
        } else if (rec.recordType === "absolute-url") {
          const dec = new TextDecoder();
          url = dec.decode(rec.data);
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
    return { id, nick, raw: blob };
  }

  function addFromNfc({ id, nick }) {
    const existing = peers.find((p) => p.id === id || (p.nfc && p.nick === nick));
    if (existing) {
      existing.on = true;
      existing.nfc = true;
      els.nfcHint.textContent = "NFC: re-enabled " + existing.nick;
    } else {
      const dir = DIR_ORDER[peers.filter((p) => !p.self).length % 4];
      peers.push({
        id,
        nick,
        dir,
        on: true,
        seed: hashStr(id),
        nfc: true,
      });
      els.nfcHint.textContent = "NFC: added " + nick + " · tap delete on tile to remove";
    }
    saveState();
    layoutAndPaint();
  }

  function hashStr(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
    return h >>> 0;
  }

  // ── animation loop ───────────────────────────────────────
  const lumBuf = new Map(); // id → Uint8Array

  function tick(now) {
    const t = (now - t0) / 1000;
    for (const p of peers) {
      if (!isDrawn(p)) continue;
      const c = canvasById.get(p.id);
      if (!c) continue;
      let buf = lumBuf.get(p.id);
      if (!buf) {
        buf = new Uint8Array(N * N);
        lumBuf.set(p.id, buf);
      }
      fillLuminance(buf, p.seed, t, !!p.self);
      drawGlyph(c, buf, !!p.self);
    }
    raf = requestAnimationFrame(tick);
  }

  function layoutAndPaint() {
    computeLayout();
    renderTiles();
  }

  // ── PWA install + SW ─────────────────────────────────────
  function registerSW() {
    if (!("serviceWorker" in navigator)) return;
    const swUrl = new URL("sw-grokglyph.js", window.location.href).href;
    navigator.serviceWorker.register(swUrl, { scope: "./" }).catch(() => {
      /* offline / file:// ok to fail */
    });
  }

  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferredInstall = e;
    if (els.installBtn) {
      els.installBtn.hidden = false;
    }
  });

  // ── wire UI ──────────────────────────────────────────────
  function wire() {
    document.querySelectorAll(".gg-cbtn[data-dir]").forEach((btn) => {
      btn.addEventListener("click", () => addInDirection(btn.getAttribute("data-dir")));
    });
    document.querySelectorAll(".gg-sub[data-sub]").forEach((btn) => {
      btn.addEventListener("click", () => subInDirection(btn.getAttribute("data-sub")));
    });
    els.addBtn.addEventListener("click", addManualPeer);
    els.meshToggle.addEventListener("click", () => {
      meshOn = !meshOn;
      els.meshToggle.textContent = meshOn ? "Mesh join" : "Mesh off";
      saveState();
      drawMesh();
      els.meshLabel.textContent = meshOn ? "mesh on" : "mesh off";
    });
    els.nfcAdd.addEventListener("click", startNfcAdd);
    els.nick.addEventListener("change", () => {
      applyNick();
      saveState();
      layoutAndPaint();
    });
    els.nick.addEventListener("input", () => {
      applyNick();
      const selfTile = els.stage.querySelector(".gg-tile.is-you .gg-name");
      if (selfTile) selfTile.textContent = els.nick.value.trim() || "you";
    });
    if (els.installBtn) {
      els.installBtn.addEventListener("click", async () => {
        if (!deferredInstall) return;
        deferredInstall.prompt();
        await deferredInstall.userChoice;
        deferredInstall = null;
        els.installBtn.hidden = true;
      });
    }

    if (!nfcSupported()) {
      els.nfcHint.textContent =
        "NFC API not in this browser — use + Peer / compass. Delete via × on a tile or roster.";
    }

    let resizeT = 0;
    window.addEventListener("resize", () => {
      clearTimeout(resizeT);
      resizeT = setTimeout(layoutAndPaint, 80);
    });
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", () => {
        clearTimeout(resizeT);
        resizeT = setTimeout(layoutAndPaint, 80);
      });
    }
  }

  // boot
  initPeers();
  els.meshToggle.textContent = meshOn ? "Mesh join" : "Mesh off";
  wire();
  layoutAndPaint();
  raf = requestAnimationFrame(tick);
  registerSW();
})();
