import { useState, useEffect, useCallback } from 'react';
import { loadFromCache, getLastSynced } from '../lib/offlineSync';

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
  const [lastSynced, setLastSynced] = useState<number | null>(() => getLastSynced());
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

  // Check if cached data is available when offline.
  const checkCache = useCallback(async () => {
    try {
      const cached = await loadFromCache('project', '__check__');
      // Even if no specific project, try to check if DB has entries.
      setCachedDataAvailable(cached !== undefined);
      setLastSynced(getLastSynced());
    } catch {
      setCachedDataAvailable(false);
    }
  }, []);

  useEffect(() => {
    if (isOffline) {
      void checkCache();
    }
  }, [isOffline, checkCache]);

  return { isOffline, lastSynced, cachedDataAvailable };
}
