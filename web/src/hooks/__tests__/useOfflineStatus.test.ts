import "fake-indexeddb/auto";
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useOfflineStatus } from '../useOfflineStatus';
import { OfflineDB } from '../../lib/offlineDb';

describe('useOfflineStatus', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns isOffline false when navigator.onLine is true', () => {
    vi.stubGlobal('navigator', { onLine: true, storage: { estimate: vi.fn().mockResolvedValue({ usage: 0, quota: 0 }) } });
    const { result } = renderHook(() => useOfflineStatus());
    expect(result.current.isOffline).toBe(false);
  });

  it('returns isOffline true when navigator.onLine is false', () => {
    vi.stubGlobal('navigator', { onLine: false, storage: { estimate: vi.fn().mockResolvedValue({ usage: 0, quota: 0 }) } });
    const { result } = renderHook(() => useOfflineStatus());
    expect(result.current.isOffline).toBe(true);
  });

  it('reports cached data and the newest sync time from IndexedDB while offline', async () => {
    vi.stubGlobal('navigator', { onLine: false });
    const syncedAt = Date.now() - 5 * 60_000;
    await new OfflineDB().clearAll();
    await new OfflineDB().putProject({ id: '/p', name: 'p', lastSynced: syncedAt, data: {} });

    const { result } = renderHook(() => useOfflineStatus());

    await waitFor(() => expect(result.current.cachedDataAvailable).toBe(true));
    await waitFor(() => expect(result.current.lastSynced).toBe(syncedAt));
  });

  it('drops cached data and the sync time once the cache is purged', async () => {
    vi.stubGlobal('navigator', { onLine: false });
    await new OfflineDB().clearAll();
    await new OfflineDB().putProject({ id: '/p', name: 'p', lastSynced: Date.now(), data: {} });

    const { result } = renderHook(() => useOfflineStatus());
    await waitFor(() => expect(result.current.cachedDataAvailable).toBe(true));

    await new OfflineDB().clearAll();
    // Re-enter the offline state so the hook re-reads the cache.
    act(() => {
      window.dispatchEvent(new Event('online'));
    });
    act(() => {
      window.dispatchEvent(new Event('offline'));
    });

    await waitFor(() => expect(result.current.cachedDataAvailable).toBe(false));
    expect(result.current.lastSynced).toBeNull();
  });
});
