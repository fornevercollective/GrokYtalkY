/**
 * Siri-sized video burst orb — hold to send face + mic, receive peer bursts.
 * Glyph Matrix: 25×25 luminance grid (Nothing Phone (3) length).
 */
(function () {
  'use strict';

  const GLYPH_N = 25;
  const el = {
    orb: document.getElementById('burst-orb'),
    face: document.getElementById('face-canvas'),
    ring: document.getElementById('ring-canvas'),
    glyph: document.getElementById('glyph-canvas'),
    label: document.getElementById('orb-label'),
    meta: document.getElementById('burst-meta'),
    btnHold: document.getElementById('btn-hold'),
    btnCam: document.getElementById('btn-cam'),
    btnConnect: document.getElementById('btn-connect'),
    hubUrl: document.getElementById('hub-url'),
  };
  if (!el.orb) return;

  const faceCtx = el.face.getContext('2d', { willReadFrequently: true });
  const ringCtx = el.ring.getContext('2d');
  const glyphCtx = el.glyph.getContext('2d');
  faceCtx.imageSmoothingEnabled = false;
  glyphCtx.imageSmoothingEnabled = false;

  let video = null;
  let stream = null;
  let ws = null;
  let nick = 'web-' + Math.random().toString(36).slice(2, 6);
  let tx = false;
  let rxFrom = '';
  let level = 0;
  let bands = new Array(32).fill(0);
  let raf = 0;
  let audioCtx = null;
  let analyser = null;
  let micSource = null;
  let mediaRecorder = null;
  let lastGlyph = new Array(GLYPH_N * GLYPH_N).fill(0);
  let remoteImg = null;
  let frameTimer = 0;

  function setMeta(t) {
    if (el.meta) el.meta.innerHTML = t;
  }
  function setLabel(t) {
    if (el.label) el.label.textContent = t;
  }

  function paintSimFace(t) {
    const n = GLYPH_N;
    const img = faceCtx.createImageData(n, n);
    const d = img.data;
    const cx = n * 0.5 + Math.sin(t * 0.001) * 1.5;
    const cy = n * 0.45;
    for (let y = 0; y < n; y++) {
      for (let x = 0; x < n; x++) {
        const i = (y * n + x) * 4;
        const dist = Math.hypot(x - cx, y - cy);
        let L = 20 + (y / n) * 40;
        if (dist < n * 0.28) L = 140 + (1 - dist / (n * 0.28)) * 80;
        if (Math.hypot(x - (cx - 3), y - (cy - 2)) < 1.4) L = 20;
        if (Math.hypot(x - (cx + 3), y - (cy - 2)) < 1.4) L = 20;
        d[i] = d[i + 1] = d[i + 2] = L;
        d[i + 3] = 255;
        lastGlyph[y * n + x] = L;
      }
    }
    faceCtx.putImageData(img, 0, 0);
    paintGlyph();
  }

  function sampleVideoToGlyph() {
    if (!video || video.readyState < 2) {
      paintSimFace(performance.now());
      return;
    }
    const n = GLYPH_N;
    // draw video into face canvas (pixelated)
    faceCtx.drawImage(video, 0, 0, n, n);
    const img = faceCtx.getImageData(0, 0, n, n);
    const d = img.data;
    for (let i = 0, g = 0; i < d.length; i += 4, g++) {
      const L = 0.299 * d[i] + 0.587 * d[i + 1] + 0.114 * d[i + 2];
      lastGlyph[g] = L;
      // slight contrast for LED readability
      const v = Math.min(255, Math.pow(L / 255, 0.85) * 255);
      d[i] = d[i + 1] = d[i + 2] = v;
    }
    faceCtx.putImageData(img, 0, 0);
    paintGlyph();
  }

  function paintGlyph() {
    const cell = el.glyph.width / GLYPH_N;
    glyphCtx.fillStyle = '#050508';
    glyphCtx.fillRect(0, 0, el.glyph.width, el.glyph.height);
    for (let y = 0; y < GLYPH_N; y++) {
      for (let x = 0; x < GLYPH_N; x++) {
        const L = lastGlyph[y * GLYPH_N + x] | 0;
        glyphCtx.fillStyle = `rgb(${L},${L},${Math.min(255, L + 20)})`;
        glyphCtx.fillRect(x * cell + 0.5, y * cell + 0.5, cell - 1, cell - 1);
      }
    }
  }

  function paintRing(now) {
    const w = el.ring.width;
    const h = el.ring.height;
    const cx = w / 2;
    const cy = h / 2;
    const r0 = w * 0.42;
    ringCtx.clearRect(0, 0, w, h);
    const n = bands.length;
    for (let i = 0; i < n; i++) {
      const a0 = (i / n) * Math.PI * 2 - Math.PI / 2;
      const a1 = ((i + 1) / n) * Math.PI * 2 - Math.PI / 2;
      const lv = bands[i] || level * 0.5;
      const r1 = r0 + 4 + lv * 22;
      ringCtx.beginPath();
      ringCtx.arc(cx, cy, r0, a0, a1);
      ringCtx.arc(cx, cy, r1, a1, a0, true);
      ringCtx.closePath();
      if (tx) {
        ringCtx.fillStyle = `rgba(248,113,113,${0.35 + lv * 0.55})`;
      } else if (rxFrom) {
        ringCtx.fillStyle = `rgba(74,222,128,${0.3 + lv * 0.5})`;
      } else {
        ringCtx.fillStyle = `rgba(125,211,252,${0.15 + lv * 0.45})`;
      }
      ringCtx.fill();
    }
    // outer soft ring
    ringCtx.beginPath();
    ringCtx.arc(cx, cy, r0 - 2, 0, Math.PI * 2);
    ringCtx.strokeStyle = tx
      ? 'rgba(248,113,113,.35)'
      : rxFrom
        ? 'rgba(74,222,128,.3)'
        : 'rgba(125,211,252,.15)';
    ringCtx.lineWidth = 2;
    ringCtx.stroke();
  }

  function decayBands() {
    level *= 0.9;
    for (let i = 0; i < bands.length; i++) bands[i] *= 0.88;
  }

  function readMic() {
    if (!analyser) return;
    const data = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(data);
    let sum = 0;
    for (let i = 0; i < bands.length; i++) {
      const idx = Math.floor((i / bands.length) * data.length);
      const v = (data[idx] || 0) / 255;
      bands[i] = Math.max(bands[i] * 0.5, v);
      sum += v;
    }
    level = sum / bands.length;
  }

  function loop(now) {
    raf = requestAnimationFrame(loop);
    decayBands();
    if (tx) readMic();
    if (rxFrom && remoteImg) {
      faceCtx.drawImage(remoteImg, 0, 0, GLYPH_N, GLYPH_N);
      const img = faceCtx.getImageData(0, 0, GLYPH_N, GLYPH_N);
      for (let i = 0, g = 0; i < img.data.length; i += 4, g++) {
        lastGlyph[g] = 0.299 * img.data[i] + 0.587 * img.data[i + 1] + 0.114 * img.data[i + 2];
      }
      paintGlyph();
    } else if (tx || stream) {
      sampleVideoToGlyph();
    } else {
      paintSimFace(now);
    }
    paintRing(now);

    // ship frames while TX
    if (tx && ws && ws.readyState === 1 && now - frameTimer > 160) {
      frameTimer = now;
      sendBurstFrame();
    }
  }

  async function enableCam() {
    const host = (location.hostname || '');
    const isLocal = host === 'localhost' || host === '127.0.0.1' || host === '[::1]';
    if (!window.isSecureContext && !isLocal) {
      setMeta('cam needs localhost — open http://127.0.0.1:9876/burst.html (not LAN IP)');
      return;
    }
    const tries = [
      { video: { facingMode: 'user', width: { ideal: 320 }, height: { ideal: 320 } }, audio: true },
      { video: true, audio: true },
      { video: true, audio: false },
    ];
    let lastErr = null;
    for (let i = 0; i < tries.length; i++) {
      try {
        stream = await navigator.mediaDevices.getUserMedia(tries[i]);
        lastErr = null;
        break;
      } catch (e) {
        lastErr = e;
        if (e && (e.name === 'NotAllowedError' || e.name === 'SecurityError')) break;
      }
    }
    if (lastErr || !stream) {
      const n = lastErr && lastErr.name;
      if (n === 'NotAllowedError') setMeta('cam permission denied — allow in address bar 🔒');
      else if (n === 'NotFoundError') setMeta('no camera found — sim face');
      else setMeta('cam blocked — sim face · ' + (n || ''));
      return;
    }
    try {
      video = document.createElement('video');
      video.playsInline = true;
      video.muted = true;
      video.srcObject = stream;
      await video.play();
      if (stream.getAudioTracks && stream.getAudioTracks().length) {
        audioCtx = new (window.AudioContext || window.webkitAudioContext)();
        if (audioCtx.state === 'suspended') await audioCtx.resume();
        analyser = audioCtx.createAnalyser();
        analyser.fftSize = 64;
        micSource = audioCtx.createMediaStreamSource(stream);
        micSource.connect(analyser);
        if (window.__gySpaces && window.__gySpaces.setAnalyser) {
          window.__gySpaces.setAnalyser(analyser);
        }
      }
      setMeta('cam ready · hold orb to burst');
      el.btnCam && (el.btnCam.textContent = 'Cam on');
    } catch (e) {
      setMeta('cam play failed — sim face');
    }
  }

  function jpegFromFace() {
    // upscale glyph canvas for a tiny jpeg
    const c = document.createElement('canvas');
    c.width = 120;
    c.height = 120;
    const ctx = c.getContext('2d');
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(el.face, 0, 0, 120, 120);
    return c.toDataURL('image/jpeg', 0.55);
  }

  function sendJSON(obj) {
    if (!ws || ws.readyState !== 1) return;
    ws.send(JSON.stringify(obj));
  }

  // expose for burst-spaces.js (waveforms · roster · RTMP panel)
  window.__gyBurst = {
    get nick() { return nick; },
    get tx() { return tx; },
    sendJSON: sendJSON,
    getAnalyser: function () { return analyser; },
    getWS: function () { return ws; },
  };

  function sendBurstFrame() {
    const dataUrl = jpegFromFace();
    const b64 = dataUrl.split(',')[1] || '';
    sendJSON({
      type: 'vburst-frame',
      from: nick,
      fmt: 'jpeg',
      b64: b64,
      w: 120,
      h: 120,
      glyph: lastGlyph.map((v) => Math.round(v)),
      glyphN: GLYPH_N,
      t: Date.now(),
    });
  }

  function startBurst() {
    if (tx) return;
    tx = true;
    el.orb.classList.add('tx');
    el.orb.classList.remove('rx');
    setLabel('TX');
    setMeta('<em>bursting</em> · video + mic');
    sendJSON({ type: 'vburst-start', from: nick, t: Date.now() });
    sendJSON({ type: 'ptt', state: 'down', from: nick });
    // stream mic via MediaRecorder chunks if connected (optional PCM path skipped —
    // browsers prefer webm; hub peers on Go still get video frames + ptt signal.
    // For full PCM parity use the terminal client.)
  }

  function stopBurst() {
    if (!tx) return;
    tx = false;
    el.orb.classList.remove('tx');
    setLabel('hold');
    setMeta('idle · release');
    sendJSON({ type: 'vburst-end', from: nick, t: Date.now() });
    sendJSON({ type: 'ptt', state: 'up', from: nick });
  }

  function connect() {
    let url = (el.hubUrl && el.hubUrl.value.trim()) || '';
    if (!url) {
      const host = location.hostname || '127.0.0.1';
      // common: hub on 9876 while pages on 8765
      url = `ws://${host === 'localhost' || host === '127.0.0.1' ? '127.0.0.1' : host}:9876/?role=peer&nick=${encodeURIComponent(nick)}`;
    } else if (!url.includes('nick=')) {
      url += (url.includes('?') ? '&' : '?') + 'role=peer&nick=' + encodeURIComponent(nick);
    }
    if (ws) try { ws.close(); } catch (_) {}
    setMeta('connecting…');
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setMeta('ws error');
      return;
    }
    ws.onopen = () => {
      setMeta('connected · <em>' + nick + '</em>');
      sendJSON({ type: 'join', nick: nick, role: 'web-burst' });
    };
    ws.onclose = () => setMeta('disconnected');
    ws.onerror = () => setMeta('ws error — is hub up?');
    ws.onmessage = (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      const typ = msg.type;
      const from = msg.from || '';
      if (from === nick) return;
      if (typ === 'vburst-start' || (typ === 'ptt' && msg.state === 'down')) {
        rxFrom = from;
        el.orb.classList.add('rx');
        setLabel(from);
        setMeta('<em>' + from + '</em> bursting');
      }
      if (typ === 'vburst-end' || (typ === 'ptt' && msg.state === 'up')) {
        if (from === rxFrom) {
          rxFrom = '';
          el.orb.classList.remove('rx');
          setLabel('hold');
          setMeta('idle');
        }
      }
      if (typ === 'vburst-frame' && msg.b64) {
        rxFrom = from;
        el.orb.classList.add('rx');
        if (Array.isArray(msg.glyph) && msg.glyph.length) {
          lastGlyph = msg.glyph.map(Number);
          // paint face from glyph
          const img = faceCtx.createImageData(GLYPH_N, GLYPH_N);
          for (let i = 0; i < lastGlyph.length; i++) {
            const v = lastGlyph[i];
            img.data[i * 4] = img.data[i * 4 + 1] = img.data[i * 4 + 2] = v;
            img.data[i * 4 + 3] = 255;
          }
          faceCtx.putImageData(img, 0, 0);
          paintGlyph();
        } else {
          const im = new Image();
          im.onload = () => {
            remoteImg = im;
            faceCtx.drawImage(im, 0, 0, GLYPH_N, GLYPH_N);
          };
          im.src = 'data:image/jpeg;base64,' + msg.b64;
        }
        // pulse ring
        for (let i = 0; i < bands.length; i++) {
          bands[i] = 0.3 + Math.random() * 0.4;
        }
        level = 0.5;
      }
      if (typ === 'audio' && msg.b64) {
        // optional: decode pcm not implemented in browser path
        level = 0.6;
      }
      // X Spaces stage (roster / levels / chat / captions)
      if (window.__gySpaces && typeof window.__gySpaces.onMesh === 'function') {
        window.__gySpaces.onMesh(msg);
      }
    };
  }

  // pointer hold
  function bindHold(target) {
    const down = (e) => {
      e.preventDefault();
      startBurst();
    };
    const up = (e) => {
      e.preventDefault();
      stopBurst();
    };
    target.addEventListener('pointerdown', down);
    target.addEventListener('pointerup', up);
    target.addEventListener('pointerleave', up);
    target.addEventListener('pointercancel', up);
  }
  bindHold(el.orb);
  if (el.btnHold) bindHold(el.btnHold);

  el.btnCam && el.btnCam.addEventListener('click', () => enableCam());
  el.btnConnect && el.btnConnect.addEventListener('click', () => connect());

  // keyboard: space = hold
  window.addEventListener('keydown', (e) => {
    if (e.code === 'Space' && !e.repeat && e.target === document.body) {
      e.preventDefault();
      startBurst();
    }
  });
  window.addEventListener('keyup', (e) => {
    if (e.code === 'Space') {
      e.preventDefault();
      stopBurst();
    }
  });

  // drag-drop video/image onto orb
  function bindOrbDrop(node) {
    if (!node) return;
    ['dragenter', 'dragover'].forEach((ev) => {
      node.addEventListener(ev, (e) => {
        e.preventDefault();
        node.classList.add('drop-hover');
      });
    });
    node.addEventListener('dragleave', () => node.classList.remove('drop-hover'));
    node.addEventListener('drop', (e) => {
      e.preventDefault();
      node.classList.remove('drop-hover');
      const f = e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files[0];
      if (!f) return;
      const url = URL.createObjectURL(f);
      if ((f.type || '').startsWith('video/')) {
        if (!video) video = document.createElement('video');
        video.playsInline = true;
        video.muted = true;
        video.loop = true;
        video.src = url;
        video.play().catch(() => {});
        setMeta('dropped video · hold to burst');
      } else if ((f.type || '').startsWith('image/')) {
        const im = new Image();
        im.onload = () => {
          remoteImg = im;
          faceCtx.drawImage(im, 0, 0, GLYPH_N, GLYPH_N);
          paintGlyph();
        };
        im.src = url;
        setMeta('dropped image · hold to burst');
      }
    });
  }
  bindOrbDrop(el.orb);
  bindOrbDrop(document.querySelector('.burst-stage'));

  setMeta('enable cam · drop file · hold orb');
  paintSimFace(0);
  raf = requestAnimationFrame(loop);
})();
