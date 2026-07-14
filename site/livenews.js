/**
 * Live news glyph wall — world catalog, section refresh, main swap column,
 * theme cluster from AI captions / transcripts + vision-take (segment_top / pose).
 */
(function () {
  'use strict';
  const G = window.GY_GLYPH;
  const NS = window.GY_NEWS;
  const TH = window.GY_NEWS_THEME;
  const LIVE = window.GY_NEWS_LIVE;
  if (!G || !NS) {
    console.error('[livenews] missing GY_GLYPH or GY_NEWS — scripts failed to load');
    // still wire buttons with error feedback
    document.addEventListener('DOMContentLoaded', function () {
      var m = document.getElementById('ln-meta');
      if (m) m.textContent = 'JS load error · hard refresh (missing glyph/news modules)';
    });
    return;
  }

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
    visionDemo: document.getElementById('ln-vision-demo'),
    cluster: document.getElementById('ln-cluster'),
    shuffle: document.getElementById('ln-main-shuffle'),
    cycle: document.getElementById('ln-main-cycle'),
    clear: document.getElementById('ln-main-clear'),
    fill: document.getElementById('ln-main-fill'),
    visionBar: document.getElementById('ln-vision-bar'),
    cast: document.getElementById('ln-cast'),
    castTv: document.getElementById('ln-cast-tv'),
    castStop: document.getElementById('ln-cast-stop'),
    goLive: document.getElementById('ln-go-live'),
    goLiveMain: document.getElementById('ln-go-live-main'),
    stopLive: document.getElementById('ln-stop-live'),
  };

  // full-res screen cast of main mosaic (glyph-cast.html)
  // Smart TV: hub fan-out → glyph-cast.html?hub=ws://LAN:9876 (cross-device)
  const WIRE = window.GY_GLYPH_CAST_WIRE || window.GY_CAST;
  let castSession = null;
  let castTimer = 0;
  let tvBanner = null;

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

    // vision-take side channels: SAM segment_top + pose
    if (capMeta && (capMeta.segment_top || capMeta.pose || capMeta.pose_hands > 0)) {
      const vrow = document.createElement('div');
      vrow.className = 'ln-vision-row';
      if (capMeta.segment_top) {
        const sam = document.createElement('span');
        sam.className = 'ln-sam-badge';
        sam.title = 'SAM segment_top';
        sam.textContent = 'SAM · ' + String(capMeta.segment_top).slice(0, 14);
        vrow.appendChild(sam);
      }
      if (capMeta.pose || capMeta.pose_hands > 0) {
        const pose = document.createElement('span');
        pose.className = 'ln-pose-badge' + (capMeta.pose_hands > 0 ? ' is-hot' : '');
        pose.title = 'MediaPipe pose · hands=' + (capMeta.pose_hands || 0);
        pose.textContent =
          'pose' +
          (capMeta.pose_hands > 0 ? ' ✋' + capMeta.pose_hands : '') +
          (capMeta.pose_joints > 0 ? ' ·j' + capMeta.pose_joints : '');
        vrow.appendChild(pose);
      }
      if (capMeta.theme && capMeta.theme !== 'unsorted') {
        const th = document.createElement('span');
        th.className = 'ln-theme-mini';
        th.textContent = capMeta.theme;
        vrow.appendChild(th);
      }
      tile.appendChild(vrow);
      tile.classList.add('has-vision');
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
      // real YT sampler: video element + recent sample or hubFrame
      if (
        t.live ||
        (t.video && t.hubFrame) ||
        (t.lastSample && Date.now() - t.lastSample < 4000)
      ) {
        live++;
      }
    });
    if (LIVE && LIVE.countLive) {
      const n = LIVE.countLive(tileMap);
      if (n > live) live = n;
    }
    const mode = ws && ws.readyState === 1 ? 'hub' : 'poster';
    const vs = TH && TH.visionStats ? TH.visionStats() : { sam: 0, pose: 0 };
    el.meta.innerHTML =
      mode +
      ' · <em>' +
      allSources().length +
      '</em> catalog · main <em>' +
      mainIds.length +
      '</em> · live <em style="color:#6ee7b7">' +
      live +
      '</em> · SAM <em>' +
      vs.sam +
      '</em> · pose <em>' +
      vs.pose +
      '</em> · stress ' +
      stressN +
      (ws && ws.readyState === 1 ? ' · <em>' + nick + '</em>' : '');
    renderVisionBar();
  }

  /** Chip strip: feeds with segment_top / pose from vision-take. */
  function renderVisionBar() {
    if (!el.visionBar || !TH) return;
    el.visionBar.innerHTML = '';
    const chips = [];
    TH.meta.forEach((m, id) => {
      if (!m.segment_top && !m.pose && !(m.pose_hands > 0)) return;
      const src = NS.findById(id);
      chips.push({
        id: id,
        label: (src && src.label) || id,
        sam: m.segment_top || '',
        hands: m.pose_hands || 0,
        theme: m.theme || '',
      });
    });
    if (!chips.length) {
      el.visionBar.hidden = true;
      return;
    }
    el.visionBar.hidden = false;
    const title = document.createElement('span');
    title.className = 'ln-vision-bar-label';
    title.textContent = 'vision';
    el.visionBar.appendChild(title);
    chips.slice(0, 16).forEach((c) => {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'ln-vision-chip';
      chip.title = c.label + (c.sam ? ' · SAM ' + c.sam : '') + (c.hands ? ' · hands ' + c.hands : '');
      chip.innerHTML =
        escapeHtml(c.label.slice(0, 10)) +
        (c.sam ? ' <em>' + escapeHtml(String(c.sam).slice(0, 10)) + '</em>' : '') +
        (c.hands > 0 ? ' <span class="ln-hand">✋' + c.hands + '</span>' : '');
      chip.addEventListener('click', () => {
        toggleMain(c.id, true);
        if (el.sort) el.sort.value = 'theme';
        clusterNow();
      });
      el.visionBar.appendChild(chip);
    });
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

  function setConnectUI(state) {
    // state: idle | connecting | open | closed | error
    if (!el.connect) return;
    el.connect.disabled = false;
    el.connect.style.pointerEvents = 'auto';
    el.connect.style.cursor = 'pointer';
    el.connect.removeAttribute('aria-disabled');
    el.connect.classList.remove('is-connected', 'is-busy');
    if (state === 'connecting') {
      el.connect.textContent = 'Connecting…';
      el.connect.classList.add('is-busy');
      el.connect.title = 'Connecting to hub WebSocket — click again to retry';
    } else if (state === 'open') {
      // keep an ACTION label so it never looks like a dead blue status chip
      el.connect.textContent = 'Reconnect hub';
      el.connect.classList.add('is-connected');
      el.connect.title = 'Hub connected — click to reconnect';
    } else if (state === 'error') {
      el.connect.textContent = 'Connect hub · retry';
      el.connect.title = 'Hub connect failed — click to retry';
    } else {
      el.connect.textContent = 'Connect hub';
      el.connect.title = 'Connect WebSocket to mesh hub (room=news)';
    }
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
    if (el.meta) el.meta.textContent = 'connecting hub…';
    setConnectUI('connecting');
    if (ws) try { ws.close(); } catch (_) {}
    try {
      ws = new WebSocket(url);
    } catch (e) {
      setConnectUI('error');
      if (el.meta) el.meta.textContent = 'hub connect error · ' + (e && e.message ? e.message : e);
      setMeta();
      return;
    }
    const sock = ws;
    const failT = setTimeout(function () {
      if (sock && sock.readyState !== WebSocket.OPEN) {
        try { sock.close(); } catch (_) {}
        setConnectUI('error');
        if (el.meta) el.meta.textContent = 'hub connect timeout — click Connect hub again';
      }
    }, 8000);
    ws.onopen = () => {
      clearTimeout(failT);
      try {
        ws.send(JSON.stringify({ type: 'join', nick: nick, role: 'news-wall', room: 'news' }));
      } catch (_) {}
      setConnectUI('open');
      if (el.meta) el.meta.textContent = 'hub connected · ' + nick;
      setMeta();
    };
    ws.onerror = () => {
      clearTimeout(failT);
      setConnectUI('error');
      if (el.meta) el.meta.textContent = 'hub error — click Connect hub to retry';
    };
    ws.onclose = () => {
      clearTimeout(failT);
      setConnectUI('closed');
      setMeta();
    };
    ws.onmessage = (ev) => {
      let msg;
      try {
        msg = JSON.parse(ev.data);
      } catch (_) {
        return;
      }
      let visionHit = false;
      if (TH) {
        const m = TH.applyMesh(msg);
        // vision/orch mesh: type news-caption with theme
        if (msg.type === 'news-caption' && msg.theme && msg.feed) {
          TH.setCaption(String(msg.feed).toLowerCase().replace(/\s+/g, ''), msg.text || msg.theme, {
            theme: msg.theme,
          });
        }
        if (msg.type === 'vision-take') {
          visionHit = !!m;
          // refresh tiles that gained SAM/pose badges
          if (m && mainIds.length) {
            if (el.sort && el.sort.value === 'theme') {
              mainIds = TH.sortIds(mainIds);
            }
            renderMain();
            // light section refresh for badges
            document.querySelectorAll('.ln-tile[data-id]').forEach((node) => {
              const id = node.dataset.id;
              const meta = TH.getMeta(id);
              if (!meta || (!meta.segment_top && !meta.pose)) return;
              // if tile lacks vision row, full rebuild of sections is heavy — patch badge
              if (!node.querySelector('.ln-vision-row')) {
                const vrow = document.createElement('div');
                vrow.className = 'ln-vision-row';
                if (meta.segment_top) {
                  const sam = document.createElement('span');
                  sam.className = 'ln-sam-badge';
                  sam.textContent = 'SAM · ' + String(meta.segment_top).slice(0, 14);
                  vrow.appendChild(sam);
                }
                if (meta.pose || meta.pose_hands > 0) {
                  const pose = document.createElement('span');
                  pose.className = 'ln-pose-badge' + (meta.pose_hands > 0 ? ' is-hot' : '');
                  pose.textContent =
                    'pose' + (meta.pose_hands > 0 ? ' ✋' + meta.pose_hands : '');
                  vrow.appendChild(pose);
                }
                node.appendChild(vrow);
                node.classList.add('has-vision');
              }
            });
          }
        }
        if (
          (m || visionHit || (msg.theme && msg.type === 'news-caption')) &&
          el.sort &&
          el.sort.value === 'theme'
        ) {
          if (mainIds.length && msg.type !== 'vision-take') {
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

  function demoVision() {
    if (!TH || !TH.demoVision) return;
    TH.demoVision();
    if (el.sort) el.sort.value = 'theme';
    fillFromSort();
    buildSections();
    renderMain();
    setMeta();
  }

  function clusterNow() {
    if (!TH) return;
    if (!mainIds.length) fillFromSort();
    mainIds = TH.sortIds(mainIds);
    renderMain();
    buildSections();
  }

  // ── Cast screen (main mosaic → glyph-cast full-res) ─────
  function hubUrlForCast() {
    let url = (el.hub && el.hub.value.trim()) || '';
    if (!url) {
      const host = location.hostname || '127.0.0.1';
      // Prefer page host so smart-TV same-LAN open works when served from 0.0.0.0 bind
      url =
        'ws://' +
        (host === 'localhost' ? '127.0.0.1' : host) +
        (location.port && location.port !== '80' && location.port !== '443'
          ? ':' + location.port
          : ':9876');
    }
    return url.replace(/\/$/, '');
  }

  /** Public http URL for glyph-cast on LAN (smart TV browser). */
  function tvPlayerURL() {
    const hub = hubUrlForCast();
    const u = new URL('glyph-cast.html', location.href);
    u.searchParams.set('source', 'livenews');
    u.searchParams.set('cast', '1');
    u.searchParams.set('tv', '1');
    u.searchParams.set('fs', '1');
    u.searchParams.set('hub', hub);
    u.searchParams.set('room', 'news');
    u.searchParams.set('layout', 'grid');
    u.searchParams.set('n', '25');
    // force non-localhost host when possible for TV
    if (u.hostname === 'localhost' || u.hostname === '127.0.0.1') {
      // leave as-is if only loopback; banner will note LAN
    }
    return u.href;
  }

  function showTvBanner(url) {
    if (!tvBanner) {
      tvBanner = document.createElement('div');
      tvBanner.id = 'ln-tv-banner';
      tvBanner.setAttribute('role', 'status');
      tvBanner.style.cssText =
        'position:fixed;bottom:12px;left:12px;right:12px;z-index:50;' +
        'background:rgba(10,12,18,0.94);border:1px solid #34d399;border-radius:12px;' +
        'padding:10px 14px;font:12px/1.45 ui-monospace,monospace;color:#d1fae5;' +
        'box-shadow:0 8px 28px rgba(0,0,0,0.45);max-width:720px;margin:0 auto;';
      document.body.appendChild(tvBanner);
    }
    tvBanner.innerHTML =
      '<strong style="color:#6ee7b7">Smart TV / cast device</strong> · same Wi‑Fi · open this URL on the TV browser' +
      '<div style="margin-top:6px;word-break:break-all;color:#a7f3d0">' +
      url +
      '</div>' +
      '<div style="margin-top:8px;display:flex;gap:8px;flex-wrap:wrap">' +
      '<button type="button" id="ln-tv-copy" style="appearance:none;border:0;background:#6ee7b7;color:#042;border-radius:8px;padding:6px 10px;font-weight:700;cursor:pointer">Copy URL</button>' +
      '<button type="button" id="ln-tv-open" style="appearance:none;border:1px solid #333;background:#15151c;color:#ddd;border-radius:8px;padding:6px 10px;cursor:pointer">Open cast player</button>' +
      '<button type="button" id="ln-tv-hide" style="appearance:none;border:1px solid #333;background:transparent;color:#9ca3af;border-radius:8px;padding:6px 10px;cursor:pointer">Hide</button>' +
      '</div>' +
      '<div style="margin-top:6px;color:#6b7280;font-size:11px">Hub fans mosaic as vburst-frame · TV joins room=news · Cast TV = Presentation API when available</div>';
    const copy = document.getElementById('ln-tv-copy');
    const openB = document.getElementById('ln-tv-open');
    const hide = document.getElementById('ln-tv-hide');
    if (copy)
      copy.onclick = function () {
        if (navigator.clipboard) navigator.clipboard.writeText(url);
        copy.textContent = 'Copied';
      };
    if (openB)
      openB.onclick = function () {
        window.open(url, 'gy-glyph-cast-tv');
      };
    if (hide)
      hide.onclick = function () {
        tvBanner.remove();
        tvBanner = null;
      };
  }

  function lumToGlyphArr(lum) {
    if (!lum || !lum.length) return null;
    const out = new Array(lum.length);
    for (let i = 0; i < lum.length; i++) {
      let v = lum[i];
      if (v <= 1) v = Math.round(v * 255);
      out[i] = Math.max(0, Math.min(255, v | 0));
    }
    return out;
  }

  /** Cross-device: publish main mosaic to hub so smart TV glyph-cast can ingest. */
  function publishCastToHub() {
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const peers = buildCastPeers();
    const t = Date.now();
    peers.forEach(function (p, i) {
      const glyph = lumToGlyphArr(p.lum);
      if (!glyph) return;
      try {
        ws.send(
          JSON.stringify({
            type: 'vburst-frame',
            from: 'livenews',
            feed: p.id,
            label: p.nick,
            glyph: glyph,
            glyphN: p.glyphN || 25,
            t: t,
            seq: t + i,
            via: 'livenews-cast',
            room: 'news',
          })
        );
      } catch (_) {}
    });
  }

  function buildCastPeers() {
    const ids = mainIds.length ? mainIds.slice() : [];
    if (!ids.length) {
      // fallback: first 8 live/catalog for demo punch
      allSources()
        .slice(0, 8)
        .forEach((s) => ids.push(s.id));
    }
    const peers = [];
    ids.forEach((id) => {
      const rec = tileMap.get(id);
      const src = NS.findById(id);
      if (!rec || !src) return;
      let lum = rec.lum;
      if (!lum || !lum.length) {
        lum = G.simLum(performance.now(), rec.seed, rec.live);
      }
      let nick = src.label;
      if (TH) {
        const m = TH.getMeta(id);
        if (m && m.segment_top) nick += ' · ' + m.segment_top;
        else if (m && m.theme && m.theme !== 'unsorted') nick += ' · ' + m.theme;
      }
      peers.push({
        id: id,
        nick: nick.slice(0, 22),
        mode: rec.live || rec.hubFrame ? 'cast' : 'sim',
        lum: lum,
        glyphN: 25,
      });
    });
    return peers;
  }

  function castPayload() {
    return {
      peers: buildCastPeers(),
      glyphN: 25,
      style: 'matrix',
      layout: mainIds.length === 1 ? 'focus' : mainIds.length === 2 ? 'dual' : 'grid',
      ledPx: 'auto',
      room: 'news',
      source: 'livenews',
      hub: hubUrlForCast(),
    };
  }

  function ensureCastSession() {
    if (!WIRE || !WIRE.createSession) return null;
    if (castSession) return castSession;
    castSession = WIRE.createSession({
      source: 'livenews',
      glyphN: 25,
      layout: 'grid',
      room: 'news',
      hub: hubUrlForCast(),
    });
    castSession.on(function (ev) {
      if (ev === 'open' || ev === 'ready') {
        if (el.cast) el.cast.textContent = 'Casting…';
        if (el.castStop) el.castStop.hidden = false;
        startCastLoop();
      }
      if (ev === 'close') {
        stopCastLoop();
        if (el.cast) el.cast.textContent = 'Cast screen';
        if (el.castStop) el.castStop.hidden = true;
      }
    });
    return castSession;
  }

  function startCastLoop() {
    stopCastLoop();
    castTimer = window.setInterval(function () {
      if (castSession && castSession.isOn()) {
        castSession.push(castPayload, false);
      }
      // always fan mosaic to hub while casting (smart TV path)
      publishCastToHub();
    }, 120);
    if (castSession) castSession.push(castPayload, true);
    publishCastToHub();
  }

  function stopCastLoop() {
    if (castTimer) {
      clearInterval(castTimer);
      castTimer = 0;
    }
  }

  function startCast(presentation) {
    if (!mainIds.length) fillFromSort();
    // ensure hub so TV clients get frames
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      connect();
    }
    const tvUrl = tvPlayerURL();
    if (presentation) {
      showTvBanner(tvUrl);
      try {
        if (navigator.clipboard) navigator.clipboard.writeText(tvUrl);
      } catch (_) {}
    }

    const s = ensureCastSession();
    if (!s) {
      window.open(tvUrl, 'gy-glyph-cast');
      startCastLoop();
      if (el.cast) el.cast.textContent = 'Casting…';
      if (el.castStop) el.castStop.hidden = false;
      return;
    }
    s.setDefaults({ hub: hubUrlForCast(), room: 'news', source: 'livenews' });
    if (presentation) {
      // Presentation API (Chromecast / second display) + open LAN player
      s.open({ presentation: true, fullscreen: true });
      window.open(tvUrl, 'gy-glyph-cast-tv');
    } else {
      s.open({ fullscreen: false });
    }
    startCastLoop();
    if (el.cast) el.cast.textContent = 'Casting…';
    if (el.castTv && presentation) el.castTv.textContent = 'TV live…';
    if (el.castStop) el.castStop.hidden = false;
    setMeta();
  }

  function stopCast() {
    stopCastLoop();
    if (castSession) castSession.close();
    if (el.cast) el.cast.textContent = 'Cast screen';
    if (el.castTv) el.castTv.textContent = 'Cast TV';
    if (el.castStop) el.castStop.hidden = true;
    if (tvBanner) {
      tvBanner.remove();
      tvBanner = null;
    }
  }

  // Single click path only (HTML onclick + addEventListener double-fire
  // used to set goLiveBusy then immediately hit "already resolving").
  function bindClick(node, fn) {
    if (!node || typeof fn !== 'function') return;
    node.onclick = function (e) {
      if (e) {
        e.preventDefault();
        e.stopPropagation();
      }
      try {
        fn(e);
      } catch (err) {
        console.error('[livenews] click', err);
        if (el.meta) el.meta.textContent = 'error · ' + (err && err.message ? err.message : err);
      }
      return false;
    };
  }

  bindClick(el.connect, function () {
    connect();
  });
  bindClick(el.expand, function () {
    el.regions.querySelectorAll('details').forEach((d) => (d.open = true));
  });
  bindClick(el.collapse, function () {
    el.regions.querySelectorAll('details').forEach((d) => (d.open = false));
  });
  bindClick(el.refreshAll, refreshAll);
  bindClick(el.stress, stressJoin);
  el.sort &&
    el.sort.addEventListener('change', () => {
      buildSections();
      if (el.sort.value === 'theme') fillFromSort();
    });
  bindClick(el.themeDemo, demoThemes);
  bindClick(el.visionDemo, demoVision);
  bindClick(el.cluster, clusterNow);
  bindClick(el.cast, function () {
    startCast(false);
  });
  bindClick(el.castTv, function () {
    startCast(true);
  });
  bindClick(el.castStop, stopCast);

  let goLiveBusy = false;
  let goLiveTimer = 0;
  let goLiveGen = 0; // cancel token for in-flight resolve

  function setGoLiveUI(text, busy) {
    [el.goLive, el.goLiveMain].forEach(function (b) {
      if (!b) return;
      const liveOk = text && /^Live\b/i.test(String(text));
      b.classList.toggle('is-on', !!liveOk);
      b.classList.toggle('is-busy', !!busy);
      // Always keep an actionable verb — never a passive blue status chip
      if (busy) {
        b.textContent = 'Cancel resolve';
        b.title = 'Resolve in progress — click to cancel and retry';
      } else {
        b.textContent = text || 'Resolve live';
        b.title =
          liveOk
            ? 'Live streams active — click to re-resolve'
            : 'Resolve YouTube /live → real glyph frames (blank + yt-dlp)';
      }
      b.disabled = false;
      b.removeAttribute('disabled');
      b.setAttribute('aria-busy', busy ? 'true' : 'false');
      b.style.opacity = '1';
      b.style.pointerEvents = 'auto';
      b.style.cursor = 'pointer';
      b.style.userSelect = 'none';
    });
    if (el.stopLive) el.stopLive.hidden = !(busy || (text && /^Live\b/i.test(String(text))));
  }

  async function goLiveMain() {
    // While busy the button reads "Cancel resolve" — click cancels (never a dead blue chip)
    if (goLiveBusy) {
      goLiveGen++;
      goLiveBusy = false;
      if (goLiveTimer) {
        clearTimeout(goLiveTimer);
        goLiveTimer = 0;
      }
      if (LIVE) LIVE.stopAllLive(tileMap);
      setGoLiveUI('Resolve live', false);
      if (el.meta) el.meta.textContent = 'resolve cancelled — click Resolve live to start again';
      if (el.mainSub) el.mainSub.textContent = 'cancelled · click Resolve live';
      return;
    }
    if (!LIVE) {
      if (el.meta) el.meta.innerHTML = '<em class="err">news-live.js missing</em> — hard refresh';
      alert('news-live.js failed to load — hard refresh (Cmd+Shift+R)');
      return;
    }
    const myGen = ++goLiveGen;
    goLiveBusy = true;
    if (goLiveTimer) clearTimeout(goLiveTimer);
    // hard unlock after 75s so UI never sticks on blue "Resolving…"
    goLiveTimer = setTimeout(function () {
      if (goLiveGen !== myGen) return;
      goLiveBusy = false;
      setGoLiveUI('Retry resolve', false);
      if (el.meta) el.meta.textContent = 'resolve timed out — click Resolve live again';
      if (el.mainSub) el.mainSub.textContent = 'timeout · retry';
    }, 75000);

    try {
      if (!mainIds.length) fillFromSort();
      if (!mainIds.length) {
        mainIds = ['cnn', 'bbc', 'sky', 'aje', 'cnbc', 'cspan'].filter((id) => NS.findById(id));
        mainIds.forEach((id) => {
          const s = NS.findById(id);
          if (s) ensureRec(s);
        });
        renderMain();
      }
      const maxN = LIVE.MAX_LIVE || 4;
      if (mainIds.length > maxN) {
        mainIds = mainIds.slice(0, maxN);
        renderMain();
      }
      setGoLiveUI('Resolving…', true);
      if (!ws || ws.readyState !== WebSocket.OPEN) connect();
      if (el.meta) el.meta.textContent = 'checking blank/hub…';
      if (el.mainSub) el.mainSub.textContent = 'resolving…';

      // Fail-fast: blank concurrent cap / down → clear error, not infinite hang
      if (LIVE.preflight) {
        const pre = await LIVE.preflight();
        if (goLiveGen !== myGen) return;
        if (!pre.ok) {
          setGoLiveUI('Retry resolve', false);
          if (el.meta) el.meta.textContent = pre.error || 'blank/hub unavailable';
          if (el.mainSub) el.mainSub.textContent = 'sim · ' + (pre.error || 'preflight fail');
          return;
        }
        if (el.meta) el.meta.textContent = 'resolving YouTube live via ' + (pre.via || 'blank/hub') + '…';
      }

      const res = await LIVE.startMainLive(tileMap, mainIds, {
        max: maxN,
        isCancelled: function () {
          return goLiveGen !== myGen;
        },
        onStatus: function (s) {
          if (goLiveGen !== myGen) return;
          if (el.meta) el.meta.textContent = s;
          if (el.mainSub) el.mainSub.textContent = s;
        },
        onSample: function (rec) {
          if (goLiveGen !== myGen) return;
          if (rec.canvas && rec.lum && G.paintGlyphCanvas) {
            G.paintGlyphCanvas(rec.canvas, rec.lum, {
              cell: rec.canvas.width <= 100 ? 3 : 5,
              tint: 0,
            });
            const parent = rec.canvas.parentElement;
            if (parent) parent.classList.add('is-live');
          }
          setMeta();
        },
      });
      if (goLiveGen !== myGen) return;
      setGoLiveUI(res.ok ? 'Live · ' + res.ok : 'Retry resolve', false);
      if (el.meta) {
        el.meta.innerHTML =
          '<em>live</em> ' +
          res.ok +
          ' · fail ' +
          res.fail +
          (res.errors.length ? ' · ' + escapeHtml(res.errors[0].slice(0, 80)) : '') +
          (res.ok ? ' · real YT frames' : ' · check blank :5173');
      }
      if (el.mainSub) {
        el.mainSub.textContent = res.ok ? 'LIVE youtube · ' + res.ok + ' streams' : 'sim · resolve failed';
      }
      if (res.ok > 0) {
        startCastLoop();
        if (el.castStop) el.castStop.hidden = false;
      }
      setMeta();
    } catch (e) {
      if (goLiveGen !== myGen) return;
      console.error('[livenews] goLiveMain', e);
      if (el.meta) el.meta.textContent = 'resolve error · ' + (e && e.message ? e.message : e);
      setGoLiveUI('Retry resolve', false);
    } finally {
      if (goLiveGen === myGen) {
        goLiveBusy = false;
        if (goLiveTimer) {
          clearTimeout(goLiveTimer);
          goLiveTimer = 0;
        }
      }
    }
  }

  function stopLiveMain() {
    goLiveGen++;
    goLiveBusy = false;
    if (goLiveTimer) {
      clearTimeout(goLiveTimer);
      goLiveTimer = 0;
    }
    if (LIVE) LIVE.stopAllLive(tileMap);
    setGoLiveUI('Resolve live', false);
    if (el.stopLive) el.stopLive.hidden = true;
    if (el.mainSub) el.mainSub.textContent = 'sim · pin · theme clump';
    if (el.meta) el.meta.textContent = 'stopped live · back to sim';
    setMeta();
  }

  // global hooks for HTML onclick + console debug
  window.LN_connect = function () {
    connect();
  };
  window.LN_goLive = goLiveMain;
  window.LN_stopLive = stopLiveMain;

  bindClick(el.goLive, goLiveMain);
  bindClick(el.goLiveMain, goLiveMain);
  bindClick(el.stopLive, stopLiveMain);

  // auto only when live=1 (not default — avoids stuck "Resolving…" on load)
  try {
    const q = new URLSearchParams(location.search);
    if (q.get('live') === '1' || q.get('golive') === '1') {
      setTimeout(function () {
        if (!ws || ws.readyState !== WebSocket.OPEN) connect();
        setTimeout(function () {
          goLiveMain();
        }, 800);
      }, 400);
    }
  } catch (_) {}
  bindClick(el.shuffle, shuffleMain);
  bindClick(el.cycle, cycleMain);
  bindClick(el.clear, function () {
    mainIds = [];
    renderMain();
    buildSections();
  });
  bindClick(el.fill, fillFromSort);

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
