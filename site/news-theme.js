/**
 * Theme cluster / sort for live news feeds from AI captions + transcripts.
 * When hub pipes type:news-caption | news-transcript | caption, re-cluster
 * the main Glyph column (and optional section order).
 */
(function (global) {
  'use strict';

  const NS = global.GY_NEWS;
  if (!NS) return;

  /** @type {Map<string, {caption: string, transcript: string, theme: string, score: number, t: number}>} */
  const meta = new Map();

  function scoreTheme(text, theme) {
    if (!text || !theme.keywords.length) return 0;
    const low = text.toLowerCase();
    let s = 0;
    theme.keywords.forEach((k) => {
      if (low.includes(k)) s += 1 + k.length * 0.05;
    });
    return s;
  }

  function detectTheme(text, tags) {
    let best = 'unsorted';
    let bestScore = 0;
    // tag boost
    const tagStr = (tags || []).join(' ');
    const blob = (text || '') + ' ' + tagStr;
    NS.THEMES.forEach((th) => {
      if (th.id === 'unsorted') return;
      let sc = scoreTheme(blob, th);
      if (tags && tags.indexOf(th.id) >= 0) sc += 2;
      if (sc > bestScore) {
        bestScore = sc;
        best = th.id;
      }
    });
    return { theme: bestScore > 0 ? best : 'unsorted', score: bestScore };
  }

  function setCaption(feedId, text, opts) {
    opts = opts || {};
    const prev = meta.get(feedId) || {
      caption: '',
      transcript: '',
      theme: 'unsorted',
      score: 0,
      t: 0,
    };
    if (opts.transcript) prev.transcript = String(text || '');
    else prev.caption = String(text || '');
    const combined = (prev.caption + ' ' + prev.transcript).trim();
    const src = NS.findById(feedId);
    const det = detectTheme(combined, src && src.tags);
    prev.theme = det.theme;
    prev.score = det.score;
    prev.t = Date.now();
    meta.set(feedId, prev);
    return prev;
  }

  function getMeta(feedId) {
    return meta.get(feedId) || null;
  }

  /**
   * Cluster feed ids into theme buckets (stable within bucket by score, then label).
   * @param {string[]} ids
   * @returns {{ theme: string, label: string, ids: string[] }[]}
   */
  function cluster(ids) {
    const buckets = {};
    NS.THEMES.forEach((th) => {
      buckets[th.id] = [];
    });
    ids.forEach((id) => {
      const m = meta.get(id);
      const src = NS.findById(id);
      let theme = (m && m.theme) || 'unsorted';
      // seed from static tags when no caption yet
      if ((!m || !m.caption) && src && src.tags) {
        if (src.kind === 'weather' || src.tags.indexOf('weather') >= 0) theme = 'weather';
        else if (src.kind === 'public' || src.tags.indexOf('public') >= 0) theme = 'local';
        else if (src.tags.indexOf('markets') >= 0) theme = 'markets';
        else if (src.tags.indexOf('politics') >= 0) theme = 'politics';
        else if (src.tags.indexOf('breaking') >= 0) theme = 'breaking';
      }
      if (!buckets[theme]) buckets[theme] = [];
      buckets[theme].push({
        id: id,
        score: (m && m.score) || 0,
        label: (src && src.label) || id,
      });
    });
    Object.keys(buckets).forEach((k) => {
      buckets[k].sort((a, b) => b.score - a.score || a.label.localeCompare(b.label));
    });
    return NS.THEMES.map((th) => ({
      theme: th.id,
      label: th.label,
      ids: (buckets[th.id] || []).map((x) => x.id),
    })).filter((b) => b.ids.length > 0);
  }

  /** Flat sort: by theme order then score. */
  function sortIds(ids) {
    const flat = [];
    cluster(ids).forEach((b) => {
      b.ids.forEach((id) => flat.push(id));
    });
    // append any missing
    ids.forEach((id) => {
      if (flat.indexOf(id) < 0) flat.push(id);
    });
    return flat;
  }

  /** Apply mesh caption messages. */
  function applyMesh(msg) {
    if (!msg || !msg.type) return null;
    const typ = msg.type;
    if (
      typ !== 'news-caption' &&
      typ !== 'news-transcript' &&
      typ !== 'space-caption' &&
      typ !== 'caption'
    ) {
      return null;
    }
    const text = msg.text || msg.caption || msg.transcript || '';
    const id =
      msg.feed ||
      msg.src ||
      msg.label ||
      msg.channel ||
      (msg.from && String(msg.from).replace(/^news-/, '')) ||
      '';
    if (!id || !text) return null;
    // fuzzy match catalog id
    let feedId = id;
    if (!NS.findById(feedId)) {
      const low = String(id).toLowerCase();
      const hit = NS.MAJOR_NEWS.find(
        (s) => s.id === low || s.label.toLowerCase().includes(low.slice(0, 6))
      );
      if (hit) feedId = hit.id;
    }
    return setCaption(feedId, text, { transcript: typ === 'news-transcript' });
  }

  /** Demo: inject synthetic captions so clustering is visible without AI. */
  function demoCaptions() {
    const samples = [
      ['cnn', 'Breaking: developing story in Washington'],
      ['bbg', 'Markets open higher as inflation data cools'],
      ['weatherch', 'Storm forecast: severe weather watches issued'],
      ['aje', 'Conflict update from the region as talks continue'],
      ['nasa', 'Live coverage of the rocket launch window'],
      ['cspan', 'House hearing continues on the public record'],
      ['bbc', 'UK politics: MPs debate the bill tonight'],
      ['nhc', 'Hurricane center: tropical storm track shifts west'],
    ];
    samples.forEach(([id, text]) => setCaption(id, text, {}));
  }

  global.GY_NEWS_THEME = {
    setCaption: setCaption,
    getMeta: getMeta,
    detectTheme: detectTheme,
    cluster: cluster,
    sortIds: sortIds,
    applyMesh: applyMesh,
    demoCaptions: demoCaptions,
    meta: meta,
  };
})(typeof window !== 'undefined' ? window : globalThis);
