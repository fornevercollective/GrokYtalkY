/**
 * X Spaces stage for burst page — host + 2 co-hosts + 10 speakers + listeners,
 * live audio waveforms, Space chat / captions, RTMP(S) Media Studio producer.
 * Default Space: https://x.com/i/spaces/1AJEmmANrPeJL
 */
(function () {
  'use strict';

  const COHOSTS = 2;
  const SPEAKERS = 10;
  const DEFAULT_SPACE = '1AJEmmANrPeJL';
  const RTMPS = 'rtmps://ca.pscp.tv:443/x';
  const RTMP = 'rtmp://ca.pscp.tv:80/x';

  const el = {
    root: document.getElementById('spaces-stage'),
    spaceUrl: document.getElementById('space-url'),
    spaceTitle: document.getElementById('space-title'),
    spaceMeta: document.getElementById('space-meta'),
    hostSlot: document.getElementById('slot-host'),
    cohosts: document.getElementById('slots-cohosts'),
    speakers: document.getElementById('slots-speakers'),
    listeners: document.getElementById('listeners-count'),
    listenerList: document.getElementById('listener-list'),
    chatLog: document.getElementById('space-chat-log'),
    chatInput: document.getElementById('space-chat-input'),
    chatSend: document.getElementById('space-chat-send'),
    caption: document.getElementById('space-caption'),
    captionInput: document.getElementById('space-caption-input'),
    captionSet: document.getElementById('space-caption-set'),
    rtmpProto: document.getElementById('rtmp-proto'),
    rtmpBase: document.getElementById('rtmp-base'),
    rtmpKey: document.getElementById('rtmp-key'),
    rtmpStatus: document.getElementById('rtmp-status'),
    rtmpCmd: document.getElementById('rtmp-cmd'),
    spaceToken: document.getElementById('space-token'),
    btnApplySpace: document.getElementById('btn-space-apply'),
    btnOpenSpace: document.getElementById('btn-space-open'),
    btnSeatMe: document.getElementById('btn-seat-me'),
    btnMuteAll: document.getElementById('btn-mute-all'),
    btnUnmuteAll: document.getElementById('btn-unmute-all'),
    btnSelfMute: document.getElementById('btn-self-mute'),
    btnAssetOffer: document.getElementById('btn-asset-offer'),
    btnKeyPull: document.getElementById('btn-key-pull'),
    roleSelect: document.getElementById('my-role'),
    slotSelect: document.getElementById('my-slot'),
  };
  if (!el.root) return;

  /** @type {{host: Slot, cohosts: Slot[], speakers: Slot[], listeners: number, listenerList: string[], caption: string, id: string, muteAll: boolean, assetOffer: boolean}} */
  let state = emptyState(DEFAULT_SPACE);
  let myRole = 'host';
  let mySlot = 0;
  let selfMuted = false;
  let analyser = null;
  let micBands = new Array(24).fill(0);
  let level = 0;
  let raf = 0;
  /** shared with burst-orb via window.__gyBurst */
  const burst = () => window.__gyBurst || {};

  function emptyState(id) {
    return {
      id: id || DEFAULT_SPACE,
      title: 'GrokYtalkY Space',
      host: { nick: '', level: 0, talking: false, muted: false, ph: 'Host — you' },
      cohosts: Array.from({ length: COHOSTS }, (_, i) => ({
        nick: '', level: 0, talking: false, muted: false, ph: 'Co-host ' + (i + 1),
      })),
      speakers: Array.from({ length: SPEAKERS }, (_, i) => ({
        nick: '', level: 0, talking: false, muted: false, ph: 'Speaker ' + (i + 1),
      })),
      listeners: 0,
      listenerList: [],
      muteAll: false,
      assetOffer: false,
      caption: '',
    };
  }

  function parseSpaceId(raw) {
    raw = (raw || '').trim();
    const m = raw.match(/(?:x|twitter)\.com\/i\/spaces\/([A-Za-z0-9]+)/i);
    if (m) return m[1];
    const bare = raw.replace(/[?#].*$/, '').replace(/\/$/, '');
    if (/^[A-Za-z0-9]{6,}$/.test(bare)) return bare;
    return DEFAULT_SPACE;
  }

  function spaceLink(id) {
    return 'https://x.com/i/spaces/' + id;
  }

  function loadStored() {
    try {
      const k = localStorage.getItem('gy_x_stream_key') || '';
      if (el.rtmpKey) el.rtmpKey.value = k;
      const p = localStorage.getItem('gy_x_rtmp_proto') || 'rtmps';
      if (el.rtmpProto) el.rtmpProto.value = p;
      const u = localStorage.getItem('gy_space_url') || spaceLink(DEFAULT_SPACE) + '?s=20';
      if (el.spaceUrl) el.spaceUrl.value = u;
      state.id = parseSpaceId(u);
    } catch (_) {}
  }

  function saveKey() {
    try {
      if (el.rtmpKey) localStorage.setItem('gy_x_stream_key', el.rtmpKey.value.trim());
      if (el.rtmpProto) localStorage.setItem('gy_x_rtmp_proto', el.rtmpProto.value);
      if (el.spaceUrl) localStorage.setItem('gy_space_url', el.spaceUrl.value.trim());
    } catch (_) {}
  }

  function mkSlotEl(kind, index, slot) {
    const div = document.createElement('div');
    div.className = 'sp-slot'
      + (slot.talking && !slot.muted ? ' talking' : '')
      + (slot.nick ? ' seated' : ' empty')
      + (slot.muted ? ' muted' : '');
    div.dataset.role = kind;
    div.dataset.index = String(index);
    const label = document.createElement('div');
    label.className = 'sp-slot-label';
    const roleTag = kind === 'host' ? 'HOST' : kind === 'cohost' ? 'CO-HOST' : 'SPEAKER';
    const who = slot.nick || slot.ph;
    const left = document.createElement('span');
    left.className = 'sp-role';
    left.textContent = roleTag + (kind !== 'host' ? ' ' + (index + 1) : '');
    const right = document.createElement('span');
    right.style.display = 'flex';
    right.style.alignItems = 'center';
    const nick = document.createElement('span');
    nick.className = 'sp-nick';
    nick.textContent = who + (slot.muted ? ' 🔇' : '');
    const muteBtn = document.createElement('button');
    muteBtn.type = 'button';
    muteBtn.className = 'sp-mute';
    muteBtn.textContent = slot.muted ? 'unmute' : 'mute';
    muteBtn.title = 'Host mute control';
    muteBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      toggleMute(kind, index, !slot.muted);
    });
    right.appendChild(nick);
    right.appendChild(muteBtn);
    label.appendChild(left);
    label.appendChild(right);
    const canvas = document.createElement('canvas');
    canvas.className = 'sp-wave';
    canvas.width = 160;
    canvas.height = 28;
    canvas.setAttribute('aria-label', 'waveform');
    div.appendChild(label);
    div.appendChild(canvas);
    div._wave = canvas;
    div._slot = slot;
    return div;
  }

  function toggleMute(role, index, muted) {
    const slot = slotFor(role, index);
    if (!slot) return;
    slot.muted = muted;
    if (muted) {
      slot.level = 0;
      slot.talking = false;
    }
    if (isMySeat(role, index)) selfMuted = muted;
    rebuildSlots();
    const b = burst();
    if (b.sendJSON) {
      b.sendJSON({
        type: 'space-mute',
        from: b.nick || 'web',
        role: role,
        slot: index,
        muted: muted,
        by: b.nick || 'web',
        space: state.id,
        t: Date.now(),
      });
    }
    pushChat('system', (muted ? 'muted' : 'unmuted') + ' ' + role + (role !== 'host' ? ' ' + (index + 1) : ''), 'host');
  }

  function applyMuteAllLocal(on) {
    state.muteAll = on;
    state.cohosts.forEach((s) => { s.muted = on; if (on) { s.level = 0; s.talking = false; } });
    state.speakers.forEach((s) => { s.muted = on; if (on) { s.level = 0; s.talking = false; } });
    rebuildSlots();
  }

  function setMuteAll(on) {
    applyMuteAllLocal(on);
    const b = burst();
    if (b.sendJSON) {
      b.sendJSON({
        type: 'space-mute-all', from: b.nick || 'web', muted: on, by: b.nick || 'web',
        space: state.id, t: Date.now(),
      });
    }
    pushChat('system', on ? 'host muted all stage seats' : 'host unmuted all', 'host');
  }

  function renderListeners() {
    if (el.listeners) el.listeners.textContent = String(state.listenerList.length || state.listeners || 0);
    if (!el.listenerList) return;
    el.listenerList.innerHTML = '';
    const list = state.listenerList || [];
    if (!list.length) {
      const chip = document.createElement('span');
      chip.className = 'listener-chip empty';
      chip.textContent = 'no listeners yet · seat as listener';
      el.listenerList.appendChild(chip);
      return;
    }
    list.slice(0, 40).forEach((n) => {
      const chip = document.createElement('span');
      chip.className = 'listener-chip';
      chip.textContent = typeof n === 'string' ? n : (n.nick || '?');
      el.listenerList.appendChild(chip);
    });
    if (list.length > 40) {
      const chip = document.createElement('span');
      chip.className = 'listener-chip empty';
      chip.textContent = '+' + (list.length - 40);
      el.listenerList.appendChild(chip);
    }
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function rebuildSlots() {
    if (el.hostSlot) {
      el.hostSlot.innerHTML = '';
      el.hostSlot.appendChild(mkSlotEl('host', 0, state.host));
    }
    if (el.cohosts) {
      el.cohosts.innerHTML = '';
      state.cohosts.forEach((s, i) => el.cohosts.appendChild(mkSlotEl('cohost', i, s)));
    }
    if (el.speakers) {
      el.speakers.innerHTML = '';
      state.speakers.forEach((s, i) => el.speakers.appendChild(mkSlotEl('speaker', i, s)));
    }
    renderListeners();
    if (el.caption) {
      el.caption.textContent = state.caption || 'Caption placeholder — pin a line or set lower-third';
      el.caption.classList.toggle('placeholder', !state.caption);
    }
    if (el.spaceMeta) {
      const nL = state.listenerList.length || state.listeners || 0;
      el.spaceMeta.innerHTML =
        '<em>' + escapeHtml(state.id) + '</em> · host + ' + COHOSTS + ' co-hosts · ' +
        SPEAKERS + ' speakers · ' + nL + ' listeners' +
        (state.muteAll ? ' · <span style="color:var(--err)">MUTE ALL</span>' : '') +
        (state.assetOffer ? ' · <span style="color:var(--grok)">ASSET</span>' : '');
    }
    if (el.spaceTitle) {
      el.spaceTitle.textContent = state.title || 'GrokYtalkY Space';
    }
    if (el.btnAssetOffer) {
      el.btnAssetOffer.textContent = state.assetOffer ? 'Asset ON' : 'Offer asset';
    }
    updateSlotSelect();
    // visual stage (host circles · speaker glyphs · listener grid)
    if (window.__gyStage && typeof window.__gyStage.render === 'function') {
      window.__gyStage.render(state, {
        myRole: myRole,
        mySlot: mySlot,
        selfMuted: selfMuted,
        nick: (burst().nick) || '',
      });
    }
  }

  function updateSlotSelect() {
    if (!el.slotSelect || !el.roleSelect) return;
    const role = el.roleSelect.value;
    el.slotSelect.innerHTML = '';
    let n = 1;
    if (role === 'cohost') n = COHOSTS;
    if (role === 'speaker') n = SPEAKERS;
    if (role === 'host' || role === 'listener') n = 1;
    for (let i = 0; i < n; i++) {
      const o = document.createElement('option');
      o.value = String(i);
      o.textContent = role === 'host' || role === 'listener' ? '—' : String(i + 1);
      el.slotSelect.appendChild(o);
    }
    el.slotSelect.disabled = role === 'host' || role === 'listener';
  }

  function paintWave(canvas, level, bands, talking) {
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    ctx.fillStyle = '#08080c';
    ctx.fillRect(0, 0, w, h);
    const n = bands && bands.length ? bands.length : 16;
    const gap = 1;
    const bw = Math.max(2, (w - gap * n) / n);
    for (let i = 0; i < n; i++) {
      let v = bands ? (bands[i] || 0) : level * (0.4 + 0.6 * Math.sin(i + level * 8));
      v = Math.max(0.04, Math.min(1, v));
      const bh = v * (h - 4);
      const x = i * (bw + gap);
      const y = h - bh - 1;
      if (talking) {
        ctx.fillStyle = 'rgba(74,222,128,' + (0.45 + v * 0.55) + ')';
      } else {
        ctx.fillStyle = 'rgba(125,211,252,' + (0.12 + v * 0.35) + ')';
      }
      ctx.fillRect(x, y, bw, bh);
    }
  }

  function slotFor(role, index) {
    if (role === 'host') return state.host;
    if (role === 'cohost') return state.cohosts[index];
    if (role === 'speaker') return state.speakers[index];
    return null;
  }

  function paintAllWaves() {
    const nodes = el.root.querySelectorAll('.sp-slot');
    nodes.forEach((node) => {
      const role = node.dataset.role;
      const index = parseInt(node.dataset.index || '0', 10);
      const slot = slotFor(role, index);
      if (!slot || !node._wave) return;
      // local mic drives our seat (unless muted)
      let lv = slot.level;
      let bands = null;
      let talking = slot.talking;
      if (slot.muted || (state.muteAll && role !== 'host') || (isMySeat(role, index) && selfMuted)) {
        lv = 0;
        talking = false;
        bands = null;
        slot.level = 0;
        slot.talking = false;
        node.classList.remove('talking');
        node.classList.add('muted');
      } else if (isMySeat(role, index) && analyser) {
        lv = level;
        bands = micBands;
        talking = lv > 0.06 || burst().tx;
        slot.level = lv;
        slot.talking = talking;
        node.classList.toggle('talking', talking);
      } else {
        // idle shimmer for empty placeholders
        if (!slot.nick) {
          lv = 0.05 + 0.03 * Math.sin(performance.now() / 800 + index);
          bands = null;
          talking = false;
        } else {
          bands = null;
        }
      }
      paintWave(node._wave, lv, bands, talking);
    });
  }

  function isMySeat(role, index) {
    if (myRole === 'listener') return false;
    if (myRole !== role) return false;
    if (role === 'host') return true;
    return index === mySlot;
  }

  function readMic() {
    if (!analyser) {
      // try attach from burst orb stream
      const b = burst();
      if (b.getAnalyser) analyser = b.getAnalyser();
      if (!analyser) return;
    }
    const data = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(data);
    let sum = 0;
    for (let i = 0; i < micBands.length; i++) {
      const idx = Math.floor((i / micBands.length) * data.length);
      const v = (data[idx] || 0) / 255;
      micBands[i] = Math.max(micBands[i] * 0.45, v);
      sum += v;
    }
    level = sum / micBands.length;
    // decay remote levels slightly
    decayRemote();
  }

  function decayRemote() {
    const decay = (s) => {
      if (!s) return;
      if (!isMySeat('host', 0) && s === state.host) {
        s.level *= 0.92;
        s.talking = s.level > 0.08;
      }
    };
    state.host.level *= isMySeat('host', 0) ? 1 : 0.92;
    state.cohosts.forEach((s, i) => {
      if (!isMySeat('cohost', i)) {
        s.level *= 0.92;
        s.talking = s.level > 0.08;
      }
    });
    state.speakers.forEach((s, i) => {
      if (!isMySeat('speaker', i)) {
        s.level *= 0.92;
        s.talking = s.level > 0.08;
      }
    });
    void decay;
  }

  function loop() {
    raf = requestAnimationFrame(loop);
    readMic();
    paintAllWaves();
    // mesh levels while speaking on stage (skip if muted)
    const b = burst();
    const mySlotObj = slotFor(myRole, mySlot);
    const muted = selfMuted || (mySlotObj && mySlotObj.muted) || (state.muteAll && myRole !== 'host');
    if (b.sendJSON && !muted && (level > 0.08 || b.tx) && myRole !== 'listener') {
      if (!loop._lastLv || performance.now() - loop._lastLv > 120) {
        loop._lastLv = performance.now();
        b.sendJSON({
          type: 'space-level',
          from: b.nick || 'web',
          role: myRole,
          slot: mySlot,
          level: Math.round(level * 1000) / 1000,
          space: state.id,
          t: Date.now(),
        });
      }
    }
  }

  function pushChat(from, text, role, opts) {
    if (!el.chatLog || !text) return;
    const line = document.createElement('div');
    line.className = 'space-chat-line' + (opts && opts.pin ? ' pin' : '');
    const who = document.createElement('span');
    who.className = 'who' + (role ? ' ' + role : '');
    who.textContent = from || 'anon';
    line.appendChild(who);
    line.appendChild(document.createTextNode(' ' + text));
    el.chatLog.appendChild(line);
    el.chatLog.scrollTop = el.chatLog.scrollHeight;
    // cap DOM
    while (el.chatLog.children.length > 120) {
      el.chatLog.removeChild(el.chatLog.firstChild);
    }
  }

  function seedPlaceholders() {
    pushChat('system', 'Space ' + state.id + ' — stage seats: host · 2 co-hosts · 10 speakers · listeners', '');
    pushChat('system', 'Chat placeholder: stage questions, pins, and captions land here', '');
    pushChat('system', 'RTMP/RTMPS → ca.pscp.tv · stream key available when ready (Media Studio Sources)', '');
    pushChat('caption', 'Lower-third caption placeholder — set below or /space caption in TUI', 'host');
  }

  function applySpaceFromInput() {
    const raw = (el.spaceUrl && el.spaceUrl.value) || '';
    state.id = parseSpaceId(raw);
    if (el.spaceUrl) el.spaceUrl.value = spaceLink(state.id) + (raw.includes('?s=') ? '?s=20' : '');
    saveKey();
    rebuildSlots();
    pushChat('system', 'bound Space → ' + spaceLink(state.id), '');
    broadcastRoster();
  }

  function seatMe() {
    myRole = (el.roleSelect && el.roleSelect.value) || 'host';
    mySlot = parseInt((el.slotSelect && el.slotSelect.value) || '0', 10) || 0;
    const nick = (burst().nick) || 'web-' + Math.random().toString(36).slice(2, 6);
    if (myRole === 'listener') {
      if (!state.listenerList.includes(nick)) state.listenerList.push(nick);
      state.listeners = state.listenerList.length;
      pushChat('system', nick + ' joined as listener', '');
      const b = burst();
      if (b.sendJSON) {
        b.sendJSON({
          type: 'space-listener-join', from: nick, nick: nick,
          space: state.id, t: Date.now(),
        });
      }
    } else {
      const slot = slotFor(myRole, mySlot);
      if (slot) {
        slot.nick = nick;
        slot.talking = false;
      }
      // leave listener list if promoting
      state.listenerList = state.listenerList.filter((n) => n !== nick);
      state.listeners = state.listenerList.length;
      pushChat('system', nick + ' seated as ' + myRole + (myRole !== 'host' ? ' ' + (mySlot + 1) : ''), myRole);
    }
    rebuildSlots();
    broadcastRoster();
  }

  function broadcastAsset(offer) {
    state.assetOffer = offer;
    rebuildSlots();
    const b = burst();
    if (b.sendJSON) {
      b.sendJSON({
        type: 'space-asset',
        from: b.nick || 'web',
        offer: offer,
        operator: b.nick || 'web',
        label: 'GrokYtalkY stream asset',
        public: true,
        space: state.id,
        url: spaceLink(state.id),
        rtmp: (el.rtmpProto && el.rtmpProto.value === 'rtmp') ? RTMP : RTMPS,
        ready: !!(el.rtmpKey && el.rtmpKey.value.trim()),
        t: Date.now(),
      });
    }
    pushChat('system', offer
      ? 'stream asset OFFERED — others can seat; operator publishes RTMP via gy stream-x'
      : 'stream asset offer stopped', '');
  }

  async function autoPullKey() {
    // 1) localStorage  2) hub /api/space/key  3) clipboard
    updateRtmpUI();
    const token = (el.spaceToken && el.spaceToken.value.trim())
      || (localStorage.getItem('gy_space_token') || '');
    if (el.spaceToken && token) el.spaceToken.value = token;
    try { localStorage.setItem('gy_space_token', token); } catch (_) {}

    // already have key?
    if (el.rtmpKey && el.rtmpKey.value.trim()) {
      if (el.rtmpStatus) {
        el.rtmpStatus.textContent = 'stream key ready (local) · publish armed';
        el.rtmpStatus.className = 'rtmp-status ready';
      }
      pushChat('system', 'key present locally', '');
      return;
    }

    // hub auto-pull
    const host = location.hostname || '127.0.0.1';
    const port = 9876;
    const base = 'http://' + (host === 'localhost' ? '127.0.0.1' : host) + ':' + port;
    if (token) {
      try {
        const r = await fetch(base + '/api/space/key?token=' + encodeURIComponent(token));
        const j = await r.json();
        if (j.stream_key) {
          el.rtmpKey.value = j.stream_key;
          saveKey();
          updateRtmpUI();
          pushChat('system', 'stream key auto-pulled from hub · ' + (j.source || 'api'), 'host');
          return;
        }
      } catch (e) {
        pushChat('system', 'hub key pull failed — is gy serve up?', '');
      }
    }

    // public status only
    try {
      const r = await fetch(base + '/api/space');
      const j = await r.json();
      if (j.rtmp && j.rtmp.ready) {
        if (el.rtmpStatus) {
          el.rtmpStatus.textContent = 'hub reports key ready · provide GY_SPACE_TOKEN to pull';
          el.rtmpStatus.className = 'rtmp-status wait';
        }
      }
    } catch (_) {}

    // clipboard
    try {
      if (navigator.clipboard && navigator.clipboard.readText) {
        const t = (await navigator.clipboard.readText()).trim().split(/\r?\n/)[0];
        if (t && t.length >= 8 && !t.includes(' ')) {
          el.rtmpKey.value = t;
          saveKey();
          updateRtmpUI();
          pushChat('system', 'stream key auto-pulled from clipboard', 'host');
          return;
        }
      }
    } catch (_) {}

    if (el.rtmpStatus) {
      el.rtmpStatus.textContent = 'stream key available when ready · paste, or gy stream-x key / GY_X_STREAM_KEY';
      el.rtmpStatus.className = 'rtmp-status wait';
    }
    pushChat('system', 'key not ready — paste Media Studio key or run gy stream-x key', '');
  }

  function broadcastRoster() {
    const b = burst();
    if (!b.sendJSON) return;
    b.sendJSON({
      type: 'space-roster',
      from: b.nick || 'web',
      space: state.id,
      url: spaceLink(state.id),
      title: state.title,
      caption: state.caption,
      host: { nick: state.host.nick, level: state.host.level, talking: state.host.talking, muted: state.host.muted },
      cohosts: state.cohosts.map((s, i) => ({ index: i, nick: s.nick, level: s.level, talking: s.talking, muted: s.muted })),
      speakers: state.speakers.map((s, i) => ({ index: i, nick: s.nick, level: s.level, talking: s.talking, muted: s.muted })),
      listeners: state.listenerList.length,
      listener_list: state.listenerList.map((n) => ({ nick: n })),
      mute_all: state.muteAll,
      asset: { offer: state.assetOffer, public: true },
      t: Date.now(),
    });
  }

  function sendChat() {
    const text = (el.chatInput && el.chatInput.value || '').trim();
    if (!text) return;
    const nick = (burst().nick) || 'web';
    pushChat(nick, text, myRole === 'listener' ? 'listener' : myRole);
    el.chatInput.value = '';
    const b = burst();
    if (b.sendJSON) {
      b.sendJSON({
        type: 'space-chat',
        from: nick,
        text: text,
        role: myRole,
        space: state.id,
        t: Date.now(),
      });
      // also hub chat for DOJO bridge
      b.sendJSON({ type: 'chat', text: text, from: nick, room: 'space:' + state.id, t: Date.now() });
    }
  }

  function setCaption() {
    const text = (el.captionInput && el.captionInput.value || '').trim();
    state.caption = text;
    rebuildSlots();
    const b = burst();
    if (b.sendJSON && text) {
      b.sendJSON({
        type: 'space-caption',
        from: b.nick || 'web',
        text: text,
        caption: text,
        space: state.id,
        t: Date.now(),
      });
    }
    pushChat('caption', text || '(cleared)', 'host', { pin: true });
  }

  function updateRtmpUI() {
    const secure = !el.rtmpProto || el.rtmpProto.value !== 'rtmp';
    const base = secure ? RTMPS : RTMP;
    if (el.rtmpBase) el.rtmpBase.textContent = base;
    const key = (el.rtmpKey && el.rtmpKey.value || '').trim();
    if (el.rtmpStatus) {
      if (!key) {
        el.rtmpStatus.textContent = 'stream key available when ready · paste from X Media Studio → Sources → RTMP';
        el.rtmpStatus.className = 'rtmp-status wait';
      } else {
        el.rtmpStatus.textContent = 'stream key ready · publish armed (do not share key)';
        el.rtmpStatus.className = 'rtmp-status ready';
      }
    }
    if (el.rtmpCmd) {
      const target = key ? base + '/' + key : base + '/<STREAM_KEY>';
      el.rtmpCmd.textContent =
        'gy space-rtmp --' + (secure ? 'rtmps' : 'rtmp') +
        ' --key ' + (key ? '***' : '$GY_X_STREAM_KEY') +
        ' --in video.mp4\n' +
        '# ffmpeg … -f flv "' + (key ? base + '/…' : target) + '"';
    }
    saveKey();
  }

  /** inbound mesh from burst-orb */
  function onMesh(msg) {
    if (!msg || !msg.type) return;
    const typ = msg.type;
    const from = msg.from || '';
    if (typ === 'space-roster') {
      if (msg.space) state.id = String(msg.space);
      if (msg.caption) state.caption = String(msg.caption);
      if (msg.title) state.title = String(msg.title);
      if (typeof msg.listeners === 'number') state.listeners = msg.listeners;
      if (typeof msg.mute_all === 'boolean') state.muteAll = msg.mute_all;
      if (msg.host) {
        if (msg.host.nick) state.host.nick = msg.host.nick;
        state.host.level = msg.host.level || 0;
        state.host.talking = !!msg.host.talking;
        state.host.muted = !!msg.host.muted;
      }
      if (Array.isArray(msg.cohosts)) {
        msg.cohosts.forEach((c) => {
          const i = c.index | 0;
          if (state.cohosts[i]) {
            state.cohosts[i].nick = c.nick || state.cohosts[i].nick;
            state.cohosts[i].level = c.level || 0;
            state.cohosts[i].talking = !!c.talking;
            state.cohosts[i].muted = !!c.muted;
          }
        });
      }
      if (Array.isArray(msg.speakers)) {
        msg.speakers.forEach((c) => {
          const i = c.index | 0;
          if (state.speakers[i]) {
            state.speakers[i].nick = c.nick || state.speakers[i].nick;
            state.speakers[i].level = c.level || 0;
            state.speakers[i].talking = !!c.talking;
            state.speakers[i].muted = !!c.muted;
          }
        });
      }
      if (Array.isArray(msg.listener_list)) {
        state.listenerList = msg.listener_list.map((x) => (typeof x === 'string' ? x : x.nick)).filter(Boolean);
        state.listeners = state.listenerList.length;
      } else if (typeof msg.listeners === 'number') {
        state.listeners = msg.listeners;
      }
      if (msg.asset && typeof msg.asset.offer === 'boolean') {
        state.assetOffer = msg.asset.offer;
      }
      rebuildSlots();
    }
    if (typ === 'space-level' && from !== (burst().nick || '')) {
      const role = msg.role;
      const slot = msg.slot | 0;
      const lv = Number(msg.level) || 0;
      const s = slotFor(role, slot);
      if (s && !s.muted) {
        s.level = lv;
        s.talking = lv > 0.08;
        if (!s.nick) s.nick = from;
      }
    }
    if (typ === 'space-mute') {
      const s = slotFor(msg.role, msg.slot | 0);
      if (s) {
        s.muted = !!msg.muted;
        if (s.muted) { s.level = 0; s.talking = false; }
        rebuildSlots();
      }
    }
    if (typ === 'space-mute-all') {
      applyMuteAllLocal(!!msg.muted);
      pushChat('system', msg.muted ? 'stage mute-all (remote host)' : 'stage unmuted (remote)', 'host');
    }
    if (typ === 'space-listener-join') {
      const n = msg.nick || from;
      if (n && !state.listenerList.includes(n)) state.listenerList.push(n);
      state.listeners = state.listenerList.length;
      renderListeners();
      pushChat('system', n + ' listening', 'listener');
    }
    if (typ === 'space-listener-leave') {
      const n = msg.nick || from;
      state.listenerList = state.listenerList.filter((x) => x !== n);
      state.listeners = state.listenerList.length;
      renderListeners();
    }
    if (typ === 'space-asset') {
      state.assetOffer = !!msg.offer;
      rebuildSlots();
      if (msg.offer) {
        pushChat('system', (msg.operator || from) + ' offers gy stream asset → X.com', '');
      }
    }
    if (typ === 'space-chat') {
      pushChat(from, msg.text || '', msg.role || '');
    }
    if (typ === 'space-caption') {
      state.caption = msg.text || msg.caption || '';
      rebuildSlots();
      pushChat('caption', state.caption, 'host', { pin: true });
    }
    if (typ === 'chat' && msg.text) {
      pushChat(from + '↗', msg.text, msg.role || '');
    }
    if (typ === 'audio' && msg.from && msg.from !== (burst().nick || '')) {
      pulseNick(msg.from, 0.55);
    }
  }

  function pulseNick(nick, lv) {
    const trySlot = (s) => {
      if (s && s.nick === nick) {
        s.level = Math.max(s.level, lv);
        s.talking = true;
      }
    };
    trySlot(state.host);
    state.cohosts.forEach(trySlot);
    state.speakers.forEach(trySlot);
  }

  // wire UI
  loadStored();
  rebuildSlots();
  seedPlaceholders();
  updateRtmpUI();

  el.btnApplySpace && el.btnApplySpace.addEventListener('click', applySpaceFromInput);
  el.btnOpenSpace && el.btnOpenSpace.addEventListener('click', () => {
    window.open(spaceLink(state.id) + '?s=20', '_blank', 'noopener');
  });
  el.btnSeatMe && el.btnSeatMe.addEventListener('click', seatMe);
  el.btnMuteAll && el.btnMuteAll.addEventListener('click', () => setMuteAll(true));
  el.btnUnmuteAll && el.btnUnmuteAll.addEventListener('click', () => setMuteAll(false));
  el.btnSelfMute && el.btnSelfMute.addEventListener('click', () => {
    selfMuted = !selfMuted;
    if (myRole !== 'listener') toggleMute(myRole, mySlot, selfMuted);
    else pushChat('system', selfMuted ? 'self muted (listener)' : 'self unmuted', '');
    if (el.btnSelfMute) el.btnSelfMute.textContent = selfMuted ? 'Self unmute' : 'Self mute';
  });
  el.btnAssetOffer && el.btnAssetOffer.addEventListener('click', () => {
    broadcastAsset(!state.assetOffer);
  });
  el.btnKeyPull && el.btnKeyPull.addEventListener('click', () => { autoPullKey(); });
  el.roleSelect && el.roleSelect.addEventListener('change', updateSlotSelect);
  el.chatSend && el.chatSend.addEventListener('click', sendChat);
  el.chatInput && el.chatInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      sendChat();
    }
  });
  el.captionSet && el.captionSet.addEventListener('click', setCaption);
  el.rtmpProto && el.rtmpProto.addEventListener('change', updateRtmpUI);
  el.rtmpKey && el.rtmpKey.addEventListener('input', updateRtmpUI);
  el.spaceUrl && el.spaceUrl.addEventListener('change', applySpaceFromInput);

  // export for burst-orb
  window.__gySpaces = {
    onMesh: onMesh,
    setAnalyser: (a) => { analyser = a; },
    state: () => state,
    broadcastRoster: broadcastRoster,
    autoPullKey: autoPullKey,
  };

  // try auto-pull on load (non-blocking)
  setTimeout(() => { autoPullKey(); }, 400);
  raf = requestAnimationFrame(loop);
})();
