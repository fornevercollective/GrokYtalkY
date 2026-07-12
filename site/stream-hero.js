/**
 * Stream hero + in-terminal video sim.
 * Modes: sim (default), camera, file.
 * Camera failure → auto sim (handles permission / insecure context).
 */
(function () {
  'use strict';

  const video = document.getElementById('hero-video');
  const canvas = document.getElementById('hero-canvas');
  const termCanvas = document.getElementById('term-canvas');
  const placeholder = document.getElementById('stream-placeholder');
  const statusEl = document.getElementById('stream-status');
  const metaEl = document.getElementById('stream-meta');
  const pill = document.getElementById('live-pill');
  const chat = document.getElementById('stream-chat');
  const btnCam = document.getElementById('btn-cam');
  const btnSim = document.getElementById('btn-sim');
  const fileInput = document.getElementById('file-video');
  const feedLine = document.getElementById('term-feed-line');
  const termLabel = document.getElementById('term-cam-label');

  if (!canvas) return;

  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  const tctx = termCanvas ? termCanvas.getContext('2d', { willReadFrequently: true }) : null;

  let stream = null;
  let raf = 0;
  let mode = 'sim'; // sim | camera | file
  let running = false;
  let objectUrl = null;
  let frames = 0;
  let fpsT0 = performance.now();
  let t0 = performance.now();

  // offscreen for sim source
  const sim = document.createElement('canvas');
  const sctx = sim.getContext('2d');

  function setStatus(text, live) {
    if (statusEl) statusEl.textContent = text;
    if (pill) {
      pill.textContent = live ? '● live' : '○ idle';
      pill.classList.toggle('on', !!live);
    }
    if (feedLine) feedLine.textContent = mode;
    if (termLabel) {
      termLabel.textContent =
        mode === 'sim' ? 'cam sim · half-block' :
        mode === 'camera' ? 'cam live · half-block' :
        'file · half-block';
    }
  }

  function pushBubble(text, cls) {
    if (!chat) return;
    const d = document.createElement('div');
    d.className = 'bubble ' + (cls || 'sys');
    d.textContent = text;
    chat.appendChild(d);
    while (chat.children.length > 6) chat.removeChild(chat.firstChild);
    chat.scrollTop = chat.scrollHeight;
  }

  function stopAll() {
    running = false;
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    if (stream) {
      stream.getTracks().forEach((t) => t.stop());
      stream = null;
    }
    if (video) {
      video.srcObject = null;
      video.removeAttribute('src');
      try { video.load(); } catch (_) {}
    }
    if (objectUrl) {
      URL.revokeObjectURL(objectUrl);
      objectUrl = null;
    }
  }

  function sizeCanvases() {
    const wrap = canvas.parentElement;
    const w = Math.max(280, wrap ? wrap.clientWidth : 480);
    const cols = Math.min(96, Math.max(48, Math.floor(w / 6)));
    const rows = Math.min(40, Math.max(18, Math.floor(cols * 0.45)));
    canvas.width = cols;
    canvas.height = rows * 2;
    canvas.style.width = '100%';
    canvas.style.height = '100%';
    canvas.style.objectFit = 'cover';
    canvas.style.imageRendering = 'pixelated';

    sim.width = cols;
    sim.height = rows * 2;

    if (termCanvas) {
      const tw = termCanvas.parentElement ? termCanvas.parentElement.clientWidth : 280;
      const tc = Math.min(80, Math.max(40, Math.floor(tw / 4)));
      const tr = Math.max(12, Math.floor(tc * 0.35));
      termCanvas.width = tc;
      termCanvas.height = tr * 2;
    }
  }

  /** Procedural "camera" sim — testsrc-like motion + soft face blob */
  function paintSim(targetCtx, w, h, time) {
    const t = time * 0.001;
    const img = targetCtx.createImageData(w, h);
    const d = img.data;
    const cx = w * 0.5 + Math.sin(t * 0.7) * w * 0.08;
    const cy = h * 0.42 + Math.cos(t * 0.5) * h * 0.05;
    const faceR = Math.min(w, h) * 0.22;

    for (let y = 0; y < h; y++) {
      for (let x = 0; x < w; x++) {
        const i = (y * w + x) * 4;
        // scanlines / room
        const scan = ((y + Math.floor(t * 40)) % 6 === 0) ? 12 : 0;
        const nx = x / w;
        const ny = y / h;
        // bg gradient
        let r = 18 + ny * 40 + Math.sin(nx * 6 + t) * 10;
        let g = 22 + ny * 35 + Math.cos(nx * 4 - t * 0.8) * 8;
        let b = 40 + nx * 50 + Math.sin(t + ny * 5) * 15;
        // floating orbs
        const o1 = Math.hypot(x - w * (0.2 + 0.1 * Math.sin(t)), y - h * 0.7);
        if (o1 < 18) {
          r += 40; g += 60; b += 30;
        }
        // soft "face" blob
        const dFace = Math.hypot(x - cx, y - cy);
        if (dFace < faceR) {
          const k = 1 - dFace / faceR;
          const skin = 0.55 + 0.45 * k;
          r = r * (1 - skin) + (180 + 40 * Math.sin(t + x * 0.1)) * skin;
          g = g * (1 - skin) + (140 + 20 * k) * skin;
          b = b * (1 - skin) + (120 + 15 * k) * skin;
        }
        // eyes
        if (Math.hypot(x - (cx - faceR * 0.35), y - (cy - faceR * 0.15)) < faceR * 0.12) {
          r = 20; g = 25; b = 40;
        }
        if (Math.hypot(x - (cx + faceR * 0.35), y - (cy - faceR * 0.15)) < faceR * 0.12) {
          r = 20; g = 25; b = 40;
        }
        d[i] = Math.max(0, Math.min(255, r + scan));
        d[i + 1] = Math.max(0, Math.min(255, g + scan * 0.5));
        d[i + 2] = Math.max(0, Math.min(255, b));
        d[i + 3] = 255;
      }
    }
    targetCtx.putImageData(img, 0, 0);
  }

  function copyToTerm() {
    if (!tctx || !termCanvas) return;
    tctx.imageSmoothingEnabled = false;
    tctx.drawImage(canvas, 0, 0, termCanvas.width, termCanvas.height);
  }

  function tick() {
    if (!running) return;
    const now = performance.now();
    const w = canvas.width;
    const h = canvas.height;

    if (mode === 'sim') {
      paintSim(sctx, sim.width, sim.height, now - t0);
      ctx.imageSmoothingEnabled = false;
      ctx.drawImage(sim, 0, 0, w, h);
    } else if (video && video.readyState >= 2) {
      ctx.drawImage(video, 0, 0, w, h);
      const img = ctx.getImageData(0, 0, w, h);
      const d = img.data;
      for (let i = 0; i < d.length; i += 4) {
        d[i] = Math.min(255, d[i] * 1.06);
        d[i + 1] = Math.min(255, d[i + 1] * 1.04);
      }
      ctx.putImageData(img, 0, 0);
    } else if (mode !== 'sim') {
      // waiting for video frames — keep last or sim briefly
      paintSim(sctx, sim.width, sim.height, now - t0);
      ctx.drawImage(sim, 0, 0, w, h);
    }

    copyToTerm();

    frames++;
    if (now - fpsT0 > 1000) {
      const fps = Math.round((frames * 1000) / (now - fpsT0));
      frames = 0;
      fpsT0 = now;
      if (metaEl) metaEl.textContent = `half-block · ${w}×${h} · ${fps} fps · ${mode}`;
    }
    raf = requestAnimationFrame(tick);
  }

  function showLiveUI() {
    placeholder && (placeholder.hidden = true);
    canvas.classList.add('active');
    running = true;
    sizeCanvases();
    fpsT0 = performance.now();
    frames = 0;
    if (raf) cancelAnimationFrame(raf);
    raf = requestAnimationFrame(tick);
  }

  function startSim(reason) {
    stopAll();
    mode = 'sim';
    t0 = performance.now();
    showLiveUI();
    setStatus(reason || 'Sim feed · camera not required', true);
    if (btnCam) btnCam.textContent = 'Start camera';
  }

  async function startCamera() {
    stopAll();
    mode = 'camera';
    if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
      pushBubble('no mediaDevices — using sim', 'sys');
      startSim('Sim feed · getUserMedia unavailable');
      return;
    }
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'user', width: { ideal: 1280 }, height: { ideal: 720 } },
        audio: false,
      });
      video.srcObject = stream;
      await video.play();
      showLiveUI();
      setStatus('Camera streaming · hero + terminal sim mirror', true);
      pushBubble('camera live', 'sys');
      if (btnCam) btnCam.textContent = 'Stop camera';
    } catch (err) {
      const msg = (err && err.message) ? err.message : String(err);
      pushBubble('camera blocked — sim feed', 'sys');
      startSim('Sim feed · camera blocked (' + msg.slice(0, 48) + '…)');
    }
  }

  function startFile(file) {
    stopAll();
    mode = 'file';
    objectUrl = URL.createObjectURL(file);
    video.src = objectUrl;
    video.muted = true;
    video.loop = true;
    video.play().then(() => {
      showLiveUI();
      setStatus('Video · ' + file.name, true);
      pushBubble('watching ' + file.name, 'me');
      if (btnCam) btnCam.textContent = 'Start camera';
    }).catch((err) => {
      pushBubble('file play failed — sim', 'sys');
      startSim('Sim feed · file error');
    });
  }

  btnCam && btnCam.addEventListener('click', () => {
    if (running && mode === 'camera' && stream) {
      startSim('Sim feed · camera stopped');
      pushBubble('camera stopped · sim', 'sys');
    } else {
      startCamera();
    }
  });

  btnSim && btnSim.addEventListener('click', () => {
    startSim('Sim feed · manual');
    pushBubble('sim feed on', 'sys');
  });

  fileInput && fileInput.addEventListener('change', () => {
    const f = fileInput.files && fileInput.files[0];
    if (f) startFile(f);
  });

  window.addEventListener('resize', () => {
    if (running) sizeCanvases();
  });

  // Auto-start sim so hero always has motion (camera optional)
  startSim('Sim feed live · try Start camera if permitted');
  pushBubble('sim feed auto-started (camera optional)', 'sys');
})();
