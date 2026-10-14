import type { PulseRow, Project } from "@/api/types";

/**
 * Pure host-crossing rules for the Pulse dashboard's remote fan-out
 * (`GET /api/pulse` answered through the `/api/remote/{host}` reverse proxy).
 *
 * Everything here is I/O-free so the store's merge stays a composition of
 * testable steps, and so the three rules it holds — row identity, host
 * stamping, path reconciliation — can be pinned independently of a server.
 *
 * The fan-out itself is deliberately CLIENT-side: `pulseStore.tsx` fans the
 * same request out to the local server and to each remote host, so one slow
 * or dead host cannot delay or empty the board. That also means this server
 * never adds a route and never holds `Handler.mu` across a remote call.
 */

/**
 * Row identity. A session id is only guaranteed unique within one host, and
 * the dashboard now merges rows from several of them, so `(session_id, host)`
 * is the key everywhere a row is looked up by id — `applyPulseEvent`'s index
 * and the known-session test in the store both use it.
 */
export function pulseRowKey(sessionId: string, host: string): string {
  return `${host}\u0000${sessionId}`;
}

/**
 * Label every row of a fetched page with the host it came from.
 *
 * A row that already carries a host is left alone: it came from a source that
 * knew better than the caller's assumption about which proxy answered.
 *
 * The local host ("") is stamped too, never skipped: the server serializes no
 * `host` field at all (`PulseRow.host` in api/types.ts), so a local row arrives
 * with `host === undefined`, and every lookup in the store compares hosts with
 * `===`. Leaving the field unset made the local rows invisible to live events.
 */
export function stampPulseHost(rows: PulseRow[], host: string): PulseRow[] {
  return rows.map((r) => (r.host || r.host === host ? r : { ...r, host }));
}

export interface ResolvedProjectPath {
  path: string;
  /** False when no saved project on this host matched; the reported path is kept. */
  resolved: boolean;
}

const unresolved = (path: string): ResolvedProjectPath => ({ path, resolved: false });

/**
 * Map the path a REMOTE server reported for a project to the path THIS server
 * registered for that (host, path) pair.
 *
 * The two disagree by design: a remote project is saved with its path verbatim
 * (`~/www/aimsai2` — the separator and `~` belong to the remote shell, per
 * `projects.AddRemote`), while the host that owns `$HOME` expands it when it
 * saves the same project. A heartbeat row therefore arrives reporting
 * `/home/james/www/aimsai2`.
 *
 * It matters because a card click jumps by matching the saved project on
 * `(path, host)`; keyed on the host's expanded form it matches nothing and the
 * jump is refused, so the remote card would render but do nothing.
 *
 * Registration is matched verbatim first, then by `~`-suffix — the remote
 * home directory is not known here and is never guessed. The longest suffix
 * wins when several registrations match, so `~/www/app` beats `~/app`.
 *
 * Returns `resolved: false` (and the reported path untouched) for the local
 * host: local rows come from this server's own registry, which already stores
 * the expanded path, so a suffix match there could only misfire.
 */
export function resolvePulseProjectPath(
  reported: string,
  host: string,
  projects: readonly Project[],
): ResolvedProjectPath {
  if (!host || !reported) return unresolved(reported);

  let best: Project | undefined;
  let bestTail = "";
  for (const p of projects) {
    if ((p.host ?? "") !== host || !p.path) continue;
    if (p.path === reported) return { path: p.path, resolved: true };
    if (!p.path.startsWith("~")) continue;
    const tail = p.path.slice(1); // "~/www/app" -> "/www/app"
    if (tail === "" || !reported.endsWith(tail)) continue;
    // Most specific registration wins, so "~/www/app" beats "~/app". The
    // explicit `.length` matters: comparing a number against a string coerces
    // to NaN and the guard silently never fires.
    if (best !== undefined && tail.length <= bestTail.length) continue;
    best = p;
    bestTail = tail;
  }
  if (best === undefined) return unresolved(reported);
  return { path: best.path, resolved: true };
}
