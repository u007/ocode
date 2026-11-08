/**
 * Registration helper for the app-shell service worker (web/public/sw.js).
 */

export type ServiceWorkerUpdateCallback = (registration: ServiceWorkerRegistration) => void;

export interface RegisterOptions {
  onUpdate?: ServiceWorkerUpdateCallback;
}

export async function registerServiceWorker(
  options: RegisterOptions = {},
): Promise<ServiceWorkerRegistration | undefined> {
  if (!('serviceWorker' in navigator)) return undefined;

  // The service worker itself is root-oriented (absolute '/sw.js', '/assets/*',
  // '/api/*', scope '/'). Under a non-root base (e.g. a tailscale path prefix)
  // we scope registration to the base path as a partial mitigation, but the SW
  // still only controls root-origin paths — full relative support is not
  // implemented. See web/public/sw.js.
  const rawBase = (import.meta.env.BASE_URL as string) || '/';
  const normalizedBase = rawBase.endsWith('/') ? rawBase : rawBase + '/';
  const isRoot = normalizedBase === '/';
  const swUrl = isRoot ? '/sw.js' : `${normalizedBase}sw.js`;
  const scope = isRoot ? '/' : normalizedBase;

  try {
    const registration = await navigator.serviceWorker.register(swUrl, { scope });

    const handleUpdate = () => {
      const installing = registration.installing;
      if (!installing) return;
      installing.addEventListener('statechange', () => {
        if (installing.state === 'installed' && navigator.serviceWorker.controller) {
          try {
            options.onUpdate?.(registration);
          } catch {
            /* ignore */
          }
          try {
            window.dispatchEvent(new CustomEvent('sw:update-available', { detail: registration }));
          } catch {
            /* ignore */
          }
        }
      });
    };

    if (typeof registration.addEventListener === 'function') {
      registration.addEventListener('updatefound', handleUpdate);
    }

    return registration;
  } catch (err) {
    console.error('[serviceWorker] registration failed:', err);
    return undefined;
  }
}
