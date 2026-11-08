import "fake-indexeddb/auto";
import { describe, it, expect, beforeEach } from 'vitest';
import { OfflineDB } from '../offlineDb';

describe('OfflineDB', () => {
  let db: OfflineDB;
  beforeEach(async () => {
    db = new OfflineDB();
    await db.clearAll();
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

  it('stores and retrieves a session', async () => {
    await db.putSession({ id: 's1', projectId: '/p', lastSynced: Date.now(), messages: [{ role: 'user', content: 'hi' }], metadata: {} });
    const result = await db.getSession('s1');
    expect(result?.messages).toHaveLength(1);
  });

  it('returns undefined for expired session (TTL 7 days)', async () => {
    await db.putSession({ id: 's2', projectId: '/p', lastSynced: Date.now() - 8 * 24 * 60 * 60 * 1000, messages: [], metadata: {} });
    const result = await db.getSession('s2');
    expect(result).toBeUndefined();
  });

  it('stores and retrieves a terminal', async () => {
    await db.putTerminal({ id: 't1', projectId: '/p', lastSynced: Date.now(), scrollback: 'line1\nline2', tabs: [] });
    const result = await db.getTerminal('t1');
    expect(result?.scrollback).toBe('line1\nline2');
  });
});
