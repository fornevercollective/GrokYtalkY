/**
 * Phone → terminal cast on same Wi‑Fi.
 * Opens hub at location.host (when served by gy hub) or typed WS URL.
 * TX: vburst-frame (glyph lattice + optional jpeg) + gyst hexlum dual-publish.
 */
(function () {
  "use strict";

  const GLYPH_N = 25;
  const CAST_MS = 100; // ~10 fps mesh — light for phone + terminal
  const STORAGE = "gy.phone.v1";

  const els = {
    status: document.getElementById("ph-status"),
    video: document.getElementById("ph-video"),
    sample: document.getElementById("ph-sample"),
    glyph: document.getElementById("ph-glyph"),
    cast: document.getElementById("ph-cast"),
    cam: document.getElementById("ph-cam"),
    hub: document.getElementById("ph-hub"),
    look: document.getElementById("ph-look"),
    camPanel: document.getElementById("ph-cam-panel"),
    nick: document.getElementById("ph-nick"),
    hubUrl: document.getElementById("ph-hub-url"),
    quick: document.getElementById("ph-quick"),
    qrWrap: document.getElementById("ph-qr-wrap"),
    qr: document.getElementById("ph-qr"),
    showQr: document.getElementById("ph-show-qr"),
    copyUrl: document.getElementById("ph-copy-url"),
    quickCopy: document.getElementById("ph-quick-copy"),
  };

  let phonePageURL = "";
  let lanInfo = null;

  const CAM = window.GY_CAMERA;
  let look = CAM ? CAM.clone(CAM.DEFAULTS) : {};
  look.facing = "user";

  const sampleCtx = els.sample ? els.sample.getContext("2d", { willReadFrequently: true }) : null;
  const glyphCtx = els.glyph ? els.glyph.getContext("2d") : null;
  if (sampleCtx) sampleCtx.imageSmoothingEnabled = true;
  if (glyphCtx) glyphCtx.imageSmoothingEnabled = false;

  let ws = null;
  let camOn = false;
  let casting = false;
  let castLock = false; // tap-to-lock continuous cast
  let mediaStream = null;
  let castTimer = 0;
  let seq = 0;
  let jpegCanvas = null;
  let jpegCtx = null;

  function setStatus(t, cls) {
    if (!els.status) return;
    els.status.textContent = t || "";
    els.status.classList.remove("is-live", "is-err");
    if (cls) els.status.classList.add(cls);
  }

  function myNick() {
    return ((els.nick && els.nick.value) || "phone").trim().slice(0, 16) || "phone";
  }

  function defaultHubWS() {
    // When this page is served by the hub, same host is the mesh.
    const host = location.host || "127.0.0.1:9876";
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    if (location.protocol === "file:") {
      return "ws://127.0.0.1:9876/";
    }
    // github pages can't reach LAN hub
    if (host.includes("github.io")) {
      return "ws://";
    }
    return proto + "//" + host + "/";
  }

  function loadState() {
    try {
      const raw = localStorage.getItem(STORAGE);
      if (!raw) return;
      const st = JSON.parse(raw);
      if (st.nick && els.nick) els.nick.value = st.nick;
      if (st.hubUrl && els.hubUrl) els.hubUrl.value = st.hubUrl;
    } catch {
      /* ignore */
    }
  }

  function saveState() {
    try {
      localStorage.setItem(
        STORAGE,
        JSON.stringify({
          nick: myNick(),
          hubUrl: els.hubUrl ? els.hubUrl.value.trim() : "",
        })
      );
    } catch {
      /* ignore */
    }
  }

  function hubWS() {
    let u = (els.hubUrl && els.hubUrl.value.trim()) || defaultHubWS();
    if (!u) u = defaultHubWS();
    if (!/^wss?:\/\//i.test(u)) {
      u = "ws://" + u.replace(/^\/\//, "");
    }
    if (!u.endsWith("/") && !u.includes("?")) u += "/";
    const nick = encodeURIComponent(myNick());
    const sep = u.includes("?") ? "&" : "?";
    // role=phone for roster clarity
    if (!/[?&]nick=/.test(u)) u += sep + "nick=" + nick + "&role=phone";
    else if (!/[?&]role=/.test(u)) u += "&role=phone";
    return u;
  }

  function apiBase() {
    if (location.protocol === "file:") return "http://127.0.0.1:9876";
    return location.origin || "http://127.0.0.1:9876";
  }

  function isLikelyDesktop() {
    // coarse: wide viewport or no touch — used to auto-show QR for laptop→phone scan
    const touch = navigator.maxTouchPoints > 0 || "ontouchstart" in window;
    return !touch || (window.innerWidth >= 900 && window.innerHeight >= 600);
  }

  function loadQR(url) {
    if (!els.qr || !url) return;
    const src = url + (url.includes("?") ? "&" : "?") + "t=" + Date.now();
    els.qr.src = src;
    els.qr.onload = () => {
      if (els.qrWrap) els.qrWrap.classList.add("is-show");
    };
    els.qr.onerror = () => {
      if (els.qrWrap) els.qrWrap.classList.remove("is-show");
    };
  }

  function showQR(force) {
    const qrURL =
      (lanInfo && lanInfo.qr) ||
      apiBase() + "/api/lan/qr";
    if (force || isLikelyDesktop()) {
      loadQR(qrURL);
      if (els.showQr) els.showQr.textContent = "Hide QR";
    }
  }

  function hideQR() {
    if (els.qrWrap) els.qrWrap.classList.remove("is-show");
    if (els.qr) els.qr.removeAttribute("src");
    if (els.showQr) els.showQr.textContent = "Show QR";
  }

  async function copyPhoneURL() {
    const u = phonePageURL || (lanInfo && lanInfo.phone) || location.href;
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(u);
      } else {
        const ta = document.createElement("textarea");
        ta.value = u;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        ta.remove();
      }
      setStatus("copied · " + u, "is-live");
    } catch (e) {
      setStatus("copy failed · " + u, "is-err");
    }
  }

  async function fetchLanHint() {
    // if page is on hub HTTP, /api/lan confirms + fills WS + QR
    try {
      const base = apiBase();
      const res = await fetch(base + "/api/lan", { headers: { Accept: "application/json" } });
      if (!res.ok) return;
      const info = await res.json();
      lanInfo = info;
      if (info && info.ws && els.hubUrl && !els.hubUrl.value.trim()) {
        els.hubUrl.value = info.ws;
      }
      if (info && info.phone) {
        phonePageURL = info.phone;
        setStatus("LAN hub · " + (info.ws || "") + " · phone: " + info.phone, "is-live");
      }
      // auto-show QR on laptop so phone can scan
      if (isLikelyDesktop() && (info.qr || true)) {
        showQR(true);
      }
    } catch {
      /* offline / wrong host — still try relative QR when served by hub */
      if (location.protocol !== "file:" && !location.host.includes("github.io") && isLikelyDesktop()) {
        showQR(true);
      }
    }
  }

  /** One-tap: connect hub + enable camera (cast still hold/lock). */
  async function quickConnect() {
    saveState();
    if (els.quick) {
      els.quick.classList.add("is-on");
      els.quick.textContent = "Connecting…";
    }
    setStatus("quick connect · hub + camera…");
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      connectHub();
    }
    // wait briefly for WS open
    const deadline = Date.now() + 2500;
    while ((!ws || ws.readyState !== WebSocket.OPEN) && Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 80));
    }
    const camOk = await enableCam();
    const hubOk = ws && ws.readyState === WebSocket.OPEN;
    if (els.quick) {
      els.quick.classList.toggle("is-on", !!(hubOk && camOk));
      els.quick.textContent =
        hubOk && camOk
          ? "✓ Connected · hold Cast"
          : hubOk
            ? "Hub ok · camera failed"
            : camOk
              ? "Camera ok · hub failed"
              : "Retry quick connect";
    }
    if (hubOk && camOk) {
      setStatus("quick connect ready · hold Cast (or double-tap lock)", "is-live");
    } else if (!hubOk) {
      setStatus("hub not ready · same Wi‑Fi + gy serve --bind 0.0.0.0", "is-err");
    }
  }

  function connectHub() {
    saveState();
    if (ws) {
      try {
        ws.close();
      } catch {
        /* */
      }
      ws = null;
    }
    const url = hubWS();
    setStatus("connecting " + url + "…");
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setStatus("WS error · " + e, "is-err");
      return;
    }
    ws.onopen = () => {
      setStatus("hub connected · " + myNick(), "is-live");
      if (els.hub) els.hub.classList.add("is-on");
      // capability-ish join (hub also uses query nick)
      sendJSON({
        type: "join",
        nick: myNick(),
        role: "phone",
        cap: {
          class: "glyph-iot",
          role: "phone",
          glyph_n: GLYPH_N,
          lanes: ["glyph", "hex", "audio"],
          bp: 16,
        },
      });
    };
    ws.onclose = () => {
      setStatus("hub closed · tap Connect", "is-err");
      if (els.hub) els.hub.classList.remove("is-on");
      ws = null;
    };
    ws.onerror = () => setStatus("hub error · check same Wi‑Fi + gy serve --bind 0.0.0.0", "is-err");
    ws.onmessage = () => {
      /* phone is primarily TX; ignore roster noise */
    };
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

  function onLookChange(l, key) {
    look = CAM ? CAM.clone(l) : l;
    // hardware constraints when track live
    if (mediaStream && CAM && CAM.applyHardware) {
      const track = mediaStream.getVideoTracks()[0];
      if (track) {
        CAM.applyHardware(track, look).then(function (r) {
          if (r && r.applied && r.applied.length) {
            setStatus("look · hw " + r.applied.join(",") + " · " + CAM.summary(look), "is-live");
          }
        });
      }
    }
    // mesh fan-out
    if (CAM) sendJSON(CAM.meshMessage(look, myNick()));
    if (els.look) els.look.textContent = "Look · " + (CAM ? CAM.summary(look) : "on");
  }

  function toggleLookPanel() {
    if (!els.camPanel || !CAM) return;
    const hide = !els.camPanel.hidden;
    els.camPanel.hidden = hide;
    if (!hide) {
      CAM.mountPanel(els.camPanel, look, onLookChange);
    }
  }

  async function enableCam() {
    if (camOn && mediaStream) return true;
    try {
      mediaStream = await navigator.mediaDevices.getUserMedia({
        video: {
          facingMode: look.facing || "user",
          width: { ideal: 480 },
          height: { ideal: 640 },
        },
        audio: false, // voice via separate path later; keep video light
      });
      if (els.video) {
        els.video.srcObject = mediaStream;
        els.video.muted = true;
        els.video.playsInline = true;
        await els.video.play().catch(() => {});
      }
      camOn = true;
      if (els.cam) els.cam.classList.add("is-on");
      // apply hardware look constraints if panel was used
      if (CAM && mediaStream) {
        const track = mediaStream.getVideoTracks()[0];
        if (track) CAM.applyHardware(track, look);
      }
      setStatus("camera on · hold Cast · Look for lighting", "is-live");
      return true;
    } catch (e) {
      setStatus("camera denied · " + (e && e.message ? e.message : e), "is-err");
      return false;
    }
  }

  function stopCam() {
    if (mediaStream) {
      mediaStream.getTracks().forEach((t) => t.stop());
      mediaStream = null;
    }
    if (els.video) els.video.srcObject = null;
    camOn = false;
    if (els.cam) els.cam.classList.remove("is-on");
  }

  function sampleGlyph() {
    if (!sampleCtx || !els.sample || !els.video) return null;
    if (els.video.readyState < 2) return null;
    const n = GLYPH_N;
    if (els.sample.width !== n) els.sample.width = n;
    if (els.sample.height !== n) els.sample.height = n;
    // center-crop square from portrait camera
    const vw = els.video.videoWidth || 1;
    const vh = els.video.videoHeight || 1;
    const side = Math.min(vw, vh);
    const sx = Math.floor((vw - side) / 2);
    const sy = Math.floor((vh - side) * 0.2); // slightly upper for faces
    sampleCtx.drawImage(els.video, sx, sy, side, side, 0, 0, n, n);
    let img;
    try {
      img = sampleCtx.getImageData(0, 0, n, n);
    } catch {
      return null;
    }
    // phone/film lighting grade (aito-aligned)
    if (CAM && CAM.applyLookToImageData) {
      CAM.applyLookToImageData(img, look);
      sampleCtx.putImageData(img, 0, 0);
    }
    const d = img.data;
    const glyph = new Array(n * n);
    for (let i = 0, g = 0; i < d.length; i += 4, g++) {
      const L = 0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2];
      glyph[g] = Math.max(0, Math.min(255, (Math.pow(L / 255, 0.85) * 255) | 0));
    }
    // draw preview
    if (glyphCtx && els.glyph) {
      if (els.glyph.width !== n) els.glyph.width = n;
      if (els.glyph.height !== n) els.glyph.height = n;
      const gimg = glyphCtx.createImageData(n, n);
      for (let i = 0; i < n * n; i++) {
        const L = glyph[i];
        const o = i * 4;
        gimg.data[o] = L;
        gimg.data[o + 1] = Math.min(255, L + 20);
        gimg.data[o + 2] = Math.min(255, L + 40);
        gimg.data[o + 3] = 255;
      }
      glyphCtx.putImageData(gimg, 0, 0);
    }
    return glyph;
  }

  function tinyJpegDataURL() {
    if (!els.video || els.video.readyState < 2) return "";
    if (!jpegCanvas) {
      jpegCanvas = document.createElement("canvas");
      jpegCanvas.width = 96;
      jpegCanvas.height = 96;
      jpegCtx = jpegCanvas.getContext("2d");
    }
    if (!jpegCtx) return "";
    const vw = els.video.videoWidth || 1;
    const vh = els.video.videoHeight || 1;
    const side = Math.min(vw, vh);
    const sx = Math.floor((vw - side) / 2);
    const sy = Math.floor((vh - side) * 0.2);
    jpegCtx.drawImage(els.video, sx, sy, side, side, 0, 0, 96, 96);
    if (CAM && CAM.applyLookToImageData) {
      try {
        const img = jpegCtx.getImageData(0, 0, 96, 96);
        CAM.applyLookToImageData(img, look);
        jpegCtx.putImageData(img, 0, 0);
      } catch (_) {}
    }
    try {
      const url = jpegCanvas.toDataURL("image/jpeg", 0.55);
      const i = url.indexOf(",");
      return i >= 0 ? url.slice(i + 1) : "";
    } catch {
      return "";
    }
  }

  function castOnce() {
    if (!casting) return;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      setStatus("not connected · Connect hub", "is-err");
      return;
    }
    const glyph = sampleGlyph();
    if (!glyph) return;
    seq++;
    const nick = myNick();
    const t = Date.now();
    const b64 = tinyJpegDataURL();

    // burst frame for dual Glyph / phone peers
    const burst = {
      type: "vburst-frame",
      from: nick,
      glyph: glyph,
      glyphN: GLYPH_N,
      w: b64 ? 96 : GLYPH_N,
      h: b64 ? 96 : GLYPH_N,
      t: t,
      via: "phone-cast",
    };
    if (b64) {
      burst.fmt = "jpeg";
      burst.b64 = b64;
    } else {
      burst.fmt = "hexlum";
    }
    if (CAM) burst.look = CAM.clone(look);
    sendJSON(burst);

    // formal gyst hexlum for stream-pub / SFU / terminal hex style
    sendJSON({
      type: "gyst",
      from: nick,
      kind: "hexlum",
      w: GLYPH_N,
      h: GLYPH_N,
      seq: seq,
      t: t,
      data: glyph,
      glyphN: GLYPH_N,
      lane: "hex",
      via: "phone-cast",
    });

    if (seq % 10 === 0) {
      setStatus("casting · seq " + seq + " · " + GLYPH_N + "² hexlum", "is-live");
    }
  }

  function startCast() {
    if (casting) return;
    casting = true;
    if (els.cast) {
      els.cast.classList.add("is-on", "danger");
      els.cast.setAttribute("aria-pressed", "true");
      els.cast.textContent = castLock ? "Casting… tap to stop" : "Casting… release to stop";
    }
    sendJSON({ type: "vburst-start", from: myNick(), t: Date.now() });
    sendJSON({ type: "ptt", state: "down", from: myNick() });
    castOnce();
    castTimer = window.setInterval(castOnce, CAST_MS);
  }

  function stopCast() {
    if (!casting) return;
    casting = false;
    if (castTimer) {
      clearInterval(castTimer);
      castTimer = 0;
    }
    sendJSON({ type: "vburst-end", from: myNick(), t: Date.now() });
    sendJSON({ type: "ptt", state: "up", from: myNick() });
    if (els.cast) {
      els.cast.classList.remove("is-on", "danger");
      els.cast.setAttribute("aria-pressed", "false");
      els.cast.textContent = "Hold to cast · or tap lock";
    }
    setStatus("cast stopped · hub ready", "is-live");
  }

  function bindCastButton() {
    if (!els.cast) return;
    // hold-to-talk style
    const down = async (e) => {
      e.preventDefault();
      if (castLock && casting) {
        castLock = false;
        stopCast();
        return;
      }
      if (!camOn) {
        const ok = await enableCam();
        if (!ok) return;
      }
      if (!ws || ws.readyState !== WebSocket.OPEN) connectHub();
      // short delay for WS
      setTimeout(() => startCast(), ws && ws.readyState === WebSocket.OPEN ? 0 : 300);
    };
    const up = (e) => {
      e.preventDefault();
      if (!castLock) stopCast();
    };
    els.cast.addEventListener("pointerdown", down);
    els.cast.addEventListener("pointerup", up);
    els.cast.addEventListener("pointerleave", up);
    els.cast.addEventListener("pointercancel", up);
    // double-tap / long-press alternative: second click locks
    let lastTap = 0;
    els.cast.addEventListener("click", (e) => {
      const now = Date.now();
      if (now - lastTap < 350) {
        castLock = true;
        if (!casting) down(e);
        if (els.cast) els.cast.textContent = "Casting locked · tap to stop";
      }
      lastTap = now;
    });
  }

  function init() {
    loadState();
    if (els.hubUrl && !els.hubUrl.value) {
      els.hubUrl.value = defaultHubWS();
    }
    if (els.nick && !els.nick.value) {
      els.nick.value = "phone";
    }
    phonePageURL = location.protocol === "file:" ? "" : location.href.split("#")[0];
    fetchLanHint();
    if (els.quick) els.quick.addEventListener("click", () => quickConnect());
    if (els.showQr) {
      els.showQr.addEventListener("click", () => {
        if (els.qrWrap && els.qrWrap.classList.contains("is-show")) hideQR();
        else showQR(true);
      });
    }
    if (els.copyUrl) els.copyUrl.addEventListener("click", () => copyPhoneURL());
    if (els.cam) els.cam.addEventListener("click", () => (camOn ? stopCam() : enableCam()));
    if (els.look) els.look.addEventListener("click", toggleLookPanel);
    if (els.hub) els.hub.addEventListener("click", () => connectHub());
    bindCastButton();
    // auto-connect when served from hub (phone already on LAN page)
    // ?quick=1|connect=1 → full quick connect (hub + cam)
    const q = new URLSearchParams(location.search);
    const wantQuick = q.get("quick") === "1" || q.get("connect") === "1" || q.get("qc") === "1";
    if (location.protocol !== "file:" && !location.host.includes("github.io")) {
      if (wantQuick) {
        // slight delay so LAN hint can fill WS
        setTimeout(() => quickConnect(), 120);
      } else {
        connectHub();
      }
    }
    setStatus(
      (els.hubUrl && els.hubUrl.value) || defaultHubWS() + " · same Wi‑Fi as laptop running gy serve",
      ""
    );
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
