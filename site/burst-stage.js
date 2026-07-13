/**
 * Burst stage visuals — host/co-host video circles, speaker 25×25 glyphs,
 * GrokGlyph-style listener join grid (pipeline stress).
 */
(function () {
  'use strict';
  const G = window.GY_GLYPH;
  if (!G) return;

  const COHOSTS = 2;
  const SPEAKERS = 10;
  const LISTENER_SLOTS = 48; // stress capacity on page

  const el = {
    hostCircles: document.getElementById('host-circles'),
    speakerGrid: document.getElementById('speaker-grid'),
    listenerGrid: document.getElementById('listener-grid'),
    joinListener: document.getElementById('btn-join-listener'),
    stress: document.getElementById('btn-stress-listeners'),
    clearStress: document.getElementById('btn-clear-stress'),
    listenersCount: document.getElementById('listeners-count'),
  };

  /** @type {Map<string, {canvas: HTMLCanvasElement, lum: Float32Array, seed: number}>} */
  const faceCache = new Map();
  let stressIds = [];
  let raf = 0;
  let lastState = null;
  let lastCtx = null;

  function burst() {
    return window.__gyBurst || {};
  }
  function spaces() {
    return window.__gySpaces || {};
  }

  function keyFor(role, index) {
    return role + ':' + index;
  }

  function ensureFace(key, size) {
    let rec = faceCache.get(key);
    if (!rec) {
      const canvas = document.createElement('canvas');
      canvas.width = size || 140;
      canvas.height = size || 140;
      rec = {
        canvas: canvas,
        lum: G.simLum(0, hash(key), false),
        seed: hash(key),
      };
      faceCache.set(key, rec);
    }
    return rec;
  }

  function hash(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
    return Math.abs(h % 90) + 1;
  }

  function render(state, ctx) {
    lastState = state;
    lastCtx = ctx || {};
    renderHosts(state, lastCtx);
    renderSpeakers(state, lastCtx);
    renderListeners(state, lastCtx);
  }

  function renderHosts(state, ctx) {
    if (!el.hostCircles) return;
    el.hostCircles.innerHTML = '';
    const seats = [
      { role: 'host', index: 0, slot: state.host, tag: 'HOST' },
      { role: 'cohost', index: 0, slot: state.cohosts[0], tag: 'CO-HOST 1' },
      { role: 'cohost', index: 1, slot: state.cohosts[1], tag: 'CO-HOST 2' },
    ];
    seats.forEach((s) => {
      const card = document.createElement('div');
      card.className =
        'host-circle-card' +
        (s.slot.nick ? ' seated' : ' empty') +
        (s.slot.talking && !s.slot.muted ? ' talking' : '') +
        (s.slot.muted ? ' muted' : '');
      const orb = document.createElement('div');
      orb.className = 'hc-orb';
      const face = ensureFace(keyFor(s.role, s.index), 140);
      // local cam → host seat when you are host
      const isMe =
        ctx.myRole === s.role &&
        (s.role === 'host' || ctx.mySlot === s.index);
      if (isMe && burst().getAnalyser) {
        // pull face from main burst face canvas if present
        const faceEl = document.getElementById('face-canvas');
        if (faceEl) {
          const tmp = document.createElement('canvas');
          tmp.width = 25;
          tmp.height = 25;
          const tctx = tmp.getContext('2d');
          tctx.drawImage(faceEl, 0, 0, 25, 25);
          const img = tctx.getImageData(0, 0, 25, 25);
          for (let i = 0, g = 0; i < img.data.length; i += 4, g++) {
            face.lum[g] =
              (0.299 * img.data[i] + 0.587 * img.data[i + 1] + 0.114 * img.data[i + 2]) / 255;
          }
        }
      } else if (!s.slot.nick) {
        face.lum = G.simLum(performance.now(), face.seed, false);
      } else {
        face.lum = G.simLum(performance.now() + s.index * 100, face.seed, s.slot.talking);
      }
      const mode = s.slot.muted ? 'idle' : s.slot.talking ? 'tx' : s.slot.nick ? 'rx' : 'idle';
      G.paintCircleFace(face.canvas, face.lum, s.slot.level || 0, mode);
      orb.appendChild(face.canvas);
      const label = document.createElement('div');
      label.className = 'hc-label';
      label.innerHTML =
        '<strong>' +
        escapeHtml(s.slot.nick || s.slot.ph || s.tag) +
        (s.slot.muted ? ' 🔇' : '') +
        '</strong>' +
        s.tag;
      const wave = document.createElement('canvas');
      wave.className = 'hc-wave';
      wave.width = 160;
      wave.height = 22;
      paintMiniWave(wave, s.slot.level || 0, s.slot.talking && !s.slot.muted);
      const row = document.createElement('div');
      row.className = 'hc-row';
      const mute = document.createElement('button');
      mute.type = 'button';
      mute.className = 'btn ghost';
      mute.style.fontSize = '0.6rem';
      mute.textContent = s.slot.muted ? 'unmute' : 'mute';
      mute.addEventListener('click', () => {
        if (spaces().state) {
          // use mesh mute via UI rebuild path
          const st = spaces().state();
          // dispatch through spaces if available
        }
        // click existing mute via synthetic: toggle on slot + mesh
        toggleMuteViaSpaces(s.role, s.index, !s.slot.muted);
      });
      row.appendChild(mute);
      card.appendChild(orb);
      card.appendChild(label);
      card.appendChild(wave);
      card.appendChild(row);
      el.hostCircles.appendChild(card);
    });
  }

  function toggleMuteViaSpaces(role, index, muted) {
    // Prefer internal API if exposed; else send mesh + mutate state
    const sp = spaces();
    const st = sp.state && sp.state();
    if (!st) return;
    const slot =
      role === 'host' ? st.host : role === 'cohost' ? st.cohosts[index] : st.speakers[index];
    if (slot) {
      slot.muted = muted;
      if (muted) {
        slot.level = 0;
        slot.talking = false;
      }
    }
    const b = burst();
    if (b.sendJSON) {
      b.sendJSON({
        type: 'space-mute',
        from: b.nick || 'web',
        role: role,
        slot: index,
        muted: muted,
        by: b.nick || 'web',
        space: st.id,
        t: Date.now(),
      });
    }
    if (sp.state) render(st, lastCtx || {});
  }

  function renderSpeakers(state, ctx) {
    if (!el.speakerGrid) return;
    el.speakerGrid.innerHTML = '';
    state.speakers.forEach((slot, i) => {
      const tile = document.createElement('div');
      tile.className =
        'spk-tile' +
        (slot.nick ? ' seated' : '') +
        (slot.talking && !slot.muted ? ' talking' : '') +
        (slot.muted ? ' muted' : '');
      tile.setAttribute('role', 'listitem');
      const face = ensureFace(keyFor('speaker', i), 125);
      const talking = !!(slot.talking && !slot.muted);
      const isMe = ctx.myRole === 'speaker' && ctx.mySlot === i;
      if (isMe) {
        const faceEl = document.getElementById('face-canvas');
        if (faceEl) {
          const tmp = document.createElement('canvas');
          tmp.width = 25;
          tmp.height = 25;
          const tctx = tmp.getContext('2d');
          tctx.drawImage(faceEl, 0, 0, 25, 25);
          const img = tctx.getImageData(0, 0, 25, 25);
          for (let p = 0, g = 0; p < img.data.length; p += 4, g++) {
            face.lum[g] =
              (0.299 * img.data[p] + 0.587 * img.data[p + 1] + 0.114 * img.data[p + 2]) / 255;
          }
        }
      } else {
        face.lum = G.simLum(performance.now() + i * 55, face.seed, talking || !!slot.nick);
      }
      G.paintGlyphCanvas(face.canvas, face.lum, { cell: 5, tint: slot.nick ? 0 : 210 });
      const meta = document.createElement('div');
      meta.className = 'spk-meta';
      meta.innerHTML =
        '<span>' +
        escapeHtml(slot.nick || slot.ph || 'Speaker ' + (i + 1)) +
        '</span><span>S' +
        (i + 1) +
        (slot.muted ? ' 🔇' : '') +
        '</span>';
      const mute = document.createElement('button');
      mute.type = 'button';
      mute.className = 'spk-mute';
      mute.textContent = slot.muted ? 'unmute' : 'mute';
      mute.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleMuteViaSpaces('speaker', i, !slot.muted);
      });
      tile.appendChild(face.canvas);
      tile.appendChild(meta);
      tile.appendChild(mute);
      tile.addEventListener('click', () => {
        // seat as this speaker if empty
        const roleSel = document.getElementById('my-role');
        const slotSel = document.getElementById('my-slot');
        if (roleSel) roleSel.value = 'speaker';
        if (slotSel) {
          slotSel.dispatchEvent(new Event('change'));
          // rebuild options
          if (window.__gySpaces) {
            /* seat via button */
          }
          // set after options
          setTimeout(() => {
            if (slotSel) slotSel.value = String(i);
            const btn = document.getElementById('btn-seat-me');
            if (btn) btn.click();
          }, 0);
        }
      });
      el.speakerGrid.appendChild(tile);
    });
  }

  function renderListeners(state, ctx) {
    if (!el.listenerGrid) return;
    const names = (state.listenerList || []).slice();
    // inject stress ids
    stressIds.forEach((id) => {
      if (!names.includes(id)) names.push(id);
    });
    if (el.listenersCount) el.listenersCount.textContent = String(names.length);
    el.listenerGrid.innerHTML = '';
    for (let i = 0; i < LISTENER_SLOTS; i++) {
      const tile = document.createElement('div');
      const nick = names[i];
      if (!nick) {
        tile.className = 'lis-tile empty';
        tile.textContent = '+ join';
        tile.setAttribute('role', 'listitem');
        tile.addEventListener('click', () => joinListener());
        el.listenerGrid.appendChild(tile);
        continue;
      }
      tile.className = 'lis-tile filled' + (nick === ctx.nick ? ' is-you' : '');
      tile.setAttribute('role', 'listitem');
      const face = ensureFace('lis:' + nick, 88);
      face.lum = G.simLum(performance.now() + i * 30, face.seed, false);
      G.paintGlyphCanvas(face.canvas, face.lum, { cell: 3, tint: 180 });
      const name = document.createElement('div');
      name.className = 'lis-name';
      name.textContent = nick;
      tile.appendChild(face.canvas);
      tile.appendChild(name);
      el.listenerGrid.appendChild(tile);
    }
  }

  function joinListener() {
    const roleSel = document.getElementById('my-role');
    if (roleSel) roleSel.value = 'listener';
    const btn = document.getElementById('btn-seat-me');
    if (btn) btn.click();
    // also connect hub if needed
    const conn = document.getElementById('btn-connect');
    if (conn && burst().getWS && (!burst().getWS() || burst().getWS().readyState !== 1)) {
      conn.click();
    }
  }

  function stressListeners() {
    const b = burst();
    const n = 12;
    for (let i = 0; i < n; i++) {
      const id = 'stress-' + Math.random().toString(36).slice(2, 6);
      stressIds.push(id);
      if (b.sendJSON) {
        b.sendJSON({
          type: 'space-listener-join',
          from: id,
          nick: id,
          t: Date.now(),
        });
        // pipeline stress: tiny vburst frames
        b.sendJSON({
          type: 'vburst-frame',
          from: id,
          glyphN: 25,
          glyph: Array.from({ length: 625 }, (_, j) => ((j * 7 + i * 3) % 120)),
          t: Date.now(),
        });
      }
    }
    if (lastState) {
      stressIds.forEach((id) => {
        if (!lastState.listenerList.includes(id)) lastState.listenerList.push(id);
      });
      lastState.listeners = lastState.listenerList.length;
      render(lastState, lastCtx || {});
    }
  }

  function clearStress() {
    const b = burst();
    stressIds.forEach((id) => {
      if (b.sendJSON) {
        b.sendJSON({ type: 'space-listener-leave', from: id, nick: id, t: Date.now() });
      }
      if (lastState) {
        lastState.listenerList = lastState.listenerList.filter((n) => n !== id);
      }
    });
    stressIds = [];
    if (lastState) {
      lastState.listeners = lastState.listenerList.length;
      render(lastState, lastCtx || {});
    }
  }

  function paintMiniWave(canvas, level, talking) {
    const ctx = canvas.getContext('2d');
    const w = canvas.width;
    const h = canvas.height;
    ctx.fillStyle = '#050508';
    ctx.fillRect(0, 0, w, h);
    const n = 20;
    const bw = w / n;
    for (let i = 0; i < n; i++) {
      const v = Math.max(0.05, (level || 0.05) * (0.4 + 0.6 * Math.abs(Math.sin(i + level * 10))));
      const bh = v * (h - 2);
      ctx.fillStyle = talking
        ? 'rgba(74,222,128,' + (0.4 + v * 0.5) + ')'
        : 'rgba(125,211,252,' + (0.15 + v * 0.4) + ')';
      ctx.fillRect(i * bw, h - bh, bw - 1, bh);
    }
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  function loop() {
    raf = requestAnimationFrame(loop);
    // animate faces ~8fps without thrashing layout every frame
    const tick = Math.floor(performance.now() / 125);
    if (lastState && tick !== loop._tick) {
      loop._tick = tick;
      render(lastState, lastCtx || {});
    }
  }

  el.joinListener && el.joinListener.addEventListener('click', joinListener);
  el.stress && el.stress.addEventListener('click', stressListeners);
  el.clearStress && el.clearStress.addEventListener('click', clearStress);

  // persist collapsible sections
  G.persistDetails(document, 'gy_burst_details');

  window.__gyStage = {
    render: render,
    stressListeners: stressListeners,
    joinListener: joinListener,
  };

  // initial empty render once spaces ready
  setTimeout(() => {
    if (spaces().state) {
      render(spaces().state(), {
        myRole: 'host',
        mySlot: 0,
        nick: (burst().nick) || '',
      });
    }
  }, 50);
  raf = requestAnimationFrame(loop);
})();
