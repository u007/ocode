// Offline read cache: IndexedDB wrapper for local projects, sessions, and terminals.
// Database: "ocode-offline", version 1. Origin-scoped (local server origin only).

const DB_NAME = 'ocode-offline';
const DB_VERSION = 1;

const STORES = {
  projects: 'projects',
  sessions: 'sessions',
  terminals: 'terminals',
} as const;

type StoreName = (typeof STORES)[keyof typeof STORES];

const TTL_MS = {
  sessions: 7 * 24 * 60 * 60 * 1000,
  projects: 30 * 24 * 60 * 60 * 1000,
  terminals: 1 * 24 * 60 * 60 * 1000,
} as const;

// Budget for the bytes this cache wrote. It is counted from each record's own
// `bytes` field. navigator.storage.estimate() is deliberately NOT used: it is
// origin-wide (service-worker assets and localStorage included), so a healthy
// cache would look full and get emptied.
const QUOTA_BYTES = 50 * 1024 * 1024;
const EVICT_TARGET_BYTES = 40 * 1024 * 1024;

export interface OfflineDBOptions {
  /** Maximum bytes of cached records. Defaults to 50 MiB. */
  quotaBytes?: number;
  /** Size eviction brings the cache down to. Defaults to 40 MiB. */
  evictTargetBytes?: number;
}

export interface CachedProject {
  id: string;
  name: string;
  lastSynced: number;
  data: unknown;
}

export interface CachedSession {
  id: string;
  /** Absent when the caller does not know the owning project. */
  projectId?: string;
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

/** What the offline banner needs: whether any unexpired record exists, and when the newest record was written. */
export interface CacheSummary {
  hasData: boolean;
  newestLastSynced: number | null;
}

/** A stored record as read back from IndexedDB. Records written before byte accounting have no `bytes`. */
interface StoredRecord {
  id?: IDBValidKey;
  lastSynced: number;
  bytes?: number;
}

interface Entry {
  store: StoreName;
  key: IDBValidKey;
  lastSynced: number;
  bytes: number;
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

/** Resolves when the transaction commits; rejects if it aborts. */
function txDone(tx: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve();
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error ?? new Error('IndexedDB transaction aborted'));
  });
}

/** Serialized size in UTF-8 bytes, the unit the budget is counted in. Throws if the value cannot be serialized. */
function encodedSize(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).length;
}

function recordBytes(record: StoredRecord): number {
  return typeof record.bytes === 'number' ? record.bytes : encodedSize(record);
}

/** Visits every record in one store with a cursor, in key order. */
function forEachRecord(
  db: IDBDatabase,
  storeName: StoreName,
  visit: (key: IDBValidKey, record: StoredRecord) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(storeName, 'readonly');
    const request = tx.objectStore(storeName).openCursor();
    request.onsuccess = () => {
      const cursor = request.result;
      if (!cursor) {
        resolve();
        return;
      }
      visit(cursor.primaryKey, cursor.value as StoredRecord);
      cursor.continue();
    };
    request.onerror = () => reject(request.error);
  });
}

async function scanEntries(db: IDBDatabase): Promise<Entry[]> {
  const entries: Entry[] = [];
  for (const storeName of Object.values(STORES)) {
    await forEachRecord(db, storeName, (key, record) => {
      entries.push({ store: storeName, key, lastSynced: record.lastSynced, bytes: recordBytes(record) });
    });
  }
  return entries;
}

function sumBytes(entries: Entry[]): number {
  return entries.reduce((total, entry) => total + entry.bytes, 0);
}

export class OfflineDB {
  private dbPromise: Promise<IDBDatabase> | null = null;
  private readonly quotaBytes: number;
  private readonly evictTargetBytes: number;
  // Running total of cached bytes. It is a hint, not the truth: another OfflineDB
  // instance (another tab, or the auth-failure purge) can change the stores
  // without updating this total. makeRoom() recounts before it evicts anything.
  private total = 0;
  private totalReady: Promise<void> | null = null;

  constructor(options: OfflineDBOptions = {}) {
    this.quotaBytes = options.quotaBytes ?? QUOTA_BYTES;
    this.evictTargetBytes = options.evictTargetBytes ?? EVICT_TARGET_BYTES;
  }

  private getDB(): Promise<IDBDatabase> {
    if (!this.dbPromise) {
      this.dbPromise = openDB();
    }
    return this.dbPromise;
  }

  async putProject(project: CachedProject): Promise<void> {
    await this.writeRecord(STORES.projects, project);
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
    await this.writeRecord(STORES.sessions, session);
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
    await this.writeRecord(STORES.terminals, terminal);
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
    this.total = 0;
  }

  /**
   * Whether any unexpired record exists, and the newest record's lastSynced.
   * Reads every record in every store, so call it on an offline transition,
   * not per render.
   */
  async summarize(): Promise<CacheSummary> {
    const db = await this.getDB();
    let hasData = false;
    let newestLastSynced: number | null = null;
    for (const storeName of Object.values(STORES)) {
      await forEachRecord(db, storeName, (_key, record) => {
        if (newestLastSynced === null || record.lastSynced > newestLastSynced) {
          newestLastSynced = record.lastSynced;
        }
        if (Date.now() - record.lastSynced <= TTL_MS[storeName]) {
          hasData = true;
        }
      });
    }
    return { hasData, newestLastSynced };
  }

  getStorageEstimate(): Promise<{ usage: number; quota: number }> {
    if (navigator.storage?.estimate) {
      return navigator.storage.estimate().then((est) => ({
        usage: est.usage ?? 0,
        quota: est.quota ?? 0,
      }));
    }
    return Promise.resolve({ usage: 0, quota: QUOTA_BYTES });
  }

  private async writeRecord(
    storeName: StoreName,
    value: { id: string; lastSynced: number },
  ): Promise<void> {
    const bytes = encodedSize(value);
    if (bytes > this.quotaBytes) {
      throw new Error(
        `offline cache record ${value.id} is ${bytes} bytes, over the ${this.quotaBytes} byte budget`,
      );
    }
    await this.ensureTotal();
    await this.makeRoom(bytes);

    const db = await this.getDB();
    const tx = db.transaction(storeName, 'readwrite');
    const store = tx.objectStore(storeName);
    const done = txDone(tx);
    // Read the record being replaced in the same transaction, so its bytes
    // come off the total and the total is not inflated by every overwrite.
    const previous = idbRequest<StoredRecord | undefined>(store.get(value.id));
    store.put({ ...value, bytes });
    const [old] = await Promise.all([previous, done]);
    this.total += bytes - (old ? recordBytes(old) : 0);
  }

  private ensureTotal(): Promise<void> {
    if (!this.totalReady) {
      this.totalReady = this.getDB()
        .then((db) => scanEntries(db))
        .then((entries) => {
          this.total = sumBytes(entries);
        })
        .catch((err: unknown) => {
          // Reset so the next write retries the scan instead of reusing a failure.
          this.totalReady = null;
          throw err;
        });
    }
    return this.totalReady;
  }

  /** Evicts oldest records (by real bytes) until `incoming` fits under the budget. */
  private async makeRoom(incoming: number): Promise<void> {
    if (this.total + incoming <= this.quotaBytes) return;

    const db = await this.getDB();
    const entries = await scanEntries(db);
    this.total = sumBytes(entries);
    if (this.total + incoming <= this.quotaBytes) return;

    const oldestFirst = [...entries].sort((a, b) => a.lastSynced - b.lastSynced);
    const victims: Entry[] = [];
    let projected = this.total;
    for (const entry of oldestFirst) {
      if (projected + incoming <= this.evictTargetBytes) break;
      victims.push(entry);
      projected -= entry.bytes;
    }
    await this.deleteEntries(db, victims);
    this.total = projected;
  }

  private async deleteEntries(db: IDBDatabase, victims: Entry[]): Promise<void> {
    for (const storeName of Object.values(STORES)) {
      const keys = victims.filter((v) => v.store === storeName).map((v) => v.key);
      if (keys.length === 0) continue;
      const tx = db.transaction(storeName, 'readwrite');
      const store = tx.objectStore(storeName);
      const done = txDone(tx);
      for (const key of keys) {
        store.delete(key);
      }
      await done;
    }
  }
}
