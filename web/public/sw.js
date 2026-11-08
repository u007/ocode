/* eslint-disable no-restricted-globals */
// NOTE: This service worker assumes a ROOT deployment (origin '/'). Paths below
// are absolute ('/sw.js', '/assets/*', '/api/*', '/'). Under a prefixed
// deployment (Vite base !== '/'), registration is scoped to the base path but
// the SW still only controls root-origin paths; full relative support is not
// implemented. See web/src/lib/serviceWorker.ts.
const CACHE_VERSION = 'ocode-shell-__BUILD_HASH__';
const PRECACHE_URLS = self.__PRECACHE_MANIFEST__ || ['/index.html'];

// Strategies
async function cacheFirst(request) {
  const cache = await caches.open(CACHE_VERSION);
  const cached = await cache.match(request);
  if (cached) return cached;
  const response = await fetch(request);
  if (response && response.ok) cache.put(request, response.clone());
  return response;
}

async function staleWhileRevalidate(request) {
  const cache = await caches.open(CACHE_VERSION);
  const cached = await cache.match(request);
  const networkPromise = fetch(request)
    .then((response) => {
      if (response && response.ok) cache.put(request, response.clone());
      return response;
    })
    .catch(() => cached);
  return cached || networkPromise;
}

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_VERSION).then((cache) => cache.addAll(PRECACHE_URLS)).catch(() => undefined),
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      // Drop caches from previous builds (cache name includes the build hash).
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => k !== CACHE_VERSION).map((k) => caches.delete(k)));

      // Prune stale entries within the current cache that are no longer precached.
      try {
        const cache = await caches.open(CACHE_VERSION);
        const requests = await cache.keys();
        const allowed = new Set(PRECACHE_URLS);
        await Promise.all(
          requests.map((req) => {
            let pathname = '/';
            try {
              pathname = new URL(req.url).pathname;
            } catch {
              return undefined;
            }
            // Always keep the app shell; prune only stale hashed assets.
            if (pathname === '/' || pathname === '/index.html') return undefined;
            if (pathname.startsWith('/assets/') && !allowed.has(pathname)) {
              return cache.delete(req);
            }
            return undefined;
          }),
        );
      } catch {
        /* ignore */
      }

      await self.clients.claim();
    })(),
  );
});

self.addEventListener('fetch', (event) => {
  const request = event.request;
  if (request.method !== 'GET') return;

  let url;
  try {
    url = new URL(request.url);
  } catch {
    return;
  }

  // Network-only for everything else, including /api/*.
  if (url.pathname.startsWith('/api/')) return;
  // Default for cross-origin or unknown: network pass-through.
  if (url.origin !== self.location.origin) return;

  // Never serve the service worker script itself from cache.
  if (url.pathname === '/sw.js') return;

  // Cache-first for hashed assets.
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(cacheFirst(request));
    return;
  }

  // Other static assets (images, fonts): cache-first.
  // /sw.js is excluded above so it never matches this branch.
  if (/\.(png|jpg|jpeg|gif|svg|webp|ico|woff2?|ttf|eot|css|js)$/i.test(url.pathname)) {
    event.respondWith(cacheFirst(request));
    return;
  }

  // Stale-while-revalidate for the app shell (index.html / root).
  if (url.pathname === '/' || url.pathname === '/index.html') {
    event.respondWith(staleWhileRevalidate(request));
    return;
  }

  // Everything else: network-only (pass through).
});
