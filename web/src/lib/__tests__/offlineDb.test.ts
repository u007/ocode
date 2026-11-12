import "fake-indexeddb/auto";
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { OfflineDB, type CachedProject } from '../offlineDb';

const DAY = 24 * 60 * 60 * 1000;

function project(id: string, lastSynced: number, padChars = 0): CachedProject {
  return { id, name: id, lastSynced, data: { pad: 'x'.repeat(padChars) } };
}

function bytesOf(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).length;
}

function runningTotal(db: OfflineDB): number {
  return (db as unknown as { total: number }).total;
}

describe('OfflineDB', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('stores and retrieves a project', async () => {
    await db.putProject({ id: '/path/to/proj', name: 'proj', lastSynced: Date.now(), data: {} });
    const result = await db.getProject('/path/to/proj');
    expect(result?.name).toBe('proj');
  });

  it('returns undefined for missing project', async () => {
    const result = await db.getProject('/nonexistent');
    expect(result).toBeUndefined();
  });

  it('clearAll removes all data', async () => {
    await db.putProject({ id: '/p', name: 'p', lastSynced: Date.now(), data: {} });
    await db.clearAll();
    const result = await db.getProject('/p');
    expect(result).toBeUndefined();
  });

  it('clearAll purges sessions and terminals too', async () => {
    await db.putProject({ id: '/p', name: 'p', lastSynced: Date.now(), data: {} });
    await db.putSession({ id: 's1', projectId: '/p', lastSynced: Date.now(), messages: [], metadata: {} });
    await db.putTerminal({ id: 't1', projectId: '/p', lastSynced: Date.now(), scrollback: 'x', tabs: [] });
    await db.clearAll();
    expect(await db.getProject('/p')).toBeUndefined();
    expect(await db.getSession('s1')).toBeUndefined();
    expect(await db.getTerminal('t1')).toBeUndefined();
  });

  it('stores and retrieves a session', async () => {
    await db.putSession({ id: 's1', projectId: '/p', lastSynced: Date.now(), messages: [{ role: 'user', content: 'hi' }], metadata: {} });
    const result = await db.getSession('s1');
    expect(result?.messages).toHaveLength(1);
  });

  it('stores a session without a project id', async () => {
    await db.putSession({ id: 's3', lastSynced: Date.now(), messages: [], metadata: {} });
    const result = await db.getSession('s3');
    expect(result?.id).toBe('s3');
    expect(result?.projectId).toBeUndefined();
  });

  it('returns undefined for expired session (TTL 7 days)', async () => {
    await db.putSession({ id: 's2', projectId: '/p', lastSynced: Date.now() - 8 * DAY, messages: [], metadata: {} });
    const result = await db.getSession('s2');
    expect(result).toBeUndefined();
  });

  it('stores and retrieves a terminal', async () => {
    await db.putTerminal({ id: 't1', projectId: '/p', lastSynced: Date.now(), scrollback: 'line1\nline2', tabs: [] });
    const result = await db.getTerminal('t1');
    expect(result?.scrollback).toBe('line1\nline2');
  });

  describe('byte budget', () => {
    it('does not evict when the origin-wide storage estimate is over 50 MB', async () => {
      // Regression: the old quota check read navigator.storage.estimate(), which
      // counts the service-worker cache and localStorage too, so every write
      // emptied the cache while the origin stayed over the line.
      const MB = 1024 * 1024;
      vi.stubGlobal('navigator', {
        storage: { estimate: vi.fn().mockResolvedValue({ usage: 900 * MB, quota: 1024 * MB }) },
      });
      await db.putProject(project('a', Date.now() - 3000));
      await db.putProject(project('b', Date.now() - 2000));
      await db.putProject(project('c', Date.now() - 1000));
      expect(await db.getProject('a')).toBeDefined();
      expect(await db.getProject('b')).toBeDefined();
      expect(await db.getProject('c')).toBeDefined();
    });

    it('evicts the oldest records by their real bytes until under the target', async () => {
      const now = Date.now();
      // Size is measured with the same timestamp digits the records will carry.
      const size = bytesOf(project('a', now, 2000));
      const small = new OfflineDB({ quotaBytes: size * 3 + 10, evictTargetBytes: size * 2 + 10 });
      await small.clearAll();
      await small.putProject(project('a', now - 3000, 2000));
      await small.putProject(project('b', now - 2000, 2000));
      await small.putProject(project('c', now - 1000, 2000));
      expect(await small.getProject('a')).toBeDefined();

      await small.putProject(project('d', now, 2000));

      expect(await small.getProject('a')).toBeUndefined();
      expect(await small.getProject('b')).toBeUndefined();
      expect(await small.getProject('c')).toBeDefined();
      expect(await small.getProject('d')).toBeDefined();
    });

    it('counts an overwrite as a replacement, not an addition', async () => {
      const size = bytesOf(project('a', Date.now(), 2000));
      const small = new OfflineDB({ quotaBytes: size * 3 + 10, evictTargetBytes: size * 2 + 10 });
      await small.clearAll();
      for (let i = 0; i < 10; i++) {
        await small.putProject(project('a', Date.now(), 2000));
      }
      expect(runningTotal(small)).toBe(size);
      expect(await small.getProject('a')).toBeDefined();
    });

    it('clearAll resets the running total', async () => {
      await db.putProject(project('a', Date.now(), 100));
      expect(runningTotal(db)).toBeGreaterThan(0);
      await db.clearAll();
      expect(runningTotal(db)).toBe(0);
    });

    it('refuses a single record larger than the whole budget', async () => {
      const tiny = new OfflineDB({ quotaBytes: 1000, evictTargetBytes: 500 });
      await tiny.clearAll();
      await expect(tiny.putProject(project('big', Date.now(), 5000))).rejects.toThrow(/byte budget/);
      expect(await tiny.getProject('big')).toBeUndefined();
    });
  });

  describe('summarize', () => {
    it('reports no data on an empty cache', async () => {
      expect(await db.summarize()).toEqual({ hasData: false, newestLastSynced: null });
    });

    it('reports the newest record and that it is fresh', async () => {
      const t = Date.now() - 60_000;
      await db.putProject(project('p', t - 1000));
      await db.putSession({ id: 's', projectId: 'p', lastSynced: t, messages: [], metadata: {} });
      expect(await db.summarize()).toEqual({ hasData: true, newestLastSynced: t });
    });

    it('reports no usable data when every record is past its TTL', async () => {
      const old = Date.now() - 8 * DAY;
      await db.putSession({ id: 's', projectId: 'p', lastSynced: old, messages: [], metadata: {} });
      expect(await db.summarize()).toEqual({ hasData: false, newestLastSynced: old });
    });
  });
});

describe('OfflineDB listProjects', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });
  it('returns cached unexpired projects', async () => {
    await db.putProject({ id: '/p1', name: 'p1', lastSynced: Date.now(), data: { path: '/p1' } });
    await db.putProject({ id: '/p2', name: 'p2', lastSynced: Date.now(), data: { path: '/p2' } });
    const list = await db.listProjects();
    expect(list).toHaveLength(2);
  });
  it('skips expired projects', async () => {
    await db.putProject({ id: '/old', name: 'old', lastSynced: Date.now() - 31 * 24 * 60 * 60 * 1000, data: {} });
    const list = await db.listProjects();
    expect(list).toHaveLength(0);
  });
});

describe('OfflineDB pruneProjects', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });

  it('removes cached projects whose key the server no longer lists', async () => {
    await db.putProject({ id: '/kept', name: 'kept', lastSynced: Date.now(), order: 0, data: {} });
    await db.putProject({ id: '/deleted', name: 'deleted', lastSynced: Date.now(), order: 1, data: {} });
    await db.putProject({ id: 'dev@box::/renamed-away', name: 'old', lastSynced: Date.now(), order: 2, data: {} });

    const removed = await db.pruneProjects(new Set(['/kept']));

    expect(removed).toBe(2);
    expect(await db.getProject('/deleted')).toBeUndefined();
    expect(await db.getProject('dev@box::/renamed-away')).toBeUndefined();
    expect((await db.listProjects()).map((p) => p.id)).toEqual(['/kept']);
  });

  it('removes nothing when every cached key is still listed', async () => {
    await db.putProject({ id: '/a', name: 'a', lastSynced: Date.now(), data: {} });
    await db.putProject({ id: '/b', name: 'b', lastSynced: Date.now(), data: {} });
    expect(await db.pruneProjects(new Set(['/a', '/b']))).toBe(0);
    expect((await db.listProjects()).map((p) => p.id).sort()).toEqual(['/a', '/b']);
  });

  it('keeps the running byte total in step with what is stored', async () => {
    const kept: CachedProject = { id: '/kept', name: 'kept', lastSynced: Date.now(), data: { pad: 'y'.repeat(300) } };
    await db.putProject(kept);
    await db.putProject(project('/gone', Date.now(), 1000));

    await db.pruneProjects(new Set(['/kept']));

    expect(runningTotal(db)).toBe(bytesOf(kept));
  });
});

describe('OfflineDB deleteProject', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
  });

  it('removes one cached project by its key and leaves the others', async () => {
    await db.putProject({ id: '/a', name: 'a', lastSynced: Date.now(), data: {} });
    await db.putProject({ id: 'dev@box::/a', name: 'remote a', lastSynced: Date.now(), data: {} });

    expect(await db.deleteProject('/a')).toBe(true);

    expect(await db.getProject('/a')).toBeUndefined();
    expect((await db.listProjects()).map((p) => p.id)).toEqual(['dev@box::/a']);
  });

  it('returns false and leaves the running total alone when the key is not cached', async () => {
    const kept: CachedProject = { id: '/kept', name: 'kept', lastSynced: Date.now(), data: { pad: 'y'.repeat(300) } };
    await db.putProject(kept);
    const before = runningTotal(db);

    expect(await db.deleteProject('/missing')).toBe(false);

    expect(runningTotal(db)).toBe(before);
    expect(runningTotal(db)).toBe(bytesOf(kept));
  });

  it('keeps the running byte total in step with what is stored', async () => {
    const kept: CachedProject = { id: '/kept', name: 'kept', lastSynced: Date.now(), data: { pad: 'y'.repeat(300) } };
    await db.putProject(kept);
    await db.putProject(project('/gone', Date.now(), 1000));

    await db.deleteProject('/gone');

    expect(runningTotal(db)).toBe(bytesOf(kept));
  });
});
