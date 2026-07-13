/**
 * Smoke: glyph-cast-wire buildPayload shape.
 * Run: node site/glyph-cast-wire_test.mjs
 */
import { readFileSync } from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import vm from 'vm';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const sandbox = {
  window: {},
  globalThis: {},
  URL,
  performance: { now: () => 0 },
  btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
  location: { href: 'https://example.com/GrokYtalkY/livenews.html' },
};
sandbox.window = sandbox;
sandbox.globalThis = sandbox;
sandbox.window.location = sandbox.location;

vm.runInNewContext(readFileSync(path.join(__dirname, 'glyph-cast-wire.js'), 'utf8'), sandbox);
const W = sandbox.GY_GLYPH_CAST_WIRE;
if (!W) {
  console.error('missing wire');
  process.exit(1);
}

const msg = W.buildPayload({
  glyphN: 25,
  source: 'livenews',
  peers: [
    { id: 'cnn', nick: 'CNN · person', mode: 'cast', lum: new Float32Array(625).fill(0.4) },
  ],
});
if (msg.type !== 'glyph-cast' || !msg.peers[0].lumB64) {
  console.error('bad payload', msg);
  process.exit(1);
}
const s = W.createSession({ source: 'livenews' });
if (!s.buildPayload || !s.playerURL) {
  console.error('session');
  process.exit(1);
}
const url = s.playerURL({ source: 'livenews', cast: '1', hub: 'ws://127.0.0.1:9876' });
if (!String(url).includes('glyph-cast') || !String(url).includes('livenews')) {
  console.error('url', url);
  process.exit(1);
}
console.log('ok glyph-cast-wire · peers=1 · b64=' + msg.peers[0].lumB64.length);
