/**
 * One-time localStorage move for the desktop shell's switch from
 * http://127.0.0.1:PORT to https://127.0.0.1:PORT (the webview now uses TLS so
 * it negotiates HTTP/2 instead of queueing behind six HTTP/1.1 connections).
 * The two are different origins, so without this every persisted UI state —
 * terminal/editor tabs, unsaved editor drafts, layout — would be stranded.
 *
 * Flow (driven by the desktop shell, which opens the http origin once with
 * `migrateTo`):
 *   1. http page: POST every localStorage entry to the desktop server, then
 *      `location.replace(migrateTo)` without rendering.
 *   2. https page (`storageImport=1`): GET the entries back and write each key
 *      the new origin does not already have.
 *
 * Runs before any app module is imported, because stores read localStorage at
 * import time. It therefore has no dependency on api/client and reads the
 * token from the URL itself (the desktop always passes ?token=).
 */

export const STORAGE_MIGRATION_PATH = "/api/desktop/storage-migration";

type Env = {
  location: Pick<Location, "search" | "protocol" | "host" | "href" | "pathname" | "hash"> & {
    replace(url: string): void;
  };
  storage: Storage;
  fetch: typeof fetch;
  replaceUrl(url: string): void;
};

function browserEnv(): Env {
  return {
    location: window.location,
    storage: window.localStorage,
    fetch: window.fetch.bind(window),
    replaceUrl: (url) => window.history.replaceState(window.history.state, "", url),
  };
}

/** The redirect target must be the same host and port over https, so a
 *  crafted link cannot bounce the token and storage elsewhere. */
export function isSameHostHTTPS(target: string, host: string): boolean {
  try {
    const u = new URL(target);
    return u.protocol === "https:" && u.host === host;
  } catch {
    return false;
  }
}

function authHeaders(search: string): Record<string, string> {
  const token = new URLSearchParams(search).get("token");
  return token ? { Authorization: `Bearer ${token}` } : {};
}

/**
 * Returns false when the page is navigating away (export step) and must not
 * render; true otherwise.
 */
export async function runDesktopStorageMigration(env: Env = browserEnv()): Promise<boolean> {
  const params = new URLSearchParams(env.location.search);
  const migrateTo = params.get("migrateTo");
  if (migrateTo !== null) {
    if (!isSameHostHTTPS(migrateTo, env.location.host)) {
      console.error("[storage-migration] refusing migrateTo outside this host over https:", migrateTo);
      return true;
    }
    const entries: Record<string, string> = {};
    for (let i = 0; i < env.storage.length; i++) {
      const key = env.storage.key(i);
      if (key === null) continue;
      const value = env.storage.getItem(key);
      if (value !== null) entries[key] = value;
    }
    try {
      const res = await env.fetch(STORAGE_MIGRATION_PATH, {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders(env.location.search) },
        body: JSON.stringify(entries),
      });
      if (!res.ok) console.error("[storage-migration] export failed:", res.status, await res.text());
    } catch (err) {
      console.error("[storage-migration] export request failed:", err);
    }
    env.location.replace(migrateTo);
    return false;
  }

  if (params.get("storageImport") === "1") {
    try {
      const res = await env.fetch(STORAGE_MIGRATION_PATH, { headers: authHeaders(env.location.search) });
      if (res.status === 200) {
        const entries = (await res.json()) as Record<string, string>;
        for (const [key, value] of Object.entries(entries)) {
          if (env.storage.getItem(key) === null) env.storage.setItem(key, value);
        }
      } else if (res.status !== 204) {
        console.error("[storage-migration] import failed:", res.status, await res.text());
      }
    } catch (err) {
      console.error("[storage-migration] import request failed:", err);
    }
    params.delete("storageImport");
    const query = params.toString();
    env.replaceUrl(env.location.pathname + (query ? `?${query}` : "") + env.location.hash);
  }
  return true;
}
