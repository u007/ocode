import "fake-indexeddb/auto";
import { describe, it, expect, beforeEach } from 'vitest';
import { syncProjectToCache, loadFromCache } from '../offlineSync';
import { OfflineDB } from '../offlineDb';

describe('offlineSync', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });

  it('syncProjectToCache writes to IndexedDB', async () => {
    await syncProjectToCache({ id: '/p', name: 'p', data: {} } as any);
    const result = await db.getProject('/p');
    expect(result?.name).toBe('p');
  });

  it('loadFromCache returns undefined when offline DB is empty', async () => {
    const result = await loadFromCache('project', '/nonexistent');
    expect(result).toBeUndefined();
  });
});
