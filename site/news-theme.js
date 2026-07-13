/**
 * Theme cluster / sort for live news feeds from AI captions + transcripts + vision.
 * Hub types: news-caption | news-transcript | caption | vision-take
 * vision-take carries theme, caption, segment_top (SAM), pose_hands / pose_joints.
 */
(function (global) {
  'use strict';

  const NS = global.GY_NEWS;
  if (!NS) return;

  /**
   * @typedef {{
   *   caption: string, transcript: string, theme: string, score: number, t: number,
   *   segment_top?: string, segments?: number,
   *   pose?: boolean, pose_hands?: number, pose_joints?: number,
   *   mute_hint?: string, style?: string, provider?: string
   * }} FeedMeta
   */
  /** @type {Map<string, FeedMeta>} */
  const meta = new Map();

  function emptyMeta() {
    return {
      caption: '',
      transcript: '',
      theme: 'unsorted',
      score: 0,
      t: 0,
      segment_top: '',
      segments: 0,
      pose: false,
      pose_hands: 0,
      pose_joints: 0,
      mute_hint: '',
      style: '',
      provider: '',
    };
  }

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

  /** Map SAM segment_top labels → theme boosts / keywords. */
  function themeFromSegment(label) {
    const lab = String(label || '').toLowerCase();
    if (!lab) return null;
    if (/person|face|speaker|anchor|host|crowd/.test(lab)) return { theme: 'breaking', score: 1.5 };
    if (/car|road|traffic|highway|bridge|building|tower|monument|skyline|cam/.test(lab))
      return { theme: 'earthcam', score: 1.4 };
    if (/cloud|rain|storm|weather|sky|snow/.test(lab)) return { theme: 'weather', score: 1.4 };
    if (/chart|ticker|board|screen|number/.test(lab)) return { theme: 'markets', score: 1.2 };
    if (/flag|capitol|podium|desk/.test(lab)) return { theme: 'politics', score: 1.2 };
    if (/weapon|smoke|fire|soldier|tank/.test(lab)) return { theme: 'conflict', score: 1.5 };
    if (/text|chyron|banner|logo/.test(lab)) return { theme: 'unsorted', score: 0.3 };
    return { theme: 'unsorted', score: 0.2 };
  }

  function resolveFeedId(raw) {
    if (!raw) return '';
    let feedId = String(raw).trim();
    if (NS.findById(feedId)) return feedId;
    const low = feedId.toLowerCase().replace(/\s+/g, '');
    if (NS.findById(low)) return low;
    const hit = NS.MAJOR_NEWS.find(
      (s) =>
        s.id === low ||
        s.label.toLowerCase().includes(low.slice(0, 6)) ||
        low.includes(s.id) ||
        s.label.toLowerCase().replace(/\s+/g, '').includes(low.slice(0, 8))
    );
    return hit ? hit.id : feedId;
  }

  function setCaption(feedId, text, opts) {
    opts = opts || {};
    const prev = meta.get(feedId) || emptyMeta();
    if (opts.transcript) prev.transcript = String(text || '');
    else prev.caption = String(text || '');
    if (opts.theme) prev.theme = String(opts.theme);
    const combined = (prev.caption + ' ' + prev.transcript + ' ' + (prev.segment_top || '')).trim();
    const src = NS.findById(feedId);
    if (!opts.theme) {
      const det = detectTheme(combined, src && src.tags);
      prev.theme = det.theme;
      prev.score = Math.max(prev.score || 0, det.score);
    } else {
      prev.score = Math.max(prev.score || 0, 1);
    }
    prev.t = Date.now();
    meta.set(feedId, prev);
    return prev;
  }

  function getMeta(feedId) {
    return meta.get(feedId) || null;
  }

  /**
   * Apply type:vision-take — theme, caption, segment_top, pose_*.
   * @returns {FeedMeta|null}
   */
  function applyVisionTake(msg) {
    if (!msg || msg.type !== 'vision-take') return null;
    const feedId = resolveFeedId(msg.feed || msg.label || msg.src || msg.from);
    if (!feedId) return null;
    const prev = meta.get(feedId) || emptyMeta();
    if (msg.caption) prev.caption = String(msg.caption);
    if (msg.theme) {
      prev.theme = String(msg.theme);
      prev.score = Math.max(prev.score || 0, 2);
    }
    if (msg.segment_top) {
      prev.segment_top = String(msg.segment_top);
      const boost = themeFromSegment(msg.segment_top);
      if (boost && (!msg.theme || prev.theme === 'unsorted')) {
        if (boost.theme !== 'unsorted' || !msg.theme) {
          prev.theme = boost.theme;
          prev.score = Math.max(prev.score || 0, boost.score);
        }
      } else if (boost) {
        prev.score = Math.max(prev.score || 0, boost.score);
      }
    }
    if (typeof msg.segments === 'number') prev.segments = msg.segments;
    if (msg.pose === true || msg.pose_hands != null || msg.pose_joints != null) {
      prev.pose = true;
      if (msg.pose_hands != null) prev.pose_hands = Number(msg.pose_hands) || 0;
      if (msg.pose_joints != null) prev.pose_joints = Number(msg.pose_joints) || 0;
      // talking pose → slight breaking/talking boost when unsorted
      if (prev.pose_hands > 0 && prev.theme === 'unsorted') {
        prev.theme = 'breaking';
        prev.score = Math.max(prev.score || 0, 0.8);
      }
    }
    if (msg.mute_hint) prev.mute_hint = String(msg.mute_hint);
    if (msg.style) prev.style = String(msg.style);
    if (msg.provider) prev.provider = String(msg.provider);
    // re-detect when we only have caption text
    if (!msg.theme && prev.caption) {
      const src = NS.findById(feedId);
      const det = detectTheme(
        prev.caption + ' ' + (prev.segment_top || ''),
        src && src.tags
      );
      if (det.score > prev.score) {
        prev.theme = det.theme;
        prev.score = det.score;
      }
    }
    prev.t = Date.now();
    meta.set(feedId, prev);
    return prev;
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
      if ((!m || !m.caption) && src) {
        const tags = src.tags || [];
        if (src.kind === 'earthcam' || tags.indexOf('earthcam') >= 0 || tags.indexOf('monument') >= 0 || tags.indexOf('highway') >= 0)
          theme = 'earthcam';
        else if (src.kind === 'weather' || tags.indexOf('weather') >= 0) theme = 'weather';
        else if (src.kind === 'public' || tags.indexOf('public') >= 0) theme = 'local';
        else if (tags.indexOf('markets') >= 0) theme = 'markets';
        else if (tags.indexOf('politics') >= 0) theme = 'politics';
        else if (tags.indexOf('breaking') >= 0) theme = 'breaking';
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

  /** Apply mesh caption + vision-take messages. */
  function applyMesh(msg) {
    if (!msg || !msg.type) return null;
    const typ = msg.type;
    if (typ === 'vision-take') {
      return applyVisionTake(msg);
    }
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
    if (!id) return null;
    const feedId = resolveFeedId(id);
    if (!text && !msg.theme) return null;
    const m = setCaption(feedId, text || msg.theme || '', {
      transcript: typ === 'news-transcript',
      theme: msg.theme || undefined,
    });
    // optional vision fields on caption messages
    if (msg.segment_top) m.segment_top = String(msg.segment_top);
    if (msg.pose_hands != null) {
      m.pose = true;
      m.pose_hands = Number(msg.pose_hands) || 0;
    }
    meta.set(feedId, m);
    return m;
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

  /** Demo: synthetic vision-take (SAM segment_top + pose) for browser UI. */
  function demoVision() {
    const samples = [
      {
        type: 'vision-take',
        feed: 'cnn',
        theme: 'breaking',
        caption: 'Anchor at desk',
        segment_top: 'person',
        segments: 2,
        pose: true,
        pose_hands: 1,
        pose_joints: 8,
        style: 'scan',
        provider: 'offline',
      },
      {
        type: 'vision-take',
        feed: 'bbg',
        theme: 'markets',
        caption: 'Trading floor boards',
        segment_top: 'screen',
        segments: 3,
        pose: true,
        pose_hands: 0,
        pose_joints: 0,
        style: 'hex',
      },
      {
        type: 'vision-take',
        feed: 'weatherch',
        theme: 'weather',
        caption: 'Radar map',
        segment_top: 'sky',
        segments: 1,
        pose: false,
        style: 'dither',
      },
      {
        type: 'vision-take',
        feed: 'ec-timessq',
        theme: 'earthcam',
        caption: 'Times Square cam',
        segment_top: 'crowd',
        segments: 4,
        pose: true,
        pose_hands: 2,
        pose_joints: 12,
        style: 'neon',
      },
      {
        type: 'vision-take',
        feed: 'aje',
        theme: 'conflict',
        caption: 'Field report',
        segment_top: 'person',
        segments: 2,
        pose: true,
        pose_hands: 1,
        pose_joints: 6,
      },
    ];
    samples.forEach((m) => applyVisionTake(m));
  }

  /** Count feeds that have vision side channels. */
  function visionStats() {
    let sam = 0;
    let pose = 0;
    meta.forEach((m) => {
      if (m.segment_top) sam++;
      if (m.pose || m.pose_hands > 0) pose++;
    });
    return { sam: sam, pose: pose, total: meta.size };
  }

  global.GY_NEWS_THEME = {
    setCaption: setCaption,
    getMeta: getMeta,
    detectTheme: detectTheme,
    cluster: cluster,
    sortIds: sortIds,
    applyMesh: applyMesh,
    applyVisionTake: applyVisionTake,
    themeFromSegment: themeFromSegment,
    resolveFeedId: resolveFeedId,
    demoCaptions: demoCaptions,
    demoVision: demoVision,
    visionStats: visionStats,
    meta: meta,
  };
})(typeof window !== 'undefined' ? window : globalThis);
