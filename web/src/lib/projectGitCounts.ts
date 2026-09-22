import { useCallback, useSyncExternalStore } from "react";
import { api } from "../api/client";
import type { GitStatus } from "../api/types";
import { eventBus, type BusEnvelope } from "./eventBus";

/**
 * projectGitCounts — per-project git changed-file counts for the project list.
 *
 * The project sidebar renders one row per saved project, but only projects
 * with an open tab are declared to the server as "viewed" (App.tsx
 * `eventBus.setProjects`), which is what makes the subscriber-aware emitter
 * (internal/server/emitters.go) push `git_status` envelopes for them. A row
 * whose project has no open tab would therefore never hear about its own
 * repo, so this store fetches `GET /api/git/status` itself instead of relying
 * on the push:
 *
 * - one module-level entry per `host\0project`, shared by the expanded row
 *   and the collapsed rail of the same project (and by every mount of it), so
 *   N sidebar rows cost one request, not two;
 * - an initial fetch on first subscription, then a 60s poll while at least
 *   one subscriber is mounted (a directory that reports `is_repo: false` is
 *   re-probed every 5 min instead — most saved projects are not repos and do
 *   not deserve four git probes a minute forever);
 * - an instant refresh when the bus pushes `git_status` for that project
 *   (open tabs get those from the emitter, including changes made by another
 *   ocode process). The envelope carries no host, and the same path can exist
 *   on two machines, so the payload is never trusted directly — the entry
 *   re-fetches through its own `host`, which is always the right one.
 *
 * Remote rows are gated on `enabled` (the host being connected, see
 * `useRemoteHostStatus`): `GET /api/git/status?host=` is the cold-connect
 * path, so probing every saved remote at boot is exactly the "sidebar hover
 * hangs the app" failure mode. While disabled the entry fetches nothing and
 * snapshots to `NO_GIT_COUNTS`, which also clears a stale badge when a host
 * disconnects.
 *
 * A failed fetch keeps the last known counts (mirrors GitPanel's background
 * refresh): one transient error must not flash every badge in the list to
 * zero, and the `enabled` gate already covers the lasting "host is gone"
 * case.
 */

/** How often one mounted project row re-reads its own git status. */
export const GIT_COUNTS_POLL_MS = 60_000;
/** A directory that answered `is_repo: false` is re-probed at most this
 *  often, so non-repo projects cost one request per 5 min, not per minute. */
export const GIT_COUNTS_NON_REPO_BACKOFF_MS = 5 * 60_000;

export type ProjectGitCounts = {
  staged: number;
  unstaged: number;
  /** staged + unstaged — the same total the Git tab badge shows, so the two
   *  surfaces can never disagree about the working tree. */
  total: number;
  isRepo: boolean;
};

/** Snapshot for "nothing known yet / not allowed to ask": a shared constant so
 *  `useSyncExternalStore` gets a stable reference instead of a new object per
 *  render. */
export const NO_GIT_COUNTS: ProjectGitCounts = {
  staged: 0,
  unstaged: 0,
  total: 0,
  isRepo: false,
};

type Entry = {
  readonly key: string;
  readonly project: string;
  readonly host?: string;
  /** Latest subscriber's view of "may we ask about this project". Every
   *  subscriber of a key derives it from the same host-status store, so there
   *  is one meaningful value, not one per subscriber. */
  enabled: boolean;
  counts: ProjectGitCounts;
  /** Wall time of the last SUCCESSFUL status read (0 = never). */
  lastOkAt: number;
  inFlight: boolean;
  timer: ReturnType<typeof setInterval> | null;
  readonly listeners: Set<() => void>;
};

const entries = new Map<string, Entry>();
let offGitStatus: (() => void) | null = null;

function keyOf(project: string, host?: string): string {
  return `${host ?? ""}\u0000${project}`;
}

function countsOf(status: GitStatus): ProjectGitCounts {
  const staged = status.staged_files?.length ?? 0;
  const unstaged = status.changed_files?.length ?? 0;
  return { staged, unstaged, total: staged + unstaged, isRepo: !!status.is_repo };
}

function sameCounts(a: ProjectGitCounts, b: ProjectGitCounts): boolean {
  return a.staged === b.staged && a.unstaged === b.unstaged && a.isRepo === b.isRepo;
}

function notify(entry: Entry): void {
  for (const listener of entry.listeners) listener();
}

/** Snapshot: an entry that does not exist yet, or one whose host is not
 *  connected, reports "no changes". */
function snapshot(entry: Entry | undefined): ProjectGitCounts {
  if (!entry || !entry.enabled) return NO_GIT_COUNTS;
  return entry.counts;
}

function refresh(entry: Entry): Promise<void> {
  if (!entry.enabled || entry.inFlight) return Promise.resolve();
  entry.inFlight = true;
  return api
    .getGitStatus(entry.project, entry.host)
    .then((status) => {
      const next = countsOf(status);
      entry.lastOkAt = Date.now();
      if (sameCounts(next, entry.counts)) return;
      entry.counts = next;
      notify(entry);
    })
    .catch((err) => {
      console.error(`project git counts for ${entry.project} failed:`, err);
    })
    .finally(() => {
      entry.inFlight = false;
    });
}

function startTimer(entry: Entry): void {
  if (entry.timer !== null) return;
  entry.timer = setInterval(() => {
    if (!entry.enabled) return;
    const backedOff =
      entry.counts.isRepo === false &&
      entry.lastOkAt > 0 &&
      Date.now() - entry.lastOkAt < GIT_COUNTS_NON_REPO_BACKOFF_MS;
    if (backedOff) return;
    void refresh(entry);
  }, GIT_COUNTS_POLL_MS);
}

function stopTimer(entry: Entry): void {
  if (entry.timer === null) return;
  clearInterval(entry.timer);
  entry.timer = null;
}

/** The bus handler refreshes (never applies the pushed payload) so a local
 *  and a remote project that happen to share a path can never cross-contaminate. */
function onGitStatus(env: BusEnvelope): void {
  if (!env.project) return;
  for (const entry of entries.values()) {
    if (entry.listeners.size > 0 && entry.project === env.project) void refresh(entry);
  }
}

/** Subscribe to the bus exactly while at least one row is mounted, so tests
 *  (and an app with no sidebar open) leave no listener behind. */
function syncBusSubscription(): void {
  const active = [...entries.values()].some((e) => e.listeners.size > 0);
  if (active && !offGitStatus) {
    offGitStatus = eventBus.on("git_status", onGitStatus);
  } else if (!active && offGitStatus) {
    offGitStatus();
    offGitStatus = null;
  }
}

function subscribeEntry(
  project: string,
  host: string | undefined,
  enabled: boolean,
  listener: () => void,
): () => void {
  const key = keyOf(project, host);
  let entry = entries.get(key);
  if (!entry) {
    entry = {
      key,
      project,
      host,
      enabled,
      counts: NO_GIT_COUNTS,
      lastOkAt: 0,
      inFlight: false,
      timer: null,
      listeners: new Set(),
    };
    entries.set(key, entry);
  }
  const wasEnabled = entry.enabled;
  entry.enabled = enabled;
  entry.listeners.add(listener);
  if (enabled) {
    startTimer(entry);
    // Fetch on first subscription and on re-subscription after the poll
    // window (row collapsed/expanded, group toggled), but not on every
    // remount within it — that would turn tab switching into request spam.
    if (entry.lastOkAt === 0 || Date.now() - entry.lastOkAt >= GIT_COUNTS_POLL_MS) {
      void refresh(entry);
    }
  } else {
    stopTimer(entry);
  }
  if (wasEnabled !== enabled) notify(entry);
  syncBusSubscription();
  return () => {
    entry!.listeners.delete(listener);
    if (entry!.listeners.size === 0) stopTimer(entry!);
    syncBusSubscription();
  };
}

function getSnapshotEntry(project: string, host?: string): Entry | undefined {
  return entries.get(keyOf(project, host));
}

/**
 * `useProjectGitCounts(project, host, enabled)` — staged/unstaged totals for
 * one saved project. `enabled` should be `!host || remoteConnected`; while it
 * is false the hook fetches nothing and reports `NO_GIT_COUNTS`.
 */
export function useProjectGitCounts(
  project: string,
  host?: string,
  enabled = true,
): ProjectGitCounts {
  const subscribe = useCallback(
    (listener: () => void) => subscribeEntry(project, host, enabled, listener),
    [project, host, enabled],
  );
  const getSnapshot = useCallback(() => snapshot(getSnapshotEntry(project, host)), [project, host]);
  return useSyncExternalStore(subscribe, getSnapshot, () => NO_GIT_COUNTS);
}

/**
 * Test-only: drop every entry (timers + the bus subscription). Entries are
 * otherwise retained after their last subscriber leaves so a collapsed row
 * re-mounts without a refetch; without this, one test's cached counts would
 * leak into the next.
 */
export function __resetProjectGitCountsForTests(): void {
  for (const entry of entries.values()) stopTimer(entry);
  entries.clear();
  if (offGitStatus) {
    offGitStatus();
    offGitStatus = null;
  }
}
