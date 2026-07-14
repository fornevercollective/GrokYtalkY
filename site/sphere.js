/**
 * Sphere Glyph — full dome, color wave, 16K addressable venue canvas.
 * L0–L5: blueprint · targets · index · bulk · cast · screen LED.
 * Click any spot → LED/seat target. Bulk activate sections/chunks/zones.
 */
(function () {
  "use strict";

  function bootMsg(t, err) {
    var b = document.getElementById("sp-boot");
    var m = document.getElementById("sp-boot-msg");
    if (m) m.textContent = t || "";
    if (b) {
      if (err) b.classList.add("is-err");
      if (t === "" || t === "done") b.classList.add("is-done");
    }
  }

  const SPHERE = window.GY_SPHERE;
  const VENUE = window.GY_VENUE;
  const LIGHT = window.GY_LIGHT;
  if (!SPHERE || !VENUE) {
    console.error("sphere-seating.js + venue-canvas.js required");
    bootMsg("Missing sphere modules — hard refresh (Cmd+Shift+R)", true);
    return;
  }

  try {
    bootMain();
  } catch (e) {
    console.error("[sphere] boot", e);
    bootMsg("Sphere failed: " + (e && e.message ? e.message : e), true);
  }

  function bootMain() {
  bootMsg("Building venue map…");

  const STORAGE = "gy.sphere.v2";
  const FEED_TTL_MS = 4500;
  const MAX_HUD_GLYPHS = 48;
  const SHELL_AZ = 140;
  const SHELL_EL = 80;
  const RES = VENUE.RES || 16000;

  const el = {
    gl: document.getElementById("sp-gl"),
    hud: document.getElementById("sp-hud"),
    status: document.getElementById("sp-status"),
    legend: document.getElementById("sp-legend"),
    feeds: document.getElementById("sp-feeds"),
    pick: document.getElementById("sp-pick"),
    hub: document.getElementById("sp-hub"),
    connect: document.getElementById("sp-connect"),
    demo: document.getElementById("sp-demo"),
    clear: document.getElementById("sp-clear"),
    wave: document.getElementById("sp-wave"),
    waveMode: document.getElementById("sp-wave-mode"),
    waveSpeed: document.getElementById("sp-wave-speed"),
    bulkKind: document.getElementById("sp-bulk-kind"),
    bulkVal: document.getElementById("sp-bulk-val"),
    bulk: document.getElementById("sp-bulk"),
    bulkCast: document.getElementById("sp-bulk-cast"),
    bulkClear: document.getElementById("sp-bulk-clear"),
    view: document.getElementById("sp-view"),
    lights: document.getElementById("sp-lights"),
    lightPanel: document.getElementById("sp-light-panel"),
  };

  // ── venue blueprint (can take ~0.5s — keep boot overlay up) ──
  const ven = VENUE.buildVenue();
  bootMsg("Placing " + ((ven && ven.seats) || "…") + " seats…");
  const seats = SPHERE.seatsCached();
  const Nseats = seats.length;
  const bulkHot = new Set(); // target ids
  let picked = null;
  const lightState = LIGHT ? LIGHT.createState() : null;
  /** free-cam from venue camera views (null = orbit) */
  let freeCam = null;
  /** Walkie mood: idle | tx | rx — drives dome colors (burst-style) */
  let burstMood = "idle";
  let walkie = null;
  let sphereNick = "sphere-" + Math.random().toString(36).slice(2, 6);

  // ── shell points (full dome LED sample) ──
  function buildShell() {
    const pts = [];
    for (let ei = 0; ei < SHELL_EL; ei++) {
      const elFrac = ei / (SHELL_EL - 1);
      const theta = elFrac * Math.PI;
      const sinT = Math.sin(theta);
      const cosT = Math.cos(theta);
      const azN = Math.max(20, Math.round(SHELL_AZ * (0.35 + 0.65 * sinT)));
      for (let ai = 0; ai < azN; ai++) {
        const azFrac = ai / azN;
        const phi = azFrac * Math.PI * 2;
        const R = 3.35;
        pts.push({
          x: Math.sin(theta) * Math.cos(phi) * R,
          y: cosT * R * 0.92 + 0.15,
          z: Math.sin(theta) * Math.sin(phi) * R,
          elFrac: elFrac,
          azFrac: azFrac,
        });
      }
    }
    return pts;
  }
  const shell = buildShell();
  const Nshell = shell.length;

  // infra targets (non-seat) as point cloud
  const infra = ven.targets.filter(function (t) {
    return t.kind !== "seat";
  });
  const Ninfra = infra.length;
  const TOTAL = Nseats + Nshell + Ninfra;

  const pX = new Float32Array(TOTAL);
  const pY = new Float32Array(TOTAL);
  const pZ = new Float32Array(TOTAL);
  const pEl = new Float32Array(TOTAL);
  const pAz = new Float32Array(TOTAL);
  const pKind = new Uint8Array(TOTAL); // 0 seat 1 shell 2 infra
  const pSeatIdx = new Int32Array(TOTAL);
  const pTargetId = new Array(TOTAL);

  for (let i = 0; i < Nseats; i++) {
    const s = seats[i];
    pX[i] = s.x * 3;
    pY[i] = s.y * 1.5;
    pZ[i] = s.z * 3;
    pEl[i] = Math.max(0, Math.min(1, 0.5 - s.y * 0.45));
    pAz[i] = (Math.atan2(s.x, s.z) / (Math.PI * 2) + 1) % 1;
    pKind[i] = 0;
    pSeatIdx[i] = i;
    pTargetId[i] = "seat:" + s.id;
  }
  for (let j = 0; j < Nshell; j++) {
    const i = Nseats + j;
    const s = shell[j];
    pX[i] = s.x;
    pY[i] = s.y;
    pZ[i] = s.z;
    pEl[i] = s.elFrac;
    pAz[i] = s.azFrac;
    pKind[i] = 1;
    pSeatIdx[i] = -1;
    pTargetId[i] = null;
  }
  for (let j = 0; j < Ninfra; j++) {
    const i = Nseats + Nshell + j;
    const t = infra[j];
    pX[i] = t.x * 3;
    pY[i] = t.y * 1.5;
    pZ[i] = t.z * 3;
    pEl[i] = Math.max(0, Math.min(1, t.py / RES));
    pAz[i] = Math.max(0, Math.min(1, t.px / RES));
    pKind[i] = 2;
    pSeatIdx[i] = -1;
    pTargetId[i] = t.id;
  }

  // ── camera ──
  let dist = 12;
  let rotX = 0.3;
  let rotY = 0.55;
  let panX = 0;
  let panY = 0.05;
  let dragging = false;
  let lastMX = 0;
  let lastMY = 0;
  let moved = false;

  let waveOn = true;
  let waveMode = "cascade";
  let waveSpeed = 1;
  let waveT0 = performance.now();

  const feeds = new Map();
  const seatLive = new Map();
  const targetLive = new Map(); // targetId → feed
  let ws = null;
  let demoTimer = 0;
  let bulkDemoTimer = 0;

  if (!el.gl) {
    bootMsg("Canvas missing — check sphere.html", true);
    return;
  }
  const gl = el.gl.getContext("webgl", { antialias: true, alpha: false }) ||
    el.gl.getContext("experimental-webgl", { antialias: true, alpha: false });
  if (!gl) {
    bootMsg("WebGL unavailable in this browser", true);
    setStatus("WebGL unavailable", "err");
    return;
  }
  bootMsg("Starting WebGL…");

  const VS =
    "attribute vec3 aPos;attribute vec3 aCol;attribute float aBrt;" +
    "uniform mat4 uMvp;uniform float uSize;varying vec3 vCol;varying float vBrt;" +
    "void main(){vCol=aCol;vBrt=aBrt;gl_Position=uMvp*vec4(aPos,1.0);" +
    "gl_PointSize=max(1.0,uSize*(0.5+aBrt*1.9));}";
  const FS =
    "precision mediump float;varying vec3 vCol;varying float vBrt;" +
    "void main(){vec2 p=gl_PointCoord-vec2(0.5);float d=length(p);if(d>0.5)discard;" +
    "float a=smoothstep(0.5,0.08,d)*(0.2+0.8*vBrt);gl_FragColor=vec4(vCol*(0.25+1.0*vBrt),a);}";

  function compile(type, src) {
    const s = gl.createShader(type);
    gl.shaderSource(s, src);
    gl.compileShader(s);
    return s;
  }
  const prog = gl.createProgram();
  gl.attachShader(prog, compile(gl.VERTEX_SHADER, VS));
  gl.attachShader(prog, compile(gl.FRAGMENT_SHADER, FS));
  gl.linkProgram(prog);
  gl.useProgram(prog);
  const aPos = gl.getAttribLocation(prog, "aPos");
  const aCol = gl.getAttribLocation(prog, "aCol");
  const aBrt = gl.getAttribLocation(prog, "aBrt");
  const uMvp = gl.getUniformLocation(prog, "uMvp");
  const uSize = gl.getUniformLocation(prog, "uSize");

  const pos = new Float32Array(TOTAL * 3);
  const col = new Float32Array(TOTAL * 3);
  const brt = new Float32Array(TOTAL);
  for (let i = 0; i < TOTAL; i++) {
    pos[i * 3] = pX[i];
    pos[i * 3 + 1] = pY[i];
    pos[i * 3 + 2] = pZ[i];
  }
  const bufPos = gl.createBuffer();
  const bufCol = gl.createBuffer();
  const bufBrt = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, bufPos);
  gl.bufferData(gl.ARRAY_BUFFER, pos, gl.STATIC_DRAW);
  gl.bindBuffer(gl.ARRAY_BUFFER, bufCol);
  gl.bufferData(gl.ARRAY_BUFFER, col, gl.DYNAMIC_DRAW);
  gl.bindBuffer(gl.ARRAY_BUFFER, bufBrt);
  gl.bufferData(gl.ARRAY_BUFFER, brt, gl.DYNAMIC_DRAW);
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
  gl.clearColor(0.02, 0.02, 0.035, 1);

  function m4I() {
    return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  }
  function m4M(a, b) {
    const o = new Float32Array(16);
    for (let c = 0; c < 4; c++)
      for (let r = 0; r < 4; r++)
        o[c * 4 + r] =
          a[r] * b[c * 4] + a[4 + r] * b[c * 4 + 1] + a[8 + r] * b[c * 4 + 2] + a[12 + r] * b[c * 4 + 3];
    return o;
  }
  function m4P(fovy, aspect, near, far) {
    const f = 1 / Math.tan(fovy / 2);
    const nf = 1 / (near - far);
    const m = new Float32Array(16);
    m[0] = f / aspect;
    m[5] = f;
    m[10] = (far + near) * nf;
    m[11] = -1;
    m[14] = 2 * far * near * nf;
    return m;
  }
  function m4T(x, y, z) {
    const m = m4I();
    m[12] = x;
    m[13] = y;
    m[14] = z;
    return m;
  }
  function m4RX(a) {
    const c = Math.cos(a),
      s = Math.sin(a);
    const m = m4I();
    m[5] = c;
    m[6] = s;
    m[9] = -s;
    m[10] = c;
    return m;
  }
  function m4RY(a) {
    const c = Math.cos(a),
      s = Math.sin(a);
    const m = m4I();
    m[0] = c;
    m[2] = -s;
    m[8] = s;
    m[10] = c;
    return m;
  }
  function m4LookAt(eye, target, up) {
    up = up || [0, 1, 0];
    let zx = eye[0] - target[0];
    let zy = eye[1] - target[1];
    let zz = eye[2] - target[2];
    let zl = Math.hypot(zx, zy, zz) || 1;
    zx /= zl;
    zy /= zl;
    zz /= zl;
    let xx = up[1] * zz - up[2] * zy;
    let xy = up[2] * zx - up[0] * zz;
    let xz = up[0] * zy - up[1] * zx;
    let xl = Math.hypot(xx, xy, xz) || 1;
    xx /= xl;
    xy /= xl;
    xz /= xl;
    const yx = zy * xz - zz * xy;
    const yy = zz * xx - zx * xz;
    const yz = zx * xy - zy * xx;
    const m = new Float32Array(16);
    m[0] = xx;
    m[1] = yx;
    m[2] = zx;
    m[4] = xy;
    m[5] = yy;
    m[6] = zy;
    m[8] = xz;
    m[9] = yz;
    m[10] = zz;
    m[12] = -(xx * eye[0] + xy * eye[1] + xz * eye[2]);
    m[13] = -(yx * eye[0] + yy * eye[1] + yz * eye[2]);
    m[14] = -(zx * eye[0] + zy * eye[1] + zz * eye[2]);
    m[15] = 1;
    return m;
  }

  function worldToRender(x_m, y_m, z_m) {
    const Rd = 78.6;
    const H = 111.6;
    return [(x_m / Rd) * 3, ((y_m / H - 0.5) * 2) * 1.5, (z_m / Rd) * 3];
  }

  function mvpMatrix(aspect) {
    const fov = freeCam && freeCam.fov ? (freeCam.fov * Math.PI) / 180 : 0.9;
    const proj = m4P(fov, aspect, 0.1, 100);
    if (freeCam && freeCam.eye) {
      const eye = worldToRender(freeCam.eye.x, freeCam.eye.y, freeCam.eye.z);
      const tgt = worldToRender(
        freeCam.lookAt.x,
        freeCam.lookAt.y,
        freeCam.lookAt.z
      );
      return m4M(proj, m4LookAt(eye, tgt, [0, 1, 0]));
    }
    return m4M(proj, m4M(m4T(panX, panY, -dist), m4M(m4RX(rotX), m4RY(rotY))));
  }
  function project(mvp, x, y, z, w, h) {
    const X = mvp[0] * x + mvp[4] * y + mvp[8] * z + mvp[12];
    const Y = mvp[1] * x + mvp[5] * y + mvp[9] * z + mvp[13];
    const W = mvp[3] * x + mvp[7] * y + mvp[11] * z + mvp[15];
    if (W < 0.05) return null;
    return { x: ((X / W) * 0.5 + 0.5) * w, y: ((-Y / W) * 0.5 + 0.5) * h, w: W };
  }
  function hsl(h, s, l) {
    h = ((h % 360) + 360) % 360;
    const c = (1 - Math.abs(2 * l - 1)) * s;
    const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
    const m = l - c / 2;
    let r = 0,
      g = 0,
      b = 0;
    if (h < 60) {
      r = c;
      g = x;
    } else if (h < 120) {
      r = x;
      g = c;
    } else if (h < 180) {
      g = c;
      b = x;
    } else if (h < 240) {
      g = x;
      b = c;
    } else if (h < 300) {
      r = x;
      b = c;
    } else {
      r = c;
      b = x;
    }
    return [r + m, g + m, b + m];
  }

  function waveAt(i, t) {
    const elF = pEl[i];
    const azF = pAz[i];
    // Walkie TX/RX recolors the living dome ball
    let speed = waveSpeed;
    let hueBias = 0;
    let satBoost = 0;
    let glowBoost = 0;
    if (burstMood === "tx") {
      speed = waveSpeed * 1.7;
      hueBias = 8; // hot red TX
      satBoost = 0.2;
      glowBoost = 0.2 + 0.18 * Math.sin(t * 16 + azF * 18);
    } else if (burstMood === "rx") {
      speed = waveSpeed * 1.4;
      hueBias = 140; // green RX
      satBoost = 0.14;
      glowBoost = 0.15 + 0.12 * Math.sin(t * 10 + elF * 10);
    }
    let distW = 0;
    if (waveMode === "azimuth" || burstMood === "rx") {
      const head = (t * 0.12 * speed) % 1;
      distW = Math.abs(azF - head);
      if (distW > 0.5) distW = 1 - distW;
    } else if (waveMode === "spiral" || burstMood === "tx") {
      const phase = (azF + elF * 2.2 - t * 0.18 * speed + 10) % 1;
      distW = Math.min(phase, 1 - phase);
    } else if (waveMode === "lat") {
      const band = Math.sin(elF * Math.PI * 6 - t * 2.2 * speed) * 0.5 + 0.5;
      let hue = (elF * 280 + t * 40 * speed) % 360;
      if (hueBias) hue = (hue * 0.35 + hueBias) % 360;
      return {
        glow: Math.min(1, band * band + glowBoost),
        hue: hue,
        sat: 0.75 + satBoost,
        light: 0.28 + 0.35 * band + glowBoost * 0.3,
      };
    } else {
      const head = (1.0 - ((t * 0.15 * speed) % 1) + 1) % 1;
      let d = elF - head;
      if (d < 0) d += 1;
      distW = d;
    }
    const front = Math.max(0, 1 - distW * 5.5);
    const trail = Math.max(0, 1 - distW * 1.35) * 0.55;
    const side = 0.5 + 0.5 * Math.sin(azF * Math.PI * 4 + t * 1.8 * speed - elF * 3);
    const glow = Math.min(1, front * 0.95 + trail * 0.7 + side * 0.12 * front + glowBoost);
    let hue =
      waveMode === "azimuth" || burstMood === "rx"
        ? (azF * 360 + t * 50 * speed) % 360
        : ((1 - elF) * 300 + azF * 80 + t * 35 * speed) % 360;
    if (hueBias) hue = (hue * 0.4 + hueBias + t * 20) % 360;
    return {
      glow: glow,
      hue: hue,
      sat: 0.72 + 0.25 * front + satBoost,
      light: 0.18 + 0.42 * glow,
    };
  }

  function setBurstMood(mood) {
    burstMood = mood || "idle";
    document.body.classList.remove("mood-tx", "mood-rx", "mood-listen", "mood-think", "mood-speak");
    if (burstMood === "tx" || burstMood === "rx") {
      document.body.classList.add("mood-" + burstMood);
    }
    if (burstMood !== "idle" && !waveOn) {
      waveOn = true;
      if (el.wave) {
        el.wave.classList.add("is-on");
        el.wave.textContent = "Wave on";
      }
    }
  }

  function sendMesh(obj) {
    if (!ws || ws.readyState !== WebSocket.OPEN) return false;
    try {
      if (!obj.from) obj.from = sphereNick;
      if (!obj.t) obj.t = Date.now();
      ws.send(JSON.stringify(obj));
      return true;
    } catch (_) {
      return false;
    }
  }

  function initWalkie() {
    if (!window.GY_SPHERE_WALKIE) {
      console.warn("[sphere] sphere-walkie.js missing");
      return;
    }
    walkie = window.GY_SPHERE_WALKIE.create({
      getNick: function () {
        return sphereNick;
      },
      sendMesh: sendMesh,
      onMood: function (m) {
        setBurstMood(m === "tx" || m === "rx" ? m : "idle");
      },
      onStatus: function (t) {
        if (burstMood === "idle") setStatus(t, "live");
        else setStatus("<strong>" + burstMood.toUpperCase() + "</strong> · " + (t || ""), "live");
      },
      onLocalFrame: function (msg) {
        // paint own burst onto the 3D ball seats immediately
        upsertFeed(msg);
      },
    });
    if (walkie) walkie.setNick(sphereNick);
  }

  function setStatus(t, kind) {
    if (!el.status) return;
    el.status.innerHTML = t || "";
    el.status.classList.remove("is-live", "is-err");
    if (kind === "live") el.status.classList.add("is-live");
    if (kind === "err") el.status.classList.add("is-err");
  }

  function defaultHubWS() {
    if (location.protocol === "file:" || (location.host || "").includes("github.io"))
      return "ws://127.0.0.1:9876/";
    return (location.protocol === "https:" ? "wss:" : "ws:") + "//" + location.host + "/";
  }
  function hubURL() {
    let u = (el.hub && el.hub.value.trim()) || defaultHubWS();
    if (!/^wss?:\/\//i.test(u)) u = "ws://" + u.replace(/^\/\//, "");
    if (!u.endsWith("/") && !u.includes("?")) u += "/";
    if (!/[?&]nick=/.test(u)) {
      u +=
        (u.includes("?") ? "&" : "?") +
        "nick=" +
        encodeURIComponent(sphereNick) +
        "&role=sphere&room=news";
    }
    return u;
  }

  function normalizeGlyph(arr) {
    if (!arr || !arr.length) return null;
    const out = new Uint8Array(arr.length);
    for (let i = 0; i < arr.length; i++) {
      let v = arr[i];
      if (v <= 1) v = Math.round(v * 255);
      out[i] = Math.max(0, Math.min(255, v | 0));
    }
    return out;
  }
  function glyphAvg(g) {
    if (!g || !g.length) return 0.5;
    let s = 0,
      n = 0;
    const step = Math.max(1, (g.length / 64) | 0);
    for (let i = 0; i < g.length; i += step) {
      s += g[i];
      n++;
    }
    return n ? s / n / 255 : 0.5;
  }

  function resolveFromPos(pos, from) {
    if (pos && pos.target) {
      const t = VENUE.findTarget(pos.target);
      if (t) return { target: t, seatIdx: t.seatIdx != null ? t.seatIdx : -1 };
    }
    if (pos && pos.px != null && pos.py != null) {
      const t = VENUE.nearestPixel(pos.px, pos.py, 800);
      return { target: t, seatIdx: t && t.seatIdx != null ? t.seatIdx : -1 };
    }
    if (pos && (pos.id || pos.idx != null || pos.section != null)) {
      const seat =
        pos.idx != null
          ? seats[pos.idx]
          : SPHERE.findSeat(pos.id || { section: pos.section, row: pos.row, col: pos.col });
      if (seat) {
        return {
          target: VENUE.findTarget("seat:" + seat.id),
          seatIdx: seat.idx,
        };
      }
    }
    const key = String(from || "anon");
    let h = 0;
    for (let i = 0; i < key.length; i++) h = (h * 33 + key.charCodeAt(i)) | 0;
    const idx = Math.abs(h) % Nseats;
    return { target: VENUE.findTarget("seat:" + seats[idx].id), seatIdx: idx };
  }

  function rebuildLiveMaps() {
    seatLive.clear();
    targetLive.clear();
    feeds.forEach(function (f) {
      if (f.seatIdx >= 0) seatLive.set(f.seatIdx, f);
      if (f.targetId) targetLive.set(f.targetId, f);
    });
  }

  function upsertFeed(msg) {
    const nick = String(msg.from || msg.nick || msg.src || "peer").slice(0, 24);
    let glyph = null;
    let gn = msg.glyphN || 25;
    if (Array.isArray(msg.glyph)) {
      glyph = normalizeGlyph(msg.glyph);
      gn = Math.round(Math.sqrt(glyph.length)) || gn;
    } else if (msg.kind === "hexlum" && Array.isArray(msg.data)) {
      glyph = normalizeGlyph(msg.data);
      gn = msg.w || Math.round(Math.sqrt(glyph.length)) || gn;
    } else return;
    const resolved = resolveFromPos(msg.pos || null, nick);
    const t = resolved.target;
    const posOut = msg.pos || (t ? VENUE.targetToMeshPos(t) : null);
    feeds.set(nick, {
      nick: nick,
      glyph: glyph,
      n: gn,
      pos: posOut,
      seatIdx: resolved.seatIdx,
      targetId: t ? t.id : null,
      t: Date.now(),
      avg: glyphAvg(glyph),
    });
    rebuildLiveMaps();
    // phone torch rides with cast frames
    if (LIGHT && lightState && msg.look && msg.look.torch) {
      LIGHT.applyMeshMessage(lightState, {
        type: "venue-light",
        kind: "flashlight",
        from: nick,
        on: true,
        pos: posOut,
        look: msg.look,
      });
    }
  }

  function pruneFeeds(now) {
    let ch = false;
    feeds.forEach(function (f, id) {
      if (now - f.t > FEED_TTL_MS) {
        feeds.delete(id);
        ch = true;
      }
    });
    if (ch) rebuildLiveMaps();
  }

  const ZONE_COL = {
    stage: [1, 0.55, 0.25],
    proscenium: [0.95, 0.4, 0.4],
    backstage: [0.55, 0.4, 0.7],
    aisle: [0.5, 0.5, 0.5],
    opening: [0.95, 0.85, 0.25],
    parking: [0.35, 0.55, 0.4],
    screen: [0.7, 0.35, 0.9],
    vip: [0.75, 0.4, 0.9],
    seat: [0.4, 0.65, 0.95],
    camera: [0.4, 0.95, 0.85],
  };

  function applyColors(nowMs) {
    const tsec = (nowMs - waveT0) / 1000;
    if (LIGHT && lightState) LIGHT.prune(lightState, nowMs);
    for (let i = 0; i < TOTAL; i++) {
      const kind = pKind[i];
      let r, g, b, brightness;
      const zone =
        kind === 2 && pTargetId[i]
          ? (VENUE.findTarget(pTargetId[i]) || {}).zone
          : kind === 0
            ? "seat"
            : "screen";

      if (waveOn && kind !== 2) {
        const w = waveAt(i, tsec);
        const rgb = hsl(w.hue, w.sat, w.light);
        const mul = kind === 1 ? 0.82 : 1;
        r = rgb[0] * mul;
        g = rgb[1] * mul;
        b = rgb[2] * mul;
        brightness = (kind === 1 ? 0.2 : 0.16) + w.glow * 0.75;
      } else if (kind === 2) {
        const tid = pTargetId[i];
        const tgt = tid ? VENUE.findTarget(tid) : null;
        const zc = (tgt && ZONE_COL[tgt.zone]) || [0.5, 0.5, 0.55];
        const hot = tid && bulkHot.has(tid);
        r = zc[0];
        g = zc[1];
        b = zc[2];
        brightness = hot ? 0.95 : tgt && tgt.kind === "camera" ? 0.55 : 0.35;
        if (waveOn) {
          const w = waveAt(i, tsec);
          r = r * 0.55 + hsl(w.hue, 0.5, 0.35)[0] * 0.45;
          g = g * 0.55 + hsl(w.hue, 0.5, 0.35)[1] * 0.45;
          b = b * 0.55 + hsl(w.hue, 0.5, 0.35)[2] * 0.45;
          brightness = Math.max(brightness, 0.25 + w.glow * 0.4);
        }
      } else {
        r = 0.15;
        g = 0.18;
        b = 0.28;
        brightness = 0.1;
      }

      // venue lighting (ambient · key · fill · stage · phone flashlights)
      if (LIGHT && lightState) {
        const L = LIGHT.sampleAt(lightState, pX[i], pY[i], pZ[i], { zone: zone });
        r = Math.min(1.5, r * L.r);
        g = Math.min(1.5, g * L.g);
        b = Math.min(1.5, b * L.b);
        brightness = Math.min(1.4, brightness * L.gain);
      }

      // bulk highlight seats
      if (kind === 0) {
        const tid = pTargetId[i];
        if (tid && bulkHot.has(tid)) {
          r = Math.min(1, r * 0.3 + 0.95);
          g = Math.min(1, g * 0.3 + 0.75);
          b = Math.min(1, b * 0.3 + 0.2);
          brightness = Math.max(brightness, 0.85);
        }
      }

      // live glyph
      if (kind === 0) {
        const f = seatLive.get(pSeatIdx[i]);
        if (f) {
          const age = 1 - Math.min(1, (nowMs - f.t) / FEED_TTL_MS);
          const pulse = 0.55 + 0.45 * Math.sin(nowMs / 160 + i * 0.02);
          const mix = 0.55 + 0.4 * age;
          const ga = f.avg;
          r = r * (1 - mix) + (0.2 + 0.5 * ga + 0.25 * pulse) * mix;
          g = g * (1 - mix) + (0.9 + 0.1 * pulse) * mix;
          b = b * (1 - mix) + (0.5 + 0.35 * (1 - ga)) * mix;
          brightness = Math.min(1, brightness * 0.35 + (0.7 + 0.3 * age * pulse) * mix);
        }
      } else if (kind === 2) {
        const f = targetLive.get(pTargetId[i]);
        if (f) {
          const age = 1 - Math.min(1, (nowMs - f.t) / FEED_TTL_MS);
          r = 0.3 + 0.5 * f.avg;
          g = 0.95;
          b = 0.55;
          brightness = 0.7 + 0.3 * age;
        }
      }

      // picked flash
      if (picked && pTargetId[i] === picked.id) {
        brightness = Math.min(1, brightness + 0.35 + 0.2 * Math.sin(nowMs / 80));
        r = Math.min(1, r + 0.3);
        g = Math.min(1, g + 0.3);
      }

      col[i * 3] = Math.min(1, r);
      col[i * 3 + 1] = Math.min(1, g);
      col[i * 3 + 2] = Math.min(1, b);
      brt[i] = Math.min(1, brightness);
    }
    gl.bindBuffer(gl.ARRAY_BUFFER, bufCol);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, col);
    gl.bindBuffer(gl.ARRAY_BUFFER, bufBrt);
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, brt);
  }

  function drawGlyphHUD(mvp, w, h, now) {
    const ctx = el.hud.getContext("2d");
    if (!ctx) return;
    ctx.clearRect(0, 0, w, h);
    const list = [];
    feeds.forEach(function (f) {
      list.push(f);
    });
    list.sort(function (a, b) {
      return b.t - a.t;
    });
    let drawn = 0;
    for (let k = 0; k < list.length && drawn < MAX_HUD_GLYPHS; k++) {
      const f = list[k];
      if (!f.glyph) continue;
      let x = 0,
        y = 0,
        z = 0,
        ok = false;
      if (f.seatIdx >= 0 && f.seatIdx < Nseats) {
        const s = seats[f.seatIdx];
        x = s.x * 3;
        y = s.y * 1.5;
        z = s.z * 3;
        ok = true;
      } else if (f.pos) {
        x = (f.pos.x || 0) * 3;
        y = (f.pos.y || 0) * 1.5;
        z = (f.pos.z || 0) * 3;
        ok = true;
      }
      if (!ok) continue;
      const p = project(mvp, x, y, z, w, h);
      if (!p) continue;
      const age = 1 - Math.min(1, (now - f.t) / FEED_TTL_MS);
      if (age < 0.05) continue;
      const n = f.n || 25;
      const cell = Math.max(2, Math.min(5, Math.floor(52 / n)));
      const side = n * cell;
      const x0 = Math.round(p.x - side / 2);
      const y0 = Math.round(p.y - side / 2);
      ctx.globalAlpha = 0.4 + 0.6 * age;
      ctx.fillStyle = "rgba(0,0,0,0.6)";
      ctx.fillRect(x0 - 2, y0 - 2, side + 4, side + 14);
      for (let gy = 0; gy < n; gy++)
        for (let gx = 0; gx < n; gx++) {
          const L = f.glyph[gy * n + gx] || 0;
          ctx.fillStyle = "rgb(" + L + "," + Math.min(255, L + 40) + "," + Math.min(255, L + 70) + ")";
          ctx.fillRect(x0 + gx * cell, y0 + gy * cell, cell, cell);
        }
      ctx.fillStyle = "#a7f3d0";
      ctx.font = "10px ui-monospace,monospace";
      ctx.fillText(f.nick, x0, y0 + side + 10);
      ctx.globalAlpha = 1;
      drawn++;
    }
  }

  function updateFeedList() {
    if (!el.feeds) return;
    if (feeds.size === 0 && bulkHot.size === 0) {
      el.feeds.hidden = true;
      return;
    }
    el.feeds.hidden = false;
    let html = "";
    if (bulkHot.size) html += "<div class='live'>bulk " + bulkHot.size + " targets</div>";
    if (feeds.size) html += "<div class='live'>" + feeds.size + " live Glyph</div>";
    const arr = [];
    feeds.forEach(function (f) {
      arr.push(f);
    });
    arr.sort(function (a, b) {
      return b.t - a.t;
    });
    for (let i = 0; i < Math.min(10, arr.length); i++) {
      const f = arr[i];
      html +=
        "<div>" +
        f.nick +
        " · " +
        (f.targetId || f.seatIdx) +
        (f.pos && f.pos.px != null ? " · px " + f.pos.px + "," + f.pos.py : "") +
        "</div>";
    }
    el.feeds.innerHTML = html;
  }

  function showPick(t, screen) {
    picked = t;
    if (!el.pick || !t) {
      if (el.pick) el.pick.classList.remove("is-show");
      return;
    }
    const phoneQ =
      t.kind === "seat"
        ? "phone.html?seat=" + encodeURIComponent(t.label) + "&quick=1"
        : "phone.html?px=" + t.px + "&py=" + t.py + "&quick=1";
    el.pick.classList.add("is-show");
    el.pick.innerHTML =
      "<div><strong>" +
      t.label +
      "</strong></div>" +
      "<div>" +
      t.zone +
      " · " +
      t.kind +
      (t.chunk ? " · " + t.chunk : "") +
      "</div>" +
      "<div>LED <strong>" +
      t.px +
      "," +
      t.py +
      "</strong> / " +
      RES +
      "²</div>" +
      "<div>target <code style='color:#c4b5fd'>" +
      t.id +
      "</code></div>" +
      "<button type='button' id='sp-pick-copy'>Copy phone URL</button> " +
      "<button type='button' id='sp-pick-cast'>Demo cast here</button>";
    const copyBtn = document.getElementById("sp-pick-copy");
    const castBtn = document.getElementById("sp-pick-cast");
    if (copyBtn)
      copyBtn.onclick = function () {
        const url =
          (location.origin && location.protocol !== "file:" ? location.origin + "/" : "") + phoneQ;
        if (navigator.clipboard) navigator.clipboard.writeText(url);
        setStatus("copied · " + phoneQ, "live");
      };
    if (castBtn)
      castBtn.onclick = function () {
        demoCastAt(t);
      };
  }

  function demoCastAt(t) {
    const n = 25;
    const glyph = new Uint8Array(n * n);
    const tt = Date.now() / 300;
    for (let i = 0; i < n * n; i++) {
      glyph[i] = (100 + 120 * Math.sin(tt + i * 0.08) + 30 * Math.sin(i)) | 0;
    }
    upsertFeed({
      type: "vburst-frame",
      from: "pick-" + (t.kind || "led"),
      glyph: Array.from(glyph),
      glyphN: n,
      pos: VENUE.targetToMeshPos(t),
    });
    setStatus("<strong>cast</strong> → " + t.id + " · px " + t.px + "," + t.py, "live");
  }

  /** Click → unproject toward sphere → 16K px,py → nearest addressable target. */
  function pickAtClient(clientX, clientY) {
    const rect = el.gl.getBoundingClientRect();
    const dpr = el.gl.width / Math.max(1, rect.width);
    const sx = (clientX - rect.left) * dpr;
    const sy = (clientY - rect.top) * dpr;
    const w = el.gl.width;
    const h = el.gl.height;
    const mvp = mvpMatrix(w / Math.max(1, h));

    // find nearest projected point in cloud (seats + infra first, then free LED from shell UV)
    let bestI = -1;
    let bestD = 28 * dpr; // px threshold
    for (let i = 0; i < TOTAL; i++) {
      if (pKind[i] === 1) continue; // skip dense shell for pick accuracy
      const p = project(mvp, pX[i], pY[i], pZ[i], w, h);
      if (!p) continue;
      const d = Math.hypot(p.x - sx, p.y - sy);
      if (d < bestD) {
        bestD = d;
        bestI = i;
      }
    }
    if (bestI >= 0 && pTargetId[bestI]) {
      const t = VENUE.findTarget(pTargetId[bestI]);
      if (t) {
        showPick(t);
        return t;
      }
    }
    // free LED from NDC → approximate az/el via sphere intersection heuristic
    const ndcX = (sx / w) * 2 - 1;
    const ndcY = -((sy / h) * 2 - 1);
    // map screen to px,py using simple view-relative unwrap (good enough for testing)
    const px = Math.floor(((ndcX * 0.5 + 0.5 + rotY * 0.08) % 1) * RES);
    const py = Math.floor((0.5 - ndcY * 0.45 - rotX * 0.05) * RES);
    const t = VENUE.nearestPixel(
      Math.max(0, Math.min(RES - 1, px)),
      Math.max(0, Math.min(RES - 1, py)),
      900
    );
    showPick(t);
    return t;
  }

  function fillBulkOptions() {
    if (!el.bulkKind || !el.bulkVal) return;
    const kind = el.bulkKind.value;
    el.bulkVal.innerHTML = "";
    let opts = [];
    if (kind === "section") opts = ven.sections.slice();
    else if (kind === "chunk")
      opts = ven.chunks
        .filter(function (c) {
          return (
            c.indexOf("chunk:1") === 0 ||
            c.indexOf("chunk:2") === 0 ||
            c.indexOf("chunk:3") === 0 ||
            c.indexOf("chunk:floor") === 0 ||
            c.indexOf("chunk:screen") === 0
          );
        })
        .slice(0, 80);
    else if (kind === "zone") opts = ven.zones.slice();
    else if (kind === "rect") opts = ["center 4K", "upper half", "full screen sample"];
    opts.forEach(function (o) {
      const op = document.createElement("option");
      op.value = o;
      op.textContent = o;
      el.bulkVal.appendChild(op);
    });
  }

  function fillViewOptions() {
    if (!el.view) return;
    const keep = el.view.value || "orbit";
    el.view.innerHTML = "";
    const o0 = document.createElement("option");
    o0.value = "orbit";
    o0.textContent = "Orbit free";
    el.view.appendChild(o0);
    (VENUE.listCameraViews() || []).forEach(function (t) {
      const op = document.createElement("option");
      op.value = t.view ? t.view.id : t.id;
      op.textContent = t.label || op.value;
      el.view.appendChild(op);
    });
    el.view.value = keep;
  }

  function applyView(id) {
    if (!id || id === "orbit") {
      freeCam = null;
      setStatus("view · orbit free", "live");
      return;
    }
    const t = VENUE.getCameraView(id);
    if (!t || !t.view) {
      freeCam = null;
      return;
    }
    freeCam = {
      eye: t.view.eye,
      lookAt: t.view.lookAt,
      fov: t.view.fov || 55,
      id: t.view.id,
    };
    setStatus(
      "<strong>camera</strong> · " + t.label + " · " + t.id,
      "live"
    );
  }

  function refreshLightPanel() {
    if (!el.lightPanel || el.lightPanel.hidden || !LIGHT || !lightState) return;
    const p = lightState.params;
    const flashes = LIGHT.listFlashlights(lightState);
    let html =
      "<h4>Venue lighting</h4>" +
      row("ambient", p.ambient, 0, 1) +
      row("key", p.key, 0, 1.5) +
      row("fill", p.fill, 0, 1) +
      row("stageWash", p.stageWash, 0, 1.5) +
      row("exposure", p.exposure, 0.4, 2) +
      row("flashIntensity", p.flashIntensity, 0.2, 3) +
      "<div class='flash-list'>🔦 phones " +
      flashes.length +
      (flashes.length
        ? ": " +
          flashes
            .map(function (f) {
              return f.from;
            })
            .join(", ")
        : "") +
      "</div>" +
      "<button type='button' id='sp-lt-preset-concert'>Concert</button> " +
      "<button type='button' id='sp-lt-preset-dim'>Dim house</button> " +
      "<button type='button' id='sp-lt-preset-flat'>Flat</button>";
    el.lightPanel.innerHTML = html;
    el.lightPanel.querySelectorAll("input[type=range]").forEach(function (inp) {
      inp.addEventListener("input", function () {
        const k = inp.dataset.key;
        const v = parseFloat(inp.value);
        LIGHT.setParams(lightState, { [k]: v });
        const em = inp.parentElement && inp.parentElement.querySelector("em");
        if (em) em.textContent = v.toFixed(2);
      });
    });
    function bindPreset(id, patch) {
      const b = document.getElementById(id);
      if (b)
        b.onclick = function () {
          LIGHT.setParams(lightState, patch);
          refreshLightPanel();
        };
    }
    bindPreset("sp-lt-preset-concert", {
      ambient: 0.18,
      key: 0.95,
      fill: 0.2,
      stageWash: 0.85,
      exposure: 1.1,
    });
    bindPreset("sp-lt-preset-dim", {
      ambient: 0.12,
      key: 0.35,
      fill: 0.1,
      stageWash: 0.25,
      exposure: 0.85,
    });
    bindPreset("sp-lt-preset-flat", {
      ambient: 0.55,
      key: 0.4,
      fill: 0.45,
      stageWash: 0.35,
      exposure: 1,
    });
  }

  function row(key, val, lo, hi) {
    return (
      "<div class='row'><span>" +
      key +
      "</span><input type='range' data-key='" +
      key +
      "' min='" +
      lo +
      "' max='" +
      hi +
      "' step='0.02' value='" +
      val +
      "'/><em>" +
      val.toFixed(2) +
      "</em></div>"
    );
  }

  function runBulkActivate() {
    const kind = el.bulkKind ? el.bulkKind.value : "section";
    const val = el.bulkVal ? el.bulkVal.value : "";
    let q = { limit: 4000 };
    if (kind === "section") q.section = val;
    else if (kind === "chunk") q.chunk = val;
    else if (kind === "zone") q.zone = val;
    else if (kind === "rect") {
      if (val === "upper half") q.rect = { x0: 0, y0: 0, x1: RES - 1, y1: RES / 2 };
      else if (val === "full screen sample") q.rect = { x0: 0, y0: 0, x1: RES - 1, y1: RES - 1, step: 256 };
      else q.rect = { x0: RES * 0.35, y0: RES * 0.35, x1: RES * 0.65, y1: RES * 0.65, step: 96 };
    }
    const res = VENUE.bulkActivate(q);
    bulkHot.clear();
    res.ids.forEach(function (id) {
      bulkHot.add(id);
    });
    setStatus(
      "<strong>bulk</strong> " +
        res.count.toLocaleString() +
        " targets · " +
        kind +
        " " +
        val,
      "live"
    );
    updateFeedList();
    return res;
  }

  function runBulkCastDemo() {
    const res = bulkHot.size ? { targets: [] } : runBulkActivate();
    let targets = res.targets;
    if (bulkHot.size && (!targets || !targets.length)) {
      targets = [];
      bulkHot.forEach(function (id) {
        const t = VENUE.findTarget(id);
        if (t) targets.push(t);
      });
    }
    if (!targets.length) {
      setStatus("bulk empty · activate a section/zone first", "err");
      return;
    }
    if (bulkDemoTimer) clearInterval(bulkDemoTimer);
    let i = 0;
    setStatus("<strong>bulk cast</strong> spraying " + targets.length + " targets…", "live");
    bulkDemoTimer = setInterval(function () {
      const batch = 12;
      for (let k = 0; k < batch && i < targets.length; k++, i++) {
        const t = targets[i];
        const n = 25;
        const glyph = new Uint8Array(n * n);
        const tt = Date.now() / 280 + i;
        for (let g = 0; g < n * n; g++) glyph[g] = (80 + 140 * Math.sin(tt + g * 0.07)) | 0;
        upsertFeed({
          type: "vburst-frame",
          from: "bulk-" + (i % 40),
          glyph: Array.from(glyph),
          glyphN: n,
          pos: VENUE.targetToMeshPos(t),
        });
      }
      if (i >= targets.length) {
        clearInterval(bulkDemoTimer);
        bulkDemoTimer = 0;
        setStatus("<strong>bulk cast done</strong> · " + targets.length + " · live TTL fades", "live");
      }
    }, 80);
  }

  function resize() {
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    const rect = el.gl.parentElement.getBoundingClientRect();
    const w = Math.max(1, Math.floor(rect.width * dpr));
    const h = Math.max(1, Math.floor(rect.height * dpr));
    if (el.gl.width !== w || el.gl.height !== h) {
      el.gl.width = w;
      el.gl.height = h;
      el.hud.width = w;
      el.hud.height = h;
      el.gl.style.width = rect.width + "px";
      el.gl.style.height = rect.height + "px";
      el.hud.style.width = rect.width + "px";
      el.hud.style.height = rect.height + "px";
    }
    gl.viewport(0, 0, el.gl.width, el.gl.height);
  }

  function frame(now) {
    resize();
    pruneFeeds(now);
    applyColors(now);
    const w = el.gl.width;
    const h = el.gl.height;
    const mvp = mvpMatrix(w / Math.max(1, h));
    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.useProgram(prog);
    gl.uniformMatrix4fv(uMvp, false, mvp);
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    gl.uniform1f(uSize, Math.max(1.0, (4.5 / dist) * dpr));
    gl.bindBuffer(gl.ARRAY_BUFFER, bufPos);
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 3, gl.FLOAT, false, 0, 0);
    gl.bindBuffer(gl.ARRAY_BUFFER, bufCol);
    gl.enableVertexAttribArray(aCol);
    gl.vertexAttribPointer(aCol, 3, gl.FLOAT, false, 0, 0);
    gl.bindBuffer(gl.ARRAY_BUFFER, bufBrt);
    gl.enableVertexAttribArray(aBrt);
    gl.vertexAttribPointer(aBrt, 1, gl.FLOAT, false, 0, 0);
    gl.drawArrays(gl.POINTS, 0, TOTAL);
    drawGlyphHUD(mvp, w, h, now);
    if (((now / 400) | 0) !== (((now - 16) / 400) | 0)) updateFeedList();
    requestAnimationFrame(frame);
  }

  function connect() {
    save();
    if (ws) try { ws.close(); } catch (_) {}
    const url = hubURL();
    setStatus("connecting " + url + "…");
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setStatus("WS error · " + e, "err");
      return;
    }
    ws.onopen = function () {
      const m = VENUE.meta();
      setStatus(
        "<strong>live</strong> · " +
          m.targets.toLocaleString() +
          " targets · 16K canvas · " +
          m.seats.toLocaleString() +
          " seats",
        "live"
      );
      if (el.connect) {
        el.connect.classList.add("is-on");
        el.connect.textContent = "Connected";
      }
      try {
        ws.send(
          JSON.stringify({
            type: "join",
            nick: sphereNick,
            role: "sphere",
            room: "news",
          })
        );
      } catch (_) {}
      setStatus(
        "<strong>live</strong> · walkie hub · hold orb to burst · " + sphereNick,
        "live"
      );
    };
    ws.onclose = function () {
      setStatus("hub closed", "err");
      if (el.connect) {
        el.connect.classList.remove("is-on");
        el.connect.textContent = "Connect";
      }
      ws = null;
    };
    ws.onerror = function () {
      setStatus("hub error", "err");
    };
    ws.onmessage = function (ev) {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      if (LIGHT && lightState) LIGHT.applyMeshMessage(lightState, msg);
      if (msg.type === "vburst-frame" || msg.type === "news-frame") upsertFeed(msg);
      else if (msg.type === "gyst" && (msg.kind === "hexlum" || Array.isArray(msg.data))) upsertFeed(msg);
      if (msg.type === "camera-controls" || msg.type === "venue-light") refreshLightPanel();
      // walkie burst + mesh chat
      if (walkie) walkie.onMesh(msg);
      if (msg.type === "vburst-start" || (msg.type === "ptt" && msg.state === "down")) {
        if (msg.from && msg.from !== sphereNick) setBurstMood("rx");
      }
      if (msg.type === "vburst-end" || (msg.type === "ptt" && msg.state === "up")) {
        if (msg.from && msg.from !== sphereNick && burstMood === "rx") setBurstMood("idle");
      }
    };
  }

  function runDemo() {
    if (demoTimer) {
      clearInterval(demoTimer);
      demoTimer = 0;
      if (el.demo) el.demo.classList.remove("is-on");
      return;
    }
    if (el.demo) el.demo.classList.add("is-on");
    demoTimer = setInterval(function () {
      for (let k = 0; k < 8; k++) {
        const seat = seats[(Math.random() * Nseats) | 0];
        const t = VENUE.findTarget("seat:" + seat.id);
        if (t) demoCastAt(t);
      }
    }, 400);
  }

  function save() {
    try {
      localStorage.setItem(
        STORAGE,
        JSON.stringify({
          hub: el.hub && el.hub.value,
          waveOn: waveOn,
          waveMode: waveMode,
          waveSpeed: waveSpeed,
        })
      );
    } catch (_) {}
  }
  function load() {
    try {
      const st = JSON.parse(localStorage.getItem(STORAGE) || "{}");
      if (st.hub && el.hub) el.hub.value = st.hub;
      if (typeof st.waveOn === "boolean") waveOn = st.waveOn;
      if (st.waveMode) waveMode = st.waveMode;
      if (st.waveSpeed) waveSpeed = st.waveSpeed;
    } catch (_) {}
  }

  el.gl.addEventListener("pointerdown", function (e) {
    dragging = true;
    moved = false;
    lastMX = e.clientX;
    lastMY = e.clientY;
    el.gl.setPointerCapture(e.pointerId);
  });
  el.gl.addEventListener("pointermove", function (e) {
    if (!dragging) return;
    const dx = e.clientX - lastMX;
    const dy = e.clientY - lastMY;
    if (Math.abs(dx) + Math.abs(dy) > 3) moved = true;
    lastMX = e.clientX;
    lastMY = e.clientY;
    // drag exits free-cam back to orbit
    if (freeCam) {
      freeCam = null;
      if (el.view) el.view.value = "orbit";
    }
    rotY += dx * 0.005;
    rotX = Math.max(-1.35, Math.min(1.35, rotX + dy * 0.005));
  });
  el.gl.addEventListener("pointerup", function (e) {
    dragging = false;
    if (!moved) pickAtClient(e.clientX, e.clientY);
  });
  el.gl.addEventListener(
    "wheel",
    function (e) {
      e.preventDefault();
      dist = Math.max(4, Math.min(30, dist + e.deltaY * 0.01));
    },
    { passive: false }
  );

  function init() {
    load();
    if (el.hub && !el.hub.value) el.hub.value = defaultHubWS();
    if (el.connect) el.connect.addEventListener("click", connect);
    if (el.demo) el.demo.addEventListener("click", runDemo);
    if (el.clear)
      el.clear.addEventListener("click", function () {
        feeds.clear();
        rebuildLiveMaps();
        updateFeedList();
      });
    if (el.wave)
      el.wave.addEventListener("click", function () {
        waveOn = !waveOn;
        el.wave.classList.toggle("is-on", waveOn);
        el.wave.textContent = waveOn ? "Wave on" : "Wave off";
        save();
      });
    if (el.waveMode)
      el.waveMode.addEventListener("change", function () {
        waveMode = el.waveMode.value;
        save();
      });
    if (el.waveSpeed)
      el.waveSpeed.addEventListener("input", function () {
        waveSpeed = parseFloat(el.waveSpeed.value) || 1;
        save();
      });
    if (el.bulkKind) el.bulkKind.addEventListener("change", fillBulkOptions);
    if (el.bulk) el.bulk.addEventListener("click", runBulkActivate);
    if (el.bulkCast) el.bulkCast.addEventListener("click", runBulkCastDemo);
    if (el.bulkClear)
      el.bulkClear.addEventListener("click", function () {
        bulkHot.clear();
        if (bulkDemoTimer) clearInterval(bulkDemoTimer);
        bulkDemoTimer = 0;
        updateFeedList();
        setStatus("bulk cleared", "live");
      });
    if (el.view)
      el.view.addEventListener("change", function () {
        applyView(el.view.value);
      });
    if (el.lights)
      el.lights.addEventListener("click", function () {
        if (!el.lightPanel) return;
        el.lightPanel.hidden = !el.lightPanel.hidden;
        el.lights.classList.toggle("is-on", !el.lightPanel.hidden);
        if (!el.lightPanel.hidden) refreshLightPanel();
      });
    if (el.wave) {
      el.wave.classList.toggle("is-on", waveOn);
      el.wave.textContent = waveOn ? "Wave on" : "Wave off";
    }
    if (el.waveMode) el.waveMode.value = waveMode;
    fillBulkOptions();
    fillViewOptions();

    const m = VENUE.meta();
    setStatus(
      "<strong>16K sphere ball</strong> · walkie burst · " +
        m.targets.toLocaleString() +
        " targets",
      "live"
    );
    if (el.legend)
      el.legend.innerHTML =
        "hold orb / Space = TX<br/>peer frames → seats<br/>" +
        TOTAL.toLocaleString() +
        " pts";

    initWalkie();
    if (location.protocol !== "file:" && !(location.host || "").includes("github.io")) connect();
    bootMsg("done");
    requestAnimationFrame(frame);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
  } // end bootMain
})();
