/**
 * Node smoke: Sphere Vegas Bloch³ seating map.
 *   node site/sphere-seating_test.mjs
 */
import { createRequire } from "module";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const GY = require(join(__dirname, "sphere-seating.js"));

function assert(cond, msg) {
  if (!cond) throw new Error(msg || "assert");
}

const seats = GY.generateSphereSeats();
assert(seats.length > 1000, "expected thousands of seats, got " + seats.length);
assert(seats.length < 30000, "too many seats " + seats.length);

const s0 = seats[0];
assert(s0.id && s0.section != null, "seat fields");
assert(Number.isFinite(s0.blochX) && Number.isFinite(s0.theta), "bloch");
assert(s0.px >= 0 && s0.px < GY.SPHERE_VEGAS.res, "px");
assert(s0.py >= 0 && s0.py < GY.SPHERE_VEGAS.res, "py");

// Bloch unit vector-ish
const n = Math.hypot(s0.blochX, s0.blochY, s0.blochZ);
assert(Math.abs(n - 1) < 1e-6, "bloch not unit: " + n);

const found = GY.findSeat(s0.id);
assert(found && found.idx === s0.idx, "find by id");

const byIdx = GY.findSeat(String(s0.idx));
assert(byIdx && byIdx.idx === s0.idx, "find by idx");

const pos = GY.seatToMeshPos(found);
assert(pos.map === "sphere-vegas-bloch3" && pos.bloch.length === 3, "mesh pos");

const m = GY.meta();
assert(m.seats === seats.length, "meta seats");
assert(m.res === 16000, "16K");

// parse formats
assert(GY.parseSeatQuery("200-R5-C12").section === "200", "parse id");
assert(GY.parseSeatQuery("42").idx === 42, "parse idx");

console.log("ok ·", GY.summary());
console.log("  sample", s0.id, "px", s0.px, s0.py, "θ", ((s0.theta * 180) / Math.PI).toFixed(1) + "°");
