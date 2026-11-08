import "fake-indexeddb/auto";
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { syncProjectToCache, syncSessionToCache, loadFromCache, loadCacheSummary } from '../offlineSync';
import { OfflineDB } from '../offlineDb';

describe('offlineSync', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });
  afterEach(() => {
    vi.restoreAllMocks();
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

  it('syncSessionToCache stores the transcript once, not again inside metadata', async () => {
    const messages = [{ role: 'user', content: 'hi' }];
    await syncSessionToCache({ id: 's1', title: 'T', messages } as any);
    const cached = await db.getSession('s1');
    expect(cached?.messages).toEqual(messages);
    expect(cached?.metadata).toEqual({ id: 's1', title: 'T' });
    expect(JSON.stringify(cached?.metadata)).not.toContain('"messages"');
  });

  it('syncSessionToCache keeps only the newest 200 messages', async () => {
    const messages = Array.from({ length: 250 }, (_, i) => ({ role: 'user', content: `m${i}` }));
    await syncSessionToCache({ id: 's2', messages } as any);
    const cached = await db.getSession('s2');
    expect(cached?.messages).toHaveLength(200);
    expect(cached?.messages[0].content).toBe('m50');
  });

  it('syncSessionToCache leaves projectId undefined when the caller has none', async () => {
    await syncSessionToCache({ id: 's3', messages: [] } as any);
    const cached = await db.getSession('s3');
    expect(cached?.projectId).toBeUndefined();
  });

  it('loadCacheSummary reflects what is stored, not an in-memory value', async () => {
    expect(await loadCacheSummary()).toEqual({ hasData: false, newestLastSynced: null });
    await syncProjectToCache({ id: '/p', name: 'p', data: {} } as any);
    const summary = await loadCacheSummary();
    expect(summary?.hasData).toBe(true);
    expect(summary?.newestLastSynced).not.toBeNull();
  });

  it('loadCacheSummary logs and returns undefined when the read fails', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(OfflineDB.prototype, 'summarize').mockRejectedValueOnce(new Error('boom'));
    expect(await loadCacheSummary()).toBeUndefined();
    expect(warn).toHaveBeenCalledWith('[offlineSync] loadCacheSummary failed:', expect.any(Error));
  });
});
