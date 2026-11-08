// Write-through cache: syncs API responses to IndexedDB for offline read access.
// All functions are best-effort: failures are logged but never thrown.

import type { CachedProject, CachedSession, CachedTerminal } from './offlineDb';

let dbPromise: Promise<import('./offlineDb').OfflineDB> | null = null;

async function getDB(): Promise<import('./offlineDb').OfflineDB> {
  if (!dbPromise) {
    dbPromise = import('./offlineDb').then((m) => new m.OfflineDB());
  }
  return dbPromise;
}

export async function syncProjectToCache(project: any): Promise<void> {
  try {
    const db = await getDB();
    const cached: CachedProject = {
      id: project.id ?? project.path ?? '',
      name: project.name,
      lastSynced: Date.now(),
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
    const messages = (session.messages ?? []).slice(-200);
    const cached: CachedSession = {
      id: session.id,
      projectId: session.projectId ?? '',
      lastSynced: Date.now(),
      messages,
      metadata: session,
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
