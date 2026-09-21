/**
 * Cross-process session-change tracking.
 *
 * ocode supports several server processes sharing one project's session
 * storage (the desktop app, a `make dev` server, the TUI). A transcript write
 * in one process — including a `/compact` that shrinks the history — is
 * durable, but it is invisible to a client connected to a *different* process,
 * whose cached transcript then goes stale ("I compacted in the browser, but my
 * desktop UI still shows the old messages").
 *
 * The fix is client-side revalidation: the server exposes an opaque `revision`
 * token on the session state and detail responses that moves whenever the
 * stored transcript changes (by any writer, in any process). This module
 * remembers the revision each session's transcript was last fetched at, so the
 * revalidation poll (`revalidateSession`) can refetch only when it moved.
 *
 * Keyed by host + session id: the same session id can exist on a remote host's
 * server and locally, and their revisions are unrelated.
 */
const revisions = new Map<string, string>();

/** Composite key — mirrors sessionPrefetch's `host\u0000id` convention. */
export function revisionKey(sessionId: string, host?: string | null): string {
  return `${host ?? ""}\u0000${sessionId}`;
}

/**
 * Record the revision the transcript for this session was just fetched at.
 * Called for every transcript fetch (the api client's `getSession`) so a later
 * change is detected relative to the data the client actually holds. An
 * absent/empty revision (bridged or unknown session) clears the baseline: the
 * server told us there is no stored transcript to watch.
 */
export function noteSessionRevision(
  sessionId: string,
  host: string | undefined,
  revision: string | undefined,
): void {
  if (!sessionId) return;
  const key = revisionKey(sessionId, host);
  if (revision) revisions.set(key, revision);
  else revisions.delete(key);
}

/** Drop a session's baseline (tab closed / session rekeyed). */
export function clearSessionRevision(sessionId: string, host?: string): void {
  revisions.delete(revisionKey(sessionId, host));
}

/**
 * True when the server's current revision differs from the one the client's
 * transcript was fetched at.
 *
 * A session with no recorded baseline returns false: there is nothing to
 * compare, and every path that loads a transcript (the tab's initial fetch,
 * the load/reconnect reconcile) records the baseline before the poll runs.
 * Treating "unknown" as "changed" would instead refetch every restored tab on
 * the first tick for no reason.
 */
export function sessionRevisionMoved(
  sessionId: string,
  host: string | undefined,
  revision: string | undefined,
): boolean {
  if (!sessionId || !revision) return false;
  const prev = revisions.get(revisionKey(sessionId, host));
  return prev !== undefined && prev !== revision;
}

/** Test seam — clears every recorded baseline. */
export function resetSessionRevisions(): void {
  revisions.clear();
}
