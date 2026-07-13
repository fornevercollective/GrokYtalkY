/**
 * Node smoke tests for news-theme vision-take consumption.
 * Run: node site/news-theme_test.mjs
 */
import { readFileSync } from 'fs';
import { pathToFileURL } from 'url';
import { createRequire } from 'module';
import vm from 'vm';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// minimal catalog stub
const MAJOR_NEWS = [
  { id: 'cnn', label: 'CNN', tags: ['breaking'], kind: 'news', region: 'us' },
  { id: 'bbg', label: 'Bloomberg', tags: ['markets'], kind: 'news', region: 'us' },
  { id: 'weatherch', label: 'Weather Channel', tags: ['weather'], kind: 'weather', region: 'weather' },
  { id: 'ec-timessq', label: 'Times Square', tags: ['earthcam'], kind: 'earthcam', region: 'earthcam' },
  { id: 'aje', label: 'Al Jazeera', tags: ['conflict'], kind: 'news', region: 'me' },
];

const THEMES = [
  { id: 'breaking', label: 'Breaking', keywords: ['breaking', 'developing'] },
  { id: 'markets', label: 'Markets', keywords: ['markets', 'stocks', 'inflation'] },
  { id: 'weather', label: 'Weather', keywords: ['storm', 'weather', 'hurricane'] },
  { id: 'conflict', label: 'Conflict', keywords: ['conflict', 'war'] },
  { id: 'earthcam', label: 'EarthCam', keywords: ['cam', 'scenic'] },
  { id: 'politics', label: 'Politics', keywords: ['politics', 'mps'] },
  { id: 'unsorted', label: 'Other', keywords: [] },
];

const sandbox = {
  window: {},
  globalThis: {},
  console,
};
sandbox.window = sandbox;
sandbox.globalThis = sandbox;
sandbox.GY_NEWS = {
  MAJOR_NEWS,
  THEMES,
  findById(id) {
    return MAJOR_NEWS.find((s) => s.id === id) || null;
  },
};

const src = readFileSync(path.join(__dirname, 'news-theme.js'), 'utf8');
vm.runInNewContext(src, sandbox);

const TH = sandbox.GY_NEWS_THEME;
if (!TH) {
  console.error('GY_NEWS_THEME missing');
  process.exit(1);
}

function assert(cond, msg) {
  if (!cond) {
    console.error('FAIL', msg);
    process.exit(1);
  }
}

// vision-take person → breaking + segment
const m = TH.applyVisionTake({
  type: 'vision-take',
  feed: 'CNN News',
  theme: 'breaking',
  caption: 'Anchor desk',
  segment_top: 'person',
  segments: 2,
  pose: true,
  pose_hands: 1,
  pose_joints: 8,
});
assert(m && m.segment_top === 'person', 'segment_top');
assert(m.pose_hands === 1, 'pose_hands');
assert(m.theme === 'breaking', 'theme');
assert(TH.resolveFeedId('CNN') === 'cnn' || TH.getMeta('cnn'), 'resolve');

const m2 = TH.applyMesh({
  type: 'vision-take',
  feed: 'bbg',
  segment_top: 'screen',
  caption: 'Boards',
});
assert(m2 && m2.segment_top === 'screen', 'mesh vision');
assert(m2.theme === 'markets' || m2.theme === 'unsorted', 'segment theme boost ' + m2.theme);

TH.demoVision();
const vs = TH.visionStats();
assert(vs.sam >= 3, 'demo sam ' + vs.sam);
assert(vs.pose >= 2, 'demo pose ' + vs.pose);

const sorted = TH.sortIds(['cnn', 'bbg', 'weatherch', 'aje']);
assert(sorted.length === 4, 'sort');

console.log('ok news-theme vision-take · sam=' + vs.sam + ' pose=' + vs.pose);
