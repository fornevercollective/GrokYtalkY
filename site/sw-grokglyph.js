/* GrokGlyph service worker — cache shell for offline PWA */
const CACHE = "grokglyph-v3";
const PRECACHE = [
  "./grokglyph.html",
  "./grokglyph.js",
  "./grokglyph.css",
  "./styles.css",
  "./manifest-grokglyph.webmanifest",
  "./icons/grokglyph.svg",
  "./icons/grokglyph-192.png",
  "./icons/grokglyph-512.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE).then((cache) => cache.addAll(PRECACHE)).then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  // same-origin only
  if (url.origin !== self.location.origin) return;

  event.respondWith(
    caches.match(req).then((hit) => {
      const fetchPromise = fetch(req)
        .then((res) => {
          if (res && res.ok && res.type === "basic") {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(req, copy));
          }
          return res;
        })
        .catch(() => hit);
      return hit || fetchPromise;
    })
  );
});
