import "fake-indexeddb/auto";
import { describe, it, expect, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useOfflineStatus } from '../useOfflineStatus';

describe('useOfflineStatus', () => {
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
});
