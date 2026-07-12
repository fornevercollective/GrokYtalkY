/**
 * Multi-feed simulcast wall (overview VideoFeedsLab / vwall inspired).
 * Controls: size, FPS, style (half | blocks | points | ascii | halftone).
 * Sources: sim | camera | file — camera fail → sim.
 */
(function () {
  'use strict';

  const MAX_FEEDS = 6;
  const STYLES = ['half', 'blocks', 'points', 'ascii', 'halftone', 'depth', 'gsplat'];

  const el = {
    wall: document.getElementById('feed-wall'),
    status: document.getElementById('stream-status'),
    meta: document.getElementById('stream-meta'),
    pill: document.getElementById('live-pill'),
    chat: document.getElementById('stream-chat'),
    termCanvas: document.getElementById('term-canvas'),
    termLabel: document.getElementById('term-cam-label'),
    feedLine: document.getElementById('term-feed-line'),
    btnAddSim: document.getElementById('btn-add-sim'),
    btnAddCam: document.getElementById('btn-add-cam'),
    btnCam: document.getElementById('btn-cam'),
    btnSim: document.getElementById('btn-sim'),
    fileInput: document.getElementById('file-video'),
    ctrlSize: document.getElementById('ctrl-size'),
    ctrlFps: document.getElementById('ctrl-fps'),
    ctrlStyle: document.getElementById('ctrl-style'),
    ctrlLayout: document.getElementById('ctrl-layout'),
    sizeVal: document.getElementById('ctrl-size-val'),
    fpsVal: document.getElementById('ctrl-fps-val'),
  };

  if (!el.wall) return;

  /** @type {Array<{
   *   id: string, kind: string, label: string,
   *   video: HTMLVideoElement|null, stream: MediaStream|null, objectUrl: string|null,
   *   canvas: HTMLCanvasElement, ctx: CanvasRenderingContext2D, seed: number
   * }>} */
  let feeds = [];
  let activeId = null;
  let settings = {
    size: 64,
    fps: 12,
    style: 'half',
    layout: 'grid', // grid | focus
  };
  let lastFrame = 0;
  let raf = 0;
  let frameCount = 0;
  let fpsT0 = performance.now();
  let displayFps = 0;
  let t0 = performance.now();
  let uid = 0;

  const termCtx = el.termCanvas ? el.termCanvas.getContext('2d') : null;

  const BAYER4 = [
    [0, 8, 2, 10],
    [12, 4, 14, 6],
    [3, 11, 1, 9],
    [15, 7, 13, 5],
  ];
  const ASCII_RAMP = ' .:-=+*#%@';

  // ── UI helpers ──────────────────────────────────────────────
  function setStatus(text, live) {
    if (el.status) el.status.textContent = text;
    if (el.pill) {
      el.pill.textContent = live ? '● live' : '○ idle';
      el.pill.classList.toggle('on', !!live);
    }
  }

  function pushBubble(text, cls) {
    if (!el.chat) return;
    const d = document.createElement('div');
    d.className = 'bubble ' + (cls || 'sys');
    d.textContent = text;
    el.chat.appendChild(d);
    while (el.chat.children.length > 8) el.chat.removeChild(el.chat.firstChild);
    el.chat.scrollTop = el.chat.scrollHeight;
  }

  function syncCtrlLabels() {
    if (el.sizeVal) el.sizeVal.textContent = String(settings.size);
    if (el.fpsVal) el.fpsVal.textContent = String(settings.fps);
    if (el.ctrlSize) el.ctrlSize.value = String(settings.size);
    if (el.ctrlFps) el.ctrlFps.value = String(settings.fps);
    if (el.ctrlStyle) el.ctrlStyle.value = settings.style;
    if (el.ctrlLayout) el.ctrlLayout.value = settings.layout;
    if (el.feedLine) {
      el.feedLine.textContent = `${feeds.length}×${settings.style}@${settings.fps}`;
    }
    if (el.termLabel) {
      el.termLabel.textContent = `vwall · ${settings.style} · ${settings.size}`;
    }
  }

  function loadSettings() {
    try {
      const raw = localStorage.getItem('gy-vwall');
      if (!raw) return;
      const s = JSON.parse(raw);
      if (typeof s.size === 'number' && s.size >= 32 && s.size <= 128) settings.size = s.size;
      if (typeof s.fps === 'number' && s.fps >= 1 && s.fps <= 60) settings.fps = s.fps;
      if (s.style && STYLES.includes(s.style)) settings.style = s.style;
      if (s.layout === 'grid' || s.layout === 'focus') settings.layout = s.layout;
    } catch (_) {}
  }

  function saveSettings() {
    try {
      localStorage.setItem('gy-vwall', JSON.stringify(settings));
    } catch (_) {}
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  // ── Sim source ──────────────────────────────────────────────
  function paintSimInto(ctx, w, h, time, seed) {
    const t = time * 0.001;
    const phase = (seed || 1) * 1.7;
    const img = ctx.createImageData(w, h);
    const d = img.data;
    const cx = w * 0.5 + Math.sin(t * 0.7 + phase) * w * 0.08;
    const cy = h * 0.42 + Math.cos(t * 0.5 + phase * 0.3) * h * 0.05;
    const faceR = Math.min(w, h) * 0.22;
    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        const i = (y * w + x) * 4;
        const scan = (y + Math.floor(t * 40)) % 6 === 0 ? 12 : 0;
        const nx = x / w;
        const ny = y / h;
        let r = 18 + ny * 40 + Math.sin(nx * 6 + t + phase) * 10;
        let g = 22 + ny * 35 + Math.cos(nx * 4 - t * 0.8 + phase) * 8;
        let b = 40 + nx * 50 + Math.sin(t + ny * 5 + phase) * 15;
        const o1 = Math.hypot(x - w * (0.2 + 0.1 * Math.sin(t + phase)), y - h * 0.7);
        if (o1 < 18 + ((seed || 1) % 3) * 4) {
          r += 30 + (seed || 1) * 5;
          g += 50;
          b += 40;
        }
        const dFace = Math.hypot(x - cx, y - cy);
        if (dFace < faceR) {
          const k = 1 - dFace / faceR;
          const skin = 0.55 + 0.45 * k;
          r = r * (1 - skin) + (160 + (seed || 1) * 12 + 40 * Math.sin(t + x * 0.1)) * skin;
          g = g * (1 - skin) + (130 + 20 * k) * skin;
          b = b * (1 - skin) + (110 + 15 * k) * skin;
        }
        if (Math.hypot(x - (cx - faceR * 0.35), y - (cy - faceR * 0.15)) < faceR * 0.12) {
          r = 20;
          g = 25;
          b = 40;
        }
        if (Math.hypot(x - (cx + faceR * 0.35), y - (cy - faceR * 0.15)) < faceR * 0.12) {
          r = 20;
          g = 25;
          b = 40;
        }
        d[i] = Math.max(0, Math.min(255, r + scan));
        d[i + 1] = Math.max(0, Math.min(255, g + scan * 0.5));
        d[i + 2] = Math.max(0, Math.min(255, b));
        d[i + 3] = 255;
      }
    }
    ctx.putImageData(img, 0, 0);
  }

  // ── Style passes (overview videoLabEffects subset) ──────────
  function lum(r, g, b) {
    return 0.299 * r + 0.587 * g + 0.114 * b;
  }

  function applyStyle(ctx, w, h, style) {
    if (style === 'half') {
      // half-block aesthetic: vertical pair average → brighten lower half slightly
      const img = ctx.getImageData(0, 0, w, h);
      const d = img.data;
      for (let y = 0; y < h - 1; y += 2) {
        for (let x = 0; x < w; x++) {
          const i0 = (y * w + x) * 4;
          const i1 = ((y + 1) * w + x) * 4;
          const r = (d[i0] + d[i1]) >> 1;
          const g = (d[i0 + 1] + d[i1 + 1]) >> 1;
          const b = (d[i0 + 2] + d[i1 + 2]) >> 1;
          d[i0] = r;
          d[i0 + 1] = g;
          d[i0 + 2] = b;
          d[i1] = Math.min(255, r + 18);
          d[i1 + 1] = Math.min(255, g + 14);
          d[i1 + 2] = Math.min(255, b + 22);
        }
      }
      ctx.putImageData(img, 0, 0);
      return;
    }

    const img = ctx.getImageData(0, 0, w, h);
    const d = img.data;

    if (style === 'blocks') {
      const cell = Math.max(3, Math.round(settings.size / 20));
      for (let y = 0; y < h; y += cell) {
        for (let x = 0; x < w; x += cell) {
          let r = 0,
            g = 0,
            b = 0,
            n = 0;
          for (let dy = 0; dy < cell && y + dy < h; dy++) {
            for (let dx = 0; dx < cell && x + dx < w; dx++) {
              const i = ((y + dy) * w + (x + dx)) * 4;
              r += d[i];
              g += d[i + 1];
              b += d[i + 2];
              n++;
            }
          }
          r = (r / n) | 0;
          g = (g / n) | 0;
          b = (b / n) | 0;
          for (let dy = 0; dy < cell && y + dy < h; dy++) {
            for (let dx = 0; dx < cell && x + dx < w; dx++) {
              const i = ((y + dy) * w + (x + dx)) * 4;
              d[i] = r;
              d[i + 1] = g;
              d[i + 2] = b;
            }
          }
        }
      }
      ctx.putImageData(img, 0, 0);
      return;
    }

    if (style === 'points') {
      const cell = Math.max(4, Math.round(settings.size / 16));
      const src = new Uint8ClampedArray(d);
      for (let i = 0; i < d.length; i += 4) {
        d[i] = d[i + 1] = d[i + 2] = 8;
      }
      for (let y = cell >> 1; y < h; y += cell) {
        for (let x = cell >> 1; x < w; x += cell) {
          const si = (y * w + x) * 4;
          const L = lum(src[si], src[si + 1], src[si + 2]) / 255;
          const rad = cell * 0.45 * (0.15 + L * 0.95);
          const r2 = rad * rad;
          for (let dy = -cell; dy <= cell; dy++) {
            for (let dx = -cell; dx <= cell; dx++) {
              if (dx * dx + dy * dy > r2) continue;
              const xx = x + dx;
              const yy = y + dy;
              if (xx < 0 || yy < 0 || xx >= w || yy >= h) continue;
              const i = (yy * w + xx) * 4;
              d[i] = src[si];
              d[i + 1] = src[si + 1];
              d[i + 2] = src[si + 2];
            }
          }
        }
      }
      ctx.putImageData(img, 0, 0);
      return;
    }

    if (style === 'halftone') {
      const cell = Math.max(4, Math.round(settings.size / 14));
      const src = new Uint8ClampedArray(d);
      for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) {
          const cx = Math.floor(x / cell) * cell + (cell >> 1);
          const cy = Math.floor(y / cell) * cell + (cell >> 1);
          const si = (Math.min(h - 1, cy) * w + Math.min(w - 1, cx)) * 4;
          const L = lum(src[si], src[si + 1], src[si + 2]) / 255;
          const dist = Math.hypot(x - cx, y - cy);
          const maxR = cell * 0.48 * (1 - L);
          const ink = dist <= maxR ? 0 : 245;
          const i = (y * w + x) * 4;
          d[i] = d[i + 1] = d[i + 2] = ink;
        }
      }
      ctx.putImageData(img, 0, 0);
      return;
    }

    if (style === 'ascii') {
      // sample luminance first (with Bayer), then paint glyphs
      const gray = new Float32Array(w * h);
      for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) {
          const i = (y * w + x) * 4;
          let L = lum(d[i], d[i + 1], d[i + 2]) / 255;
          const b = (BAYER4[y % 4][x % 4] + 0.5) / 16 - 0.5;
          L = Math.max(0, Math.min(1, L + b * 0.18));
          gray[y * w + x] = L;
        }
      }
      const cell = Math.max(6, Math.floor(settings.size / 12));
      ctx.fillStyle = '#0a0a0e';
      ctx.fillRect(0, 0, w, h);
      ctx.font = `bold ${cell}px "IBM Plex Mono", ui-monospace, monospace`;
      ctx.textBaseline = 'top';
      for (let y = 0; y < h; y += cell) {
        for (let x = 0; x < w; x += cell) {
          const sx = Math.min(w - 1, x + (cell >> 1));
          const sy = Math.min(h - 1, y + (cell >> 1));
          const L = gray[sy * w + sx];
          const idx = Math.min(ASCII_RAMP.length - 1, Math.floor(L * (ASCII_RAMP.length - 1)));
          const ch = ASCII_RAMP[idx];
          const v = Math.round(L * 255);
          ctx.fillStyle = `rgb(${v},${Math.min(255, v + 24)},${Math.min(255, v + 48)})`;
          ctx.fillText(ch, x, y);
        }
      }
      return;
    }

    // zip-lite-ish mono depth false-color (aito / ZipDepth lineage)
    if (style === 'depth' || style === 'gsplat') {
      const L0 = new Float32Array(w * h);
      for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) {
          const i = (y * w + x) * 4;
          L0[y * w + x] = lum(d[i], d[i + 1], d[i + 2]) / 255;
        }
      }
      const cx0 = w * 0.5;
      const cy0 = h * 0.42;
      const maxR = Math.hypot(cx0, cy0) || 1;
      const t = performance.now() * 0.001;
      for (let y = 0; y < h; y++) {
        for (let x = 0; x < w; x++) {
          const i = (y * w + x) * 4;
          const L = L0[y * w + x];
          const radial = Math.hypot(x - cx0, y - cy0) / maxR;
          const vert = y / Math.max(1, h - 1);
          let z = 0.36 * (1 - radial) + 0.34 * vert + 0.3 * (1 - L);
          z = Math.max(0, Math.min(1, z));
          const tr = Math.round((0.15 + 0.85 * z * z + 0.4 * z) * 255);
          const tg = Math.round((0.08 + 0.55 * z + 0.25 * (1 - Math.abs(z - 0.5) * 2)) * 255);
          const tb = Math.round((0.35 + 0.65 * (1 - z)) * 255);
          if (style === 'depth') {
            d[i] = tr;
            d[i + 1] = tg;
            d[i + 2] = tb;
          } else {
            // gsplat stack: shade + thermal blend
            const edge = Math.min(
              1,
              Math.hypot(
                L0[y * w + Math.min(w - 1, x + 1)] - L0[y * w + Math.max(0, x - 1)],
                L0[Math.min(h - 1, y + 1) * w + x] - L0[Math.max(0, y - 1) * w + x],
              ) * 20,
            );
            const shade = 0.22 + 0.78 * (0.55 + 0.2 * (1 - radial));
            const sweep = Math.sin(t * 1.65 + z * Math.PI * 4.25) * 0.5 + 0.5;
            const aT = Math.min(0.85, 0.1 + 0.36 * sweep + 0.46 * Math.pow(1 - z, 1.28));
            d[i] = Math.round(d[i] * shade * (1 - aT) + tr * aT + edge * 20);
            d[i + 1] = Math.round(d[i + 1] * shade * (1 - aT) + tg * aT);
            d[i + 2] = Math.round(d[i + 2] * shade * (1 - aT) + tb * aT);
          }
        }
      }
      ctx.putImageData(img, 0, 0);
    }
  }

  // ── Feed management ─────────────────────────────────────────
  function createFeedTile(feed) {
    const tile = document.createElement('div');
    tile.className = 'vwall-tile' + (feed.id === activeId ? ' active' : '');
    tile.dataset.id = feed.id;
    tile.innerHTML =
      `<div class="vwall-tile-bar">` +
      `<span class="vwall-tile-name">${escapeHtml(feed.label)}</span>` +
      `<span class="vwall-tile-kind">${escapeHtml(feed.kind)}</span>` +
      `<button type="button" class="vwall-tile-x" title="Remove" aria-label="Remove feed">×</button>` +
      `</div>`;
    tile.appendChild(feed.canvas);
    feed.canvas.className = 'vwall-tile-canvas';
    tile.addEventListener('click', (e) => {
      if (e.target.classList && e.target.classList.contains('vwall-tile-x')) {
        removeFeed(feed.id);
        return;
      }
      setActive(feed.id);
    });
    return tile;
  }

  function renderWall() {
    el.wall.innerHTML = '';
    el.wall.className =
      'feed-wall layout-' + settings.layout + ' n-' + Math.min(Math.max(feeds.length, 1), MAX_FEEDS);
    feeds.forEach((f) => {
      el.wall.appendChild(createFeedTile(f));
    });
    setStatus(
      feeds.length
        ? `${feeds.length} feed(s) · ${settings.style} · ${settings.size}px · ${settings.fps} fps`
        : 'No feeds — add sim / camera / file',
      feeds.length > 0,
    );
    syncCtrlLabels();
  }

  function setActive(id) {
    activeId = id;
    renderWall();
  }

  function removeFeed(id) {
    const f = feeds.find((x) => x.id === id);
    if (!f) return;
    if (f.stream) f.stream.getTracks().forEach((t) => t.stop());
    if (f.objectUrl) URL.revokeObjectURL(f.objectUrl);
    if (f.video) {
      f.video.pause();
      f.video.srcObject = null;
      f.video.removeAttribute('src');
    }
    feeds = feeds.filter((x) => x.id !== id);
    if (activeId === id) activeId = feeds[0] ? feeds[0].id : null;
    renderWall();
    pushBubble('removed ' + (f.label || id), 'sys');
  }

  function addFeed(kind, label, videoEl, stream, objectUrl) {
    if (feeds.length >= MAX_FEEDS) {
      pushBubble('max ' + MAX_FEEDS + ' feeds', 'sys');
      return null;
    }
    const id = 'f' + ++uid;
    const canvas = document.createElement('canvas');
    const ctx = canvas.getContext('2d', { willReadFrequently: true });
    const feed = {
      id,
      kind,
      label: label || kind,
      video: videoEl || null,
      stream: stream || null,
      objectUrl: objectUrl || null,
      canvas,
      ctx,
      seed: uid,
    };
    feeds.push(feed);
    activeId = id;
    renderWall();
    pushBubble('+' + feed.label + ' (' + kind + ')', 'sys');
    return feed;
  }

  function addSim() {
    addFeed('sim', 'sim-' + (uid + 1), null, null, null);
  }

  async function addCamera() {
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      pushBubble('no mediaDevices — sim', 'sys');
      addSim();
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'user', width: { ideal: 1280 }, height: { ideal: 720 } },
        audio: false,
      });
      const v = document.createElement('video');
      v.playsInline = true;
      v.muted = true;
      v.srcObject = stream;
      await v.play();
      addFeed('camera', 'cam-' + (uid + 1), v, stream, null);
    } catch (err) {
      const msg = err && err.message ? err.message : String(err);
      pushBubble('camera blocked — sim', 'sys');
      addSim();
      setStatus('Sim (camera: ' + msg.slice(0, 40) + '…)', true);
    }
  }

  function addFile(file) {
    const url = URL.createObjectURL(file);
    const v = document.createElement('video');
    v.playsInline = true;
    v.muted = true;
    v.loop = true;
    v.src = url;
    v.play()
      .then(() => {
        addFeed('file', file.name.slice(0, 18), v, null, url);
      })
      .catch(() => {
        URL.revokeObjectURL(url);
        pushBubble('file failed — sim', 'sys');
        addSim();
      });
  }

  // ── Frame loop (FPS capped) ─────────────────────────────────
  function feedSize() {
    const s = settings.size;
    const aspect = 16 / 10;
    let w = s;
    let h = Math.round(s / aspect);
    if (h % 2) h++;
    if (h < 8) h = 8;
    return { w, h };
  }

  function drawFeed(feed, now) {
    const { w, h } = feedSize();
    if (feed.canvas.width !== w || feed.canvas.height !== h) {
      feed.canvas.width = w;
      feed.canvas.height = h;
    }
    const ctx = feed.ctx;
    const hasVideo = feed.video && feed.video.readyState >= 2;
    if (feed.kind === 'sim' || !hasVideo) {
      paintSimInto(ctx, w, h, now - t0 + feed.seed * 900, feed.seed);
    } else {
      ctx.imageSmoothingEnabled = settings.size < 96;
      ctx.drawImage(feed.video, 0, 0, w, h);
    }
    applyStyle(ctx, w, h, settings.style);
  }

  function copyActiveToTerm() {
    if (!termCtx || !el.termCanvas) return;
    const f = feeds.find((x) => x.id === activeId) || feeds[0];
    if (!f) return;
    const parent = el.termCanvas.parentElement;
    const tw = parent ? Math.max(120, parent.clientWidth - 8) : 200;
    const tc = Math.min(160, Math.max(64, Math.floor(tw / 2.2)));
    const tr = Math.max(24, Math.floor(tc * 0.45));
    if (el.termCanvas.width !== tc || el.termCanvas.height !== tr) {
      el.termCanvas.width = tc;
      el.termCanvas.height = tr;
    }
    termCtx.imageSmoothingEnabled = false;
    termCtx.fillStyle = '#050508';
    termCtx.fillRect(0, 0, tc, tr);
    termCtx.drawImage(f.canvas, 0, 0, tc, tr);
  }

  function loop(now) {
    raf = requestAnimationFrame(loop);
    const interval = 1000 / Math.max(1, settings.fps);
    if (now - lastFrame < interval) return;
    lastFrame = now - ((now - lastFrame) % interval);

    feeds.forEach((f) => drawFeed(f, now));
    copyActiveToTerm();

    frameCount++;
    if (now - fpsT0 > 1000) {
      displayFps = Math.round((frameCount * 1000) / (now - fpsT0));
      frameCount = 0;
      fpsT0 = now;
      if (el.meta) {
        el.meta.textContent = `${feeds.length} feeds · ${settings.style} · ${settings.size} · ${displayFps} fps`;
      }
    }
  }

  // ── Wire controls ───────────────────────────────────────────
  loadSettings();
  syncCtrlLabels();

  if (el.ctrlSize) {
    el.ctrlSize.addEventListener('input', () => {
      settings.size = Number(el.ctrlSize.value) || 64;
      syncCtrlLabels();
      saveSettings();
    });
  }
  if (el.ctrlFps) {
    el.ctrlFps.addEventListener('input', () => {
      settings.fps = Number(el.ctrlFps.value) || 12;
      syncCtrlLabels();
      saveSettings();
    });
  }
  if (el.ctrlStyle) {
    el.ctrlStyle.addEventListener('change', () => {
      settings.style = STYLES.includes(el.ctrlStyle.value) ? el.ctrlStyle.value : 'half';
      syncCtrlLabels();
      saveSettings();
      pushBubble('style → ' + settings.style, 'sys');
    });
  }
  if (el.ctrlLayout) {
    el.ctrlLayout.addEventListener('change', () => {
      settings.layout = el.ctrlLayout.value === 'focus' ? 'focus' : 'grid';
      renderWall();
      saveSettings();
      pushBubble('layout → ' + settings.layout, 'sys');
    });
  }

  if (el.btnAddSim) el.btnAddSim.addEventListener('click', () => addSim());
  if (el.btnSim) el.btnSim.addEventListener('click', () => addSim());
  if (el.btnAddCam) el.btnAddCam.addEventListener('click', () => addCamera());
  if (el.btnCam) el.btnCam.addEventListener('click', () => addCamera());
  if (el.fileInput) {
    el.fileInput.addEventListener('change', () => {
      const f = el.fileInput.files && el.fileInput.files[0];
      if (f) addFile(f);
      el.fileInput.value = '';
    });
  }

  // seed wall with 2 sims (simulcast demo)
  addSim();
  addSim();
  setStatus('vwall simulcast · sim feeds (add cam/file)', true);
  pushBubble('vwall: size / fps / style · multi-feed', 'sys');

  t0 = performance.now();
  raf = requestAnimationFrame(loop);

  // cleanup on navigate away
  window.addEventListener('beforeunload', () => {
    cancelAnimationFrame(raf);
    feeds.slice().forEach((f) => removeFeed(f.id));
  });
})();
