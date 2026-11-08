// Offline read cache: IndexedDB wrapper for local projects, sessions, and terminals.
// Database: "ocode-offline", version 1. Origin-scoped (local server origin only).

const DB_NAME = 'ocode-offline';
const DB_VERSION = 1;

const STORES = {
  projects: 'projects',
  sessions: 'sessions',
  terminals: 'terminals',
} as const;

const TTL_MS = {
  sessions: 7 * 24 * 60 * 60 * 1000,
  projects: 30 * 24 * 60 * 60 * 1000,
  terminals: 1 * 24 * 60 * 60 * 1000,
} as const;

const QUOTA_BYTES = 50 * 1024 * 1024;
const EVICT_TARGET_BYTES = 40 * 1024 * 1024;

export interface CachedProject {
  id: string;
  name: string;
  lastSynced: number;
  data: unknown;
}

export interface CachedSession {
  id: string;
  projectId: string;
  lastSynced: number;
  messages: Array<{ role: string; content: string }>;
  metadata: unknown;
}

export interface CachedTerminal {
  id: string;
  projectId: string;
  lastSynced: number;
  scrollback: string;
  tabs: unknown[];
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(STORES.projects)) {
        db.createObjectStore(STORES.projects, { keyPath: 'id' });
      }
      if (!db.objectStoreNames.contains(STORES.sessions)) {
        db.createObjectStore(STORES.sessions, { keyPath: 'id' });
      }
      if (!db.objectStoreNames.contains(STORES.terminals)) {
        db.createObjectStore(STORES.terminals, { keyPath: 'id' });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function idbRequest<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

function idbTransaction(
  db: IDBDatabase,
  storeName: string,
  mode: IDBTransactionMode,
): IDBObjectStore {
  const tx = db.transaction(storeName, mode);
  return tx.objectStore(storeName);
}

export class OfflineDB {
  private dbPromise: Promise<IDBDatabase> | null = null;

  private getDB(): Promise<IDBDatabase> {
    if (!this.dbPromise) {
      this.dbPromise = openDB();
    }
    return this.dbPromise;
  }

  async putProject(project: CachedProject): Promise<void> {
    await this.enforceQuota();
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.projects, 'readwrite');
    await idbRequest(store.put(project));
  }

  async getProject(id: string): Promise<CachedProject | undefined> {
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.projects, 'readonly');
    const result = await idbRequest<CachedProject | undefined>(store.get(id));
    if (!result) return undefined;
    if (Date.now() - result.lastSynced > TTL_MS.projects) return undefined;
    return result;
  }

  async putSession(session: CachedSession): Promise<void> {
    await this.enforceQuota();
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.sessions, 'readwrite');
    await idbRequest(store.put(session));
  }

  async getSession(id: string): Promise<CachedSession | undefined> {
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.sessions, 'readonly');
    const result = await idbRequest<CachedSession | undefined>(store.get(id));
    if (!result) return undefined;
    if (Date.now() - result.lastSynced > TTL_MS.sessions) return undefined;
    return result;
  }

  async putTerminal(terminal: CachedTerminal): Promise<void> {
    await this.enforceQuota();
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.terminals, 'readwrite');
    await idbRequest(store.put(terminal));
  }

  async getTerminal(id: string): Promise<CachedTerminal | undefined> {
    const db = await this.getDB();
    const store = idbTransaction(db, STORES.terminals, 'readonly');
    const result = await idbRequest<CachedTerminal | undefined>(store.get(id));
    if (!result) return undefined;
    if (Date.now() - result.lastSynced > TTL_MS.terminals) return undefined;
    return result;
  }

  async clearAll(): Promise<void> {
    const db = await this.getDB();
    for (const storeName of Object.values(STORES)) {
      const store = idbTransaction(db, storeName, 'readwrite');
      await idbRequest(store.clear());
    }
  }

  async getStorageEstimate(): Promise<{ usage: number; quota: number }> {
    if (navigator.storage?.estimate) {
      const est = await navigator.storage.estimate();
      return { usage: est.usage ?? 0, quota: est.quota ?? 0 };
    }
    return { usage: 0, quota: QUOTA_BYTES };
  }

  private async enforceQuota(): Promise<void> {
    try {
      const est = await this.getStorageEstimate();
      if (est.usage <= QUOTA_BYTES) return;
      // Evict oldest entries across all stores until under target.
      const db = await this.getDB();
      const allEntries: Array<{ store: string; key: IDBValidKey; lastSynced: number }> = [];
      for (const storeName of Object.values(STORES)) {
        const store = idbTransaction(db, storeName, 'readonly');
        const keys = await idbRequest<IDBValidKey[]>(store.getAllKeys());
        for (const key of keys) {
          const record = await idbRequest<{ lastSynced: number }>(store.get(key));
          if (record) {
            allEntries.push({ store: storeName, key, lastSynced: record.lastSynced });
          }
        }
      }
      allEntries.sort((a, b) => a.lastSynced - b.lastSynced);
      let usage = est.usage;
      for (const entry of allEntries) {
        if (usage <= EVICT_TARGET_BYTES) break;
        const store = idbTransaction(db, entry.store, 'readwrite');
        await idbRequest(store.delete(entry.key));
        usage -= 1024; // rough estimate
      }
    } catch {
      // Quota enforcement is best-effort.
    }
  }
}
