// Write-through cache: syncs API responses to IndexedDB for offline read access.
// All functions are best-effort: failures are logged but never thrown.

import type { CacheSummary, CachedProject, CachedSession, CachedTerminal } from './offlineDb';
import type { Project } from '../api/types';

let dbPromise: Promise<import('./offlineDb').OfflineDB> | null = null;

async function getDB(): Promise<import('./offlineDb').OfflineDB> {
  if (!dbPromise) {
    dbPromise = import('./offlineDb').then((m) => new m.OfflineDB());
  }
  return dbPromise;
}

/**
 * The offline cache key for a project. A local project is keyed by its bare path,
 * which is what existing records already use. A remote (SSH/WSL) project lives in
 * its own namespace, so its path can equal a local project's; it is qualified by
 * host with the same `host::path` format as projectStore's `projectSessionKey`.
 * Every reader and writer of a project record must go through this.
 */
export function projectCacheKey(project: { path: string; host?: string }): string {
  return project.host ? `${project.host}::${project.path}` : project.path;
}

export async function syncProjectToCache(project: any, order?: number): Promise<void> {
  try {
    const db = await getDB();
    const cached: CachedProject = {
      id: projectCacheKey(project),
      name: project.name,
      lastSynced: Date.now(),
      order: order ?? project.order,
      data: project,
    };
    await db.putProject(cached);
  } catch (err) {
    console.debug('[offlineSync] syncProjectToCache failed:', err);
  }
}

export async function syncSessionToCache(session: {
  id: string;
  projectId?: string;
  messages?: Array<{ role: string; content: string }>;
  [key: string]: unknown;
}): Promise<void> {
  try {
    const db = await getDB();
    const { messages: transcript, ...rest } = session;
    const cached: CachedSession = {
      id: session.id,
      // Left undefined when the caller does not know the project: an empty
      // string would read as a real project id.
      projectId: session.projectId,
      lastSynced: Date.now(),
      messages: (transcript ?? []).slice(-200),
      // Metadata carries everything except the transcript, which lives in
      // `messages`. Keeping both would store the transcript twice.
      metadata: rest,
    };
    await db.putSession(cached);
  } catch (err) {
    console.debug('[offlineSync] syncSessionToCache failed:', err);
  }
}

export async function syncTerminalToCache(terminal: {
  id: string;
  projectId?: string;
  scrollback?: string;
  tabs?: unknown[];
  [key: string]: unknown;
}): Promise<void> {
  try {
    const db = await getDB();
    const scrollback = (terminal.scrollback ?? '').split('\n').slice(-1000).join('\n');
    const cached: CachedTerminal = {
      id: terminal.id,
      projectId: terminal.projectId ?? '',
      lastSynced: Date.now(),
      scrollback,
      tabs: terminal.tabs ?? [],
    };
    await db.putTerminal(cached);
  } catch (err) {
    console.debug('[offlineSync] syncTerminalToCache failed:', err);
  }
}

export async function loadFromCache(
  type: 'project' | 'session' | 'terminal',
  key: string,
): Promise<CachedProject | CachedSession | CachedTerminal | undefined> {
  try {
    const db = await getDB();
    if (type === 'project') return db.getProject(key);
    if (type === 'session') return db.getSession(key);
    if (type === 'terminal') return db.getTerminal(key);
  } catch (err) {
    console.debug('[offlineSync] loadFromCache failed:', err);
  }
  return undefined;
}

/**
 * Reads the cache's own state for the offline banner. Derived from the stored
 * records, not from a value kept in memory, so it survives a reload and agrees
 * with the data after a purge or eviction. Returns undefined when the read fails.
 */
export async function loadCacheSummary(): Promise<CacheSummary | undefined> {
  try {
    const db = await getDB();
    return await db.summarize();
  } catch (err) {
    console.warn('[offlineSync] loadCacheSummary failed:', err);
    return undefined;
  }
}

/**
 * Drops cached project records whose key is not in `keepKeys`. Call only after a
 * successful project-list response, with the keys of that list. An empty list
 * means the server reported no projects, so every record is removed.
 */
export async function pruneProjectCache(keepKeys: readonly string[]): Promise<void> {
  try {
    const db = await getDB();
    await db.pruneProjects(new Set(keepKeys));
  } catch (err) {
    console.debug('[offlineSync] pruneProjectCache failed:', err);
  }
}

/**
 * Drops one project's cache record. Call only after the server confirmed the
 * project was removed or moved, so a project that is gone from the server cannot
 * come back from the cache while the server is unreachable.
 */
export async function removeProjectFromCache(project: { path: string; host?: string }): Promise<void> {
  try {
    const db = await getDB();
    await db.deleteProject(projectCacheKey(project));
  } catch (err) {
    console.debug('[offlineSync] removeProjectFromCache failed:', err);
  }
}

export async function loadCachedProjects(): Promise<Project[]> {
  try {
    const db = await getDB();
    const cached = await db.listProjects();
    return cached.map((c) => c.data).filter(Boolean) as Project[];
  } catch (err) {
    console.debug('[offlineSync] loadCachedProjects failed:', err);
    return [];
  }
}
