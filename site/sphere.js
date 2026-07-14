/**
 * Sphere Glyph — live mesh Glyphs on Sphere Vegas Bloch³ seats.
 * Focused viewer only: seats + hub vburst/gyst. No music/MOPA/VR/kBatch.
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
  };

  const seats = SPHERE.seatsCached();
  const N = seats.length;
  const meta = SPHERE.meta();

  // ── camera ──
  let dist = 9.5;
  let rotX = 0.35;
  let rotY = 0.4;
  let panX = 0;
  let panY = 0.15;
  let dragging = false;
  let lastMX = 0;
  let lastMY = 0;

  // ── live feeds: id → { nick, glyph, n, pos, t, seatIdx } ──
  const feeds = new Map();
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
      gl_PointSize = max(1.0, uSize * (0.55 + aBrt * 1.6));
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
      float a = smoothstep(0.5, 0.12, d) * (0.25 + 0.75 * vBrt);
      gl_FragColor = vec4(vCol * (0.35 + 0.9 * vBrt), a);
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

  const pos = new Float32Array(N * 3);
  const col = new Float32Array(N * 3);
  const brt = new Float32Array(N);
  // base colors by section
  const SEC_COL = {
    100: [0.45, 0.55, 0.95],
    200: [0.4, 0.75, 0.9],
    300: [0.5, 0.85, 0.65],
    400: [0.75, 0.65, 0.4],
    500: [0.85, 0.45, 0.5],
    floor: [0.35, 0.8, 0.4],
  };
  for (let i = 0; i < N; i++) {
    const s = seats[i];
    pos[i * 3] = s.x * 3;
    pos[i * 3 + 1] = s.y * 1.5;
    pos[i * 3 + 2] = s.z * 3;
    const c = SEC_COL[s.section] || [0.4, 0.45, 0.55];
    col[i * 3] = c[0] * 0.35;
    col[i * 3 + 1] = c[1] * 0.35;
    col[i * 3 + 2] = c[2] * 0.35;
    brt[i] = 0.08;
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
  gl.clearColor(0.027, 0.027, 0.039, 1);

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
    const proj = m4P(0.85, aspect, 0.1, 100);
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

  function setStatus(t, kind) {
    if (!el.status) return;
    el.status.textContent = t || "";
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
    if (!pos) return -1;
    if (typeof pos.idx === "number" && pos.idx >= 0 && pos.idx < N) return pos.idx | 0;
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
    // stable hash fallback into seat cloud
    const key = String(from || "anon");
    let h = 0;
    for (let i = 0; i < key.length; i++) h = (h * 33 + key.charCodeAt(i)) | 0;
    return Math.abs(h) % N;
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
    const pos = msg.pos || null;
    const seatIdx = resolveSeatIdx(pos, nick);
    const id = nick;
    feeds.set(id, {
      nick,
      glyph,
      n: gn,
      pos,
      seatIdx,
      t: Date.now(),
      avg: glyphAvg(glyph),
    });
  }

  function pruneFeeds(now) {
    feeds.forEach((f, id) => {
      if (now - f.t > FEED_TTL_MS) feeds.delete(id);
    });
  }

  function applySeatColors(now) {
    // reset base
    for (let i = 0; i < N; i++) {
      const s = seats[i];
      const c = SEC_COL[s.section] || [0.4, 0.45, 0.55];
      col[i * 3] = c[0] * 0.32;
      col[i * 3 + 1] = c[1] * 0.32;
      col[i * 3 + 2] = c[2] * 0.32;
      brt[i] = 0.07 + (s.haptic ? 0.03 : 0);
    }
    // light live seats
    feeds.forEach((f) => {
      const i = f.seatIdx;
      if (i < 0 || i >= N) return;
      const age = 1 - Math.min(1, (now - f.t) / FEED_TTL_MS);
      const pulse = 0.55 + 0.45 * Math.sin(now / 180 + i * 0.01);
      const g = f.avg;
      col[i * 3] = 0.25 + 0.55 * g + 0.2 * pulse;
      col[i * 3 + 1] = 0.85 + 0.15 * pulse;
      col[i * 3 + 2] = 0.55 + 0.35 * (1 - g);
      brt[i] = Math.min(1, 0.55 + 0.45 * age * pulse);
    });
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
      ctx.globalAlpha = 0.35 + 0.65 * age;
      ctx.fillStyle = "rgba(0,0,0,0.55)";
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
    let html = "<div class='live'>" + feeds.size + " live</div>";
    const arr = [];
    feeds.forEach((f) => arr.push(f));
    arr.sort((a, b) => b.t - a.t);
    for (let i = 0; i < Math.min(12, arr.length); i++) {
      const f = arr[i];
      const seat = f.seatIdx >= 0 ? seats[f.seatIdx] : null;
      const sid = seat ? seat.id : "?";
      html +=
        "<div>" +
        f.nick +
        " · " +
        sid +
        (f.pos && f.pos.map ? "" : " · hash") +
        "</div>";
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
    applySeatColors(now);

    const w = el.gl.width;
    const h = el.gl.height;
    const aspect = w / Math.max(1, h);
    const mvp = mvpMatrix(aspect);

    gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
    gl.useProgram(prog);
    gl.uniformMatrix4fv(uMvp, false, mvp);
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    gl.uniform1f(uSize, Math.max(1.2, (5.5 / dist) * dpr));

    gl.bindBuffer(gl.ARRAY_BUFFER, bufPos);
    gl.enableVertexAttribArray(aPos);
    gl.vertexAttribPointer(aPos, 3, gl.FLOAT, false, 0, 0);
    gl.bindBuffer(gl.ARRAY_BUFFER, bufCol);
    gl.enableVertexAttribArray(aCol);
    gl.vertexAttribPointer(aCol, 3, gl.FLOAT, false, 0, 0);
    gl.bindBuffer(gl.ARRAY_BUFFER, bufBrt);
    gl.enableVertexAttribArray(aBrt);
    gl.vertexAttribPointer(aBrt, 1, gl.FLOAT, false, 0, 0);
    gl.drawArrays(gl.POINTS, 0, N);

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
        "live · " + meta.seats.toLocaleString() + " seats · " + SPHERE.summary(),
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
      if (t === "vburst-frame" || t === "news-frame") {
        upsertFeed(msg);
      } else if (t === "gyst" && (msg.kind === "hexlum" || Array.isArray(msg.data))) {
        upsertFeed(msg);
      }
    };
  }

  function runDemo() {
    if (demoTimer) {
      clearInterval(demoTimer);
      demoTimer = 0;
      if (el.demo) el.demo.classList.remove("is-on");
      setStatus("demo off · " + SPHERE.summary());
      return;
    }
    if (el.demo) el.demo.classList.add("is-on");
    setStatus("demo Glyphs on random seats · Connect hub for real casts", "live");
    demoTimer = setInterval(() => {
      const n = 25;
      const glyph = new Uint8Array(n * n);
      const t = Date.now() / 400;
      for (let i = 0; i < n * n; i++) {
        const x = i % n;
        const y = (i / n) | 0;
        glyph[i] = (128 + 100 * Math.sin(t + x * 0.4 + y * 0.3) + 40 * Math.sin(t * 1.7 + i * 0.05)) | 0;
      }
      const count = 6 + ((Math.random() * 6) | 0);
      for (let k = 0; k < count; k++) {
        const idx = (Math.random() * N) | 0;
        const seat = seats[idx];
        upsertFeed({
          type: "vburst-frame",
          from: "demo-" + (k + 1),
          glyph: Array.from(glyph),
          glyphN: n,
          pos: SPHERE.seatToMeshPos(seat),
        });
      }
    }, 280);
  }

  function clearLive() {
    feeds.clear();
    if (demoTimer) {
      clearInterval(demoTimer);
      demoTimer = 0;
      if (el.demo) el.demo.classList.remove("is-on");
    }
    updateFeedList();
    setStatus("cleared · " + SPHERE.summary());
  }

  function save() {
    try {
      localStorage.setItem(STORAGE, JSON.stringify({ hub: el.hub ? el.hub.value : "" }));
    } catch (_) {}
  }
  function load() {
    try {
      const st = JSON.parse(localStorage.getItem(STORAGE) || "{}");
      if (st.hub && el.hub) el.hub.value = st.hub;
    } catch (_) {}
  }

  // pointer orbit
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
    rotX = Math.max(-1.2, Math.min(1.2, rotX + dy * 0.005));
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
      dist = Math.max(3.5, Math.min(22, dist + e.deltaY * 0.01));
    },
    { passive: false }
  );

  // optional BroadcastChannel from glyph-cast / same-origin
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
    setStatus(
      SPHERE.summary() + " · open phone?seat=… then Connect",
      ""
    );
    if (el.legend) {
      el.legend.innerHTML =
        meta.seats.toLocaleString() +
        " seats · 16K map<br/>Bloch³ · live Glyph only";
    }
    // auto-connect when served by hub
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
