/**
 * Shared 25×25 Glyph Matrix helpers for burst / livenews / listener grids.
 */
(function (global) {
  'use strict';

  const N = 25;

  function paintGlyphCanvas(canvas, lum /* Float32Array|number[] len N*N */, opts) {
    if (!canvas) return;
    const cell = opts && opts.cell ? opts.cell : Math.max(2, Math.floor(canvas.width / N));
    const ctx = canvas.getContext('2d');
    const w = N * cell;
    const h = N * cell;
    if (canvas.width !== w) canvas.width = w;
    if (canvas.height !== h) canvas.height = h;
    ctx.fillStyle = '#050508';
    ctx.fillRect(0, 0, w, h);
    const tint = (opts && opts.tint) || 0; // 0–360 optional hue shift
    for (let y = 0; y < N; y++) {
      for (let x = 0; x < N; x++) {
        let L = lum ? lum[y * N + x] : 0;
        if (L > 1) L = L / 255;
        L = Math.max(0, Math.min(1, L));
        const v = Math.round(L * 255);
        if (tint) {
          // false-color luminance on brand hue
          const s = 0.35 + L * 0.55;
          ctx.fillStyle = `hsl(${tint} ${Math.round(s * 100)}% ${Math.round(18 + L * 62)}%)`;
        } else {
          ctx.fillStyle = `rgb(${v},${v},${Math.min(255, v + 18)})`;
        }
        ctx.fillRect(x * cell + 0.4, y * cell + 0.4, cell - 0.8, cell - 0.8);
      }
    }
  }

  /** Synthetic live-looking field (agency poster / offline stress). */
  function simLum(t, seed, talking) {
    const out = new Float32Array(N * N);
    const s = seed || 1;
    const pulse = talking ? 0.25 + 0.2 * Math.sin(t * 0.008) : 0.08;
    for (let y = 0; y < N; y++) {
      for (let x = 0; x < N; x++) {
        const n =
          Math.sin((x + s) * 0.55 + t * 0.003) *
            Math.cos((y - s) * 0.48 - t * 0.0025) *
            0.35 +
          Math.sin((x * y + s * 3) * 0.08 + t * 0.004) * 0.2;
        const cx = N * 0.5;
        const cy = N * 0.45;
        const d = Math.hypot(x - cx, y - cy) / (N * 0.55);
        let L = 0.18 + n * 0.35 + (1 - Math.min(1, d)) * 0.35 + pulse * (1 - d);
        // scanline
        if ((y + Math.floor(t / 40)) % 7 === 0) L *= 0.75;
        out[y * N + x] = Math.max(0, Math.min(1, L));
      }
    }
    return out;
  }

  /** Sample HTMLVideoElement or Image into luminance. */
  function sampleVideoToLum(video, tmpCanvas) {
    const out = new Float32Array(N * N);
    if (!video || video.readyState < 2) return out;
    const c = tmpCanvas || document.createElement('canvas');
    c.width = N;
    c.height = N;
    const ctx = c.getContext('2d', { willReadFrequently: true });
    ctx.drawImage(video, 0, 0, N, N);
    const img = ctx.getImageData(0, 0, N, N);
    for (let i = 0, g = 0; i < img.data.length; i += 4, g++) {
      out[g] = (0.299 * img.data[i] + 0.587 * img.data[i + 1] + 0.114 * img.data[i + 2]) / 255;
    }
    return out;
  }

  function paintCircleFace(canvas, lum, ringLevel, mode) {
    // mode: idle|tx|rx
    if (!canvas) return;
    const size = canvas.width;
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, size, size);
    // disk clip
    ctx.save();
    ctx.beginPath();
    ctx.arc(size / 2, size / 2, size / 2 - 2, 0, Math.PI * 2);
    ctx.clip();
    ctx.fillStyle = '#050508';
    ctx.fillRect(0, 0, size, size);
    const cell = size / N;
    for (let y = 0; y < N; y++) {
      for (let x = 0; x < N; x++) {
        let L = lum ? lum[y * N + x] : 0;
        if (L > 1) L = L / 255;
        const v = Math.round(Math.max(0, Math.min(1, L)) * 255);
        ctx.fillStyle = `rgb(${v},${v},${Math.min(255, v + 20)})`;
        ctx.fillRect(x * cell, y * cell, cell + 0.5, cell + 0.5);
      }
    }
    ctx.restore();
    // ring
    const lv = ringLevel || 0;
    ctx.beginPath();
    ctx.arc(size / 2, size / 2, size / 2 - 3, 0, Math.PI * 2);
    let col = 'rgba(125,211,252,0.35)';
    if (mode === 'tx') col = `rgba(248,113,113,${0.35 + lv * 0.5})`;
    if (mode === 'rx') col = `rgba(74,222,128,${0.3 + lv * 0.45})`;
    ctx.strokeStyle = col;
    ctx.lineWidth = 2 + lv * 4;
    ctx.stroke();
  }

  /** Bind collapsible details persistence. */
  function persistDetails(root, storageKey) {
    const key = storageKey || 'gy_details';
    let store = {};
    try {
      store = JSON.parse(localStorage.getItem(key) || '{}') || {};
    } catch (_) {}
    (root || document).querySelectorAll('details[data-persist]').forEach((d) => {
      const id = d.getAttribute('data-persist');
      if (id && store[id] === false) d.open = false;
      if (id && store[id] === true) d.open = true;
      d.addEventListener('toggle', () => {
        try {
          store[id] = d.open;
          localStorage.setItem(key, JSON.stringify(store));
        } catch (_) {}
      });
    });
  }

  global.GY_GLYPH = {
    N,
    paintGlyphCanvas,
    simLum,
    sampleVideoToLum,
    paintCircleFace,
    persistDetails,
  };
})(typeof window !== 'undefined' ? window : globalThis);
