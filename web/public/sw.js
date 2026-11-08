/* eslint-disable no-restricted-globals */
// Fetch routing is scope-relative: every path check compares against SCOPE_PATH,
// the directory this script is served from ('/' for a plain mount, or a mount
// prefix such as '/rc/'). The precache list (PRECACHE_URLS) and the activate-time
// prune still use root-absolute manifest paths ('/index.html', '/assets/*'), so
// under a prefixed mount they do not match the URLs the page requests. See
// web/src/lib/serviceWorker.ts.
const CACHE_VERSION = 'ocode-shell-__BUILD_HASH__';
const PRECACHE_URLS = self.__PRECACHE_MANIFEST__ || ['/index.html'];

// The directory this script is served from: '/' for a plain mount, or the mount
// prefix (e.g. '/rc/'). Cache-first matching is relative to it.
const SCOPE_PATH = new URL('./', self.location.href).pathname;

// Cache-first is limited to the SPA's own hashed build output under <scope>assets/.
// Published artifacts (<scope>p/...) are served through artifact auth and change
// whenever they are republished, so they must never be served from this cache.
function isCacheFirstAsset(pathname) {
  if (!pathname.startsWith(SCOPE_PATH)) return false;
  const rel = pathname.slice(SCOPE_PATH.length);
  if (rel === 'p' || rel.startsWith('p/')) return false;
  return rel.startsWith('assets/');
}

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
  if (url.pathname.startsWith(SCOPE_PATH + 'api/')) return;
  // Default for cross-origin or unknown: network pass-through.
  if (url.origin !== self.location.origin) return;

  // Never serve the service worker script itself from cache.
  if (url.pathname === SCOPE_PATH + 'sw.js') return;

  // Cache-first for the SPA's hashed build output only. Anything else under the
  // scope (artifact pages under p/, other static-looking paths) passes through to
  // the network below.
  if (isCacheFirstAsset(url.pathname)) {
    event.respondWith(cacheFirst(request));
    return;
  }

  // Stale-while-revalidate for the app shell (index.html / root).
  if (url.pathname === SCOPE_PATH || url.pathname === SCOPE_PATH + 'index.html') {
    event.respondWith(staleWhileRevalidate(request));
    return;
  }

  // Everything else: network-only (pass through).
});
