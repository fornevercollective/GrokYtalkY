/**
 * GrokGlyph mesh region presets (lat/lon for nearest + facing triangulation).
 * room id → hub query room=… (same host by default).
 */
(function (global) {
  "use strict";

  /** @typedef {{ id: string, name: string, lat: number, lon: number, hub?: string }} MeshPreset */

  /** @type {Record<string, MeshPreset[]>} */
  const PRESETS = {
    world: [
      { id: "global", name: "Global", lat: 0, lon: 0 },
      { id: "eq-pacific", name: "Pacific", lat: 0, lon: -160 },
      { id: "eq-atlantic", name: "Atlantic", lat: 0, lon: -30 },
      { id: "arctic", name: "Arctic", lat: 75, lon: 0 },
      { id: "antarctic", name: "Antarctic", lat: -75, lon: 0 },
    ],
    regions: [
      { id: "na", name: "North America", lat: 45, lon: -100 },
      { id: "sa", name: "South America", lat: -15, lon: -60 },
      { id: "eu", name: "Europe", lat: 50, lon: 10 },
      { id: "af", name: "Africa", lat: 5, lon: 20 },
      { id: "mena", name: "MENA", lat: 28, lon: 35 },
      { id: "sa-asia", name: "South Asia", lat: 22, lon: 78 },
      { id: "e-asia", name: "East Asia", lat: 35, lon: 120 },
      { id: "se-asia", name: "SE Asia", lat: 10, lon: 110 },
      { id: "oceania", name: "Oceania", lat: -25, lon: 135 },
    ],
    states: [
      { id: "us-ca", name: "California", lat: 36.78, lon: -119.42 },
      { id: "us-ny", name: "New York", lat: 42.95, lon: -75.5 },
      { id: "us-tx", name: "Texas", lat: 31.0, lon: -100.0 },
      { id: "us-fl", name: "Florida", lat: 27.8, lon: -81.7 },
      { id: "us-wa", name: "Washington", lat: 47.4, lon: -120.5 },
      { id: "us-il", name: "Illinois", lat: 40.0, lon: -89.0 },
      { id: "us-co", name: "Colorado", lat: 39.0, lon: -105.5 },
      { id: "us-ma", name: "Massachusetts", lat: 42.3, lon: -71.8 },
      { id: "ca-on", name: "Ontario", lat: 50.0, lon: -85.0 },
      { id: "ca-bc", name: "British Columbia", lat: 53.7, lon: -127.6 },
      { id: "mx-cmx", name: "CDMX area", lat: 19.43, lon: -99.13 },
      { id: "gb-eng", name: "England", lat: 52.3, lon: -1.2 },
      { id: "de-by", name: "Bavaria", lat: 48.8, lon: 11.5 },
      { id: "jp-13", name: "Tokyo-to", lat: 35.68, lon: 139.69 },
      { id: "au-nsw", name: "New South Wales", lat: -32.0, lon: 147.0 },
    ],
    towns: [
      { id: "us-ca-sf", name: "San Francisco", lat: 37.7749, lon: -122.4194 },
      { id: "us-ca-la", name: "Los Angeles", lat: 34.0522, lon: -118.2437 },
      { id: "us-ny-nyc", name: "New York City", lat: 40.7128, lon: -74.006 },
      { id: "us-il-chi", name: "Chicago", lat: 41.8781, lon: -87.6298 },
      { id: "us-tx-aus", name: "Austin", lat: 30.2672, lon: -97.7431 },
      { id: "us-wa-sea", name: "Seattle", lat: 47.6062, lon: -122.3321 },
      { id: "us-co-den", name: "Denver", lat: 39.7392, lon: -104.9903 },
      { id: "gb-lon", name: "London", lat: 51.5074, lon: -0.1278 },
      { id: "fr-par", name: "Paris", lat: 48.8566, lon: 2.3522 },
      { id: "de-ber", name: "Berlin", lat: 52.52, lon: 13.405 },
      { id: "jp-tyo", name: "Tokyo", lat: 35.6762, lon: 139.6503 },
      { id: "kr-sel", name: "Seoul", lat: 37.5665, lon: 126.978 },
      { id: "sg-sin", name: "Singapore", lat: 1.3521, lon: 103.8198 },
      { id: "au-syd", name: "Sydney", lat: -33.8688, lon: 151.2093 },
      { id: "br-sao", name: "São Paulo", lat: -23.5505, lon: -46.6333 },
      { id: "in-bom", name: "Mumbai", lat: 19.076, lon: 72.8777 },
      { id: "za-jnb", name: "Johannesburg", lat: -26.2041, lon: 28.0473 },
      { id: "ae-dxb", name: "Dubai", lat: 25.2048, lon: 55.2708 },
    ],
  };

  function allPresets() {
    /** @type {MeshPreset[]} */
    const out = [];
    Object.keys(PRESETS).forEach((k) => {
      PRESETS[k].forEach((p) => out.push(p));
    });
    return out;
  }

  function findPreset(id) {
    if (!id) return null;
    const all = allPresets();
    return all.find((p) => p.id === id) || null;
  }

  /** Haversine distance in km */
  function haversineKm(lat1, lon1, lat2, lon2) {
    const R = 6371;
    const toR = Math.PI / 180;
    const dLat = (lat2 - lat1) * toR;
    const dLon = (lon2 - lon1) * toR;
    const a =
      Math.sin(dLat / 2) ** 2 +
      Math.cos(lat1 * toR) * Math.cos(lat2 * toR) * Math.sin(dLon / 2) ** 2;
    return 2 * R * Math.asin(Math.min(1, Math.sqrt(a)));
  }

  /** Initial bearing degrees 0–360 (from → to) */
  function bearingDeg(lat1, lon1, lat2, lon2) {
    const toR = Math.PI / 180;
    const φ1 = lat1 * toR;
    const φ2 = lat2 * toR;
    const Δλ = (lon2 - lon1) * toR;
    const y = Math.sin(Δλ) * Math.cos(φ2);
    const x = Math.cos(φ1) * Math.sin(φ2) - Math.sin(φ1) * Math.cos(φ2) * Math.cos(Δλ);
    return ((Math.atan2(y, x) * 180) / Math.PI + 360) % 360;
  }

  function angleDiffDeg(a, b) {
    let d = Math.abs(a - b) % 360;
    if (d > 180) d = 360 - d;
    return d;
  }

  /**
   * Nearest presets by GPS; optional heading picks "facing" among top-K nearest.
   * @param {number} lat
   * @param {number} lon
   * @param {number|null} heading
   * @param {MeshPreset[]} [list]
   */
  function triangulate(lat, lon, heading, list) {
    const pool = list && list.length ? list : allPresets().filter((p) => p.id !== "global");
    const ranked = pool
      .map((p) => ({
        preset: p,
        km: haversineKm(lat, lon, p.lat, p.lon),
        bearing: bearingDeg(lat, lon, p.lat, p.lon),
      }))
      .sort((a, b) => a.km - b.km);

    const nearest = ranked[0] || null;
    let facing = nearest;
    if (heading != null && !Number.isNaN(heading) && ranked.length > 1) {
      const top = ranked.slice(0, Math.min(6, ranked.length));
      facing = top.reduce((best, cur) => {
        const db = angleDiffDeg(heading, cur.bearing);
        const dbBest = angleDiffDeg(heading, best.bearing);
        // prefer facing alignment; break ties with distance
        if (db < dbBest - 2) return cur;
        if (Math.abs(db - dbBest) <= 2 && cur.km < best.km) return cur;
        return best;
      }, top[0]);
    }
    return { nearest, facing, ranked };
  }

  global.GrokGlyphPresets = {
    PRESETS,
    allPresets,
    findPreset,
    haversineKm,
    bearingDeg,
    angleDiffDeg,
    triangulate,
  };
})(typeof window !== "undefined" ? window : globalThis);
