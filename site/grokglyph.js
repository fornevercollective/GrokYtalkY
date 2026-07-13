/**
 * GrokGlyph PWA — full-page multi-user 25×25 luminance stack.
 * Side-by-side tiles with 1px column/row gutters; slim header;
 * footer pop-up drawer for compass / roster / NFC / rest.
 */
(function () {
  "use strict";

  const N = 25; // Glyph Matrix side
  const GAP = 1; // px between glyphs (column + row)
  const STORAGE_KEY = "grokglyph.v2";
  const DIR_ORDER = ["n", "e", "s", "w"];
  const DIR_DELTA = {
    n: { x: 0, y: -1 },
    e: { x: 1, y: 0 },
    s: { x: 0, y: 1 },
    w: { x: -1, y: 0 },
  };

  /** @typedef {{ id: string, nick: string, dir: string, on: boolean, seed: number, self?: boolean, nfc?: boolean, _capOff?: boolean }} Peer */

  const els = {
    stage: document.getElementById("gg-stage"),
    wrap: document.getElementById("gg-stage-wrap"),
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
    drawer: document.getElementById("gg-drawer"),
    drawerRoot: document.getElementById("gg-drawer-root"),
    drawerToggle: document.getElementById("gg-drawer-toggle"),
    drawerHandle: document.getElementById("gg-drawer-handle"),
    drawerScrim: document.getElementById("gg-drawer-scrim"),
  };

  /** @type {Peer[]} */
  let peers = [];
  let meshOn = true;
  let drawerOpen = false;
  let gridCols = 1;
  let gridRows = 1;
  let cellPx = 100;
  let maxVisible = 4;
  let raf = 0;
  let t0 = performance.now();
  /** @type {Map<string, HTMLCanvasElement>} */
  const canvasById = new Map();
  /** @type {BeforeInstallPromptEvent | null} */
  let deferredInstall = null;
  const lumBuf = new Map();

  // ── persistence ──────────────────────────────────────────
  function loadState() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY) || localStorage.getItem("grokglyph.v1");
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
          nick: (els.nick && els.nick.value.trim()) || "you",
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
      /* ignore */
    }
  }

  function uid(prefix) {
    return (prefix || "p") + "-" + Math.random().toString(36).slice(2, 8) + Date.now().toString(36).slice(-3);
  }

  function defaultSelf() {
    return { id: "self", nick: "you", dir: "c", on: true, seed: 42, self: true };
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

  function initPeers() {
    const st = loadState();
    if (st && Array.isArray(st.peers) && st.peers.length) {
      peers = st.peers;
      meshOn = st.meshOn !== false;
      if (st.nick && els.nick) els.nick.value = st.nick;
      if (!peers.some((p) => p.self || p.id === "self")) peers.unshift(defaultSelf());
    } else {
      peers = [defaultSelf(), makePeer("n", "north"), makePeer("e", "east")];
    }
    applyNick();
  }

  function applyNick() {
    if (!els.nick) return;
    const n = (els.nick.value || "you").trim().slice(0, 16) || "you";
    const self = peers.find((p) => p.self || p.id === "self");
    if (self) self.nick = n;
  }

  // ── layout: full stage, 1px gutters, maximize cell ───────
  /**
   * Fit all drawn peers into the stage with 1px column/row gaps.
   * Picks cols/rows that maximize square cell size for count n.
   */
  function computeLayout() {
    const wrap = els.wrap;
    if (!wrap) return;

    const rect = wrap.getBoundingClientRect();
    const W = Math.max(1, Math.floor(rect.width));
    const H = Math.max(1, Math.floor(rect.height));
    const dpr = Math.min(window.devicePixelRatio || 1, 3);

    // how many peers want to be drawn (on + not capacity-forced yet)
    // first pass: assume all "on" peers; capacity = what fits at min readable size
    const want = peers.filter((p) => p.on || p.self);
    // always try to show as many as fit; self first
    const nWant = Math.max(1, want.length);

    // min cell ~ 25px (1 CSS px per LED); prefer larger
    const minCell = 25;
    let best = { cols: 1, rows: nWant, cell: minCell };

    for (let cols = 1; cols <= nWant; cols++) {
      const rows = Math.ceil(nWant / cols);
      const cellW = Math.floor((W - (cols - 1) * GAP) / cols);
      const cellH = Math.floor((H - (rows - 1) * GAP) / rows);
      const cell = Math.min(cellW, cellH);
      if (cell < minCell) continue;
      // prefer larger cells; tie-break: more square grid, then more columns (side-by-side)
      if (
        cell > best.cell ||
        (cell === best.cell && Math.abs(cols - rows) < Math.abs(best.cols - best.rows)) ||
        (cell === best.cell && cols > best.cols)
      ) {
        best = { cols, rows, cell };
      }
    }

    // if nothing fit minCell, shrink to fill with all peers
    if (best.cell < minCell || (best.cols === 1 && best.rows === nWant && best.cell === minCell)) {
      for (let cols = 1; cols <= nWant; cols++) {
        const rows = Math.ceil(nWant / cols);
        const cellW = Math.floor((W - (cols - 1) * GAP) / cols);
        const cellH = Math.floor((H - (rows - 1) * GAP) / rows);
        const cell = Math.max(1, Math.min(cellW, cellH));
        if (cell > best.cell) best = { cols, rows, cell };
      }
    }

    // optional: snap down to multiple of N for integer LED scale when close
    let cell = best.cell;
    if (cell >= N) {
      const snapped = Math.floor(cell / N) * N;
      if (snapped >= N && snapped >= cell * 0.85) cell = snapped;
    }

    gridCols = best.cols;
    gridRows = best.rows;
    cellPx = cell;
    maxVisible = gridCols * gridRows;

    // capacity: mark overflow peers (_capOff) after ordering
    const ordered = visibleOrder();
    ordered.forEach((p, i) => {
      if (p.self) {
        p.on = true;
        p._capOff = false;
        return;
      }
      p._capOff = i >= maxVisible;
    });

    const root = document.documentElement;
    root.style.setProperty("--gg-gap", GAP + "px");
    root.style.setProperty("--gg-cell", cellPx + "px");
    root.style.setProperty("--gg-cols", String(gridCols));
    root.style.setProperty("--gg-rows", String(gridRows));

    const drawn = peers.filter((p) => isDrawn(p)).length;
    if (els.scaleLabel) {
      els.scaleLabel.textContent =
        cellPx + "px · " + gridCols + "×" + gridRows + " · " + dpr.toFixed(1) + "×";
    }
    if (els.countLabel) {
      els.countLabel.textContent = drawn + "/" + peers.length;
    }
    if (els.capHint) {
      els.capHint.textContent =
        "(" + gridCols + "×" + gridRows + " · " + cellPx + "px · 1px gap · cap " + maxVisible + ")";
    }
    if (els.meshLabel) {
      els.meshLabel.textContent = meshOn ? "mesh" : "off";
    }
    if (els.meshToggle) {
      els.meshToggle.setAttribute("aria-pressed", meshOn ? "true" : "false");
      els.meshToggle.textContent = meshOn ? "mesh" : "mesh";
    }
  }

  function visibleOrder() {
    const self = peers.filter((p) => p.self);
    const rest = peers.filter((p) => !p.self);
    rest.sort((a, b) => {
      // on first, then compass order
      if (a.on !== b.on) return a.on ? -1 : 1;
      const ai = DIR_ORDER.indexOf(a.dir);
      const bi = DIR_ORDER.indexOf(b.dir);
      if (ai !== bi) return (ai < 0 ? 9 : ai) - (bi < 0 ? 9 : bi);
      return a.nick.localeCompare(b.nick);
    });
    return self.concat(rest);
  }

  function isDrawn(p) {
    return !!(p.on && !p._capOff);
  }

  // ── luminance ────────────────────────────────────────────
  function fillLuminance(out, seed, t, isSelf) {
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

  function drawGlyph(canvas, data, accent) {
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    if (canvas.width !== N) canvas.width = N;
    if (canvas.height !== N) canvas.height = N;
    const img = ctx.createImageData(N, N);
    const d = img.data;
    for (let i = 0; i < N * N; i++) {
      const L = data[i];
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

  // ── tiles ────────────────────────────────────────────────
  function renderTiles() {
    if (!els.stage) return;
    els.stage.innerHTML = "";
    canvasById.clear();

    const order = visibleOrder().filter(isDrawn);
    for (const p of order) {
      const tile = document.createElement("div");
      tile.className = "gg-tile" + (p.self ? " is-you" : "") + (p.on ? "" : " is-off");
      tile.dataset.id = p.id;
      tile.setAttribute("role", "listitem");
      tile.tabIndex = 0;
      tile.title = p.nick + (p.self ? " (you)" : "");

      if (p.dir && p.dir !== "c") {
        const badge = document.createElement("span");
        badge.className = "gg-dir-badge";
        badge.textContent = p.dir.toUpperCase();
        tile.appendChild(badge);
      }

      const c = document.createElement("canvas");
      c.width = N;
      c.height = N;
      c.setAttribute("aria-label", "Glyph 25×25 " + p.nick);
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
        del.setAttribute("aria-label", "Delete " + p.nick);
        del.textContent = "×";
        del.addEventListener("click", (e) => {
          e.stopPropagation();
          removePeer(p.id);
        });
        meta.appendChild(del);
      }
      tile.appendChild(meta);

      // long-press delete (touch)
      let pressT = 0;
      tile.addEventListener("pointerdown", (e) => {
        if (p.self || e.button === 2) return;
        pressT = window.setTimeout(() => {
          removePeer(p.id);
        }, 650);
      });
      const clearPress = () => {
        if (pressT) clearTimeout(pressT);
        pressT = 0;
      };
      tile.addEventListener("pointerup", clearPress);
      tile.addEventListener("pointerleave", clearPress);
      tile.addEventListener("pointercancel", clearPress);

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
    if (!els.roster) return;
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
      rid.textContent = (p.dir || "·").toUpperCase();
      li.appendChild(rid);

      if (!p.self) {
        const tog = document.createElement("button");
        tog.type = "button";
        tog.textContent = p.on ? "off" : "on";
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

  // ── mesh: join 4-neighbors in the grid (side-by-side stack) ─
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

    // place lines through the 1px gutters between adjacent grid cells
    const centers = tiles.map((t, idx) => {
      const r = t.getBoundingClientRect();
      return {
        idx,
        id: t.dataset.id,
        col: idx % gridCols,
        row: Math.floor(idx / gridCols),
        x: r.left + r.width / 2 - wrapRect.left,
        y: r.top + r.height / 2 - wrapRect.top,
        you: t.classList.contains("is-you"),
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
      if (a.you || b.you) line.classList.add("gg-mesh-strong");
      svg.appendChild(line);
    }

    centers.forEach((c) => {
      const right = byPos.get(c.col + 1 + "," + c.row);
      const down = byPos.get(c.col + "," + (c.row + 1));
      if (right) link(c, right);
      if (down) link(c, down);
    });
  }

  // ── peers ────────────────────────────────────────────────
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
    saveState();
    layoutAndPaint();
  }

  function addManualPeer() {
    const dir = DIR_ORDER[peers.filter((p) => !p.self).length % 4];
    peers.push(makePeer(dir, "peer" + peers.length));
    saveState();
    layoutAndPaint();
  }

  // ── NFC ──────────────────────────────────────────────────
  function nfcSupported() {
    return "NDEFReader" in window;
  }

  async function startNfcAdd() {
    if (!nfcSupported()) {
      if (els.nfcHint) {
        els.nfcHint.textContent =
          "Web NFC not available. Use + or compass. (Chrome on Android + HTTPS.)";
      }
      setDrawer(true);
      return;
    }
    try {
      // @ts-ignore
      const reader = new NDEFReader();
      await reader.scan();
      if (els.nfcAdd) els.nfcAdd.textContent = "nfc…";
      if (els.nfcHint) els.nfcHint.textContent = "Hold a tag / peer phone near the device…";
      setDrawer(true);
      reader.addEventListener("reading", ({ message, serialNumber }) => {
        addFromNfc(parseNdef(message, serialNumber));
      });
      reader.addEventListener("readingerror", () => {
        if (els.nfcHint) els.nfcHint.textContent = "NFC read error — try again.";
      });
    } catch (err) {
      if (els.nfcHint) {
        els.nfcHint.textContent =
          "NFC unavailable: " + (err && err.message ? err.message : String(err));
      }
      if (els.nfcAdd) els.nfcAdd.textContent = "nfc";
      setDrawer(true);
    }
  }

  function parseNdef(message, serialNumber) {
    let text = "";
    let url = "";
    try {
      for (const rec of message.records || []) {
        if (rec.recordType === "text") {
          const dec = new TextDecoder(rec.encoding || "utf-8");
          text += dec.decode(rec.data).replace(/^[a-z]{2,3}\|?/, "") + " ";
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

  function addFromNfc({ id, nick }) {
    const existing = peers.find((p) => p.id === id || (p.nfc && p.nick === nick));
    if (existing) {
      existing.on = true;
      existing.nfc = true;
      if (els.nfcHint) els.nfcHint.textContent = "NFC: re-enabled " + existing.nick;
    } else {
      const dir = DIR_ORDER[peers.filter((p) => !p.self).length % 4];
      peers.push({ id, nick, dir, on: true, seed: hashStr(id), nfc: true });
      if (els.nfcHint) els.nfcHint.textContent = "NFC: added " + nick;
    }
    saveState();
    layoutAndPaint();
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
    if (els.drawerToggle) {
      els.drawerToggle.setAttribute("aria-expanded", drawerOpen ? "true" : "false");
    }
    if (els.drawerScrim) {
      els.drawerScrim.hidden = !drawerOpen;
    }
    document.body.classList.toggle("gg-drawer-open", drawerOpen);
  }

  function toggleDrawer() {
    setDrawer(!drawerOpen);
  }

  // ── animation ────────────────────────────────────────────
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
    if (els.meshToggle) {
      els.meshToggle.addEventListener("click", () => {
        meshOn = !meshOn;
        saveState();
        if (els.meshLabel) els.meshLabel.textContent = meshOn ? "mesh" : "off";
        els.meshToggle.setAttribute("aria-pressed", meshOn ? "true" : "false");
        drawMesh();
      });
    }
    if (els.nfcAdd) els.nfcAdd.addEventListener("click", startNfcAdd);

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
    if (els.drawerScrim) {
      els.drawerScrim.addEventListener("click", () => setDrawer(false));
    }

    // swipe down on handle to close / up on footer to open
    let touchY0 = null;
    const swipeTarget = els.drawerHandle || els.drawer;
    if (swipeTarget) {
      swipeTarget.addEventListener(
        "touchstart",
        (e) => {
          touchY0 = e.changedTouches[0].clientY;
        },
        { passive: true }
      );
      swipeTarget.addEventListener(
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

    document.addEventListener("keydown", (e) => {
      if (e.key === "Escape" && drawerOpen) setDrawer(false);
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
        if (selfTile) selfTile.textContent = els.nick.value.trim() || "you";
      });
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
        "NFC API not in this browser — use + / compass. Delete via × overlay or roster.";
    }

    let resizeT = 0;
    const onResize = () => {
      clearTimeout(resizeT);
      resizeT = setTimeout(layoutAndPaint, 60);
    };
    window.addEventListener("resize", onResize);
    if (window.visualViewport) window.visualViewport.addEventListener("resize", onResize);

    // re-layout after fonts / first paint
    if (document.fonts && document.fonts.ready) {
      document.fonts.ready.then(() => layoutAndPaint());
    }
  }

  // boot
  initPeers();
  wire();
  setDrawer(false);
  layoutAndPaint();
  raf = requestAnimationFrame(tick);
  registerSW();
})();
