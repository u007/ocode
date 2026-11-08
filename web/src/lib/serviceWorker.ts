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

  // Resolve the script and scope from the document's <base href>, not from
  // import.meta.env.BASE_URL. vite.config.ts sets base: "./", so BASE_URL is
  // "./" in every production build and says nothing about the mount point.
  // index.html injects <base href> at runtime (the mount prefix, e.g. "/rc/", or
  // "/" for a plain mount), so resolving against it is correct on deep links
  // such as /session/abc too. With no <base> element the page is treated as
  // mounted at the origin root.
  const baseHref = document.querySelector('base[href]')
    ? document.baseURI
    : new URL('/', window.location.href).href;
  const swUrl = new URL('sw.js', baseHref).pathname;
  const scope = new URL('./', baseHref).pathname;

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
