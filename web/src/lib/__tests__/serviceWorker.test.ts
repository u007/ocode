import { describe, it, expect, vi, beforeEach } from 'vitest';
import { registerServiceWorker } from '../serviceWorker';

describe('registerServiceWorker', () => {
  beforeEach(() => {
    vi.stubGlobal('navigator', { serviceWorker: { register: vi.fn().mockResolvedValue({}) } });
  });

  it('registers sw.js at root scope', async () => {
    await registerServiceWorker();
    expect(navigator.serviceWorker.register).toHaveBeenCalledWith('/sw.js', { scope: '/' });
  });

  it('does nothing when serviceWorker is not supported', async () => {
    vi.stubGlobal('navigator', {});
    await expect(registerServiceWorker()).resolves.toBeUndefined();
  });
});
