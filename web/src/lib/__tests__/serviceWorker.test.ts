import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { registerServiceWorker } from '../serviceWorker';

function setRuntimeBase(href: string) {
  const base = document.createElement('base');
  base.href = href;
  document.head.appendChild(base);
}

describe('registerServiceWorker', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', '/');
    vi.stubGlobal('navigator', { serviceWorker: { register: vi.fn().mockResolvedValue({}) } });
  });

  afterEach(() => {
    document.querySelectorAll('base').forEach((el) => el.remove());
    window.history.replaceState({}, '', '/');
    vi.unstubAllGlobals();
  });

  it('registers sw.js at root scope', async () => {
    await registerServiceWorker();
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/' });
  });

  it('resolves sw.js from the <base href> prefix on a deep link', async () => {
    setRuntimeBase('/rc/');
    window.history.replaceState({}, '', '/rc/session/abc');
    await registerServiceWorker();
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/rc/sw.js', { scope: '/rc/' });
  });

  it('resolves sw.js at root on a deep link when no <base> is present', async () => {
    window.history.replaceState({}, '', '/session/abc');
    await registerServiceWorker();
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/' });
  });

  it('does nothing when serviceWorker is not supported', async () => {
    vi.stubGlobal('navigator', {});
    await expect(registerServiceWorker()).resolves.toBeUndefined();
  });
});
