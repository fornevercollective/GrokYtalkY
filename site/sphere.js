/**
 * Sphere Glyph — full dome + color wave sweep + live mesh Glyphs.
 * Seats (Bloch³) + interior LED shell. No music/MOPA/VR/kBatch.
 */
(function () {
  "use strict";

  const SPHERE = window.GY_SPHERE;
  if (!SPHERE) {
    console.error("sphere-seating.js required");
    return;
  }

  const STORAGE = "gy.sphere.v1";
  const FEED_TTL_MS = 4000;
  const MAX_HUD_GLYPHS = 48;
  // dense interior LED shell (full sphere surface sample)
  const SHELL_AZ = 160;
  const SHELL_EL = 90;

  const el = {
    gl: document.getElementById("sp-gl"),
    hud: document.getElementById("sp-hud"),
    status: document.getElementById("sp-status"),
    legend: document.getElementById("sp-legend"),
    feeds: document.getElementById("sp-feeds"),
    hub: document.getElementById("sp-hub"),
    connect: document.getElementById("sp-connect"),
    demo: document.getElementById("sp-demo"),
    clear: document.getElementById("sp-clear"),
    wave: document.getElementById("sp-wave"),
    waveMode: document.getElementById("sp-wave-mode"),
    waveSpeed: document.getElementById("sp-wave-speed"),
  };

  const seats = SPHERE.seatsCached();
  const Nseats = seats.length;
  const meta = SPHERE.meta();

  // ── build full point cloud: seats first, then dome shell ──
  function buildShell() {
    const pts = [];
    // unit sphere surface, denser near equator; full 360° az × full elevation
    for (let ei = 0; ei < SHELL_EL; ei++) {
      const elFrac = ei / (SHELL_EL - 1); // 0 top → 1 bottom
      const theta = elFrac * Math.PI; // 0..π
      const sinT = Math.sin(theta);
      const cosT = Math.cos(theta);
      // more az samples at equator
      const azN = Math.max(24, Math.round(SHELL_AZ * (0.35 + 0.65 * sinT)));
      for (let ai = 0; ai < azN; ai++) {
        const azFrac = ai / azN;
        const phi = azFrac * Math.PI * 2;
        // slight bulge so shell sits outside seats
        const R = 3.35;
        pts.push({
          x: Math.sin(theta) * Math.cos(phi) * R,
          y: cosT * R * 0.92 + 0.15,
          z: Math.sin(theta) * Math.sin(phi) * R,
          elFrac: elFrac,
          azFrac: azFrac,
          kind: "shell",
        });
      }
    }
    return pts;
  }

  const shell = buildShell();
  const Nshell = shell.length;
  const TOTAL = Nseats + Nshell;

  // per-point static params for wave (precomputed)
  const pX = new Float32Array(TOTAL);
  const pY = new Float32Array(TOTAL);
  const pZ = new Float32Array(TOTAL);
  const pEl = new Float32Array(TOTAL); // 0 top → 1 bottom
  const pAz = new Float32Array(TOTAL); // 0..1
  const pKind = new Uint8Array(TOTAL); // 0 seat 1 shell
  const seatIdxOfPoint = new Int32Array(TOTAL); // for seats: seat index; shell: -1

  for (let i = 0; i < Nseats; i++) {
    const s = seats[i];
    pX[i] = s.x * 3;
    pY[i] = s.y * 1.5;
    pZ[i] = s.z * 3;
    // map seat y (-1..1ish) to elFrac; az from atan2
    pEl[i] = Math.max(0, Math.min(1, 0.5 - s.y * 0.45));
    pAz[i] = (Math.atan2(s.x, s.z) / (Math.PI * 2) + 1) % 1;
    pKind[i] = 0;
    seatIdxOfPoint[i] = i;
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
    seatIdxOfPoint[i] = -1;
  }

  // ── camera ──
  let dist = 11;
  let rotX = 0.28;
  let rotY = 0.55;
  let panX = 0;
  let panY = 0.05;
  let dragging = false;
  let lastMX = 0;
  let lastMY = 0;

  // ── wave sim ──
  let waveOn = true;
  let waveMode = "cascade"; // cascade | azimuth | spiral | lat
  let waveSpeed = 1.0;
  let waveT0 = performance.now();

  // ── live feeds ──
  const feeds = new Map();
  /** seatIdx → feed (latest) for O(1) color pass */
  const seatLive = new Map();
  let ws = null;
  let demoTimer = 0;

  // ── WebGL ──
  const gl = el.gl.getContext("webgl", { antialias: true, alpha: false });
  if (!gl) {
    setStatus("WebGL unavailable", "err");
    return;
  }

  const VS = `
    attribute vec3 aPos;
    attribute vec3 aCol;
    attribute float aBrt;
    uniform mat4 uMvp;
    uniform float uSize;
    varying vec3 vCol;
    varying float vBrt;
    void main() {
      vCol = aCol;
      vBrt = aBrt;
      gl_Position = uMvp * vec4(aPos, 1.0);
      gl_PointSize = max(1.0, uSize * (0.5 + aBrt * 1.85));
    }
  `;
  const FS = `
    precision mediump float;
    varying vec3 vCol;
    varying float vBrt;
    void main() {
      vec2 p = gl_PointCoord - vec2(0.5);
      float d = length(p);
      if (d > 0.5) discard;
      float core = smoothstep(0.5, 0.08, d);
      float a = core * (0.2 + 0.8 * vBrt);
      gl_FragColor = vec4(vCol * (0.25 + 1.0 * vBrt), a);
    }
  `;

  function compile(type, src) {
    const s = gl.createShader(type);
    gl.shaderSource(s, src);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
      console.error(gl.getShaderInfoLog(s));
    }
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
    col[i * 3] = 0.15;
    col[i * 3 + 1] = 0.18;
    col[i * 3 + 2] = 0.25;
    brt[i] = 0.12;
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

  // ── math ──
  function m4I() {
    return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);
  }
  function m4M(a, b) {
    const o = new Float32Array(16);
    for (let c = 0; c < 4; c++) {
      for (let r = 0; r < 4; r++) {
        o[c * 4 + r] =
          a[0 * 4 + r] * b[c * 4 + 0] +
          a[1 * 4 + r] * b[c * 4 + 1] +
          a[2 * 4 + r] * b[c * 4 + 2] +
          a[3 * 4 + r] * b[c * 4 + 3];
      }
    }
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
  function mvpMatrix(aspect) {
    const proj = m4P(0.9, aspect, 0.1, 100);
    const view = m4M(m4T(panX, panY, -dist), m4M(m4RX(rotX), m4RY(rotY)));
    return m4M(proj, view);
  }
  function project(mvp, x, y, z, w, h) {
    const X = mvp[0] * x + mvp[4] * y + mvp[8] * z + mvp[12];
    const Y = mvp[1] * x + mvp[5] * y + mvp[9] * z + mvp[13];
    const W = mvp[3] * x + mvp[7] * y + mvp[11] * z + mvp[15];
    if (W < 0.05) return null;
    return {
      x: ((X / W) * 0.5 + 0.5) * w,
      y: ((-Y / W) * 0.5 + 0.5) * h,
    };
  }

  /** HSL → RGB 0..1 */
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

  /**
   * Wave field over full sphere.
   * Returns { glow 0..1, hue deg, sat, light }.
   */
  function waveAt(i, t) {
    const elF = pEl[i];
    const azF = pAz[i];
    let head = 0;
    let distW = 0;
    // t in seconds, speed scales cycles
    const speed = waveSpeed;
    if (waveMode === "azimuth") {
      // horizontal sweep around the bowl
      head = (t * 0.12 * speed) % 1;
      distW = Math.abs(azF - head);
      if (distW > 0.5) distW = 1 - distW;
    } else if (waveMode === "spiral") {
      // helix: elevation + azimuth lock
      const phase = (azF + elF * 2.2 - t * 0.18 * speed + 10) % 1;
      distW = Math.min(phase, 1 - phase);
      head = phase;
    } else if (waveMode === "lat") {
      // latitude bands pulse
      const band = Math.sin(elF * Math.PI * 6 - t * 2.2 * speed) * 0.5 + 0.5;
      return {
        glow: band * band,
        hue: (elF * 280 + t * 40 * speed + azF * 60) % 360,
        sat: 0.75,
        light: 0.28 + 0.35 * band,
      };
    } else {
      // cascade (default): top → bottom ribbon, like Sphere LED cascade
      head = (1.0 - ((t * 0.15 * speed) % 1) + 1) % 1; // moves top→bottom
      // wrap-friendly distance along elevation
      let d = elF - head;
      if (d < 0) d += 1;
      distW = d; // trailing wake below head
    }

    // sharp front + long trail
    const front = Math.max(0, 1 - distW * 5.5);
    const trail = Math.max(0, 1 - distW * 1.35) * 0.55;
    const side =
      waveMode === "cascade"
        ? 0.5 + 0.5 * Math.sin(azF * Math.PI * 4 + t * 1.8 * speed - elF * 3)
        : 0.5 + 0.5 * Math.sin(elF * 8 + t * 2 * speed);
    const glow = Math.min(1, front * 0.95 + trail * 0.7 + side * 0.12 * front);

    const hue =
      waveMode === "azimuth"
        ? (azF * 360 + t * 50 * speed) % 360
        : waveMode === "spiral"
          ? ((elF + azF) * 200 + t * 55 * speed) % 360
          : ((1 - elF) * 300 + azF * 80 + t * 35 * speed) % 360;

    return {
      glow: glow,
      hue: hue,
      sat: 0.72 + 0.25 * front,
      light: 0.18 + 0.42 * glow + 0.08 * side,
    };
  }

  function setStatus(t, kind) {
    if (!el.status) return;
    el.status.innerHTML = t || "";
    el.status.classList.remove("is-live", "is-err");
    if (kind === "live") el.status.classList.add("is-live");
    if (kind === "err") el.status.classList.add("is-err");
  }

  function defaultHubWS() {
    if (location.protocol === "file:" || (location.host || "").includes("github.io")) {
      return "ws://127.0.0.1:9876/";
    }
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    return proto + "//" + location.host + "/";
  }

  function hubURL() {
    let u = (el.hub && el.hub.value.trim()) || defaultHubWS();
    if (!/^wss?:\/\//i.test(u)) u = "ws://" + u.replace(/^\/\//, "");
    if (!u.endsWith("/") && !u.includes("?")) u += "/";
    if (!/[?&]nick=/.test(u)) {
      u += (u.includes("?") ? "&" : "?") + "nick=sphere&role=sphere";
    } else if (!/[?&]role=/.test(u)) {
      u += "&role=sphere";
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

  function glyphAvg(glyph) {
    if (!glyph || !glyph.length) return 0.5;
    let s = 0;
    const step = Math.max(1, (glyph.length / 64) | 0);
    let n = 0;
    for (let i = 0; i < glyph.length; i += step) {
      s += glyph[i];
      n++;
    }
    return n ? s / n / 255 : 0.5;
  }

  function resolveSeatIdx(pos, from) {
    if (!pos) {
      const key = String(from || "anon");
      let h = 0;
      for (let i = 0; i < key.length; i++) h = (h * 33 + key.charCodeAt(i)) | 0;
      return Math.abs(h) % Nseats;
    }
    if (typeof pos.idx === "number" && pos.idx >= 0 && pos.idx < Nseats) return pos.idx | 0;
    if (pos.id) {
      const s = SPHERE.findSeat(pos.id);
      if (s) return s.idx;
    }
    if (pos.section != null && pos.row != null && pos.col != null) {
      const s = SPHERE.findSeat({
        section: pos.section,
        row: pos.row,
        col: pos.col,
      });
      if (s) return s.idx;
    }
    const key = String(from || "anon");
    let h = 0;
    for (let i = 0; i < key.length; i++) h = (h * 33 + key.charCodeAt(i)) | 0;
    return Math.abs(h) % Nseats;
  }

  function rebuildSeatLive() {
    seatLive.clear();
    feeds.forEach((f) => {
      if (f.seatIdx >= 0) seatLive.set(f.seatIdx, f);
    });
  }

  function upsertFeed(msg) {
    const nick = String(msg.from || msg.nick || msg.src || "peer").slice(0, 24);
    let glyph = null;
    let gn = msg.glyphN || msg.glyph_n || 25;
    if (Array.isArray(msg.glyph)) {
      glyph = normalizeGlyph(msg.glyph);
      gn = Math.round(Math.sqrt(glyph.length)) || gn;
    } else if (msg.kind === "hexlum" && Array.isArray(msg.data)) {
      glyph = normalizeGlyph(msg.data);
      gn = msg.w || Math.round(Math.sqrt(glyph.length)) || gn;
    } else {
      return;
    }
    const posMsg = msg.pos || null;
    const seatIdx = resolveSeatIdx(posMsg, nick);
    feeds.set(nick, {
      nick,
      glyph,
      n: gn,
      pos: posMsg,
      seatIdx,
      t: Date.now(),
      avg: glyphAvg(glyph),
    });
    rebuildSeatLive();
  }

  function pruneFeeds(now) {
    let changed = false;
    feeds.forEach((f, id) => {
      if (now - f.t > FEED_TTL_MS) {
        feeds.delete(id);
        changed = true;
      }
    });
    if (changed) rebuildSeatLive();
  }

  /** Paint wave across ALL points; live Glyph seats punch through. */
  function applyColors(nowMs) {
    const t = (nowMs - waveT0) / 1000;
    for (let i = 0; i < TOTAL; i++) {
      const isShell = pKind[i] === 1;
      let r, g, b, brightness;

      if (waveOn) {
        const w = waveAt(i, t);
        const rgb = hsl(w.hue, w.sat, w.light);
        // shell slightly dimmer base so seats read
        const shellMul = isShell ? 0.85 : 1.0;
        r = rgb[0] * shellMul;
        g = rgb[1] * shellMul;
        b = rgb[2] * shellMul;
        brightness = (isShell ? 0.22 : 0.18) + w.glow * (isShell ? 0.72 : 0.78);
        // ambient floor so full sphere always readable
        brightness = Math.max(brightness, isShell ? 0.1 : 0.08);
      } else {
        // quiet dome
        if (isShell) {
          r = 0.12;
          g = 0.14;
          b = 0.22;
          brightness = 0.1;
        } else {
          r = 0.18;
          g = 0.22;
          b = 0.32;
          brightness = 0.09;
        }
      }

      // live Glyph seat override (only seat indices)
      if (!isShell) {
        const si = seatIdxOfPoint[i];
        const f = seatLive.get(si);
        if (f) {
          const age = 1 - Math.min(1, (nowMs - f.t) / FEED_TTL_MS);
          const pulse = 0.55 + 0.45 * Math.sin(nowMs / 160 + si * 0.02);
          const ga = f.avg;
          // mint Glyph punch mixes over wave
          const mix = 0.55 + 0.4 * age;
          r = r * (1 - mix) + (0.2 + 0.5 * ga + 0.25 * pulse) * mix;
          g = g * (1 - mix) + (0.9 + 0.1 * pulse) * mix;
          b = b * (1 - mix) + (0.5 + 0.35 * (1 - ga)) * mix;
          brightness = Math.min(1, brightness * 0.4 + (0.65 + 0.35 * age * pulse) * mix);
        }
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
    feeds.forEach((f) => list.push(f));
    list.sort((a, b) => b.t - a.t);
    let drawn = 0;
    for (let k = 0; k < list.length && drawn < MAX_HUD_GLYPHS; k++) {
      const f = list[k];
      if (!f.glyph || f.seatIdx < 0) continue;
      const s = seats[f.seatIdx];
      const p = project(mvp, s.x * 3, s.y * 1.5, s.z * 3, w, h);
      if (!p) continue;
      const age = 1 - Math.min(1, (now - f.t) / FEED_TTL_MS);
      if (age < 0.05) continue;
      const n = f.n || 25;
      const cell = Math.max(2, Math.min(5, Math.floor(56 / n)));
      const side = n * cell;
      const x0 = Math.round(p.x - side / 2);
      const y0 = Math.round(p.y - side / 2);
      ctx.globalAlpha = 0.4 + 0.6 * age;
      ctx.fillStyle = "rgba(0,0,0,0.6)";
      ctx.fillRect(x0 - 2, y0 - 2, side + 4, side + 14);
      for (let gy = 0; gy < n; gy++) {
        for (let gx = 0; gx < n; gx++) {
          const L = f.glyph[gy * n + gx] || 0;
          ctx.fillStyle =
            "rgb(" + L + "," + Math.min(255, L + 40) + "," + Math.min(255, L + 70) + ")";
          ctx.fillRect(x0 + gx * cell, y0 + gy * cell, cell, cell);
        }
      }
      ctx.fillStyle = "#a7f3d0";
      ctx.font = "10px ui-monospace, monospace";
      ctx.fillText(f.nick, x0, y0 + side + 10);
      ctx.globalAlpha = 1;
      drawn++;
    }
  }

  function updateFeedList() {
    if (!el.feeds) return;
    if (feeds.size === 0) {
      el.feeds.hidden = true;
      el.feeds.innerHTML = "";
      return;
    }
    el.feeds.hidden = false;
    let html = "<div class='live'>" + feeds.size + " live Glyph</div>";
    const arr = [];
    feeds.forEach((f) => arr.push(f));
    arr.sort((a, b) => b.t - a.t);
    for (let i = 0; i < Math.min(12, arr.length); i++) {
      const f = arr[i];
      const seat = f.seatIdx >= 0 ? seats[f.seatIdx] : null;
      html += "<div>" + f.nick + " · " + (seat ? seat.id : "?") + "</div>";
    }
    el.feeds.innerHTML = html;
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
    const aspect = w / Math.max(1, h);
    const mvp = mvpMatrix(aspect);

    gl.clear(gl.COLOR_BUFFER_BIT);
    gl.useProgram(prog);
    gl.uniformMatrix4fv(uMvp, false, mvp);
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    gl.uniform1f(uSize, Math.max(1.1, (4.8 / dist) * dpr));

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
    if ((now / 400) | 0 !== ((now - 16) / 400) | 0) updateFeedList();

    requestAnimationFrame(frame);
  }

  function connect() {
    save();
    if (ws) {
      try {
        ws.close();
      } catch (_) {}
      ws = null;
    }
    const url = hubURL();
    setStatus("connecting " + url + "…");
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setStatus("WS error · " + e, "err");
      return;
    }
    ws.onopen = () => {
      setStatus(
        "<strong>live</strong> · " +
          meta.seats.toLocaleString() +
          " seats + " +
          Nshell.toLocaleString() +
          " shell · wave " +
          waveMode,
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
            nick: "sphere",
            role: "sphere",
            cap: { class: "bridge", role: "sphere", lanes: ["glyph", "hex"] },
          })
        );
      } catch (_) {}
    };
    ws.onclose = () => {
      setStatus("hub closed · tap Connect", "err");
      if (el.connect) {
        el.connect.classList.remove("is-on");
        el.connect.textContent = "Connect";
      }
      ws = null;
    };
    ws.onerror = () => setStatus("hub error · gy serve --bind 0.0.0.0", "err");
    ws.onmessage = (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      const t = msg.type;
      if (t === "vburst-frame" || t === "news-frame") upsertFeed(msg);
      else if (t === "gyst" && (msg.kind === "hexlum" || Array.isArray(msg.data))) upsertFeed(msg);
    };
  }

  function runDemo() {
    if (demoTimer) {
      clearInterval(demoTimer);
      demoTimer = 0;
      if (el.demo) el.demo.classList.remove("is-on");
      setStatus("demo off · wave still runs on full sphere");
      return;
    }
    if (el.demo) el.demo.classList.add("is-on");
    setStatus("<strong>demo Glyphs</strong> on seats · wave sweeps full dome", "live");
    demoTimer = setInterval(() => {
      const n = 25;
      const glyph = new Uint8Array(n * n);
      const t = Date.now() / 400;
      for (let i = 0; i < n * n; i++) {
        const x = i % n;
        const y = (i / n) | 0;
        glyph[i] =
          (128 +
            100 * Math.sin(t + x * 0.4 + y * 0.3) +
            40 * Math.sin(t * 1.7 + i * 0.05)) |
          0;
      }
      // spray along the wave head so demos ride the cascade
      const head = (1.0 - ((performance.now() / 1000) * 0.15 * waveSpeed) % 1 + 1) % 1;
      const count = 10 + ((Math.random() * 8) | 0);
      for (let k = 0; k < count; k++) {
        // bias seats near wave elevation
        let idx = (Math.random() * Nseats) | 0;
        for (let tries = 0; tries < 8; tries++) {
          const cand = (Math.random() * Nseats) | 0;
          if (Math.abs(pEl[cand] - head) < 0.12) {
            idx = cand;
            break;
          }
        }
        const seat = seats[idx];
        upsertFeed({
          type: "vburst-frame",
          from: "demo-" + (k + 1),
          glyph: Array.from(glyph),
          glyphN: n,
          pos: SPHERE.seatToMeshPos(seat),
        });
      }
    }, 260);
  }

  function clearLive() {
    feeds.clear();
    seatLive.clear();
    if (demoTimer) {
      clearInterval(demoTimer);
      demoTimer = 0;
      if (el.demo) el.demo.classList.remove("is-on");
    }
    updateFeedList();
    setStatus("cleared Glyphs · wave continues on full sphere");
  }

  function syncWaveUI() {
    if (el.wave) {
      el.wave.classList.toggle("is-on", waveOn);
      el.wave.textContent = waveOn ? "Wave on" : "Wave off";
    }
    if (el.waveMode) el.waveMode.value = waveMode;
    if (el.waveSpeed) el.waveSpeed.value = String(waveSpeed);
  }

  function save() {
    try {
      localStorage.setItem(
        STORAGE,
        JSON.stringify({
          hub: el.hub ? el.hub.value : "",
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
      if (typeof st.waveSpeed === "number") waveSpeed = st.waveSpeed;
    } catch (_) {}
  }

  // pointer
  el.gl.addEventListener("pointerdown", (e) => {
    dragging = true;
    lastMX = e.clientX;
    lastMY = e.clientY;
    el.gl.setPointerCapture(e.pointerId);
  });
  el.gl.addEventListener("pointermove", (e) => {
    if (!dragging) return;
    const dx = e.clientX - lastMX;
    const dy = e.clientY - lastMY;
    lastMX = e.clientX;
    lastMY = e.clientY;
    rotY += dx * 0.005;
    rotX = Math.max(-1.35, Math.min(1.35, rotX + dy * 0.005));
  });
  el.gl.addEventListener("pointerup", () => {
    dragging = false;
  });
  el.gl.addEventListener("pointercancel", () => {
    dragging = false;
  });
  el.gl.addEventListener(
    "wheel",
    (e) => {
      e.preventDefault();
      dist = Math.max(4, Math.min(28, dist + e.deltaY * 0.01));
    },
    { passive: false }
  );

  try {
    const bc = new BroadcastChannel("gy-glyph-cast");
    bc.onmessage = (ev) => {
      const d = ev.data;
      if (!d || d.type !== "glyph-cast" || !Array.isArray(d.peers)) return;
      d.peers.forEach((p, i) => {
        if (!p.lumB64) return;
        try {
          const bin = atob(p.lumB64);
          const glyph = new Uint8Array(bin.length);
          for (let j = 0; j < bin.length; j++) glyph[j] = bin.charCodeAt(j);
          const gn = p.glyphN || Math.round(Math.sqrt(glyph.length)) || 25;
          upsertFeed({
            type: "vburst-frame",
            from: p.nick || p.id || "cast-" + i,
            glyph: Array.from(glyph),
            glyphN: gn,
            pos: p.pos || null,
          });
        } catch (_) {}
      });
    };
  } catch (_) {}

  function init() {
    load();
    if (el.hub && !el.hub.value) el.hub.value = defaultHubWS();
    if (el.connect) el.connect.addEventListener("click", connect);
    if (el.demo) el.demo.addEventListener("click", runDemo);
    if (el.clear) el.clear.addEventListener("click", clearLive);
    if (el.wave) {
      el.wave.addEventListener("click", () => {
        waveOn = !waveOn;
        syncWaveUI();
        save();
      });
    }
    if (el.waveMode) {
      el.waveMode.addEventListener("change", () => {
        waveMode = el.waveMode.value || "cascade";
        save();
      });
    }
    if (el.waveSpeed) {
      el.waveSpeed.addEventListener("input", () => {
        waveSpeed = parseFloat(el.waveSpeed.value) || 1;
        save();
      });
    }
    syncWaveUI();
    setStatus(
      "<strong>full sphere</strong> · " +
        meta.seats.toLocaleString() +
        " seats + " +
        Nshell.toLocaleString() +
        " shell · color wave · Connect for live Glyphs",
      "live"
    );
    if (el.legend) {
      el.legend.innerHTML =
        "cascade wave (all pts)<br/>mint = live Glyph seat<br/>" +
        TOTAL.toLocaleString() +
        " points";
    }
    if (location.protocol !== "file:" && !(location.host || "").includes("github.io")) {
      connect();
    }
    requestAnimationFrame(frame);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
