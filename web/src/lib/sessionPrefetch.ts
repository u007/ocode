import { api } from "../api/client";
import type { SessionDetail } from "../api/types";

/**
 * Warm the session-transcript fetch so opening a chat tab does not start from
 * a cold round-trip.
 *
 * `ChatPanel` fetches `GET /api/sessions/:id?limit=…` on first mount, and until
 * that resolves the tab shows its loading state. Acts that reliably precede
 * opening a tab — hovering its pill, hovering a session-dialog row, or hovering
 * the project that owns it — can start that same request early so the response
 * is already in flight (or landed) by the time the panel mounts.
 *
 * Design constraints:
 * - **Single-use.** `takePrefetchedSession` removes the entry, so a prefetched
 *   promise can never be silently reused for a later, unrelated mount.
 * - **Short TTL.** A preview that sat in the map for a long time is stale; the
 *   caller refetches instead. This bounds the staleness window even if a hover
 *   is followed by minutes of reading.
 * - **Self-cancelling on failure.** The promise's rejection is swallowed here
 *   so a prefetch that nobody consumes cannot surface as an unhandled
 *   rejection. A consumer that does attach a handler still sees the error.
 * - **Correctness is unchanged.** `ChatPanel` only merges a snapshot into an
 *   empty slice (see the `current.messages.length > 0` guard); the live SSE
 *   mirror always wins if it already populated the slice. A prefetched
 *   snapshot is therefore never able to overwrite newer state.
 */

/** Must match `ChatPanel`'s initial transcript fetch (`PAGE_SIZE * 2`). */
export const SESSION_PREFETCH_LIMIT = 100;

/** How long a prefetched response may sit unconsumed before it is considered
 *  stale and the caller refetches from scratch. */
export const SESSION_PREFETCH_TTL_MS = 15_000;

interface Entry {
  at: number;
  promise: Promise<SessionDetail>;
}

const cache = new Map<string, Entry>();

/** Cache key includes the project host so a remote session's prefetch is never
 *  mistaken for (or served to) a local session with the same id. */
function prefetchKey(sessionId: string, host?: string): string {
  return `${host ?? ""}\u0000${sessionId}`;
}

/** A `new-*` draft tab has no server session yet — nothing to fetch. */
function isPrefetchable(sessionId: string | null | undefined): sessionId is string {
  return !!sessionId && !sessionId.startsWith("new-");
}

/**
 * Start (or join) the transcript fetch for a session. Idempotent per session:
 * repeated hovers reuse the in-flight request instead of stacking duplicates.
 * Fire-and-forget — safe to call from a hover handler.
 */
export function prefetchSession(sessionId: string | null | undefined, host?: string): void {
  if (!isPrefetchable(sessionId)) return;
  const key = prefetchKey(sessionId, host);
  if (cache.has(key)) return;
  const entry: Entry = {
    at: Date.now(),
    promise: api.getSession(sessionId, { limit: SESSION_PREFETCH_LIMIT }, host),
  };
  // A prefetch nobody consumes must not become an unhandled rejection.
  entry.promise.catch(() => {});
  cache.set(key, entry);
}

/**
 * Hand the warm promise to a caller, if one was started recently. Removes the
 * entry (single-use). Returns `undefined` when there is nothing warm or the
 * entry is older than the TTL, in which case the caller should fetch normally.
 *
 * `host` must match the prefetch that started the request; a remote session's
 * warm entry is never served to a local fetch (or vice versa).
 */
export function takePrefetchedSession(
  sessionId: string | null | undefined,
  host?: string,
): Promise<SessionDetail> | undefined {
  if (!isPrefetchable(sessionId)) return undefined;
  const key = prefetchKey(sessionId, host);
  const entry = cache.get(key);
  if (!entry) return undefined;
  cache.delete(key);
  if (Date.now() - entry.at > SESSION_PREFETCH_TTL_MS) return undefined;
  return entry.promise;
}

/** Drop a session's prefetch (e.g. the tab was closed before it was visited).
 *  When `host` is omitted, every host variant for the id is dropped. Exported
 *  for tests and for callers that know the id is gone for good. */
export function dropPrefetchedSession(sessionId: string, host?: string): void {
  if (host !== undefined) {
    cache.delete(prefetchKey(sessionId, host));
    return;
  }
  const suffix = `\u0000${sessionId}`;
  for (const key of [...cache.keys()]) {
    if (key.endsWith(suffix)) cache.delete(key);
  }
}
