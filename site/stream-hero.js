/**
 * Streaming camera/video hero — conversation-center feed.
 * Live getUserMedia or local file → half-block canvas (terminal pipeline preview).
 */
(function () {
  'use strict';

  const video = document.getElementById('hero-video');
  const canvas = document.getElementById('hero-canvas');
  const placeholder = document.getElementById('stream-placeholder');
  const statusEl = document.getElementById('stream-status');
  const metaEl = document.getElementById('stream-meta');
  const pill = document.getElementById('live-pill');
  const chat = document.getElementById('stream-chat');
  const btnCam = document.getElementById('btn-cam');
  const fileInput = document.getElementById('file-video');

  if (!video || !canvas) return;

  const ctx = canvas.getContext('2d', { willReadFrequently: true });
  let stream = null;
  let raf = 0;
  let running = false;
  let cols = 72;
  let rows = 28; // half-block rows (each = 2 source px rows)
  let frames = 0;
  let fpsT0 = performance.now();
  let fps = 0;
  let objectUrl = null;

  function setStatus(text, live) {
    if (statusEl) statusEl.textContent = text;
    if (pill) {
      pill.textContent = live ? '● live' : '○ idle';
      pill.classList.toggle('on', !!live);
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

  function stopStream() {
    running = false;
    if (raf) cancelAnimationFrame(raf);
    raf = 0;
    if (stream) {
      stream.getTracks().forEach((t) => t.stop());
      stream = null;
    }
    video.srcObject = null;
    video.removeAttribute('src');
    video.load();
    if (objectUrl) {
      URL.revokeObjectURL(objectUrl);
      objectUrl = null;
    }
    placeholder && (placeholder.hidden = false);
    canvas.classList.remove('active');
    setStatus('Idle · allow camera or pick mp4/mkv/mov', false);
    if (metaEl) metaEl.textContent = 'half-block · 0 fps';
  }

  function sizeCanvas() {
    const wrap = canvas.parentElement;
    const w = Math.max(280, wrap ? wrap.clientWidth : 480);
    // cell size ~ target readability
    cols = Math.min(96, Math.max(40, Math.floor(w / 7)));
    rows = Math.min(36, Math.max(16, Math.floor(cols * 0.42)));
    canvas.width = cols;
    canvas.height = rows * 2;
    // display scale
    canvas.style.width = '100%';
    canvas.style.height = 'auto';
    canvas.style.imageRendering = 'pixelated';
  }

  function drawHalfBlocks() {
    if (!running || video.readyState < 2) {
      raf = requestAnimationFrame(drawHalfBlocks);
      return;
    }
    const cw = canvas.width;
    const ch = canvas.height;
    ctx.drawImage(video, 0, 0, cw, ch);
    const img = ctx.getImageData(0, 0, cw, ch);
    const d = img.data;

    // paint half-block style by averaging pairs — already at cell resolution
    // enhance contrast slightly for "hex" readability
    for (let i = 0; i < d.length; i += 4) {
      d[i] = Math.min(255, d[i] * 1.08);
      d[i + 1] = Math.min(255, d[i + 1] * 1.05);
      d[i + 2] = Math.min(255, d[i + 2] * 1.02);
    }
    ctx.putImageData(img, 0, 0);

    frames++;
    const now = performance.now();
    if (now - fpsT0 > 1000) {
      fps = Math.round((frames * 1000) / (now - fpsT0));
      frames = 0;
      fpsT0 = now;
      if (metaEl) metaEl.textContent = `half-block · ${cw}×${ch} · ${fps} fps`;
    }
    raf = requestAnimationFrame(drawHalfBlocks);
  }

  async function startCamera() {
    stopStream();
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'user', width: { ideal: 1280 }, height: { ideal: 720 } },
        audio: false,
      });
      video.srcObject = stream;
      await video.play();
      sizeCanvas();
      placeholder && (placeholder.hidden = true);
      canvas.classList.add('active');
      running = true;
      setStatus('Camera streaming · hero feed live', true);
      pushBubble('camera stream attached to conversation hero', 'sys');
      btnCam.textContent = 'Stop stream';
      fpsT0 = performance.now();
      frames = 0;
      drawHalfBlocks();
    } catch (err) {
      setStatus('Camera blocked: ' + (err.message || err), false);
      pushBubble('camera error — try Open video file', 'sys');
      btnCam.textContent = 'Start camera stream';
    }
  }

  function startFile(file) {
    stopStream();
    objectUrl = URL.createObjectURL(file);
    video.src = objectUrl;
    video.muted = true;
    video.loop = true;
    video.play().then(() => {
      sizeCanvas();
      placeholder && (placeholder.hidden = true);
      canvas.classList.add('active');
      running = true;
      setStatus('Video file · ' + file.name, true);
      pushBubble('watching ' + file.name, 'me');
      btnCam.textContent = 'Start camera stream';
      fpsT0 = performance.now();
      frames = 0;
      drawHalfBlocks();
    }).catch((err) => {
      setStatus('Video play failed: ' + err.message, false);
    });
  }

  btnCam && btnCam.addEventListener('click', () => {
    if (running && stream) {
      stopStream();
      btnCam.textContent = 'Start camera stream';
      pushBubble('stream stopped', 'sys');
    } else {
      startCamera();
    }
  });

  fileInput && fileInput.addEventListener('change', () => {
    const f = fileInput.files && fileInput.files[0];
    if (f) startFile(f);
  });

  window.addEventListener('resize', () => {
    if (running) sizeCanvas();
  });

  // gentle demo animation when idle (optional gradient noise) — skip auto cam for privacy
  setStatus('Idle · start camera or open video — feed is the conversation hero', false);
})();
