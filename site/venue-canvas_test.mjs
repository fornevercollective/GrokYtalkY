/**
 * node site/venue-canvas_test.mjs
 */
import { createRequire } from "module";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
// order: sphere then venue
const SPHERE = require(join(__dirname, "sphere-seating.js"));
globalThis.GY_SPHERE = SPHERE;
const V = require(join(__dirname, "venue-canvas.js"));

function assert(c, m) {
  if (!c) throw new Error(m || "assert");
}

const ven = V.buildVenue();
assert(ven.targets.length > 20000, "targets " + ven.targets.length);
assert(ven.seats > 10000, "seats");

const stage = V.bulkActivate({ zone: "stage" });
assert(stage.count > 10, "stage bulk");

const sec = V.bulkActivate({ section: "200", limit: 2000 });
assert(sec.count > 100, "section 200");

const chunks = V.listChunks("200");
assert(chunks.length > 0, "chunks");
const ch = V.bulkActivate({ chunk: chunks[0] });
assert(ch.count > 0, "chunk activate " + chunks[0]);

const led = V.resolvePos({ px: 8000, py: 4000 });
assert(led && led.px === 8000 || led.addressable, "led pos " + JSON.stringify(led));
assert(led.res === 16000, "res");

const free = V.nearestPixel(100, 100, 50);
// may be free LED or nearest
assert(free && free.px != null, "nearest");

const park = V.bulkActivate({ zone: "parking", limit: 500 });
assert(park.count > 20, "parking");

const open = V.bulkActivate({ zone: "opening" });
assert(open.count > 5, "openings");

const seatPos = V.resolvePos({ seat: "200-R5-C12" });
assert(seatPos && seatPos.target && seatPos.target.indexOf("seat:") === 0, "seat target");

console.log("ok ·", V.summary());
console.log("  meta", JSON.stringify(V.meta().zones));
console.log("  bulk stage", stage.count, "sec200", sec.count, "chunk0", ch.count);
console.log("  led", led.target, "px", led.px, led.py);
