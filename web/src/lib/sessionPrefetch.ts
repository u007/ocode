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

/** A `new-*` draft tab has no server session yet — nothing to fetch. */
function isPrefetchable(sessionId: string | null | undefined): sessionId is string {
  return !!sessionId && !sessionId.startsWith("new-");
}

/**
 * Start (or join) the transcript fetch for a session. Idempotent per session:
 * repeated hovers reuse the in-flight request instead of stacking duplicates.
 * Fire-and-forget — safe to call from a hover handler.
 */
export function prefetchSession(sessionId: string | null | undefined): void {
  if (!isPrefetchable(sessionId)) return;
  if (cache.has(sessionId)) return;
  const entry: Entry = {
    at: Date.now(),
    promise: api.getSession(sessionId, { limit: SESSION_PREFETCH_LIMIT }),
  };
  // A prefetch nobody consumes must not become an unhandled rejection.
  entry.promise.catch(() => {});
  cache.set(sessionId, entry);
}

/**
 * Hand the warm promise to a caller, if one was started recently. Removes the
 * entry (single-use). Returns `undefined` when there is nothing warm or the
 * entry is older than the TTL, in which case the caller should fetch normally.
 */
export function takePrefetchedSession(
  sessionId: string | null | undefined,
): Promise<SessionDetail> | undefined {
  if (!isPrefetchable(sessionId)) return undefined;
  const entry = cache.get(sessionId);
  if (!entry) return undefined;
  cache.delete(sessionId);
  if (Date.now() - entry.at > SESSION_PREFETCH_TTL_MS) return undefined;
  return entry.promise;
}

/** Drop a session's prefetch (e.g. the tab was closed before it was visited).
 *  Exported for tests and for callers that know the id is gone for good. */
export function dropPrefetchedSession(sessionId: string): void {
  cache.delete(sessionId);
}
