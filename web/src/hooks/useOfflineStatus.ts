import { useState, useEffect, useCallback } from 'react';
import { loadCacheSummary } from '../lib/offlineSync';

export interface OfflineStatus {
  isOffline: boolean;
  lastSynced: number | null;
  cachedDataAvailable: boolean;
}

export function useOfflineStatus(): OfflineStatus {
  const [isOffline, setIsOffline] = useState(() => {
    if (typeof navigator !== 'undefined') return !navigator.onLine;
    return false;
  });
  const [lastSynced, setLastSynced] = useState<number | null>(null);
  const [cachedDataAvailable, setCachedDataAvailable] = useState(false);

  useEffect(() => {
    const handleOnline = () => setIsOffline(false);
    const handleOffline = () => setIsOffline(true);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  // Read the cache's state when the app goes offline. A failed read yields
  // undefined, which is logged in offlineSync and shown as no cached data.
  const checkCache = useCallback(async () => {
    const summary = await loadCacheSummary();
    setCachedDataAvailable(summary?.hasData ?? false);
    setLastSynced(summary?.newestLastSynced ?? null);
  }, []);

  useEffect(() => {
    if (isOffline) {
      void checkCache();
    }
  }, [isOffline, checkCache]);

  return { isOffline, lastSynced, cachedDataAvailable };
}
