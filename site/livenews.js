/**
 * Live news glyph wall — world catalog, section refresh, main swap column,
 * theme cluster from AI captions / transcripts.
 */
(function () {
  'use strict';
  const G = window.GY_GLYPH;
  const NS = window.GY_NEWS;
  const TH = window.GY_NEWS_THEME;
  if (!G || !NS) return;

  const MAIN_CAP = 24; // tiles in main column (speakers-style)

  const el = {
    regions: document.getElementById('ln-regions'),
    mainGrid: document.getElementById('ln-main-grid'),
    mainSub: document.getElementById('ln-main-sub'),
    themeBar: document.getElementById('ln-theme-bar'),
    meta: document.getElementById('ln-meta'),
    hub: document.getElementById('ln-hub'),
    connect: document.getElementById('ln-connect'),
    expand: document.getElementById('ln-expand'),
    collapse: document.getElementById('ln-collapse'),
    refreshAll: document.getElementById('ln-refresh-all'),
    stress: document.getElementById('ln-stress'),
    sort: document.getElementById('ln-sort'),
    themeDemo: document.getElementById('ln-theme-demo'),
    cluster: document.getElementById('ln-cluster'),
    shuffle: document.getElementById('ln-main-shuffle'),
    cycle: document.getElementById('ln-main-cycle'),
    clear: document.getElementById('ln-main-clear'),
    fill: document.getElementById('ln-main-fill'),
  };

  /** @type {Map<string, TileRec>} */
  const tileMap = new Map();
  /** ordered ids currently in main column */
  let mainIds = [];
  let cycleOffset = 0;
  let focusTheme = '';
  let ws = null;
  let nick = 'news-' + Math.random().toString(36).slice(2, 6);
  let stressN = 0;
  let raf = 0;

  /**
   * @typedef {{ src: object, canvas: HTMLCanvasElement|null, lum: Float32Array, live: boolean, seed: number, hue: number, hubFrame: boolean, gen: number }} TileRec
   */

  function allSources() {
    return NS.MAJOR_NEWS.slice();
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

  function ensureRec(src) {
    let rec = tileMap.get(src.id);
    if (!rec) {
      rec = {
        src: src,
        canvas: null,
        lum: G.simLum(0, hash(src.id), false),
        live: false,
        seed: hash(src.id),
        hue: src.hue || 200,
        hubFrame: false,
        gen: 0,
      };
      tileMap.set(src.id, rec);
    }
    return rec;
  }

  function mkTileEl(src, opts) {
    opts = opts || {};
    const rec = ensureRec(src);
    const tile = document.createElement('div');
    tile.className =
      'ln-tile' +
      (mainIds.indexOf(src.id) >= 0 ? ' is-in-main' : '') +
      (opts.main ? ' is-pinned' : '');
    tile.tabIndex = 0;
    tile.dataset.id = src.id;
    tile.setAttribute('role', 'listitem');
    tile.setAttribute('aria-label', src.label);

    const canvas = document.createElement('canvas');
    canvas.width = opts.main ? 90 : 125;
    canvas.height = opts.main ? 90 : 125;
    rec.canvas = canvas;

    const capMeta = TH && TH.getMeta(src.id);
    if (capMeta && (capMeta.caption || capMeta.transcript)) {
      const cap = document.createElement('div');
      cap.className = 'ln-cap';
      cap.textContent = (capMeta.caption || capMeta.transcript).slice(0, 80);
      tile.appendChild(cap);
    }

    const meta = document.createElement('div');
    meta.className = 'ln-tile-meta';
    meta.innerHTML =
      '<span class="ln-name">' +
      escapeHtml(src.label) +
      '</span><span class="ln-kind-badge">' +
      escapeHtml(src.kind || src.region) +
      '</span>';

    const actions = document.createElement('div');
    actions.className = 'ln-tile-actions';
    const pin = document.createElement('button');
    pin.type = 'button';
    pin.textContent = mainIds.indexOf(src.id) >= 0 ? 'unpin' : '→ main';
    pin.title = 'Swap into main Glyph column';
    pin.addEventListener('click', (e) => {
      e.stopPropagation();
      toggleMain(src.id);
    });
    const open = document.createElement('a');
    open.href = src.url;
    open.target = '_blank';
    open.rel = 'noopener';
    open.textContent = '↗';
    open.title = 'Open live';
    open.addEventListener('click', (e) => e.stopPropagation());
    actions.appendChild(pin);
    actions.appendChild(open);

    tile.appendChild(canvas);
    tile.appendChild(meta);
    tile.appendChild(actions);

    tile.addEventListener('click', () => {
      document.querySelectorAll('.ln-tile.is-focus').forEach((t) => t.classList.remove('is-focus'));
      tile.classList.add('is-focus');
      if (!opts.main) toggleMain(src.id, true);
    });

    G.paintGlyphCanvas(canvas, rec.lum, {
      cell: opts.main ? 3 : 5,
      tint: rec.live ? 0 : rec.hue,
    });
    return tile;
  }

  function toggleMain(id, forceOn) {
    const i = mainIds.indexOf(id);
    if (i >= 0 && !forceOn) {
      mainIds.splice(i, 1);
    } else if (i < 0) {
      mainIds.unshift(id);
      if (mainIds.length > MAIN_CAP) mainIds.length = MAIN_CAP;
    }
    renderMain();
    // update section tile highlights without full rebuild
    document.querySelectorAll('.ln-tile[data-id]').forEach((node) => {
      const id2 = node.dataset.id;
      node.classList.toggle('is-in-main', mainIds.indexOf(id2) >= 0);
      const btn = node.querySelector('.ln-tile-actions button');
      if (btn) btn.textContent = mainIds.indexOf(id2) >= 0 ? 'unpin' : '→ main';
    });
    setMeta();
  }

  function renderMain() {
    if (!el.mainGrid) return;
    el.mainGrid.innerHTML = '';
    if (!mainIds.length) {
      const empty = document.createElement('div');
      empty.className = 'ln-tile';
      empty.style.gridColumn = '1 / -1';
      empty.style.minHeight = '80px';
      empty.style.display = 'flex';
      empty.style.alignItems = 'center';
      empty.style.justifyContent = 'center';
      empty.style.color = 'var(--faint)';
      empty.style.fontFamily = 'var(--mono)';
      empty.style.fontSize = '0.65rem';
      empty.textContent = 'pin feeds → main · or Fill from sort';
      el.mainGrid.appendChild(empty);
    } else {
      mainIds.forEach((id) => {
        const src = NS.findById(id);
        if (src) el.mainGrid.appendChild(mkTileEl(src, { main: true }));
      });
    }
    renderThemeBar();
    if (el.mainSub) {
      el.mainSub.textContent =
        mainIds.length +
        '/' +
        MAIN_CAP +
        ' · ' +
        (el.sort && el.sort.value === 'theme' ? 'theme clump' : 'manual swap');
    }
  }

  function renderThemeBar() {
    if (!el.themeBar || !TH) return;
    el.themeBar.innerHTML = '';
    const clusters = TH.cluster(mainIds.length ? mainIds : allSources().map((s) => s.id).slice(0, 40));
    clusters.forEach((c) => {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'ln-theme-chip' + (focusTheme === c.theme ? ' is-on' : '');
      chip.innerHTML = escapeHtml(c.label) + ' <em>' + c.ids.length + '</em>';
      chip.title = 'Filter / fill main with ' + c.label;
      chip.addEventListener('click', () => {
        focusTheme = focusTheme === c.theme ? '' : c.theme;
        if (focusTheme) {
          mainIds = c.ids.slice(0, MAIN_CAP);
        }
        renderMain();
        renderThemeBar();
      });
      el.themeBar.appendChild(chip);
    });
  }

  function sourcesForRegion(reg) {
    return allSources().filter((s) => s.region === reg.id);
  }

  function buildSections() {
    if (!el.regions) return;
    el.regions.innerHTML = '';
    NS.REGIONS.forEach((reg) => {
      let sources = sourcesForRegion(reg);
      if (!sources.length) return;
      const sortMode = (el.sort && el.sort.value) || 'region';
      if (sortMode === 'alpha') {
        sources = sources.slice().sort((a, b) => a.label.localeCompare(b.label));
      } else if (sortMode === 'kind') {
        sources = sources.slice().sort((a, b) => (a.kind || '').localeCompare(b.kind || '') || a.label.localeCompare(b.label));
      } else if (sortMode === 'theme' && TH) {
        const order = TH.sortIds(sources.map((s) => s.id));
        const map = {};
        sources.forEach((s) => (map[s.id] = s));
        sources = order.map((id) => map[id]).filter(Boolean);
      }

      const details = document.createElement('details');
      details.className = 'ln-region';
      details.open =
        reg.id === 'us' ||
        reg.id === 'weather' ||
        reg.id === 'public' ||
        reg.id === 'world' ||
        reg.id === 'earthcam' ||
        reg.id === 'earthcam-us' ||
        reg.id === 'earthcam-highway';
      details.setAttribute('data-persist', 'ln-' + reg.id);
      details.dataset.region = reg.id;

      const sum = document.createElement('summary');
      const refresh = document.createElement('button');
      refresh.type = 'button';
      refresh.className = 'ln-refresh';
      refresh.textContent = '↻ refresh';
      refresh.title = 'Refresh this section';
      refresh.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        refreshSection(reg.id);
      });
      const swap = document.createElement('button');
      swap.type = 'button';
      swap.className = 'ln-swap';
      swap.textContent = '→ main';
      swap.title = 'Swap entire section into main column';
      swap.addEventListener('click', (e) => {
        e.preventDefault();
        e.stopPropagation();
        swapSectionToMain(reg.id);
      });

      sum.innerHTML =
        '<span class="ln-live-dot" aria-hidden="true"></span>' +
        '<span>' +
        escapeHtml(reg.label) +
        '</span>';
      const count = document.createElement('span');
      count.className = 'ln-count';
      count.textContent = sources.length + ' streams';
      sum.appendChild(count);
      sum.appendChild(refresh);
      sum.appendChild(swap);
      details.appendChild(sum);

      const body = document.createElement('div');
      body.className = 'ln-region-body';
      const grid = document.createElement('div');
      grid.className = 'ln-grid';
      grid.setAttribute('role', 'list');
      grid.dataset.region = reg.id;
      sources.forEach((src) => {
        ensureRec(src);
        grid.appendChild(mkTileEl(src, {}));
      });
      body.appendChild(grid);
      details.appendChild(body);
      el.regions.appendChild(details);
    });
    G.persistDetails(el.regions, 'gy_livenews_details_v2');
    setMeta();
  }

  function refreshSection(regionId) {
    const sources = allSources().filter((s) => s.region === regionId);
    sources.forEach((src) => {
      const rec = ensureRec(src);
      rec.hubFrame = false;
      rec.live = false;
      rec.gen++;
      rec.lum = G.simLum(performance.now() + rec.gen * 17, rec.seed, false);
      if (rec.canvas) {
        G.paintGlyphCanvas(rec.canvas, rec.lum, { cell: 5, tint: rec.hue });
      }
    });
    // flash meta
    if (el.meta) {
      el.meta.innerHTML = 'refreshed · <em>' + escapeHtml(regionId) + '</em> · ' + sources.length + ' tiles';
    }
    setTimeout(setMeta, 1200);
  }

  function refreshAll() {
    NS.REGIONS.forEach((r) => refreshSection(r.id));
    mainIds.forEach((id) => {
      const rec = tileMap.get(id);
      if (rec) {
        rec.hubFrame = false;
        rec.gen++;
      }
    });
    renderMain();
  }

  function swapSectionToMain(regionId) {
    const sources = allSources().filter((s) => s.region === regionId);
    mainIds = sources.map((s) => s.id).slice(0, MAIN_CAP);
    cycleOffset = 0;
    renderMain();
    buildSections(); // refresh pin marks
    setMeta();
  }

  function fillFromSort() {
    let ids = allSources().map((s) => s.id);
    const mode = (el.sort && el.sort.value) || 'region';
    if (mode === 'theme' && TH) ids = TH.sortIds(ids);
    else if (mode === 'alpha') {
      ids = allSources()
        .slice()
        .sort((a, b) => a.label.localeCompare(b.label))
        .map((s) => s.id);
    } else if (mode === 'kind') {
      ids = allSources()
        .slice()
        .sort((a, b) => (a.kind || '').localeCompare(b.kind || '') || a.label.localeCompare(b.label))
        .map((s) => s.id);
    }
    if (focusTheme && TH) {
      const cl = TH.cluster(ids).find((c) => c.theme === focusTheme);
      if (cl) ids = cl.ids;
    }
    const start = cycleOffset % Math.max(1, ids.length);
    const rotated = ids.slice(start).concat(ids.slice(0, start));
    mainIds = rotated.slice(0, MAIN_CAP);
    renderMain();
    buildSections();
    setMeta();
  }

  function shuffleMain() {
    for (let i = mainIds.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      const t = mainIds[i];
      mainIds[i] = mainIds[j];
      mainIds[j] = t;
    }
    renderMain();
  }

  function cycleMain() {
    cycleOffset += MAIN_CAP;
    fillFromSort();
  }

  function setMeta() {
    if (!el.meta) return;
    let live = 0;
    tileMap.forEach((t) => {
      if (t.live) live++;
    });
    const mode = ws && ws.readyState === 1 ? 'hub' : 'poster';
    el.meta.innerHTML =
      mode +
      ' · <em>' +
      allSources().length +
      '</em> catalog · main <em>' +
      mainIds.length +
      '</em> · live ' +
      live +
      ' · stress ' +
      stressN +
      (ws && ws.readyState === 1 ? ' · <em>' + nick + '</em>' : '');
  }

  function loop(now) {
    raf = requestAnimationFrame(loop);
    let i = 0;
    tileMap.forEach((t) => {
      if ((Math.floor(now / 120) + i) % 3 !== 0) {
        i++;
        return;
      }
      if (!t.hubFrame && t.canvas && t.canvas.isConnected) {
        t.lum = G.simLum(now + i * 40 + t.gen * 9, t.seed, t.live || (now / 900 + i) % 6 < 2);
        G.paintGlyphCanvas(t.canvas, t.lum, {
          cell: t.canvas.width <= 100 ? 3 : 5,
          tint: t.live ? 0 : t.hue,
        });
        const parent = t.canvas.parentElement;
        if (parent) parent.classList.toggle('is-live', !!t.live);
      }
      i++;
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
      if (TH) {
        const m = TH.applyMesh(msg);
        // vision/orch mesh: type news-caption with theme
        if (msg.type === 'news-caption' && msg.theme && msg.feed) {
          TH.setCaption(String(msg.feed).toLowerCase().replace(/\s+/g, ''), msg.text || msg.theme, {});
          // also try label match
          if (NS.findById) {
            /* theme stored by applyMesh if id resolves */
          }
        }
        if ((m || (msg.theme && msg.type === 'news-caption')) && el.sort && el.sort.value === 'theme') {
          if (mainIds.length) {
            mainIds = TH.sortIds(mainIds);
            renderMain();
          }
        }
      }
      if ((msg.type === 'vburst-frame' || msg.type === 'news-frame') && Array.isArray(msg.glyph)) {
        const label = (msg.label || msg.src || msg.from || msg.feed || '').toLowerCase();
        let rec = null;
        tileMap.forEach((t) => {
          if (
            label.includes(t.src.id) ||
            label.includes(t.src.label.toLowerCase().slice(0, 5))
          ) {
            rec = t;
          }
        });
        if (rec) {
          rec.lum = Float32Array.from(msg.glyph.map((v) => (v > 1 ? v / 255 : v)));
          rec.live = true;
          rec.hubFrame = true;
          if (rec.canvas && rec.canvas.isConnected) {
            G.paintGlyphCanvas(rec.canvas, rec.lum, { cell: rec.canvas.width <= 100 ? 3 : 5 });
          }
        }
      }
      setMeta();
    };
  }

  function stressJoin() {
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
    tileMap.forEach((t) => {
      t.live = true;
    });
    setMeta();
  }

  function demoThemes() {
    if (!TH) return;
    TH.demoCaptions();
    if (el.sort) el.sort.value = 'theme';
    fillFromSort();
    buildSections();
    setMeta();
  }

  function clusterNow() {
    if (!TH) return;
    if (!mainIds.length) fillFromSort();
    mainIds = TH.sortIds(mainIds);
    renderMain();
    buildSections();
  }

  // wire
  el.connect && el.connect.addEventListener('click', connect);
  el.expand &&
    el.expand.addEventListener('click', () => {
      el.regions.querySelectorAll('details').forEach((d) => (d.open = true));
    });
  el.collapse &&
    el.collapse.addEventListener('click', () => {
      el.regions.querySelectorAll('details').forEach((d) => (d.open = false));
    });
  el.refreshAll && el.refreshAll.addEventListener('click', refreshAll);
  el.stress && el.stress.addEventListener('click', stressJoin);
  el.sort &&
    el.sort.addEventListener('change', () => {
      buildSections();
      if (el.sort.value === 'theme') fillFromSort();
    });
  el.themeDemo && el.themeDemo.addEventListener('click', demoThemes);
  el.cluster && el.cluster.addEventListener('click', clusterNow);
  el.shuffle && el.shuffle.addEventListener('click', shuffleMain);
  el.cycle && el.cycle.addEventListener('click', cycleMain);
  el.clear &&
    el.clear.addEventListener('click', () => {
      mainIds = [];
      renderMain();
      buildSections();
    });
  el.fill && el.fill.addEventListener('click', fillFromSort);

  // init
  allSources().forEach(ensureRec);
  buildSections();
  // default main: mix of us news + weather + world
  mainIds = [
    'cnn',
    'bbc',
    'aje',
    'reu',
    'weatherch',
    'earthcam',
    'ec-timessq',
    'ec-ggbridge',
    'ec-eiffel',
    'ec-shibuya',
    'ec-traffic-la',
    'nasa',
  ]
    .filter((id) => NS.findById(id))
    .slice(0, MAIN_CAP);
  renderMain();
  raf = requestAnimationFrame(loop);
  setMeta();
})();
