/**
 * GrokGlyph PWA — full-page multi-user 25×25 luminance video cast.
 * Side-by-side 1px stack · cam/file → glyph · cast via hub vburst-frame · RX peers.
 */
(function () {
  "use strict";

  let glyphN = 25; // 13|25|37|49
  const GAP = 1;
  const CAST_MS = 110; // ~9 fps cast (glyph grid is tiny)
  const RX_STALE_MS = 2500;
  const STORAGE_KEY = "grokglyph.v5";
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
    screenBtn: document.getElementById("gg-screen"),
    screenOpen: document.getElementById("gg-screen-open"),
    screenFs: document.getElementById("gg-screen-fs"),
    screenLayout: document.getElementById("gg-screen-layout"),
    screenLed: document.getElementById("gg-screen-led"),
    screenStatus: document.getElementById("gg-screen-status"),
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
    captionBar: document.getElementById("gg-caption-bar"),
    captionMeta: document.getElementById("gg-caption-meta"),
    captionText: document.getElementById("gg-caption-text"),
    clearRoom: document.getElementById("gg-clear-room"),
    clearRoomDrawer: document.getElementById("gg-clear-room-drawer"),
    roomId: document.getElementById("gg-room-id"),
    roomLabel: document.getElementById("gg-room-label"),
    regionList: document.getElementById("gg-region-list"),
    regionHint: document.getElementById("gg-region-hint"),
    geoStatus: document.getElementById("gg-geo-status"),
    geoLat: document.getElementById("gg-geo-lat"),
    geoLon: document.getElementById("gg-geo-lon"),
    geoAcc: document.getElementById("gg-geo-acc"),
    geoHeadingEl: document.getElementById("gg-geo-heading"),
    geoNearest: document.getElementById("gg-geo-nearest"),
    geoFacing: document.getElementById("gg-geo-facing"),
    geoLocate: document.getElementById("gg-geo-locate"),
    gyroToggle: document.getElementById("gg-gyro-toggle"),
    geoAutoJoin: document.getElementById("gg-geo-autojoin"),
    styleHint: document.getElementById("gg-style-hint"),
    socialQ: document.getElementById("gg-social-q"),
    socialGo: document.getElementById("gg-social-go"),
    socialStack: document.getElementById("gg-social-stack"),
    socialStatus: document.getElementById("gg-social-status"),
    socialLazy: document.getElementById("gg-social-lazy"),
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
  /** @type {string} matrix|blocks|braille|ascii */
  let renderStyle = "matrix";
  /** @type {string} mesh room id */
  let meshRoom = "global";
  /** @type {{lat:number,lon:number,acc:number}|null} */
  let geoFix = null;
  /** @type {number|null} compass heading degrees */
  let geoHeading = null;
  let gyroOn = false;
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
  /** @type {Window|null} */
  let screenWin = null;
  /** @type {BroadcastChannel|null} */
  let screenBC = null;
  /** @type {any} PresentationConnection */
  let screenPres = null;
  let screenOn = false;
  let screenLayout = "grid";
  let screenLed = "auto";
  let lastScreenPush = 0;
  /** mobile double-stack GrokGlyph scale (portrait 1:2) */
  let mobileStack = false;
  /** @type {Array<{url:string,title:string,kind:string,platform?:string,mobile?:boolean}>} */
  let socialLazyQueue = [];
  let socialLazyTimer = 0;
  /** @type {MediaStream | null} primary stream (compat) */
  let mediaStream = null;
  /** BitChat dual-path helper (BLE/Nostr via hub bridge) */
  let bitchat = null;
  /**
   * All active camera lanes (phone front + back + ultra-wide, etc.)
   * @type {Array<{
   *   deviceId: string,
   *   label: string,
   *   stream: MediaStream,
   *   video: HTMLVideoElement,
   *   peerId: string,
   *   short: string
   * }>}
   */
  let camLanes = [];
  /** @type {WebSocket | null} */
  let ws = null;
  /** @type {Map<string, HTMLCanvasElement>} */
  const canvasById = new Map();
  const lumBuf = new Map();
  /** @type {BeforeInstallPromptEvent | null} */
  let deferredInstall = null;
  // jpeg encode helper (reuse)
  const jpegCanvas = document.createElement("canvas");
  jpegCanvas.width = Math.max(100, glyphN * 4);
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
          meshRoom,
          glyphN,
          renderStyle,
          peers: peers.map((p) => ({
            id: p.id,
            nick: p.nick,
            dir: p.dir,
            on: p.on,
            seed: p.seed,
            self: !!p.self,
            nfc: !!p.nfc,
            room: p.room || meshRoom,
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
      room: meshRoom,
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
      room: meshRoom,
    };
  }

  function initPeers() {
    const st = loadState();
    if (st && Array.isArray(st.peers) && st.peers.length) {
      peers = st.peers.map((p) => ({ ...p, source: p.self ? "sim" : p.source || "sim" }));
      meshOn = st.meshOn !== false;
      if (st.nick && els.nick) els.nick.value = st.nick;
      if (st.hubUrl && els.hubUrl) els.hubUrl.value = st.hubUrl;
      if (st.meshRoom) meshRoom = st.meshRoom;
      if (st.glyphN) glyphN = [13, 25, 37, 49].includes(st.glyphN) ? st.glyphN : 25;
      if (st.renderStyle) renderStyle = st.renderStyle;
      if (!peers.some((p) => p.self || p.id === "self")) peers.unshift(defaultSelf());
    } else {
      peers = [defaultSelf()];
      if (els.hubUrl && !els.hubUrl.value) {
        els.hubUrl.value = defaultHubURL();
      }
    }
    applyNick();
    if (els.hubUrl && !els.hubUrl.value) els.hubUrl.value = defaultHubURL();
    if (els.roomId) els.roomId.value = meshRoom;
    if (els.roomLabel) els.roomLabel.textContent = meshRoom;
    syncSampleCanvas();
    document.querySelectorAll(".gg-chip[data-res]").forEach((b) => {
      b.classList.toggle("is-on", parseInt(b.getAttribute("data-res"), 10) === glyphN);
    });
    document.querySelectorAll(".gg-chip[data-style]").forEach((b) => {
      b.classList.toggle("is-on", b.getAttribute("data-style") === renderStyle);
    });
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
   * Sample a video/image element into out (length glyphN*glyphN). Returns false if not ready.
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
    // mobile double-stack: center-crop portrait (9:16-ish) into glyph face
    let sx = 0,
      sy = 0,
      sw = 0,
      sh = 0;
    try {
      // @ts-ignore
      const vw = src.videoWidth || src.naturalWidth || src.width || 0;
      // @ts-ignore
      const vh = src.videoHeight || src.naturalHeight || src.height || 0;
      if (vw > 0 && vh > 0) {
        if (mobileStack || vh > vw * 1.15) {
          // portrait source — crop center square from mid-upper (faces) or full double-stack mid
          const side = Math.min(vw, Math.floor(vh / (mobileStack ? 2 : 1)));
          const sideSq = Math.min(vw, vh);
          const use = mobileStack ? Math.min(vw, Math.floor(vh * 0.5)) : sideSq;
          sw = use;
          sh = use;
          sx = Math.floor((vw - sw) / 2);
          sy = mobileStack ? Math.floor(vh * 0.12) : Math.floor((vh - sh) / 2);
        } else {
          // landscape — center square
          const side = Math.min(vw, vh);
          sw = side;
          sh = side;
          sx = Math.floor((vw - side) / 2);
          sy = Math.floor((vh - side) / 2);
        }
      }
    } catch {
      /* draw full */
    }
    if (sw > 0 && sh > 0) {
      sampleCtx.drawImage(src, sx, sy, sw, sh, 0, 0, glyphN, glyphN);
    } else {
      sampleCtx.drawImage(src, 0, 0, glyphN, glyphN);
    }
    let img;
    try {
      img = sampleCtx.getImageData(0, 0, glyphN, glyphN);
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

  function setMobileStack(on) {
    mobileStack = !!on;
    document.body.classList.toggle("is-mobile-stack", mobileStack);
    if (els.socialStack) {
      els.socialStack.setAttribute("aria-pressed", mobileStack ? "true" : "false");
      els.socialStack.classList.toggle("is-on", mobileStack);
    }
    computeLayout();
    layoutAndPaint();
    saveState();
  }

  /** Hub WS URL → HTTP origin for /api/social */
  function hubHttpBase() {
    const raw = (els.hubUrl && els.hubUrl.value.trim()) || defaultHubURL();
    try {
      const u = new URL(raw.replace(/^ws/i, (m) => (m.toLowerCase() === "wss" ? "https" : "http")));
      return u.origin;
    } catch {
      return "http://127.0.0.1:9876";
    }
  }

  function setSocialStatus(t) {
    if (els.socialStatus) els.socialStatus.textContent = t || "";
  }

  async function loadSocialHandle() {
    const q = (els.socialQ && els.socialQ.value.trim()) || "";
    if (!q) {
      setSocialStatus("enter @user · twitch:name · yt:@ch");
      return;
    }
    setSocialStatus("resolving live first…");
    try {
      const url = hubHttpBase() + "/api/social?q=" + encodeURIComponent(q);
      const res = await fetch(url, { headers: { Accept: "application/json" } });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setSocialStatus((data && data.error) || "resolve failed · is hub + yt-dlp up?");
        return;
      }
      const live = data.live ? "LIVE" : "vod";
      const plat = data.platform || "social";
      const handle = data.handle ? "@" + data.handle : q;
      setSocialStatus(plat + "/" + handle + " · " + live + " · " + (data.title || "").slice(0, 36));
      if (data.mobile || (data.stack && data.stack.mode === "double")) {
        setMobileStack(true);
      }
      // try primary stream into file video (CORS may block CDN — still attempt)
      if (data.video && els.fileVideo) {
        try {
          stopCamTracks();
          camOn = false;
          els.fileVideo.src = data.video;
          els.fileVideo.load();
          await els.fileVideo.play().catch(() => {});
          fileOn = true;
          const self = peers.find((p) => p.self);
          if (self) self.source = "file";
          setCastLabel("social");
        } catch (e) {
          setSocialStatus((els.socialStatus.textContent || "") + " · play blocked (CORS) — use gy /social");
        }
      }
      // lazy secondary: stagger into peer slots + list UI
      socialLazyQueue = Array.isArray(data.lazy) ? data.lazy.slice() : [];
      renderSocialLazyList();
      scheduleSocialLazy();
    } catch (e) {
      setSocialStatus("hub unreachable · gy serve · " + (e && e.message ? e.message : e));
    }
  }

  function renderSocialLazyList() {
    if (!els.socialLazy) return;
    els.socialLazy.innerHTML = "";
    if (!socialLazyQueue.length) {
      els.socialLazy.hidden = true;
      return;
    }
    els.socialLazy.hidden = false;
    socialLazyQueue.forEach((item, i) => {
      const li = document.createElement("li");
      if (item.kind === "live") li.classList.add("is-live");
      const kind = document.createElement("span");
      kind.className = "gg-lazy-kind" + (item.kind === "live" ? " live" : "");
      kind.textContent = (item.kind || "vod").slice(0, 5);
      const title = document.createElement("span");
      title.textContent = (item.title || item.url || "item").slice(0, 42);
      title.title = item.url || "";
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "btn ghost";
      btn.textContent = "open";
      btn.addEventListener("click", () => {
        if (item.url) window.open(item.url, "_blank", "noopener");
      });
      li.appendChild(kind);
      li.appendChild(title);
      li.appendChild(btn);
      els.socialLazy.appendChild(li);
      void i;
    });
  }

  function scheduleSocialLazy() {
    if (socialLazyTimer) {
      clearTimeout(socialLazyTimer);
      socialLazyTimer = 0;
    }
    if (!socialLazyQueue.length) return;
    let idx = 0;
    const step = () => {
      if (idx >= socialLazyQueue.length) {
        setSocialStatus((els.socialStatus && els.socialStatus.textContent) || "lazy done");
        return;
      }
      const item = socialLazyQueue[idx++];
      // soft-add peer slot labeled with content (lazy — no heavy decode)
      const nick = (item.title || item.kind || "lazy").slice(0, 12);
      const p = {
        id: "social-" + Date.now() + "-" + idx,
        nick: nick,
        dir: DIR_ORDER[idx % 4],
        on: true,
        seed: (Math.random() * 1e9) | 0,
        source: "sim",
        socialUrl: item.url,
        socialKind: item.kind,
      };
      peers.push(p);
      ensureLum(p.id);
      layoutAndPaint();
      setSocialStatus("lazy " + idx + "/" + socialLazyQueue.length + " · " + nick);
      socialLazyTimer = window.setTimeout(step, 2800);
    };
    socialLazyTimer = window.setTimeout(step, 1200);
  }

  function stopCamTracks() {
    if (mediaStream) {
      mediaStream.getTracks().forEach((t) => t.stop());
      mediaStream = null;
    }
    if (els.localVideo) els.localVideo.srcObject = null;
  }

  function ensureLum(id) {
    let buf = lumBuf.get(id);
    const need = glyphN * glyphN;
    if (!buf || buf.length !== need) {
      buf = new Uint8Array(need);
      lumBuf.set(id, buf);
    }
    return buf;
  }

  function syncSampleCanvas() {
    if (!els.sample) return;
    if (els.sample.width !== glyphN) els.sample.width = glyphN;
    if (els.sample.height !== glyphN) els.sample.height = glyphN;
  }

  function setGlyphResolution(n) {
    n = parseInt(n, 10);
    if (![13, 25, 37, 49].includes(n)) n = 25;
    if (n === glyphN) return;
    glyphN = n;
    lumBuf.clear();
    syncSampleCanvas();
    document.querySelectorAll(".gg-chip[data-res]").forEach((b) => {
      b.classList.toggle("is-on", parseInt(b.getAttribute("data-res"), 10) === glyphN);
    });
    saveState();
    layoutAndPaint();
    if (screenOn) pushScreenCast(true);
  }

  function setRenderStyle(st) {
    const ok = ["matrix", "blocks", "braille", "ascii"];
    if (!ok.includes(st)) st = "matrix";
    renderStyle = st;
    document.querySelectorAll(".gg-chip[data-style]").forEach((b) => {
      b.classList.toggle("is-on", b.getAttribute("data-style") === renderStyle);
    });
    if (els.styleHint) {
      const hints = {
        matrix: "LED luminance grid (default Glyph look)",
        blocks: "chunky block LEDs with gutters",
        braille: "Unicode braille 2×4 luminance",
        ascii: "ASCII ramp characters",
      };
      els.styleHint.textContent = hints[renderStyle] || "";
    }
    saveState();
    layoutAndPaint();
    if (screenOn) pushScreenCast(true);
  }

  function clearRoom(confirmFirst) {
    if (confirmFirst && peers.filter((p) => !p.self).length > 0) {
      if (!window.confirm("Clear room? Remove all peers (keep you).")) return;
    }
    peers = [defaultSelf()];
    applyNick();
    if (peers[0]) peers[0].room = meshRoom;
    focusPeerId = null;
    lumBuf.clear();
    canvasById.clear();
    hideCaptionBar();
    saveState();
    setCastLabel("room cleared");
    layoutAndPaint();
  }

  function setMeshRoom(id, opts) {
    opts = opts || {};
    id = String(id || "global").trim().toLowerCase().replace(/[^a-z0-9._-]+/g, "-") || "global";
    const prev = meshRoom;
    meshRoom = id;
    if (els.roomId) els.roomId.value = meshRoom;
    if (els.roomLabel) els.roomLabel.textContent = meshRoom;
    const self = peers.find((p) => p.self);
    if (self) self.room = meshRoom;
    // drop peers from other rooms
    peers = peers.filter((p) => p.self || !p.room || p.room === meshRoom);
    saveState();
    if (els.regionHint) els.regionHint.textContent = "Room · " + meshRoom;
    if (prev !== meshRoom && ws && ws.readyState === WebSocket.OPEN) {
      sendJSON({
        type: "join",
        nick: myNick(),
        role: "grokglyph",
        room: meshRoom,
        geo: geoFix ? { lat: geoFix.lat, lon: geoFix.lon, heading: geoHeading } : null,
        cap: {
          class: "term-lean",
          role: "peer",
          lanes: ["glyph", "hex", "chat", "gyst"],
          glyph_n: glyphN,
          forge: false,
        },
      });
      if (opts.reconnect) {
        // soft rejoin label
        setCastLabel("room " + meshRoom);
      }
    }
    layoutAndPaint();
  }


  function fillSimLuminance(out, seed, t, isSelf) {
    const cx = (glyphN - 1) / 2,
      cy = (glyphN - 1) / 2;
    const rad = glyphN * 0.52;
    const pulse = 0.55 + 0.45 * Math.sin(t * (isSelf ? 2.1 : 1.4) + (seed % 7) * 0.3);
    for (let y = 0; y < glyphN; y++) {
      for (let x = 0; x < glyphN; x++) {
        const dx = x - cx;
        const dy = y - cy;
        const r = Math.sqrt(dx * dx + dy * dy);
        let v = Math.max(0, 1 - r / rad);
        v = v * v;
        const h = hash2(x + seed * 3, y + seed * 7);
        v = v * (0.72 + 0.28 * h) * pulse;
        if (isSelf) {
          const ring = Math.abs(r - glyphN * 0.32 - 2 * Math.sin(t * 1.5));
          if (ring < 1.2) v = Math.min(1, v + 0.35 * (1 - ring / 1.2));
        } else {
          v *= 0.55 + 0.45 * Math.sin((x + y) * 0.35 + t + seed * 0.01);
        }
        out[y * glyphN + x] = Math.max(0, Math.min(255, (v * 255) | 0));
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
    const n = glyphN;
    const style = renderStyle || "matrix";
    // high-res display canvas for text styles
    const scale = style === "matrix" ? 1 : Math.max(4, Math.min(12, Math.floor(96 / n) || 4));
    const W = n * scale;
    const H = n * scale;
    if (canvas.width !== W) canvas.width = W;
    if (canvas.height !== H) canvas.height = H;
    ctx.imageSmoothingEnabled = false;

    function tint(L) {
      if (mode === "cast") return [Math.min(255, L), (L * 0.55) | 0, (L * 0.5) | 0];
      if (mode === "rx") return [(L * 0.4) | 0, (L * 0.85) | 0, Math.min(255, (L * 1.05) | 0)];
      if (mode === "self") return [(L * 0.55) | 0, (L * 0.95) | 0, Math.min(255, L)];
      return [(L * 0.35) | 0, (L * 0.85) | 0, (L * 0.95) | 0];
    }

    if (style === "ascii" || style === "braille") {
      ctx.fillStyle = "#050508";
      ctx.fillRect(0, 0, W, H);
      if (style === "ascii") {
        const ramp = " .'`^\",:;Il!i><~+_-?][}{1)(|\\/tfjrxnuvczXYUJCLQ0OZmwqpdbkhao*#MW&8%B@$";
        ctx.font = `bold ${Math.max(4, scale)}px "IBM Plex Mono", ui-monospace, monospace`;
        ctx.textBaseline = "top";
        for (let y = 0; y < n; y++) {
          for (let x = 0; x < n; x++) {
            const L = data[y * n + x] || 0;
            const idx = Math.min(ramp.length - 1, (L / 255) * (ramp.length - 1) | 0);
            const [r, g, b] = tint(L);
            ctx.fillStyle = `rgb(${r},${g},${b})`;
            ctx.fillText(ramp[idx], x * scale, y * scale);
          }
        }
        return;
      }
      // braille: 2x4 cells → U+2800
      ctx.font = `${Math.max(6, scale * 2)}px "IBM Plex Mono", ui-monospace, monospace`;
      ctx.textBaseline = "top";
      const thr = 96;
      for (let y = 0; y < n; y += 4) {
        for (let x = 0; x < n; x += 2) {
          let bits = 0;
          let sum = 0;
          let cnt = 0;
          // standard braille dot order
          const dots = [
            [0, 0, 0x01], [1, 0, 0x08],
            [0, 1, 0x02], [1, 1, 0x10],
            [0, 2, 0x04], [1, 2, 0x20],
            [0, 3, 0x40], [1, 3, 0x80],
          ];
          for (const [dx, dy, bit] of dots) {
            const xx = x + dx;
            const yy = y + dy;
            if (xx >= n || yy >= n) continue;
            const L = data[yy * n + xx] || 0;
            sum += L;
            cnt++;
            if (L >= thr) bits |= bit;
          }
          const Lavg = cnt ? (sum / cnt) | 0 : 0;
          const [r, g, b] = tint(Lavg);
          ctx.fillStyle = `rgb(${r},${g},${b})`;
          ctx.fillText(String.fromCharCode(0x2800 + bits), x * scale, y * scale);
        }
      }
      return;
    }

    if (style === "blocks") {
      ctx.fillStyle = "#050508";
      ctx.fillRect(0, 0, W, H);
      const gap = Math.max(1, (scale * 0.15) | 0);
      const cell = Math.max(1, scale - gap);
      for (let y = 0; y < n; y++) {
        for (let x = 0; x < n; x++) {
          const L = data[y * n + x] || 0;
          const [r, g, b] = tint(L);
          ctx.fillStyle = `rgb(${r},${g},${b})`;
          ctx.fillRect(x * scale, y * scale, cell, cell);
        }
      }
      return;
    }

    // matrix (1:1 LED pixels)
    if (canvas.width !== n) canvas.width = n;
    if (canvas.height !== n) canvas.height = n;
    const img = ctx.createImageData(n, n);
    const d = img.data;
    for (let i = 0; i < n * n; i++) {
      const L = data[i] || 0;
      const [r, g, b] = tint(L);
      const o = i * 4;
      d[o] = r;
      d[o + 1] = g;
      d[o + 2] = b;
      d[o + 3] = 255;
    }
    ctx.putImageData(img, 0, 0);
  }

  // ── camera / file (multi-cam: all phone lenses simultaneously) ──

  function shortCamLabel(label, index, deviceId) {
    const L = String(label || "").toLowerCase();
    if (/ultra|wide/.test(L) && !/tele/.test(L)) return "uw";
    if (/tele|zoom/.test(L)) return "tele";
    if (/back|rear|environment|world/.test(L)) return "back";
    if (/front|user|face|selfie/.test(L)) return "front";
    if (label && label !== "camera" && label.length < 14) {
      return label.replace(/\s+/g, "-").slice(0, 10);
    }
    return "cam" + (index + 1);
  }

  function stopCamLanes() {
    camLanes.forEach((lane) => {
      try {
        lane.stream.getTracks().forEach((t) => t.stop());
      } catch {
        /* ignore */
      }
      try {
        lane.video.pause();
        lane.video.srcObject = null;
      } catch {
        /* ignore */
      }
      // remove extra self-cam peers (keep primary self)
      if (lane.peerId && lane.peerId !== "self") {
        peers = peers.filter((p) => p.id !== lane.peerId);
        canvasById.delete(lane.peerId);
        lumBuf.delete(lane.peerId);
      }
    });
    camLanes = [];
  }

  function ensureCamPeer(lane, index) {
    if (index === 0) {
      // primary self tile
      let self = peers.find((p) => p.self || p.id === "self");
      if (!self) {
        self = defaultSelf();
        peers.unshift(self);
      }
      self.source = "cam";
      self.nick = myNick();
      self.camLane = 0;
      self.selfCam = true;
      lane.peerId = self.id;
      return self;
    }
    const id = "cam-" + (lane.deviceId || String(index)).replace(/\W/g, "").slice(0, 12);
    let p = peers.find((x) => x.id === id);
    if (!p) {
      p = {
        id: id,
        nick: myNick() + "-" + lane.short,
        dir: DIR_ORDER[index % DIR_ORDER.length],
        on: true,
        seed: hashStr(id) % 1e9,
        self: false,
        selfCam: true,
        source: "cam",
        room: meshRoom,
        camLane: index,
      };
      peers.push(p);
    } else {
      p.source = "cam";
      p.selfCam = true;
      p.nick = myNick() + "-" + lane.short;
      p.on = true;
    }
    lane.peerId = p.id;
    return p;
  }

  async function openDeviceStream(deviceId) {
    const tries = [
      {
        video: {
          deviceId: deviceId ? { exact: deviceId } : undefined,
          width: { ideal: 640 },
          height: { ideal: 480 },
        },
        audio: false,
      },
      { video: deviceId ? { deviceId: { exact: deviceId } } : true, audio: false },
      { video: true, audio: false },
    ];
    let last = null;
    for (let i = 0; i < tries.length; i++) {
      try {
        // strip undefined deviceId
        const c = tries[i];
        if (c.video && c.video.deviceId === undefined) c.video = true;
        return await navigator.mediaDevices.getUserMedia(c);
      } catch (e) {
        last = e;
        if (e && (e.name === "NotAllowedError" || e.name === "SecurityError")) throw e;
      }
    }
    throw last || new Error("getUserMedia failed");
  }

  /**
   * Open every videoinput on the phone at once (front + back + extras).
   * Each lens becomes a glyph tile; cast ships all lanes to the mesh / sphere.
   */
  async function enableCam(opts) {
    opts = opts || {};
    const allCams = opts.all !== false; // default: all cameras simultaneously
    if (camOn && camLanes.length && !opts.force) return true;
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      setCastLabel("no cam API");
      return false;
    }
    // Secure context tip (https GH Pages is fine; http LAN is not)
    try {
      if (!window.isSecureContext) {
        setCastLabel("need https/localhost");
        if (els.hubHint) {
          els.hubHint.textContent =
            "Camera needs HTTPS or localhost. Use https://fornevercollective.github.io/GrokYtalkY/grokglyph.html";
        }
        return false;
      }
    } catch {
      /* ignore */
    }

    try {
      // Permission probe (also unlocks device labels on iOS/Android)
      let probe = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: "user" },
        audio: false,
      });
      let devices = [];
      try {
        devices = await navigator.mediaDevices.enumerateDevices();
      } catch {
        devices = [];
      }
      const videoInputs = devices.filter((d) => d.kind === "videoinput");
      // stop probe before opening named devices (some phones allow only one open at a time otherwise)
      probe.getTracks().forEach((t) => t.stop());
      probe = null;

      stopCamLanes();
      if (els.localVideo) els.localVideo.srcObject = null;

      const list =
        allCams && videoInputs.length
          ? videoInputs
          : videoInputs.length
            ? [videoInputs[0]]
            : [{ deviceId: "", label: "camera" }];

      // Prefer opening user then environment if no labels yet
      const ordered = list.slice().sort((a, b) => {
        const la = (a.label || "").toLowerCase();
        const lb = (b.label || "").toLowerCase();
        const score = (L) =>
          /front|user|face/.test(L) ? 0 : /back|rear|environment/.test(L) ? 1 : 2;
        return score(la) - score(lb);
      });

      for (let i = 0; i < ordered.length; i++) {
        const d = ordered[i];
        try {
          const stream = await openDeviceStream(d.deviceId);
          const video = document.createElement("video");
          video.setAttribute("playsinline", "");
          video.playsInline = true;
          video.muted = true;
          video.autoplay = true;
          video.srcObject = stream;
          await video.play().catch(() => {});
          const short = shortCamLabel(d.label, i, d.deviceId);
          const lane = {
            deviceId: d.deviceId || "default-" + i,
            label: d.label || short,
            stream: stream,
            video: video,
            peerId: "",
            short: short,
          };
          ensureCamPeer(lane, i);
          camLanes.push(lane);
          if (i === 0) {
            mediaStream = stream;
            if (els.localVideo) {
              els.localVideo.srcObject = stream;
              els.localVideo.muted = true;
              els.localVideo.playsInline = true;
              await els.localVideo.play().catch(() => {});
            }
          }
        } catch (e) {
          console.warn("[grokglyph] cam lane skip", d.label || d.deviceId, e);
        }
      }

      if (!camLanes.length) {
        setCastLabel("cam failed");
        return false;
      }

      camOn = true;
      fileOn = false;
      if (els.camBtn) {
        els.camBtn.setAttribute("aria-pressed", "true");
        els.camBtn.textContent = camLanes.length > 1 ? "cams " + camLanes.length : "cam";
      }
      setCastLabel(
        casting
          ? "casting " + camLanes.length + " cam"
          : camLanes.length > 1
            ? camLanes.length + " cams live"
            : "cam live"
      );
      if (els.hubHint) {
        els.hubHint.textContent =
          camLanes.length > 1
            ? "All cameras open (" +
              camLanes.map((l) => l.short).join(" · ") +
              "). Cast ships each lens as a glyph lane."
            : "Cam live. Cast → mesh / sphere. Tip: multi-lens phones open all cameras at once.";
      }
      layoutAndPaint();
      return true;
    } catch (err) {
      camOn = false;
      stopCamLanes();
      setCastLabel("cam blocked");
      if (els.hubHint) {
        const n = err && err.name;
        els.hubHint.textContent =
          n === "NotAllowedError"
            ? "Camera permission denied — 🔒 in address bar → Allow, then cam again. Or use a video file."
            : "Camera blocked — allow permission or use a video file. " +
              (err && err.message ? err.message : "");
      }
      return false;
    }
  }

  function stopCam() {
    stopCamLanes();
    if (mediaStream) {
      try {
        mediaStream.getTracks().forEach((t) => t.stop());
      } catch {
        /* ignore */
      }
      mediaStream = null;
    }
    if (els.localVideo) els.localVideo.srcObject = null;
    camOn = false;
    if (els.camBtn) {
      els.camBtn.setAttribute("aria-pressed", "false");
      els.camBtn.textContent = "cam";
    }
    peers.forEach((p) => {
      if (p.selfCam || p.source === "cam") {
        if (p.self || p.id === "self") {
          p.source = fileOn ? "file" : "sim";
          p.selfCam = false;
        }
      }
    });
    // drop non-primary cam peers
    peers = peers.filter((p) => p.self || p.id === "self" || !String(p.id).startsWith("cam-"));
    layoutAndPaint();
  }

  async function toggleCam() {
    if (camOn) {
      if (casting) await stopCast();
      stopCam();
      setCastLabel("idle");
      return;
    }
    await enableCam({ all: true });
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

  function setScreenStatus(t) {
    if (els.screenStatus) els.screenStatus.textContent = t;
  }

  function lumToB64(lum) {
    try {
      let s = "";
      const n = lum.length;
      for (let i = 0; i < n; i++) s += String.fromCharCode(lum[i] & 255);
      return btoa(s);
    } catch {
      return "";
    }
  }

  function ensureScreenChannel() {
    if (screenBC) return screenBC;
    try {
      screenBC = new BroadcastChannel("gy-glyph-cast");
      screenBC.onmessage = (ev) => {
        if (ev.data && ev.data.type === "glyph-cast-ready") {
          setScreenStatus("screen player ready · streaming " + glyphN + "²");
          pushScreenCast(true);
        }
      };
    } catch {
      screenBC = null;
    }
    return screenBC;
  }

  function screenPlayerURL(fs) {
    const u = new URL("glyph-cast.html", window.location.href);
    if (fs) {
      u.searchParams.set("fs", "1");
      u.searchParams.set("cast", "1");
    }
    return u.href;
  }

  function openScreenPlayer(opts) {
    opts = opts || {};
    ensureScreenChannel();
    // Presentation API (Chromecast / second display) when available
    if (opts.presentation && "PresentationRequest" in window) {
      try {
        const req = new PresentationRequest([screenPlayerURL(true)]);
        req.start()
          .then((conn) => {
            screenPres = conn;
            screenOn = true;
            if (els.screenBtn) els.screenBtn.setAttribute("aria-pressed", "true");
            setScreenStatus("presentation · full LED scale");
            setCastLabel("screen on");
            conn.addEventListener("close", () => {
              screenPres = null;
              if (!screenWin || screenWin.closed) {
                screenOn = false;
                if (els.screenBtn) els.screenBtn.setAttribute("aria-pressed", "false");
                setScreenStatus("screen closed");
              }
            });
            conn.addEventListener("message", (e) => {
              try {
                const m = typeof e.data === "string" ? JSON.parse(e.data) : e.data;
                if (m && m.type === "glyph-cast-ready") pushScreenCast(true);
              } catch (_) {}
            });
            pushScreenCast(true);
          })
          .catch(() => openScreenPopup(opts.fullscreen));
        return;
      } catch (_) {
        /* fall through */
      }
    }
    openScreenPopup(opts.fullscreen);
  }

  function openScreenPopup(fullscreen) {
    ensureScreenChannel();
    const url = screenPlayerURL(!!fullscreen);
    if (screenWin && !screenWin.closed) {
      try {
        screenWin.focus();
      } catch (_) {}
    } else {
      screenWin = window.open(
        url,
        "gy-glyph-cast",
        "popup=yes,width=1280,height=800,menubar=no,toolbar=no,location=no,status=no"
      );
    }
    screenOn = true;
    if (els.screenBtn) els.screenBtn.setAttribute("aria-pressed", "true");
    setScreenStatus(
      (fullscreen ? "fullscreen player" : "screen player") +
        " · " +
        glyphN +
        "×" +
        glyphN +
        " · LED " +
        screenLed
    );
    setCastLabel("screen on");
    // kick first frames after window loads
    setTimeout(() => pushScreenCast(true), 300);
    setTimeout(() => pushScreenCast(true), 800);
  }

  function toggleScreenCast() {
    if (screenOn && ((screenWin && !screenWin.closed) || screenPres)) {
      closeScreenCast();
      return;
    }
    openScreenPlayer({ fullscreen: false, presentation: true });
  }

  function closeScreenCast() {
    screenOn = false;
    if (els.screenBtn) els.screenBtn.setAttribute("aria-pressed", "false");
    if (screenWin && !screenWin.closed) {
      try {
        screenWin.close();
      } catch (_) {}
    }
    screenWin = null;
    if (screenPres) {
      try {
        screenPres.terminate && screenPres.terminate();
        screenPres.close && screenPres.close();
      } catch (_) {}
      screenPres = null;
    }
    setScreenStatus("screen idle · press p or Screen");
    setCastLabel(
      casting ? "casting" : ws && ws.readyState === WebSocket.OPEN ? "hub on" : "idle"
    );
  }

  function buildScreenPayload() {
    if (els.screenLayout) screenLayout = els.screenLayout.value || "grid";
    if (els.screenLed) screenLed = els.screenLed.value || "auto";
    const list = stageOrder().filter((p) => isDrawn(p));
    const outPeers = [];
    for (const p of list) {
      const buf = ensureLum(p.id);
      // ensure latest self sample already in buf from tick; for others use lum
      let lum = buf;
      if (!p.self && p.lum && p.lum.length >= glyphN * glyphN) {
        lum = p.lum;
      }
      let mode = "sim";
      if (p.self) mode = casting ? "cast" : camOn || fileOn ? "self" : "sim";
      else if (isLivePeer(p)) mode = "rx";
      outPeers.push({
        id: p.id,
        nick: p.nick,
        mode: mode,
        self: !!p.self,
        casting: !!(p.self && casting),
        glyphN: glyphN,
        lumB64: lumToB64(lum),
      });
    }
    return {
      type: "glyph-cast",
      glyphN: glyphN,
      style: renderStyle === "ascii" || renderStyle === "braille" ? "matrix" : renderStyle,
      layout: screenLayout,
      ledPx: screenLed,
      peers: outPeers,
      room: meshRoom,
      t: Date.now(),
    };
  }

  function pushScreenCast(force) {
    if (!screenOn && !force) return;
    // still open?
    if (screenWin && screenWin.closed && !screenPres) {
      closeScreenCast();
      return;
    }
    const now = performance.now();
    if (!force && now - lastScreenPush < 90) return;
    lastScreenPush = now;
    const msg = buildScreenPayload();
    try {
      if (screenBC) screenBC.postMessage(msg);
    } catch (_) {}
    try {
      if (screenWin && !screenWin.closed) screenWin.postMessage(msg, "*");
    } catch (_) {}
    try {
      if (screenPres && screenPres.state === "connected") {
        screenPres.send(JSON.stringify(msg));
      }
    } catch (_) {}
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
    const room = meshRoom || "global";
    if (!url.includes("nick=")) {
      url += (url.includes("?") ? "&" : "?") + "role=peer&nick=" + encodeURIComponent(nick);
    }
    if (!url.includes("room=")) {
      url += (url.includes("?") ? "&" : "?") + "room=" + encodeURIComponent(room);
    } else {
      // refresh room= param
      url = url.replace(/([?&])room=[^&]*/, "$1room=" + encodeURIComponent(room));
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
        room: meshRoom,
        geo: geoFix ? { lat: geoFix.lat, lon: geoFix.lon, heading: geoHeading } : null,
        cap: {
          class: "term-lean",
          role: "peer",
          lanes: ["glyph", "hex", "chat", "gyst"],
          glyph_n: glyphN,
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
    const img = sampleCtx.createImageData(glyphN, glyphN);
    for (let i = 0; i < glyphN * glyphN; i++) {
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

  function sendCastFrame(lum, fromNick, laneMeta) {
    if (!casting || !ws || ws.readyState !== WebSocket.OPEN) return;
    castSeq = (castSeq + 1) >>> 0;
    const from = (fromNick || myNick()).slice(0, 32);
    const glyph = new Array(glyphN * glyphN);
    const raw = new Uint8Array(glyphN * glyphN);
    for (let i = 0; i < glyphN * glyphN; i++) {
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
      from: from,
      kind: "hexlum",
      w: glyphN,
      h: glyphN,
      seq: castSeq,
      t: t,
      b64: b64raw,
      data: glyph,
      glyphN: glyphN,
      lane: "hex",
      room: meshRoom,
      via: "grokglyph-cast",
      cam: laneMeta || null,
    });
    // 2) walkie / sphere-compatible vburst — project across dome when cast=sphere
    const b64jpeg = jpegB64FromLum(lum);
    sendJSON({
      type: "vburst-frame",
      from: from,
      fmt: "jpeg",
      b64: b64jpeg,
      w: 100,
      h: 100,
      glyph: glyph,
      glyphN: glyphN,
      seq: castSeq,
      t: t,
      room: meshRoom,
      hex_lane: true,
      cast: "sphere",
      project: true,
      cam: laneMeta || null,
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
    // BitChat dual-path (may be same nick with bt: prefix)
    if (bitchat) bitchat.onMesh(msg);
    if (!from || from === myNick()) return;

    if (typ === "bitchat-chat" || (typ === "chat" && msg.meta && msg.meta.via === "bitchat")) {
      ensureRxPeer(from, true);
      const p = findPeerByNick(from);
      if (p) {
        p.source = "rx";
        p.via = "bitchat";
      }
      setCastLabel("bt " + from);
      if (els.hubHint) {
        els.hubHint.textContent =
          "BitChat · " + from + " · " + String(msg.text || "").slice(0, 80);
      }
    }
    if (typ === "bitchat-control" && msg.action) {
      if (msg.action === "cast-start" && !casting) startCast();
      if (msg.action === "cast-stop" && casting) stopCast();
    }

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
    // on-air caption (program bus full update or soft type:caption)
    if (typ === "program") {
      applyProgramCaption(msg);
    }
    if (typ === "caption") {
      showCaptionBar({
        text: msg.text || msg.caption || "",
        lang: msg.lang || "",
        role: msg.role || "",
        speaker: msg.speaker || from,
        source: msg.source || "chat",
        soft: true,
      });
    }
  }

  function applyProgramCaption(msg) {
    const bus = msg.bus || msg;
    if (!bus || typeof bus !== "object") return;
    let meta = bus.caption_meta;
    if (meta && typeof meta === "object") {
      showCaptionBar({
        text: meta.text || bus.caption || "",
        lang: meta.lang || "",
        role: meta.role || "",
        speaker: meta.speaker || "",
        source: meta.source || "program",
        soft: false,
      });
      return;
    }
    const text = (bus.caption || "").trim();
    if (!text) {
      hideCaptionBar();
      return;
    }
    showCaptionBar({
      text: text,
      lang: "",
      role: "",
      speaker: "",
      source: "program",
      soft: false,
    });
  }

  function showCaptionBar(c) {
    if (!els.captionBar || !els.captionText) return;
    const text = (c.text || "").trim();
    if (!text) {
      hideCaptionBar();
      return;
    }
    els.captionBar.hidden = false;
    els.captionText.textContent = c.speaker ? c.speaker + ": " + text : text;
    const bits = [];
    if (c.soft) bits.push("soft");
    if (c.source) bits.push(c.source);
    if (c.lang) bits.push(c.lang);
    if (c.role) bits.push(c.role);
    if (els.captionMeta) {
      els.captionMeta.textContent = bits.length ? bits.join(" · ") : "caption";
    }
  }

  function hideCaptionBar() {
    if (!els.captionBar) return;
    els.captionBar.hidden = true;
    if (els.captionText) els.captionText.textContent = "";
    if (els.captionMeta) els.captionMeta.textContent = "";
  }

  function findPeerByNick(nick) {
    const low = String(nick).toLowerCase();
    return peers.find((p) => !p.self && p.nick.toLowerCase() === low);
  }

  function ensureRxPeer(nick, turnOn, room) {
    const r = room || meshRoom;
    // only show peers in our mesh room (or unscoped legacy)
    if (r && room && room !== meshRoom && room !== "global" && meshRoom !== "global") {
      // different hard room — still track if same global overlay
    }
    let p = findPeerByNick(nick);
    if (p && p.room && meshRoom !== "global" && p.room !== meshRoom && p.room !== "global") {
      // re-home if we got a frame in this room
      if (room === meshRoom) p.room = meshRoom;
      else return p;
    }
    if (!p) {
      const dir = DIR_ORDER[peers.filter((x) => !x.self).length % 4];
      p = makePeer(dir, nick);
      p.source = "rx";
      p.room = r || meshRoom;
      p.on = turnOn !== false;
      peers.push(p);
      saveState();
      layoutAndPaint();
    } else {
      p.source = "rx";
      if (room) p.room = room;
      if (turnOn !== false) p.on = true;
    }
    return p;
  }

  function applyRxFrame(from, msg) {
    const p = ensureRxPeer(from, true, msg.room);
    const buf = ensureLum(p.id);
    let ok = false;
    if (Array.isArray(msg.glyph) && msg.glyph.length >= glyphN * glyphN) {
      for (let i = 0; i < glyphN * glyphN; i++) buf[i] = Math.max(0, Math.min(255, Number(msg.glyph[i]) | 0));
      ok = true;
    } else if (Array.isArray(msg.data) && msg.data.length >= glyphN * glyphN) {
      for (let i = 0; i < glyphN * glyphN; i++) buf[i] = Math.max(0, Math.min(255, Number(msg.data[i]) | 0));
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
    if (msg.room && meshRoom !== "global" && msg.room !== meshRoom && msg.room !== "global") {
      return; // other mesh room
    }
    const p = ensureRxPeer(from || msg.mark || "stream", true, msg.room);
    const buf = ensureLum(p.id);
    if (Array.isArray(data) && data.length >= glyphN * glyphN) {
      for (let i = 0; i < glyphN * glyphN; i++) buf[i] = Math.max(0, Math.min(255, Number(data[i]) | 0));
    } else if (typeof data === "string") {
      // base64 bytes
      try {
        const bin = atob(data);
        const n = Math.min(glyphN * glyphN, bin.length);
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

    // mobile double-stack: tiles are 1:2 (width:height) — cell is width
    const stackH = mobileStack ? 2 : 1;
    for (let cols = 1; cols <= nWant; cols++) {
      const rows = Math.ceil(nWant / cols);
      const cellW = Math.floor((W - (cols - 1) * GAP) / cols);
      const cellH = Math.floor((H - (rows - 1) * GAP) / rows);
      // in double-stack, height budget is 2× width for portrait faces
      const cell = Math.max(1, Math.min(cellW, Math.floor(cellH / stackH)));
      if (
        cell > best.cell ||
        (cell === best.cell && Math.abs(cols - rows) < Math.abs(best.cols - best.rows)) ||
        (cell === best.cell && cols > best.cols)
      ) {
        best = { cols, rows, cell };
      }
    }

    let cell = best.cell;
    if (cell >= glyphN) {
      const snapped = Math.floor(cell / glyphN) * glyphN;
      if (snapped >= glyphN && snapped >= cell * 0.85) cell = snapped;
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
      els.scaleLabel.textContent =
        glyphN +
        " · " +
        renderStyle +
        " · " +
        cellPx +
        "px · " +
        gridCols +
        "×" +
        gridRows;
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

  function peerInRoom(p) {
    if (p.self) return true;
    if (!p.room || p.room === "global" || meshRoom === "global") return true;
    return p.room === meshRoom;
  }

  function visibleOrder() {
    const self = peers.filter((p) => p.self);
    const rest = sortPeers(peers.filter((p) => !p.self && peerInRoom(p)));
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
      c.width = glyphN;
      c.height = glyphN;
      c.setAttribute(
        "aria-label",
        "Glyph matrix video " + p.nick + (isVideo ? " live" : "")
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

  function videoForPeer(p) {
    if (!p) return null;
    if (p.selfCam || p.source === "cam") {
      const lane = camLanes.find((l) => l.peerId === p.id);
      if (lane) return lane.video;
      if ((p.self || p.id === "self") && els.localVideo) return els.localVideo;
    }
    return null;
  }

  // ── animation: sample video every frame ──────────────────
  function tick(now) {
    const t = (now - t0) / 1000;
    // throttle mesh cast across all lanes as a group
    let castDue = casting && now - lastCastAt >= CAST_MS;
    if (castDue) lastCastAt = now;

    for (const p of peers) {
      if (!isDrawn(p)) continue;
      const c = canvasById.get(p.id);
      if (!c) continue;
      const buf = ensureLum(p.id);
      let mode = "sim";

      if (p.self || p.selfCam) {
        let sampled = false;
        const vid = videoForPeer(p);
        if (camOn && vid) {
          sampled = sampleSourceToLum(vid, buf);
          if (sampled) {
            p.source = "cam";
            mode = casting ? "cast" : "self";
          }
        }
        if (!sampled && p.self && fileOn && els.fileVideo) {
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
          // cast each camera lane (and file/self) to mesh + sphere
          if (castDue) {
            const lane = camLanes.find((l) => l.peerId === p.id);
            const from =
              lane && camLanes.length > 1
                ? myNick() + "-" + lane.short
                : myNick();
            sendCastFrame(buf, from, lane ? { id: lane.short, label: lane.label } : null);
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

    // full-resolution external screen player (integer LED scale)
    if (screenOn) pushScreenCast(false);

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
    if (els.screenBtn) els.screenBtn.addEventListener("click", () => toggleScreenCast());
    if (els.screenOpen) {
      els.screenOpen.addEventListener("click", () =>
        openScreenPlayer({ fullscreen: false, presentation: true })
      );
    }
    if (els.screenFs) {
      els.screenFs.addEventListener("click", () =>
        openScreenPlayer({ fullscreen: true, presentation: true })
      );
    }
    if (els.screenLayout) {
      els.screenLayout.addEventListener("change", () => {
        screenLayout = els.screenLayout.value || "grid";
        pushScreenCast(true);
      });
    }
    if (els.screenLed) {
      els.screenLed.addEventListener("change", () => {
        screenLed = els.screenLed.value || "auto";
        pushScreenCast(true);
      });
    }
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
    if (els.socialGo) els.socialGo.addEventListener("click", () => loadSocialHandle());
    if (els.socialQ) {
      els.socialQ.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          loadSocialHandle();
        }
      });
    }
    if (els.socialStack) {
      els.socialStack.addEventListener("click", () => setMobileStack(!mobileStack));
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

      // shortcuts: c cam, v cast, p screen, h hub
      if (inField) return;
      if (e.key === "p" || e.key === "P") {
        e.preventDefault();
        toggleScreenCast();
        return;
      }
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


  // ── drawer tabs · regions · geo · display ─────────────────
  let regionCat = "regions";

  function switchTab(name) {
    document.querySelectorAll(".gg-tab").forEach((t) => {
      const on = t.getAttribute("data-tab") === name;
      t.classList.toggle("is-on", on);
      t.setAttribute("aria-selected", on ? "true" : "false");
    });
    document.querySelectorAll(".gg-tab-panel").forEach((panel) => {
      const on = panel.getAttribute("data-panel") === name;
      panel.classList.toggle("is-on", on);
      panel.hidden = !on;
    });
  }

  function renderRegionList() {
    const GP = window.GrokGlyphPresets;
    if (!els.regionList || !GP) return;
    const list = GP.PRESETS[regionCat] || [];
    els.regionList.innerHTML = "";
    list.forEach((preset) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "gg-region-item" + (meshRoom === preset.id ? " is-on" : "");
      btn.setAttribute("role", "option");
      btn.innerHTML =
        "<strong>" +
        escapeHtml(preset.name) +
        "</strong><span>" +
        escapeHtml(preset.id) +
        " · " +
        preset.lat.toFixed(1) +
        "," +
        preset.lon.toFixed(1) +
        "</span>";
      btn.addEventListener("click", () => {
        setMeshRoom(preset.id, { reconnect: true });
        if (preset.hub && els.hubUrl) {
          els.hubUrl.value = preset.hub;
          saveState();
        }
        renderRegionList();
        setCastLabel("room " + preset.id);
      });
      els.regionList.appendChild(btn);
    });
  }

  function escapeHtml(t) {
    return String(t)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }

  function updateGeoReadout() {
    if (els.geoLat) els.geoLat.textContent = geoFix ? geoFix.lat.toFixed(5) : "—";
    if (els.geoLon) els.geoLon.textContent = geoFix ? geoFix.lon.toFixed(5) : "—";
    if (els.geoAcc) els.geoAcc.textContent = geoFix ? Math.round(geoFix.acc) + " m" : "—";
    if (els.geoHeadingEl) {
      els.geoHeadingEl.textContent =
        geoHeading != null && !Number.isNaN(geoHeading) ? Math.round(geoHeading) + "°" : "—";
    }
    const GP = window.GrokGlyphPresets;
    if (geoFix && GP) {
      const tri = GP.triangulate(geoFix.lat, geoFix.lon, geoHeading);
      if (els.geoNearest) {
        els.geoNearest.textContent = tri.nearest
          ? tri.nearest.preset.name + " (" + tri.nearest.km.toFixed(0) + " km)"
          : "—";
      }
      if (els.geoFacing) {
        els.geoFacing.textContent = tri.facing
          ? tri.facing.preset.name + " (" + Math.round(tri.facing.bearing) + "°)"
          : "—";
      }
      return tri;
    }
    if (els.geoNearest) els.geoNearest.textContent = "—";
    if (els.geoFacing) els.geoFacing.textContent = "—";
    return null;
  }

  function locateMe() {
    if (!navigator.geolocation) {
      if (els.geoStatus) els.geoStatus.textContent = "Geolocation not available.";
      return;
    }
    if (els.geoStatus) els.geoStatus.textContent = "Locating…";
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        geoFix = {
          lat: pos.coords.latitude,
          lon: pos.coords.longitude,
          acc: pos.coords.accuracy || 0,
        };
        if (pos.coords.heading != null && !Number.isNaN(pos.coords.heading) && pos.coords.heading >= 0) {
          geoHeading = pos.coords.heading;
        }
        if (els.geoStatus) {
          els.geoStatus.textContent =
            "Fix ±" + Math.round(geoFix.acc) + " m · tap Auto-join or enable gyro.";
        }
        updateGeoReadout();
      },
      (err) => {
        if (els.geoStatus) {
          els.geoStatus.textContent = "Locate failed: " + (err && err.message ? err.message : "denied");
        }
      },
      { enableHighAccuracy: true, timeout: 12000, maximumAge: 10000 }
    );
  }

  async function enableGyro() {
    try {
      if (
        typeof DeviceOrientationEvent !== "undefined" &&
        typeof DeviceOrientationEvent.requestPermission === "function"
      ) {
        const perm = await DeviceOrientationEvent.requestPermission();
        if (perm !== "granted") {
          if (els.geoStatus) els.geoStatus.textContent = "Motion permission denied.";
          return false;
        }
      }
    } catch (e) {
      if (els.geoStatus) els.geoStatus.textContent = "Motion API unavailable.";
      return false;
    }
    window.addEventListener("deviceorientationabsolute", onOrient, true);
    window.addEventListener("deviceorientation", onOrient, true);
    gyroOn = true;
    if (els.gyroToggle) els.gyroToggle.setAttribute("aria-pressed", "true");
    if (els.geoStatus) els.geoStatus.textContent = "Gyro heading on — rotate device.";
    return true;
  }

  function disableGyro() {
    window.removeEventListener("deviceorientationabsolute", onOrient, true);
    window.removeEventListener("deviceorientation", onOrient, true);
    gyroOn = false;
    if (els.gyroToggle) els.gyroToggle.setAttribute("aria-pressed", "false");
  }

  function onOrient(ev) {
    // webkitCompassHeading (iOS) or alpha
    let h = null;
    if (typeof ev.webkitCompassHeading === "number") {
      h = ev.webkitCompassHeading;
    } else if (typeof ev.alpha === "number") {
      // alpha: 0 when pointing north on some devices; invert for compass-like
      h = (360 - ev.alpha) % 360;
    }
    if (h == null || Number.isNaN(h)) return;
    geoHeading = h;
    updateGeoReadout();
  }

  function autoJoinMesh() {
    const GP = window.GrokGlyphPresets;
    if (!geoFix) {
      locateMe();
      if (els.geoStatus) els.geoStatus.textContent = "Locate first, then Auto-join.";
      return;
    }
    if (!GP) return;
    const tri = updateGeoReadout();
    if (!tri || !tri.facing) {
      if (els.geoStatus) els.geoStatus.textContent = "No preset match.";
      return;
    }
    // prefer facing when gyro on, else nearest
    const pick = gyroOn && tri.facing ? tri.facing.preset : tri.nearest.preset;
    setMeshRoom(pick.id, { reconnect: true });
    if (els.geoStatus) {
      els.geoStatus.textContent =
        "Joined " + pick.name + " (" + pick.id + ") via " + (gyroOn ? "facing+GPS" : "nearest GPS");
    }
    renderRegionList();
    // announce geo room
    if (ws && ws.readyState === WebSocket.OPEN) {
      const line =
        "joined mesh " + pick.id + " @ " + geoFix.lat.toFixed(3) + "," + geoFix.lon.toFixed(3);
      if (bitchat) {
        bitchat.sendChat(line);
      } else {
        sendJSON({
          type: "chat",
          from: myNick(),
          text: line,
          t: Date.now(),
          meta: { via: "wifi", dual: true },
        });
      }
    }
  }

  function wireExtra() {
    document.querySelectorAll(".gg-tab[data-tab]").forEach((tab) => {
      tab.addEventListener("click", () => switchTab(tab.getAttribute("data-tab")));
    });
    document.querySelectorAll(".gg-chip[data-region-cat]").forEach((btn) => {
      btn.addEventListener("click", () => {
        regionCat = btn.getAttribute("data-region-cat") || "regions";
        document.querySelectorAll(".gg-chip[data-region-cat]").forEach((b) => {
          b.classList.toggle("is-on", b === btn);
        });
        renderRegionList();
      });
    });
    document.querySelectorAll(".gg-chip[data-res]").forEach((btn) => {
      btn.addEventListener("click", () => setGlyphResolution(btn.getAttribute("data-res")));
    });
    document.querySelectorAll(".gg-chip[data-style]").forEach((btn) => {
      btn.addEventListener("click", () => setRenderStyle(btn.getAttribute("data-style")));
    });
    if (els.clearRoom) els.clearRoom.addEventListener("click", () => clearRoom(true));
    if (els.clearRoomDrawer) els.clearRoomDrawer.addEventListener("click", () => clearRoom(true));
    if (els.roomId) {
      els.roomId.addEventListener("change", () => setMeshRoom(els.roomId.value, { reconnect: true }));
    }
    if (els.geoLocate) els.geoLocate.addEventListener("click", locateMe);
    if (els.geoAutoJoin) els.geoAutoJoin.addEventListener("click", autoJoinMesh);
    if (els.gyroToggle) {
      els.gyroToggle.addEventListener("click", async () => {
        if (gyroOn) disableGyro();
        else await enableGyro();
      });
    }
    renderRegionList();
    setRenderStyle(renderStyle);
  }


  // boot
  initPeers();
  wire();
  wireExtra();
  if (window.GY_BITCHAT) {
    bitchat = window.GY_BITCHAT.create({
      getNick: myNick,
      getRoom: function () {
        return meshRoom;
      },
      sendMesh: function (obj) {
        return sendJSON(obj);
      },
      onChat: function (row) {
        if (row && row.from) ensureRxPeer(row.from);
        setCastLabel("bt " + (row && row.from ? row.from : ""));
      },
      onControl: function (c) {
        if (!c) return;
        if (c.action === "cast-start" && !casting) startCast();
        if (c.action === "cast-stop" && casting) stopCast();
      },
    });
    bitchat.startPoll(12000);
  }
  setDrawer(false);
  switchTab("room");
  layoutAndPaint();
  raf = requestAnimationFrame(tick);
  registerSW();
})();
