/**
 * Live news glyph wall — collapsible regions, 25×25 previews, hub + stress join.
 */
(function () {
  'use strict';
  const G = window.GY_GLYPH;
  const NS = window.GY_NEWS;
  if (!G || !NS) return;

  const el = {
    regions: document.getElementById('ln-regions'),
    meta: document.getElementById('ln-meta'),
    hub: document.getElementById('ln-hub'),
    connect: document.getElementById('ln-connect'),
    expand: document.getElementById('ln-expand'),
    collapse: document.getElementById('ln-collapse'),
    stress: document.getElementById('ln-stress'),
  };

  const tiles = []; // { src, canvas, lum, live, seed }
  let ws = null;
  let nick = 'news-' + Math.random().toString(36).slice(2, 6);
  let stressN = 0;
  let raf = 0;

  function build() {
    if (!el.regions) return;
    el.regions.innerHTML = '';
    NS.REGIONS.forEach((reg) => {
      const sources = NS.MAJOR_NEWS.filter((s) => s.region === reg.id);
      if (!sources.length) return;
      const details = document.createElement('details');
      details.className = 'ln-region';
      details.open = true;
      details.setAttribute('data-persist', 'ln-' + reg.id);
      const sum = document.createElement('summary');
      sum.innerHTML =
        '<span class="ln-live-dot" aria-hidden="true"></span>' +
        '<span>' +
        reg.label +
        '</span>' +
        '<span class="ln-count">' +
        sources.length +
        ' streams</span>';
      details.appendChild(sum);
      const body = document.createElement('div');
      body.className = 'ln-region-body';
      const grid = document.createElement('div');
      grid.className = 'ln-grid';
      grid.setAttribute('role', 'list');
      sources.forEach((src) => {
        grid.appendChild(mkTile(src));
      });
      body.appendChild(grid);
      details.appendChild(body);
      el.regions.appendChild(details);
    });
    G.persistDetails(el.regions, 'gy_livenews_details');
    setMeta();
  }

  function mkTile(src) {
    const tile = document.createElement('div');
    tile.className = 'ln-tile';
    tile.tabIndex = 0;
    tile.dataset.id = src.id;
    tile.setAttribute('role', 'listitem');
    tile.setAttribute('aria-label', src.label);
    const canvas = document.createElement('canvas');
    canvas.width = 125;
    canvas.height = 125;
    const meta = document.createElement('div');
    meta.className = 'ln-tile-meta';
    meta.innerHTML =
      '<span class="ln-name">' +
      escapeHtml(src.label) +
      '</span><span>' +
      src.region.toUpperCase() +
      '</span>';
    const actions = document.createElement('div');
    actions.className = 'ln-tile-actions';
    const open = document.createElement('a');
    open.href = src.url;
    open.target = '_blank';
    open.rel = 'noopener';
    open.textContent = '↗ live';
    open.title = 'Open live page';
    actions.appendChild(open);
    tile.appendChild(canvas);
    tile.appendChild(meta);
    tile.appendChild(actions);
    tile.addEventListener('click', () => {
      document.querySelectorAll('.ln-tile.is-focus').forEach((t) => t.classList.remove('is-focus'));
      tile.classList.add('is-focus');
    });
    const rec = {
      src: src,
      canvas: canvas,
      lum: G.simLum(0, hash(src.id), false),
      live: false,
      seed: hash(src.id),
      hue: src.hue || 200,
    };
    tiles.push(rec);
    return tile;
  }

  function hash(s) {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
    return Math.abs(h % 97) + 1;
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
  }

  function setMeta() {
    if (!el.meta) return;
    const live = tiles.filter((t) => t.live).length;
    const mode = ws && ws.readyState === 1 ? 'hub' : 'poster';
    el.meta.innerHTML =
      mode +
      ' · <em>' +
      tiles.length +
      '</em> tiles · ' +
      live +
      ' live · stress ' +
      stressN +
      (ws && ws.readyState === 1 ? ' · <em>' + nick + '</em>' : '');
  }

  function loop(now) {
    raf = requestAnimationFrame(loop);
    tiles.forEach((t, i) => {
      // refresh sim ~8fps staggered
      if ((Math.floor(now / 120) + i) % 2 === 0) {
        if (!t.hubFrame) {
          t.lum = G.simLum(now + i * 40, t.seed, t.live || (now / 800 + i) % 5 < 2);
        }
        G.paintGlyphCanvas(t.canvas, t.lum, { cell: 5, tint: t.live ? 0 : t.hue });
        const parent = t.canvas.parentElement;
        if (parent) parent.classList.toggle('is-live', !!t.live);
      }
    });
  }

  function connect() {
    let url = (el.hub && el.hub.value.trim()) || '';
    if (!url) {
      const host = location.hostname || '127.0.0.1';
      url =
        'ws://' +
        (host === 'localhost' || host === '127.0.0.1' ? '127.0.0.1' : host) +
        ':9876/?role=peer&nick=' +
        encodeURIComponent(nick) +
        '&room=news';
    } else if (!url.includes('nick=')) {
      url += (url.includes('?') ? '&' : '?') + 'role=peer&nick=' + encodeURIComponent(nick) + '&room=news';
    }
    if (ws) try { ws.close(); } catch (_) {}
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setMeta();
      return;
    }
    ws.onopen = () => {
      ws.send(JSON.stringify({ type: 'join', nick: nick, role: 'news-wall', room: 'news' }));
      setMeta();
      if (el.connect) el.connect.textContent = 'Connected';
    };
    ws.onclose = () => {
      if (el.connect) el.connect.textContent = 'Connect hub';
      setMeta();
    };
    ws.onmessage = (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      // map label → tile when hub ships news/vburst frames
      if ((msg.type === 'vburst-frame' || msg.type === 'news-frame') && Array.isArray(msg.glyph)) {
        const label = (msg.label || msg.src || msg.from || '').toLowerCase();
        const t = tiles.find(
          (x) =>
            label.includes(x.src.id) ||
            label.includes(x.src.label.toLowerCase().slice(0, 4))
        );
        if (t) {
          t.lum = msg.glyph.map((v) => (v > 1 ? v / 255 : v));
          t.live = true;
          t.hubFrame = true;
          G.paintGlyphCanvas(t.canvas, t.lum, { cell: 5 });
        }
      }
      if (msg.type === 'roster' && Array.isArray(msg.peers)) {
        // mark activity
        tiles.forEach((t, i) => {
          t.live = i < Math.min(tiles.length, msg.peers.length);
        });
      }
      setMeta();
    };
  }

  function stressJoin() {
    // announce N fake listeners on mesh for pipeline stress (client-side + optional hub)
    const n = 8;
    stressN += n;
    for (let i = 0; i < n; i++) {
      const id = 'stress-' + Math.random().toString(36).slice(2, 5);
      if (ws && ws.readyState === 1) {
        ws.send(
          JSON.stringify({
            type: 'space-listener-join',
            from: id,
            nick: id,
            room: 'news',
            t: Date.now(),
          })
        );
        // light glyph noise as chat ping
        ws.send(
          JSON.stringify({
            type: 'vburst-frame',
            from: id,
            glyphN: 25,
            glyph: Array.from({ length: 625 }, () => Math.floor(Math.random() * 80)),
            label: 'stress',
            t: Date.now(),
          })
        );
      }
    }
    // also pulse all tiles as "busy"
    tiles.forEach((t) => {
      t.live = true;
    });
    setMeta();
  }

  el.connect && el.connect.addEventListener('click', connect);
  el.expand &&
    el.expand.addEventListener('click', () => {
      el.regions.querySelectorAll('details').forEach((d) => (d.open = true));
    });
  el.collapse &&
    el.collapse.addEventListener('click', () => {
      el.regions.querySelectorAll('details').forEach((d) => (d.open = false));
    });
  el.stress && el.stress.addEventListener('click', stressJoin);

  build();
  raf = requestAnimationFrame(loop);
})();
