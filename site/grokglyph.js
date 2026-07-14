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
   * All active camera lanes (laptop webcam + phone front/back)
   * @type {Array<{
   *   deviceId: string,
   *   label: string,
   *   stream: MediaStream,
   *   video: HTMLVideoElement,
   *   peerId: string,
   *   short: string,
   *   kind?: string,
   *   slot?: string,
   *   sceneOrder?: number
   * }>}
   */
  let camLanes = [];
  /** Filmmaker scene: L2 · L1 · C(laptop) · R1 · R2 */
  let sceneMode = true;
  /** URL ?slot=L1 forces this device into a scene seat */
  let forcedSlot = "";
  /** laptop | phone — UI role for this browser */
  let deviceRole = "laptop";
  const SCENE_SLOTS = ["L2", "L1", "C", "R1", "R2"];
  const SCENE_ORD = { L2: 0, L1: 1, C: 2, R1: 3, R2: 4 };
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
    // GitHub Pages cannot open LAN mesh — open via gy serve instead
    if ((location.hostname || "").includes("github.io")) {
      return "ws://";
    }
    if (location.protocol === "file:") {
      return "ws://127.0.0.1:9876/";
    }
    if (location.protocol === "http:" || location.protocol === "https:") {
      const proto = location.protocol === "https:" ? "wss:" : "ws:";
      // gy serve: page and hub share host:9876
      if (location.port === "9876") {
        return proto + "//" + location.host + "/";
      }
      return "ws://" + (location.hostname || "127.0.0.1") + ":9876/";
    }
    return "ws://127.0.0.1:9876/";
  }

  /** HTTP origin for /api/lan discovery */
  function hubHttpOrigin() {
    try {
      const raw = (els.hubUrl && els.hubUrl.value.trim()) || defaultHubURL();
      if (!raw || raw === "ws://" || raw === "wss://") {
        if (location.protocol === "http:" || location.protocol === "https:") return location.origin;
        return "http://127.0.0.1:9876";
      }
      const u = new URL(raw.replace(/^ws/i, "http"));
      return u.origin;
    } catch {
      return location.protocol === "http:" || location.protocol === "https:"
        ? location.origin
        : "http://127.0.0.1:9876";
    }
  }

  let lanInfo = null;
  let pairPeers = 0;

  function updatePairStatus(text, isErr) {
    const el = document.getElementById("gg-pair-status");
    if (!el) return;
    el.textContent = text || "hub · idle";
    el.classList.toggle("is-err", !!isErr);
    el.classList.toggle("is-live", !isErr && /linked|peer|room|joined|hub on/i.test(text || ""));
  }

  function buildPhonePairURL(slot) {
    slot = (slot || (deviceRole === "phone" ? forcedSlot || "L1" : "L1")).toUpperCase();
    const room = meshRoom || "global";
    let base = "";
    if (lanInfo && lanInfo.http) {
      base = String(lanInfo.http).replace(/\/?$/, "/");
    } else if (location.protocol === "http:" || location.protocol === "https:") {
      base = location.origin + "/";
    } else if (lanInfo && lanInfo.glyph) {
      base = String(lanInfo.glyph).replace(/grokglyph\.html.*$/i, "");
    } else {
      base = "http://127.0.0.1:9876/";
    }
    const nick = "phone-" + slot;
    return (
      base +
      "grokglyph.html?role=phone&slot=" +
      encodeURIComponent(slot) +
      "&room=" +
      encodeURIComponent(room) +
      "&nick=" +
      encodeURIComponent(nick) +
      "&hub=1&connect=1"
    );
  }

  function renderPairQR(text) {
    const img = document.getElementById("gg-pair-qr");
    const wrap = document.getElementById("gg-pair-qr-wrap");
    if (!img || !wrap || !text) return false;
    if (typeof qrcode !== "function") {
      wrap.hidden = false;
      wrap.title = "Open /qr.html?url=… for QR";
      return false;
    }
    try {
      const q = qrcode(0, "M");
      q.addData(text);
      q.make();
      img.src = q.createDataURL(4, 2);
      img.alt = "QR: " + text;
      wrap.hidden = false;
      return true;
    } catch {
      wrap.hidden = true;
      return false;
    }
  }

  function refreshPairUI() {
    const phoneUrl = buildPhonePairURL("L1");
    const phoneInput = document.getElementById("gg-pair-phone-url");
    const lanInput = document.getElementById("gg-pair-lan");
    if (phoneInput) phoneInput.value = phoneUrl;
    if (lanInput) {
      lanInput.value =
        (lanInfo && lanInfo.ws) ||
        (els.hubUrl && els.hubUrl.value) ||
        defaultHubURL();
    }
    // keep QR in sync if visible
    const wrap = document.getElementById("gg-pair-qr-wrap");
    if (wrap && !wrap.hidden) renderPairQR(phoneUrl);
  }

  async function fetchLanPair() {
    try {
      const res = await fetch(hubHttpOrigin() + "/api/lan", {
        headers: { Accept: "application/json" },
      });
      if (!res.ok) throw new Error("lan " + res.status);
      lanInfo = await res.json();
      if (lanInfo && lanInfo.ws && els.hubUrl) {
        // Prefer LAN advertise IP so phone QR uses reachable host
        const cur = (els.hubUrl.value || "").trim();
        if (!cur || cur.indexOf("127.0.0.1") >= 0 || cur.indexOf("localhost") >= 0) {
          // laptop: keep 127.0.0.1 for self if already local page, but store LAN for phone
          if ((location.hostname === "127.0.0.1" || location.hostname === "localhost") && lanInfo.ws) {
            // leave hub field as 127.0.0.1 for laptop loopback — phone URL uses LAN
          } else if (lanInfo.ws) {
            els.hubUrl.value = lanInfo.ws;
          }
        }
      }
      if (lanInfo && lanInfo.room && (!meshRoom || meshRoom === "global") && lanInfo.room !== "global") {
        // optional env GY_ROOM
      }
      refreshPairUI();
      updatePairStatus(
        "LAN · " +
          (lanInfo.ws || "?") +
          " · open hub pages at " +
          (lanInfo.http || "")
      );
      return lanInfo;
    } catch (e) {
      updatePairStatus(
        "LAN offline · run: gy serve --bind 0.0.0.0  · then open http://<laptop-ip>:9876/grokglyph.html",
        true
      );
      refreshPairUI();
      return null;
    }
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

  // ── camera / file (multi-cam simulcast · filmmaker scene L2 L1 C R1 R2) ──

  function isMobileUA() {
    try {
      return /Android|iPhone|iPad|iPod|Mobile/i.test(navigator.userAgent || "");
    } catch {
      return false;
    }
  }

  /**
   * Classify a camera for filmmaker scene layout.
   * Laptop/webcam → center (C); phone front → face (L1); back → scene (R1).
   */
  function classifyCam(label, index, facingHint) {
    const L = String(label || "").toLowerCase();
    const fh = String(facingHint || "").toLowerCase();
    const mobile = isMobileUA();

    // Built-in laptop / desktop webcam → center of the film scene
    if (
      /macbook|facetime|integrated|built-?in|logitech|webcam|hd pro|c920|c922|brio|obs|virtual|capture|elgato|cam link/i.test(
        L
      ) ||
      (!mobile && L && !/back|rear|front|ultra|tele|environment|user/.test(L) && /camera|usb|hd/.test(L))
    ) {
      return { kind: "laptop", short: "laptop", slot: "C", order: 50, facing: "user" };
    }
    if (/ultra|wide/.test(L) && !/tele/.test(L)) {
      return { kind: "uw", short: "uw", slot: "R2", order: 80, facing: "environment" };
    }
    if (/tele|zoom|×2|x2|×3|x3/.test(L)) {
      return { kind: "tele", short: "tele", slot: "L2", order: 15, facing: "environment" };
    }
    if (/back|rear|environment|world|main camera/.test(L) || fh === "environment") {
      return { kind: "back", short: "back", slot: "R1", order: 70, facing: "environment" };
    }
    if (/front|user|face|selfie|truedepth/.test(L) || fh === "user") {
      return { kind: "front", short: "front", slot: "L1", order: 30, facing: "user" };
    }
    // Desktop unlabeled single cam → treat as laptop center
    if (!mobile) {
      return { kind: "laptop", short: "webcam", slot: "C", order: 50, facing: "user" };
    }
    // Mobile unlabeled: first = front, second = back
    if (index === 0) {
      return { kind: "front", short: "front", slot: "L1", order: 30, facing: "user" };
    }
    return { kind: "back", short: "back", slot: "R1", order: 70, facing: "environment" };
  }

  function shortCamLabel(label, index, deviceId) {
    return classifyCam(label, index, "").short;
  }

  function slotDir(slot) {
    if (slot === "C") return "c";
    if (slot === "L1" || slot === "L2") return "w";
    if (slot === "R1" || slot === "R2") return "e";
    return "e";
  }

  function sceneOrderOf(slotOrPeer) {
    if (typeof slotOrPeer === "string") {
      return SCENE_ORD[slotOrPeer] != null ? SCENE_ORD[slotOrPeer] : 50;
    }
    const p = slotOrPeer;
    if (p && p.sceneSlot && SCENE_ORD[p.sceneSlot] != null) return SCENE_ORD[p.sceneSlot];
    if (p && p.sceneOrder != null) return p.sceneOrder;
    // parse nick-L1 / nick-C
    const m = String((p && p.nick) || "").match(/-(L2|L1|C|R1|R2)$/i);
    if (m) return SCENE_ORD[m[1].toUpperCase()];
    if (p && (p.self || p.id === "self")) return SCENE_ORD.C;
    return 50;
  }

  /**
   * Assign unique scene slots L2·L1·C·R1·R2 for local lanes.
   * Prefer laptop/webcam at C; phone front left; phone back right.
   * forcedSlot only pins the *primary* lens (matching role), not every lane.
   */
  function assignSceneSlots(lanes) {
    const used = new Set();
    const prefer = {
      laptop: "C",
      webcam: "C",
      front: "L1",
      back: "R1",
      uw: "R2",
      tele: "L2",
    };
    const primaryKindForForced = {
      C: "laptop",
      L1: "front",
      L2: "tele",
      R1: "back",
      R2: "uw",
    };
    // sort by preferred film order (kind priority)
    const ranked = lanes.slice().sort((a, b) => {
      const pa = a.kind === "laptop" ? 0 : a.kind === "front" ? 1 : a.kind === "back" ? 2 : a.kind === "uw" ? 3 : 4;
      const pb = b.kind === "laptop" ? 0 : b.kind === "front" ? 1 : b.kind === "back" ? 2 : b.kind === "uw" ? 3 : 4;
      return pa - pb || (a.order || 0) - (b.order || 0);
    });
    let primaryPinned = false;
    ranked.forEach((lane) => {
      let slot = prefer[lane.kind] || lane.slot || "R1";
      // Pin primary lens to forced seat once (phone ?slot=L1 keeps front on L1, other lenses free)
      if (
        forcedSlot &&
        SCENE_SLOTS.includes(forcedSlot) &&
        !primaryPinned &&
        (!primaryKindForForced[forcedSlot] ||
          lane.kind === primaryKindForForced[forcedSlot] ||
          ranked.length === 1)
      ) {
        slot = forcedSlot;
        primaryPinned = true;
      }
      if (used.has(slot)) {
        slot = SCENE_SLOTS.find((s) => !used.has(s)) || slot;
      }
      used.add(slot);
      lane.slot = slot;
      lane.sceneOrder = SCENE_ORD[slot];
      lane.short =
        slot === "C"
          ? lane.kind === "laptop" || lane.kind === "webcam"
            ? "laptop"
            : "C"
          : lane.kind || slot;
    });
    // re-sort lanes array left→right for stable peer indices
    lanes.sort((a, b) => a.sceneOrder - b.sceneOrder);
    return lanes;
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
      if (lane.peerId && lane.peerId !== "self") {
        peers = peers.filter((p) => p.id !== lane.peerId);
        canvasById.delete(lane.peerId);
        lumBuf.delete(lane.peerId);
      }
    });
    camLanes = [];
  }

  function ensureCamPeer(lane, index) {
    const slot = lane.slot || "C";
    const displayNick = sceneMode
      ? myNick() + "-" + slot
      : myNick() + (lane.short ? "-" + lane.short : "");
    // Exactly one self tile — always the C / first lane; others are satellite selfCams
    const isSelfTile = slot === "C" || !peers.some((p) => p.self || p.id === "self");

    if (isSelfTile) {
      let self = peers.find((p) => p.self || p.id === "self");
      if (!self) {
        self = defaultSelf();
        peers.unshift(self);
      }
      self.source = "cam";
      self.nick = displayNick;
      self.camLane = index;
      self.selfCam = true;
      self.sceneSlot = slot;
      self.sceneOrder = SCENE_ORD[slot] != null ? SCENE_ORD[slot] : 2;
      self.camKind = lane.kind || "cam";
      self.dir = slotDir(slot);
      lane.peerId = self.id;
      return self;
    }
    const id =
      "cam-" +
      slot +
      "-" +
      (lane.deviceId || String(index)).replace(/\W/g, "").slice(0, 12);
    let p = peers.find((x) => x.id === id);
    if (!p) {
      p = {
        id: id,
        nick: displayNick,
        dir: slotDir(slot),
        on: true,
        seed: hashStr(id) % 1e9,
        self: false,
        selfCam: true,
        source: "cam",
        room: meshRoom,
        camLane: index,
        sceneSlot: slot,
        sceneOrder: SCENE_ORD[slot] != null ? SCENE_ORD[slot] : 50,
        camKind: lane.kind || "cam",
      };
      peers.push(p);
    } else {
      p.source = "cam";
      p.selfCam = true;
      p.nick = displayNick;
      p.on = true;
      p.sceneSlot = slot;
      p.sceneOrder = SCENE_ORD[slot] != null ? SCENE_ORD[slot] : 50;
      p.camKind = lane.kind || "cam";
      p.dir = slotDir(slot);
    }
    lane.peerId = p.id;
    return p;
  }

  /** Known videoinput devices after last permission grant (for flip / retry). */
  let availableVideoInputs = [];
  let flipLensIndex = 0;
  let lastCamFailNotes = [];

  function isIOSUA() {
    try {
      return /iPhone|iPad|iPod/i.test(navigator.userAgent || "") ||
        (navigator.platform === "MacIntel" && navigator.maxTouchPoints > 1);
    } catch {
      return false;
    }
  }

  /**
   * Soft constraints first (ideal) — exact often fails after re-open on Android/iOS.
   */
  async function openDeviceStream(deviceId, facingMode) {
    const tries = [];
    if (deviceId) {
      tries.push({
        video: {
          deviceId: { ideal: deviceId },
          width: { ideal: 1280 },
          height: { ideal: 720 },
        },
        audio: false,
      });
      tries.push({
        video: {
          deviceId: { exact: deviceId },
          width: { ideal: 1280 },
          height: { ideal: 720 },
        },
        audio: false,
      });
      tries.push({ video: { deviceId: { exact: deviceId } }, audio: false });
    }
    if (facingMode) {
      tries.push({
        video: {
          facingMode: { ideal: facingMode },
          width: { ideal: 1280 },
          height: { ideal: 720 },
        },
        audio: false,
      });
      tries.push({
        video: {
          facingMode: { exact: facingMode },
          width: { ideal: 1280 },
          height: { ideal: 720 },
        },
        audio: false,
      });
      tries.push({ video: { facingMode: facingMode }, audio: false });
    }
    if (!deviceId && !facingMode) {
      tries.push({
        video: { width: { ideal: 1280 }, height: { ideal: 720 } },
        audio: false,
      });
      tries.push({ video: true, audio: false });
    }
    let last = null;
    for (let i = 0; i < tries.length; i++) {
      try {
        return await navigator.mediaDevices.getUserMedia(tries[i]);
      } catch (e) {
        last = e;
        if (e && (e.name === "NotAllowedError" || e.name === "SecurityError")) throw e;
      }
    }
    throw last || new Error("getUserMedia failed");
  }

  async function attachLane(stream, label, deviceId, index, facingHint) {
    const video = document.createElement("video");
    video.setAttribute("playsinline", "");
    video.setAttribute("webkit-playsinline", "true");
    video.playsInline = true;
    video.muted = true;
    video.autoplay = true;
    video.srcObject = stream;
    await video.play().catch(() => {});
    // wait briefly for dimensions (some phones report 0 until first frame)
    for (let w = 0; w < 12 && !(video.videoWidth > 0); w++) {
      await new Promise((r) => setTimeout(r, 40));
    }
    const track = stream.getVideoTracks()[0];
    const settings = track && track.getSettings ? track.getSettings() : {};
    const face =
      facingHint ||
      settings.facingMode ||
      "";
    const cls = classifyCam(label || (track && track.label) || "", index, face);
    const lane = {
      deviceId: deviceId || settings.deviceId || "default-" + index,
      label: label || (track && track.label) || cls.short,
      stream: stream,
      video: video,
      peerId: "",
      short: cls.short,
      kind: cls.kind,
      slot: cls.slot,
      order: cls.order,
      facing: cls.facing || face || "",
      sceneOrder: SCENE_ORD[cls.slot] || 50,
      groupId: "",
    };
    try {
      // match MediaDeviceInfo.groupId when available
      const dev = availableVideoInputs.find((d) => d.deviceId === lane.deviceId);
      if (dev && dev.groupId) lane.groupId = dev.groupId;
    } catch {
      /* ignore */
    }
    return lane;
  }

  /**
   * Choose which enumerated devices to open.
   * Mobile: front + back + ultra + tele (dedupe labels), not every virtual clone.
   */
  function selectDevicesToOpen(videoInputs, allCams) {
    if (!videoInputs || !videoInputs.length) return [];
    if (!allCams) return [videoInputs[0]];
    if (!isMobileUA()) {
      // desktop: all physical inputs (skip obvious virtual dupes if many)
      const real = videoInputs.filter(
        (d) => !/virtual|obs|snap|manycam|iriun|epoccam|continuity/i.test(d.label || "")
      );
      return (real.length ? real : videoInputs).slice(0, 6);
    }

    const ranked = videoInputs.map((d, i) => ({
      d: d,
      i: i,
      cls: classifyCam(d.label, i, ""),
      label: d.label || "",
    }));
    const picks = [];
    const takenIds = new Set();
    const take = (pred) => {
      const hit = ranked.find((r) => !takenIds.has(r.d.deviceId) && pred(r));
      if (!hit) return;
      takenIds.add(hit.d.deviceId);
      picks.push(hit.d);
    };
    take((r) => r.cls.kind === "front");
    take((r) => r.cls.kind === "back");
    take((r) => r.cls.kind === "uw");
    take((r) => r.cls.kind === "tele");
    // unlabeled phone cameras — take remaining up to 4
    ranked.forEach((r) => {
      if (picks.length >= 4) return;
      if (takenIds.has(r.d.deviceId)) return;
      // skip pure virtual
      if (/virtual|obs|snap/i.test(r.label)) return;
      takenIds.add(r.d.deviceId);
      picks.push(r.d);
    });
    // iOS often lists 2–3 with empty labels until permission; take first two if empty
    if (picks.length < 2 && videoInputs.length >= 2) {
      return videoInputs.slice(0, Math.min(3, videoInputs.length));
    }
    return picks.length ? picks : videoInputs.slice(0, 2);
  }

  function preferredFacingForRole() {
    if (forcedSlot === "R1" || forcedSlot === "R2") return "environment";
    if (forcedSlot === "L1" || forcedSlot === "L2") return "user";
    if (deviceRole === "phone") return "user";
    return "user";
  }

  function finishCamOpen(opened) {
    assignSceneSlots(opened);
    opened.forEach((lane, i) => {
      ensureCamPeer(lane, i);
      camLanes.push(lane);
    });

    const center = camLanes.find((l) => l.slot === "C") || camLanes[0];
    if (center) {
      mediaStream = center.stream;
      if (els.localVideo) {
        els.localVideo.srcObject = center.stream;
        els.localVideo.muted = true;
        els.localVideo.playsInline = true;
        els.localVideo.play().catch(() => {});
      }
    }

    camOn = true;
    fileOn = false;
    if (els.camBtn) els.camBtn.setAttribute("aria-pressed", "true");
    setCamButtonLabel(true);
    const dockCam = document.getElementById("gg-dock-cam");
    if (dockCam) {
      dockCam.classList.add("is-on");
      dockCam.textContent = camLanes.length > 1 ? String(camLanes.length) : "on";
    }
    const flipBtn = document.getElementById("gg-cam-flip");
    const dockFlip = document.getElementById("gg-dock-flip");
    const canFlip = availableVideoInputs.length > 1 || camLanes.length > 0;
    if (flipBtn) {
      flipBtn.disabled = !canFlip;
      flipBtn.classList.toggle("is-on", camLanes.length === 1 && availableVideoInputs.length > 1);
    }
    if (dockFlip) {
      dockFlip.disabled = !canFlip;
      dockFlip.classList.toggle("is-on", camLanes.length === 1 && availableVideoInputs.length > 1);
    }

    const orderStr = camLanes.map((l) => l.slot + ":" + l.short).join(" · ");
    const listed = availableVideoInputs.length;
    setCastLabel(
      casting
        ? "casting " + camLanes.length + "/" + Math.max(listed, camLanes.length)
        : camLanes.length > 1
          ? camLanes.length + " cams live"
          : listed > 1
            ? "1 cam · flip for more"
            : "cam live"
    );
    if (els.hubHint) {
      let tip =
        "Active " +
        camLanes.length +
        "/" +
        Math.max(listed, camLanes.length) +
        " · " +
        orderStr +
        ". ";
      if (camLanes.length < 2 && listed > 1) {
        tip += isIOSUA()
          ? "iOS usually allows only one live camera — use Flip lens to cycle front/back/ultra, or open a second phone for another seat."
          : "Only one lens opened — tap Flip lens or Open cams again. Some Androids block concurrent front+back.";
      } else if (camLanes.length >= 2) {
        tip += "Multi-lens live · cast ships every lane. ";
      }
      if (lastCamFailNotes.length) {
        tip += " Skipped: " + lastCamFailNotes.slice(0, 3).join("; ") + ".";
      }
      els.hubHint.textContent = tip;
    }
    const sceneBtn = document.getElementById("gg-scene");
    if (sceneBtn) {
      sceneBtn.setAttribute("aria-pressed", sceneMode ? "true" : "false");
      sceneBtn.classList.toggle("is-on", sceneMode);
    }
    layoutAndPaint();
  }

  /**
   * Open all local cameras (laptop webcam + phone front/back/ultra when OS allows)
   * and place them in filmmaker scene order.
   */
  async function enableCam(opts) {
    opts = opts || {};
    const allCams = opts.all !== false;
    if (camOn && camLanes.length && !opts.force) return true;
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      setCastLabel("no cam API");
      return false;
    }
    try {
      if (!window.isSecureContext) {
        setCastLabel("need secure cam");
        if (els.hubHint) {
          els.hubHint.textContent =
            "Camera needs a secure context. On phone use the LAN hub over http://192.168.x.x only if the browser allows it, or open via localhost tunnel / HTTPS. Prefer: open gy serve page and Allow when prompted.";
        }
        // still attempt — some WebViews / flags allow LAN
      }
    } catch {
      /* ignore */
    }

    lastCamFailNotes = [];
    try {
      setCastLabel("opening cams…");
      // Prefer seat-facing first (L1=front, R1=back) and KEEP stream as first lane
      const preferFace = preferredFacingForRole();
      let firstStream = null;
      try {
        firstStream = await navigator.mediaDevices.getUserMedia({
          video: {
            facingMode: { ideal: preferFace },
            width: { ideal: 1280 },
            height: { ideal: 720 },
          },
          audio: false,
        });
      } catch {
        firstStream = await navigator.mediaDevices.getUserMedia({
          video: true,
          audio: false,
        });
      }

      let devices = [];
      try {
        devices = await navigator.mediaDevices.enumerateDevices();
      } catch {
        devices = [];
      }
      availableVideoInputs = devices.filter((d) => d.kind === "videoinput");

      // tear down previous lanes only (keep firstStream)
      stopCamLanes();
      if (els.localVideo) els.localVideo.srcObject = null;

      const opened = [];
      const openIds = new Set();
      const openGroup = new Set();

      const t0 = firstStream.getVideoTracks()[0];
      const s0 = t0 && t0.getSettings ? t0.getSettings() : {};
      const firstLane = await attachLane(
        firstStream,
        (t0 && t0.label) || availableVideoInputs[0] && availableVideoInputs[0].label || "cam",
        s0.deviceId || "",
        0,
        s0.facingMode || preferFace
      );
      opened.push(firstLane);
      if (firstLane.deviceId) openIds.add(firstLane.deviceId);
      if (firstLane.groupId) openGroup.add(firstLane.groupId);

      const list = selectDevicesToOpen(availableVideoInputs, allCams);

      // Open remaining selected devices while keeping first stream live
      for (let i = 0; i < list.length; i++) {
        const d = list[i];
        if (!d || !d.deviceId) continue;
        if (openIds.has(d.deviceId)) continue;
        // same groupId often can't open twice (iOS dual-cam)
        if (d.groupId && openGroup.has(d.groupId) && isIOSUA()) continue;
        try {
          const stream = await openDeviceStream(d.deviceId, "");
          const track = stream.getVideoTracks()[0];
          const st = track && track.getSettings ? track.getSettings() : {};
          const did = st.deviceId || d.deviceId;
          // if browser remapped to already-open camera, drop
          if (did && openIds.has(did)) {
            stream.getTracks().forEach((t) => t.stop());
            lastCamFailNotes.push((d.label || "cam") + "→dup");
            continue;
          }
          const lane = await attachLane(
            stream,
            d.label || (track && track.label) || "cam",
            did,
            opened.length,
            st.facingMode || ""
          );
          // verify we actually got frames (or at least a live track)
          if (track && track.readyState === "ended") {
            stream.getTracks().forEach((t) => t.stop());
            lastCamFailNotes.push((d.label || "cam") + " ended");
            continue;
          }
          opened.push(lane);
          if (did) openIds.add(did);
          if (lane.groupId) openGroup.add(lane.groupId);
        } catch (e) {
          const name = (e && e.name) || "err";
          lastCamFailNotes.push((d.label || d.deviceId || "cam").slice(0, 18) + " " + name);
          console.warn("[grokglyph] cam device skip", d.label || d.deviceId, e);
        }
      }

      // Explicit opposite facing if we still only have one side (Android concurrent)
      if (allCams && isMobileUA() && opened.length < 2) {
        const haveUser = opened.some(
          (l) => l.facing === "user" || l.kind === "front"
        );
        const haveEnv = opened.some(
          (l) =>
            l.facing === "environment" ||
            l.kind === "back" ||
            l.kind === "uw" ||
            l.kind === "tele"
        );
        const tryList = [];
        if (!haveUser) tryList.push("user");
        if (!haveEnv) tryList.push("environment");
        if (!tryList.length) tryList.push(haveUser ? "environment" : "user");
        for (let f = 0; f < tryList.length; f++) {
          const facing = tryList[f];
          try {
            const stream = await openDeviceStream("", facing);
            const track = stream.getVideoTracks()[0];
            const st = track && track.getSettings ? track.getSettings() : {};
            const did = st.deviceId || "";
            if (did && openIds.has(did)) {
              stream.getTracks().forEach((t) => t.stop());
              continue;
            }
            const label =
              (track && track.label) ||
              (facing === "user" ? "Front Camera" : "Back Camera");
            const lane = await attachLane(stream, label, did, opened.length, facing);
            opened.push(lane);
            if (did) openIds.add(did);
          } catch (e) {
            lastCamFailNotes.push(facing + " " + ((e && e.name) || "fail"));
            console.warn("[grokglyph] facingMode", facing, e && e.name);
          }
        }
      }

      if (!opened.length) {
        throw new Error("no camera streams");
      }

      finishCamOpen(opened);
      return true;
    } catch (err) {
      camOn = false;
      stopCamLanes();
      setCastLabel("cam blocked");
      if (els.hubBtn) {
        /* keep */
      }
      if (els.hubHint) {
        const n = err && err.name;
        els.hubHint.textContent =
          n === "NotAllowedError"
            ? "Camera permission denied — Allow camera, then Open cams. Multi-lens: Android may open front+back; iOS often needs Flip lens."
            : n === "NotFoundError"
              ? "No camera found on this device."
              : "Camera blocked — " +
                (err && err.message ? err.message : String(err)) +
                (window.isSecureContext === false
                  ? " · page is not a secure context (use hub on this device / HTTPS)."
                  : "");
      }
      return false;
    }
  }

  /**
   * Cycle to next enumerated lens (iOS / single-stream phones).
   * Stops current lanes and opens the next deviceId.
   */
  async function flipLens() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) return false;
    // ensure we have a device list
    if (!availableVideoInputs.length) {
      try {
        // need permission first
        const s = await navigator.mediaDevices.getUserMedia({ video: true, audio: false });
        s.getTracks().forEach((t) => t.stop());
        const devices = await navigator.mediaDevices.enumerateDevices();
        availableVideoInputs = devices.filter((d) => d.kind === "videoinput");
      } catch (e) {
        setCastLabel("flip need cam");
        return false;
      }
    }
    if (availableVideoInputs.length < 2) {
      // try facing toggle with single device list
      const cur = camLanes[0];
      const nextFace =
        cur && (cur.facing === "user" || cur.kind === "front") ? "environment" : "user";
      setCastLabel("flip " + nextFace + "…");
      try {
        const stream = await openDeviceStream("", nextFace);
        stopCamLanes();
        const track = stream.getVideoTracks()[0];
        const st = track && track.getSettings ? track.getSettings() : {};
        const lane = await attachLane(
          stream,
          (track && track.label) || nextFace,
          st.deviceId || "",
          0,
          nextFace
        );
        finishCamOpen([lane]);
        setCastLabel("lens " + lane.short);
        return true;
      } catch (e) {
        setCastLabel("flip fail");
        return false;
      }
    }

    flipLensIndex = (flipLensIndex + 1) % availableVideoInputs.length;
    // prefer a device not currently open
    const openSet = new Set(camLanes.map((l) => l.deviceId));
    let pick = availableVideoInputs[flipLensIndex];
    for (let i = 0; i < availableVideoInputs.length; i++) {
      const idx = (flipLensIndex + i) % availableVideoInputs.length;
      const d = availableVideoInputs[idx];
      if (!openSet.has(d.deviceId)) {
        pick = d;
        flipLensIndex = idx;
        break;
      }
    }
    setCastLabel("flip…");
    try {
      // single-lens swap: release others so iOS can open next
      const stream = await openDeviceStream(pick.deviceId, "");
      stopCamLanes();
      const track = stream.getVideoTracks()[0];
      const st = track && track.getSettings ? track.getSettings() : {};
      const lane = await attachLane(
        stream,
        pick.label || (track && track.label) || "lens",
        st.deviceId || pick.deviceId,
        0,
        st.facingMode || ""
      );
      finishCamOpen([lane]);
      setCastLabel("lens " + (lane.short || lane.label || flipLensIndex + 1));
      return true;
    } catch (e) {
      console.warn("[grokglyph] flip", e);
      setCastLabel("flip fail");
      // try restore previous
      if (!camLanes.length) await enableCam({ all: true, force: true });
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
    if (els.camBtn) els.camBtn.setAttribute("aria-pressed", "false");
    setCamButtonLabel(false);
    const dockCamOff = document.getElementById("gg-dock-cam");
    if (dockCamOff) {
      dockCamOff.classList.remove("is-on");
      dockCamOff.textContent = "cams";
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
    const role =
      deviceRole === "phone" ? "phone" : deviceRole === "laptop" ? "laptop" : "peer";
    // ensure trailing path before query
    if (!/[/?#]/.test(url.slice(-1)) && !url.includes("?")) url += "/";
    if (!/[?&]nick=/.test(url)) {
      url += (url.includes("?") ? "&" : "?") + "nick=" + encodeURIComponent(nick);
    } else {
      url = url.replace(/([?&])nick=[^&]*/, "$1nick=" + encodeURIComponent(nick));
    }
    if (!/[?&]role=/.test(url)) {
      url += (url.includes("?") ? "&" : "?") + "role=" + encodeURIComponent(role);
    } else {
      url = url.replace(/([?&])role=[^&]*/, "$1role=" + encodeURIComponent(role));
    }
    if (!/[?&]room=/.test(url)) {
      url += (url.includes("?") ? "&" : "?") + "room=" + encodeURIComponent(room);
    } else {
      url = url.replace(/([?&])room=[^&]*/, "$1room=" + encodeURIComponent(room));
    }
    return url;
  }

  function connectHub() {
    const url = hubURL();
    if (!url) {
      setCastLabel("set hub url");
      updatePairStatus("set hub URL (not GitHub Pages) · gy serve LAN", true);
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
    updatePairStatus("connecting " + url.replace(/\?.*/, "") + " …");
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setCastLabel("hub error");
      updatePairStatus("WS error · " + (e && e.message ? e.message : e), true);
      return;
    }
    ws.onopen = () => {
      // advertise hex + gyst lanes so room can pick 25×25 hexlum
      const role =
        deviceRole === "phone"
          ? "phone"
          : deviceRole === "laptop"
            ? "laptop"
            : "grokglyph";
      sendJSON({
        type: "join",
        nick: myNick(),
        role: role,
        room: meshRoom,
        slot: forcedSlot || (deviceRole === "phone" ? "L1" : "C"),
        device: deviceRole,
        geo: geoFix ? { lat: geoFix.lat, lon: geoFix.lon, heading: geoHeading } : null,
        cap: {
          class: deviceRole === "phone" ? "glyph-iot" : "term-lean",
          role: role,
          lanes: ["glyph", "hex", "chat", "gyst"],
          glyph_n: glyphN,
          forge: false,
          slot: forcedSlot || "",
        },
      });
      if (els.hubBtn) els.hubBtn.setAttribute("aria-pressed", "true");
      const pairBtn = document.getElementById("gg-pair-connect");
      if (pairBtn) {
        pairBtn.classList.add("is-on");
        pairBtn.textContent = "Hub linked";
      }
      setCastLabel(casting ? "casting" : "hub on");
      updatePairStatus(
        "linked · " +
          deviceRole +
          " · seat " +
          (forcedSlot || "?") +
          " · room " +
          meshRoom +
          " · waiting for peer cast"
      );
      saveState();
      refreshPairUI();
    };
    ws.onclose = () => {
      if (els.hubBtn) els.hubBtn.setAttribute("aria-pressed", "false");
      const pairBtn = document.getElementById("gg-pair-connect");
      if (pairBtn) {
        pairBtn.classList.remove("is-on");
        pairBtn.textContent = "Connect hub";
      }
      if (castSession) {
        castSession = false;
      }
      if (casting) {
        casting = false;
        if (els.castBtn) els.castBtn.setAttribute("aria-pressed", "false");
      }
      setCastLabel("hub off");
      updatePairStatus("hub closed · tap Connect hub", true);
    };
    ws.onerror = () => {
      setCastLabel("hub err");
      updatePairStatus("hub error · same Wi‑Fi + gy serve --bind 0.0.0.0", true);
    };
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
      cam: Object.assign(
        {
          kind: deviceRole === "phone" ? "phone" : "laptop",
          slot: forcedSlot || (deviceRole === "phone" ? "L1" : "C"),
          device: deviceRole,
        },
        laneMeta || {}
      ),
    });
    // 2) walkie / sphere-compatible vburst — project across dome when cast=sphere
    const b64jpeg = jpegB64FromLum(lum);
    const camMeta = Object.assign(
      {
        kind: deviceRole === "phone" ? "phone" : "laptop",
        slot: forcedSlot || (deviceRole === "phone" ? "L1" : "C"),
        device: deviceRole,
      },
      laneMeta || {}
    );
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
      cam: camMeta,
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

    // Room control + link status (must run even when msg has no from)
    if (typ === "hello") {
      updatePairStatus("linked · id " + (msg.id || "?") + " · room " + (msg.room || meshRoom));
      return;
    }
    if (typ === "join" || typ === "roster" || typ === "leave") {
      if (typ === "roster" && Array.isArray(msg.peers)) {
        msg.peers.forEach((pr) => {
          const n = pr.nick || pr.id;
          if (n && n !== myNick()) ensureRxPeer(n, false);
        });
        const n = msg.peers.length;
        updatePairStatus(
          "room " +
            (msg.room || meshRoom) +
            " · " +
            n +
            " peer" +
            (n === 1 ? "" : "s") +
            " on hub"
        );
      }
      if (typ === "join" && from && from !== myNick()) {
        ensureRxPeer(from, false);
        updatePairStatus("joined · " + from + (msg.role ? " (" + msg.role + ")" : ""));
        setCastLabel("peer " + from);
      }
      if (typ === "leave" && from) {
        updatePairStatus("left · " + from);
      }
      return;
    }
    if (typ === "error") {
      updatePairStatus("hub error · " + (msg.text || msg.code || "error"), true);
      setCastLabel(msg.code || "hub err");
      return;
    }

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
    // Tag RX peers with scene slot from cast meta or nick suffix (phone L1/R1 around laptop)
    if (
      (typ === "vburst-frame" || typ === "gyst") &&
      from &&
      from !== myNick()
    ) {
      const p = ensureRxPeer(from);
      if (p) {
        const cam = msg.cam || {};
        let slot = cam.slot || "";
        if (!slot) {
          const m = String(from).match(/-(L2|L1|C|R1|R2)$/i);
          if (m) slot = m[1].toUpperCase();
        }
        if (slot && SCENE_ORD[slot] != null) {
          p.sceneSlot = slot;
          p.sceneOrder = SCENE_ORD[slot];
          p.dir = slotDir(slot);
          p.camKind = cam.kind || p.camKind;
        }
      }
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
    const want = stageOrder().filter((p) => p.on || p.self || p.selfCam);
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
      if (p.self || p.selfCam) {
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
    if (p.self || p.selfCam) return !!(camOn || fileOn || casting);
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

  function peerSceneKey(p) {
    if (!p) return 50;
    if (p.sceneOrder != null) return p.sceneOrder;
    if (p.sceneSlot && SCENE_ORD[p.sceneSlot] != null) return SCENE_ORD[p.sceneSlot];
    const m = String(p.nick || "").match(/-(L2|L1|C|R1|R2)$/i);
    if (m) return SCENE_ORD[m[1].toUpperCase()];
    if (p.cam && p.cam.slot && SCENE_ORD[p.cam.slot] != null) return SCENE_ORD[p.cam.slot];
    if (p.self || p.id === "self") return SCENE_ORD.C;
    return 50 + (p.seed || 0) % 10;
  }

  function visibleOrder() {
    if (sceneMode) {
      // Filmmaker scene: left → laptop center → right (all selfCams + live RX)
      const list = peers.filter((p) => peerInRoom(p) || p.self || p.selfCam);
      return list.slice().sort((a, b) => {
        const d = peerSceneKey(a) - peerSceneKey(b);
        if (d !== 0) return d;
        if (a.self && !b.self) return -1;
        if (!a.self && b.self) return 1;
        return String(a.nick).localeCompare(String(b.nick));
      });
    }
    const self = peers.filter((p) => p.self);
    const rest = sortPeers(peers.filter((p) => !p.self && peerInRoom(p)));
    return self.concat(rest);
  }

  /** Stage order: scene L2·L1·C·R1·R2 or self-first roster. */
  function stageOrder() {
    const filtering = rosterFilter !== "all" || !!rosterQuery.trim();
    let list = visibleOrder();
    if (filtering) {
      const matched = list.filter((p) => p.self || p.selfCam || peerMatchesRoster(p));
      if (matched.length) list = matched;
    }
    // focus pin (still keep scene order among others)
    if (focusPeerId && !sceneMode) {
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
        (p.selfCam && !p.self ? " is-selfcam" : "") +
        ((p.self || p.selfCam) && casting ? " is-casting" : "") +
        (p.source === "rx" ? " is-rx" : "") +
        (isVideo ? " is-video" : "") +
        (p.on ? "" : " is-off") +
        (p.sceneSlot === "C" ? " is-scene-c" : "") +
        (focusPeerId && p.id === focusPeerId ? " is-focus" : "");
      tile.dataset.id = p.id;
      if (p.sceneSlot) tile.dataset.slot = p.sceneSlot;
      tile.setAttribute("role", "listitem");
      tile.tabIndex = 0;
      tile.title =
        p.nick +
        (p.sceneSlot ? " · slot " + p.sceneSlot : "") +
        (p.camKind ? " · " + p.camKind : "") +
        (p.self ? " (you)" : "") +
        (isVideo ? " · video" : "");

      const badge = document.createElement("span");
      badge.className = "gg-dir-badge" + (isVideo ? " gg-live-badge" : "");
      if (sceneMode && p.sceneSlot) badge.textContent = p.sceneSlot;
      else if ((p.self || p.selfCam) && casting) badge.textContent = "TX";
      else if (isVideo && p.source === "rx") badge.textContent = "RX";
      else if ((p.self || p.selfCam) && (camOn || fileOn)) badge.textContent = "CAM";
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
            const slot = (p.sceneSlot || (lane && lane.slot) || "C").toUpperCase();
            const from =
              sceneMode || (lane && camLanes.length > 1)
                ? myNick() + "-" + slot
                : myNick();
            sendCastFrame(buf, from, {
              id: lane ? lane.short : slot,
              label: lane ? lane.label : p.nick,
              slot: slot,
              kind: p.camKind || (lane && lane.kind) || "cam",
              sceneOrder: peerSceneKey(p),
            });
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
    if (els.camBtn) {
      els.camBtn.addEventListener("click", () => toggleCam());
      // double-click re-opens every lens (force)
      els.camBtn.addEventListener("dblclick", () => enableCam({ all: true, force: true }));
    }
    const camFlip = document.getElementById("gg-cam-flip");
    if (camFlip) camFlip.addEventListener("click", () => flipLens());
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


  // boot — filmmaker scene params + device role + mesh room from URL
  let wantAutoHub = false;
  try {
    const q = new URLSearchParams(location.search);
    if (q.get("scene") === "0" || q.get("scene") === "off") sceneMode = false;
    if (q.get("scene") === "1" || q.get("scene") === "on") sceneMode = true;
    const sl = (q.get("slot") || q.get("seat") || "").toUpperCase();
    if (SCENE_SLOTS.indexOf(sl) >= 0) {
      forcedSlot = sl;
      sceneMode = true;
    }
    const roleQ = (q.get("role") || q.get("device") || "").toLowerCase();
    if (roleQ === "phone" || roleQ === "mobile") deviceRole = "phone";
    if (roleQ === "laptop" || roleQ === "desk" || roleQ === "mac") deviceRole = "laptop";
    // auto: mobile UA + non-C slot → phone
    if (!roleQ && isMobileUA()) {
      deviceRole = forcedSlot && forcedSlot !== "C" ? "phone" : "phone";
    } else if (!roleQ && !isMobileUA()) {
      deviceRole = "laptop";
      if (!forcedSlot) forcedSlot = "C";
    }
    if (deviceRole === "phone" && !forcedSlot) forcedSlot = "L1";
    if (deviceRole === "laptop" && !forcedSlot) forcedSlot = "C";
    const roomQ = (q.get("room") || q.get("mesh") || "").trim();
    if (roomQ) meshRoom = roomQ.slice(0, 48);
    const nickQ = (q.get("nick") || "").trim();
    if (nickQ && els.nick) els.nick.value = nickQ.slice(0, 16);
    // ?hub=1|connect=1|link=1 → auto WebSocket join
    wantAutoHub =
      q.get("hub") === "1" ||
      q.get("connect") === "1" ||
      q.get("link") === "1" ||
      q.get("auto") === "1";
  } catch {
    /* ignore */
  }

  function setCamButtonLabel(on) {
    const long = on
      ? deviceRole === "phone"
        ? "phone on"
        : "laptop on"
      : deviceRole === "phone"
        ? "open phone cams"
        : "open laptop cam";
    const short = on ? "on" : "cams";
    ["gg-cam", "gg-dock-cam"].forEach((id) => {
      const el = document.getElementById(id);
      if (!el) return;
      const l = el.querySelector(".gg-lbl-long");
      const s = el.querySelector(".gg-lbl-short");
      if (l || s) {
        if (l) l.textContent = long;
        if (s) s.textContent = short;
      } else {
        el.textContent = short === "cams" || short === "on" ? short : long;
      }
      el.title =
        deviceRole === "phone"
          ? "Open front/back on THIS phone only · then cast"
          : "Open webcam on THIS laptop (C) · does not start phones";
    });
  }

  function setCastButtonLabel(on) {
    const long = on ? "casting…" : deviceRole === "phone" ? "cast phone" : "cast live";
    const short = on ? "live" : "cast";
    ["gg-cast", "gg-dock-cast"].forEach((id) => {
      const el = document.getElementById(id);
      if (!el) return;
      const l = el.querySelector(".gg-lbl-long");
      const s = el.querySelector(".gg-lbl-short");
      if (l || s) {
        if (l) l.textContent = long;
        if (s) s.textContent = short;
      } else {
        el.textContent = short;
      }
      el.setAttribute("aria-pressed", on ? "true" : "false");
      el.classList.toggle("is-live", !!on);
    });
  }

  function syncDeviceRoleUI() {
    const isPhone = deviceRole === "phone";
    const slot = forcedSlot || (isPhone ? "L1" : "C");
    const rl = document.getElementById("gg-role-laptop");
    const rp = document.getElementById("gg-role-phone");
    if (rl) {
      rl.setAttribute("aria-pressed", isPhone ? "false" : "true");
      rl.classList.toggle("is-on", !isPhone);
    }
    if (rp) {
      rp.setAttribute("aria-pressed", isPhone ? "true" : "false");
      rp.classList.toggle("is-on", isPhone);
    }
    document.querySelectorAll(".gg-slot-btn, .gg-dock-slot").forEach((btn) => {
      const on = btn.getAttribute("data-slot") === slot;
      btn.classList.toggle("is-on", on);
      btn.setAttribute("aria-pressed", on ? "true" : "false");
    });
    // mobile dock role highlights
    document.querySelectorAll(".gg-dock-btn[data-dock='role-phone']").forEach((b) => {
      b.classList.toggle("is-phone-on", isPhone);
      b.classList.toggle("is-on", isPhone);
    });
    document.querySelectorAll(".gg-dock-btn[data-dock='role-laptop']").forEach((b) => {
      b.classList.toggle("is-on", !isPhone);
    });
    const chip = document.getElementById("gg-role-chip");
    if (chip) {
      chip.textContent = (isPhone ? "phone" : "laptop") + " · " + slot;
      chip.classList.toggle("is-phone", isPhone);
      chip.title = isPhone
        ? "Phone cam · seat " + slot + " · open cams then cast"
        : "Laptop · seat " + slot + " · open laptop cam";
    }
    if (!camOn) setCamButtonLabel(false);
    else setCamButtonLabel(true);
    if (!casting) setCastButtonLabel(false);
    else setCastButtonLabel(true);
    const cl = document.getElementById("gg-card-laptop");
    const cp = document.getElementById("gg-card-phone");
    if (cl) cl.classList.toggle("is-on", !isPhone);
    if (cp) cp.classList.toggle("is-on", isPhone);
    const dl = document.getElementById("gg-device-label");
    if (dl) dl.textContent = isPhone ? "phone" : "laptop";
  }

  function setDeviceRole(role, opts) {
    opts = opts || {};
    deviceRole = role === "phone" ? "phone" : "laptop";
    if (deviceRole === "laptop") {
      if (!forcedSlot || opts.resetSlot) forcedSlot = "C";
    } else {
      if (!forcedSlot || forcedSlot === "C" || opts.resetSlot) forcedSlot = "L1";
    }
    // reflect in URL without reload
    try {
      const u = new URL(location.href);
      u.searchParams.set("role", deviceRole);
      u.searchParams.set("slot", forcedSlot);
      history.replaceState(null, "", u.pathname + u.search + u.hash);
    } catch {
      /* ignore */
    }
    syncDeviceRoleUI();
    if (els.hubHint) {
      els.hubHint.textContent =
        deviceRole === "phone"
          ? "Phone mode · seat " +
            forcedSlot +
            ". Tap open phone cams, then cast phone. Laptop is separate — it won’t open this camera."
          : "Laptop mode · seat " +
            forcedSlot +
            " (center). Tap open laptop cam for webcam. Phones open their own page with seat L1/R1.";
    }
    setCastLabel(deviceRole + " · " + forcedSlot);
    // re-open cams with new slot preference if already on
    if (camOn && opts.reopen) {
      enableCam({ all: true, force: true });
    } else {
      layoutAndPaint();
    }
  }

  function setSceneSlot(slot, opts) {
    opts = opts || {};
    slot = String(slot || "C").toUpperCase();
    if (SCENE_SLOTS.indexOf(slot) < 0) return;
    forcedSlot = slot;
    if (slot === "C") deviceRole = "laptop";
    else if (deviceRole === "laptop" && slot !== "C") {
      // choosing L/R on a desk browser still marks as satellite
      deviceRole = isMobileUA() ? "phone" : "laptop";
    }
    try {
      const u = new URL(location.href);
      u.searchParams.set("slot", forcedSlot);
      u.searchParams.set("role", deviceRole);
      history.replaceState(null, "", u.pathname + u.search + u.hash);
    } catch {
      /* ignore */
    }
    syncDeviceRoleUI();
    setCastLabel((deviceRole === "phone" ? "phone" : "laptop") + " · seat " + slot);
    if (camOn && opts.reopen !== false) {
      enableCam({ all: true, force: true });
    } else {
      layoutAndPaint();
    }
  }

  initPeers();
  wire();
  wireExtra();
  syncDeviceRoleUI();
  if (els.roomId) els.roomId.value = meshRoom;
  if (els.roomLabel) els.roomLabel.textContent = meshRoom;

  // Device role: laptop vs phone
  const roleLaptop = document.getElementById("gg-role-laptop");
  const rolePhone = document.getElementById("gg-role-phone");
  if (roleLaptop) roleLaptop.addEventListener("click", () => setDeviceRole("laptop", { resetSlot: true }));
  if (rolePhone) rolePhone.addEventListener("click", () => setDeviceRole("phone", { resetSlot: true }));
  const cardL = document.getElementById("gg-card-laptop");
  const cardP = document.getElementById("gg-card-phone");
  if (cardL) cardL.addEventListener("click", () => setDeviceRole("laptop", { resetSlot: true }));
  if (cardP) cardP.addEventListener("click", () => setDeviceRole("phone", { resetSlot: true }));

  // Live link · phone ↔ laptop (same Wi‑Fi hub)
  const pairConnect = document.getElementById("gg-pair-connect");
  const pairQrBtn = document.getElementById("gg-pair-qr-btn");
  const pairCopy = document.getElementById("gg-pair-copy");
  const pairFilm = document.getElementById("gg-pair-film");
  if (pairConnect) {
    pairConnect.addEventListener("click", () => {
      if (ws && ws.readyState === WebSocket.OPEN) disconnectHub();
      else connectHub();
    });
  }
  if (pairQrBtn) {
    pairQrBtn.addEventListener("click", () => {
      const wrap = document.getElementById("gg-pair-qr-wrap");
      if (wrap && !wrap.hidden) {
        wrap.hidden = true;
        pairQrBtn.textContent = "Show phone QR";
        return;
      }
      refreshPairUI();
      const url = buildPhonePairURL("L1");
      renderPairQR(url);
      if (pairQrBtn) pairQrBtn.textContent = "Hide QR";
      setDrawer(true);
    });
  }
  if (pairCopy) {
    pairCopy.addEventListener("click", async () => {
      const url = buildPhonePairURL("L1");
      try {
        if (navigator.clipboard && navigator.clipboard.writeText) {
          await navigator.clipboard.writeText(url);
        } else {
          const ta = document.createElement("textarea");
          ta.value = url;
          document.body.appendChild(ta);
          ta.select();
          document.execCommand("copy");
          ta.remove();
        }
        updatePairStatus("copied phone link · paste on phone browser");
        setCastLabel("link copied");
      } catch {
        updatePairStatus(url);
      }
    });
  }
  if (pairFilm) {
    pairFilm.addEventListener("click", () => {
      setMeshRoom("film", { reconnect: true });
      refreshPairUI();
      updatePairStatus("room = film · reconnect both devices");
    });
  }
  // Discover LAN + auto-link when served by gy hub
  fetchLanPair().then(() => {
    const onHubHost =
      location.protocol !== "file:" &&
      !(location.hostname || "").includes("github.io") &&
      (location.port === "9876" || wantAutoHub);
    if (onHubHost || wantAutoHub) {
      // slight delay so UI settles
      setTimeout(() => {
        if (!ws || ws.readyState !== WebSocket.OPEN) connectHub();
        // laptop: show phone QR for live test
        if (deviceRole === "laptop" && !isMobileUA()) {
          const wrap = document.getElementById("gg-pair-qr-wrap");
          if (wrap) {
            renderPairQR(buildPhonePairURL("L1"));
            if (pairQrBtn) pairQrBtn.textContent = "Hide QR";
          }
        }
      }, 180);
    }
  });

  // Scene seat segment (header + mobile dock)
  document.querySelectorAll(".gg-slot-btn, .gg-dock-slot").forEach((btn) => {
    btn.addEventListener("click", () => {
      setSceneSlot(btn.getAttribute("data-slot"), { reopen: camOn });
    });
  });

  // Mobile sticky dock — always on-screen primary actions
  const mobileDock = document.getElementById("gg-mobile-dock");
  if (mobileDock) {
    mobileDock.addEventListener("click", (e) => {
      const t = e.target.closest("[data-dock], .gg-dock-slot");
      if (!t || !mobileDock.contains(t)) return;
      if (t.classList.contains("gg-dock-slot")) return; // handled above
      const act = t.getAttribute("data-dock");
      if (act === "role-phone") setDeviceRole("phone", { resetSlot: true });
      else if (act === "role-laptop") setDeviceRole("laptop", { resetSlot: true });
      else if (act === "cam") toggleCam();
      else if (act === "flip") flipLens();
      else if (act === "cast") {
        if (typeof toggleCast === "function") toggleCast();
        else if (els.castBtn) els.castBtn.click();
      } else if (act === "hub") {
        if (typeof toggleHub === "function") toggleHub();
        else if (els.hubBtn) els.hubBtn.click();
      } else if (act === "more") {
        setDrawer(true);
      }
    });
  }

  // Scene layout toggle (stage order)
  const sceneBtn = document.getElementById("gg-scene");
  if (sceneBtn) {
    sceneBtn.setAttribute("aria-pressed", sceneMode ? "true" : "false");
    sceneBtn.classList.toggle("is-on", sceneMode);
    sceneBtn.addEventListener("click", () => {
      sceneMode = !sceneMode;
      sceneBtn.setAttribute("aria-pressed", sceneMode ? "true" : "false");
      sceneBtn.classList.toggle("is-on", sceneMode);
      setCastLabel(sceneMode ? "layout L2·L1·C·R1·R2" : "layout free");
      if (els.hubHint) {
        els.hubHint.textContent = sceneMode
          ? "Stage ordered L2·L1·[laptop C]·R1·R2. Set device role + seat, then open cams on each machine."
          : "Free layout (self first).";
      }
      layoutAndPaint();
    });
  }

  // HDRI hurdle hop + Three.js sphere view
  let lastHdri = null;
  let hdriMiniViewer = null;

  function stashHdriForView(result) {
    if (!result || !result.equirect || !window.GY_HDRI_VIEW) return false;
    return window.GY_HDRI_VIEW.stashEquirect(result.equirect, {
      from: myNick() + "-hdri",
      slots: result.slots || [],
      w: result.equirect.width,
      h: result.equirect.height,
      t: result.t || Date.now(),
      quality: 0.8,
    });
  }

  function mountHdriMini(result) {
    const mini = document.getElementById("gg-hdri-mini");
    if (!mini || !result || !result.equirect || !window.GY_HDRI_VIEW) return;
    mini.hidden = false;
    const boot = () => {
      if (hdriMiniViewer) {
        hdriMiniViewer.setMap(result.equirect).catch(function () {});
        return;
      }
      window.GY_HDRI_VIEW.createViewer(mini, { mode: "outside" })
        .then(function (v) {
          hdriMiniViewer = v;
          return v.setMap(result.equirect);
        })
        .catch(function (e) {
          console.warn("[gg] hdri mini", e);
          mini.hidden = true;
        });
    };
    boot();
  }

  function showHdriPanel(result) {
    const panel = document.getElementById("gg-hdri-panel");
    if (!panel || !result || !result.ok) return;
    panel.hidden = false;
    const meta = document.getElementById("gg-hdri-meta");
    if (meta) {
      meta.textContent =
        (result.slots || []).join("·") +
        (result.equirect
          ? " · " + result.equirect.width + "×" + result.equirect.height
          : "");
    }
    const stripC = document.getElementById("gg-hdri-strip");
    const eqC = document.getElementById("gg-hdri-eq");
    if (stripC && result.strip) {
      stripC.width = result.strip.width;
      stripC.height = result.strip.height;
      stripC.getContext("2d").drawImage(result.strip, 0, 0);
    }
    if (eqC && result.equirect) {
      eqC.width = result.equirect.width;
      eqC.height = result.equirect.height;
      eqC.getContext("2d").drawImage(result.equirect, 0, 0);
    }
    stashHdriForView(result);
    mountHdriMini(result);
  }
  function runHdriProbe(opts) {
    opts = opts || {};
    if (!window.GY_HDRI) {
      setCastLabel("hdri js missing");
      return;
    }
    if (!camLanes.length) {
      setCastLabel("cam first");
      if (els.hubHint) {
        els.hubHint.textContent =
          "Open cam (laptop C + phones L/R), then HDRI. Multi-device: phones ?slot=L1 / R1 + Cast so RX fills strip.";
      }
      return;
    }
    // Build lanes from local cams + live RX peers with scene slots
    const lanes = camLanes.map((l) => ({
      slot: l.slot,
      video: l.video,
      short: l.short,
      kind: l.kind,
      label: l.label,
    }));
    peers.forEach((p) => {
      if (p.selfCam || p.self) return;
      if (p.source !== "rx" || !p.sceneSlot) return;
      // RX peers: use last glyph canvas if present
      const c = canvasById.get(p.id);
      if (!c) return;
      lanes.push({
        slot: p.sceneSlot,
        canvas: c,
        short: p.sceneSlot,
        kind: p.camKind || "rx",
        label: p.nick,
      });
    });
    setCastLabel("HDRI stitch…");
    const result = window.GY_HDRI.runProbe(lanes, {
      download: !!opts.download,
      postHub: true,
      sendMesh: opts.cast !== false ? sendJSON : null,
      from: myNick() + "-hdri",
      glyphN: glyphN,
      tonemap: true,
    });
    lastHdri = result;
    if (!result.ok) {
      setCastLabel(result.error || "hdri fail");
      return;
    }
    showHdriPanel(result);
    setCastLabel("HDRI · " + (result.slots || []).join("·"));
    if (els.hubHint) {
      els.hubHint.textContent =
        "HDRI probe ready — mini sphere + view 3D (Three.js). Cast sphere paints venue LEDs. Not multi-bracket VFX HDRI.";
    }
  }
  const hdriBtn = document.getElementById("gg-hdri");
  if (hdriBtn) {
    hdriBtn.addEventListener("click", () => runHdriProbe({ cast: true }));
  }
  const hdriClose = document.getElementById("gg-hdri-close");
  if (hdriClose) {
    hdriClose.addEventListener("click", () => {
      const panel = document.getElementById("gg-hdri-panel");
      if (panel) panel.hidden = true;
      if (hdriMiniViewer) {
        try {
          hdriMiniViewer.dispose();
        } catch (_) {}
        hdriMiniViewer = null;
      }
    });
  }
  const hdriDlEq = document.getElementById("gg-hdri-dl-eq");
  if (hdriDlEq) {
    hdriDlEq.addEventListener("click", () => {
      if (lastHdri && lastHdri.equirect && window.GY_HDRI) {
        window.GY_HDRI.downloadCanvas(lastHdri.equirect, "gy-hdri-probe.png");
      }
    });
  }
  const hdriDlStrip = document.getElementById("gg-hdri-dl-strip");
  if (hdriDlStrip) {
    hdriDlStrip.addEventListener("click", () => {
      if (lastHdri && lastHdri.strip && window.GY_HDRI) {
        window.GY_HDRI.downloadCanvas(lastHdri.strip, "gy-subject-strip.png");
      }
    });
  }
  const hdriView3d = document.getElementById("gg-hdri-view3d");
  if (hdriView3d) {
    hdriView3d.addEventListener("click", () => {
      if (!lastHdri || !lastHdri.ok || !lastHdri.equirect) {
        runHdriProbe({ cast: false });
        if (!lastHdri || !lastHdri.ok) return;
      }
      stashHdriForView(lastHdri);
      if (window.GY_HDRI_VIEW) {
        window.GY_HDRI_VIEW.openViewerPage({ mode: "outside" });
        setCastLabel("HDRI → 3D view");
      } else {
        window.open("hdri-view.html", "_blank", "noopener");
      }
    });
  }
  const hdriCast = document.getElementById("gg-hdri-cast");
  if (hdriCast) {
    hdriCast.addEventListener("click", () => {
      if (!lastHdri || !lastHdri.ok) {
        runHdriProbe({ cast: true });
        return;
      }
      stashHdriForView(lastHdri);
      if (lastHdri.glyph) {
        sendJSON({
          type: "vburst-frame",
          from: myNick() + "-hdri",
          glyph: lastHdri.glyph,
          glyphN: glyphN,
          cast: "sphere",
          project: true,
          t: Date.now(),
          cam: { kind: "hdri", slot: "EQ" },
        });
      }
      if (lastHdri.equirect) {
        const jpeg = lastHdri.equirect.toDataURL("image/jpeg", 0.72);
        sendJSON({
          type: "hdri-probe",
          from: myNick() + "-hdri",
          slots: lastHdri.slots,
          w: lastHdri.equirect.width,
          h: lastHdri.equirect.height,
          b64: jpeg.split(",")[1] || "",
          fmt: "jpeg",
          glyph: lastHdri.glyph,
          glyphN: glyphN,
          cast: "sphere",
          t: Date.now(),
        });
      }
      setCastLabel("HDRI → sphere");
      // also open venue sphere so cast is visible (same origin session stash)
      try {
        window.open("sphere.html?hdri=1", "_blank", "noopener");
      } catch (_) {}
    });
  }
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
