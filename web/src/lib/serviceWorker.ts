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

  try {
    const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });

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