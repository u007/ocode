import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { RemoteHostStatus } from "../api/types";
import { eventBus } from "../lib/eventBus";
import {
  __resetRemoteReadGatesForTests,
  isHostUnreachable,
  noteRemoteHostReachable,
  resetRemoteHostGate,
  withRemoteRead,
} from "../lib/remoteHostGate";

export type RemoteHostBusy = "idle" | "connecting" | "restarting";

/**
 * What the stalled-host prompt offers. `reconnect` is a forced retry (it
 * supersedes the in-flight attempt); `restart` kills and relaunches the host's
 * remote server, so it is offered only after a retry has also stalled.
 */
export type RemoteHostEscalation = "none" | "reconnect" | "restart";

/** How long an attempt may run without connecting before the user is prompted. */
export const REMOTE_STALL_AFTER_MS = 10_000;

export interface RemoteHostStatusState {
  status: RemoteHostStatus | null;
  loading: boolean;
  error: string | null;
  busy: RemoteHostBusy;
  /** True once the current attempt has run past REMOTE_STALL_AFTER_MS. */
  stalled: boolean;
  /** Which recovery the prompt should offer; `none` while healthy or still starting. */
  escalation: RemoteHostEscalation;
  refresh: () => void;
  connect: () => void;
  /** Forced retry: supersedes an attempt that is still running. */
  reconnect: () => void;
  restart: () => void;
}

/** How often a remote host's status is re-read while it has subscribers. */
const REMOTE_STATUS_POLL_MS = 30_000;

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

type StatusSnapshot = Pick<
  RemoteHostStatusState,
  "status" | "loading" | "error" | "busy" | "stalled" | "escalation"
>;

const EMPTY_STATE: StatusSnapshot = {
  status: null,
  loading: false,
  error: null,
  busy: "idle",
  stalled: false,
  escalation: "none",
};

// ---------------------------------------------------------------------------
// Shared per-host store
// ---------------------------------------------------------------------------
// The sidebar row and the chat's remote-version banner both need the SAME
// host's status. A module-level entry per host gives them one poll and one
// in-flight action instead of two, and an update triggered from either surface
// is reflected in the other immediately (rather than after the other's next
// 30s poll). An entry exists only while at least one hook is subscribed, so
// closed projects and tests without a mounted consumer leak no timers.

interface RemoteHostEntry {
  readonly host: string;
  status: RemoteHostStatus | null;
  loading: boolean;
  error: string | null;
  busy: RemoteHostBusy;
  /** Derived by refreshStall; never written by callers. */
  stalled: boolean;
  escalation: RemoteHostEscalation;
  /** The current episode already offered (and used) a reconnect. */
  retriedAfterStall: boolean;
  /** Client-side start of the attempt, used when the status has no timestamp yet. */
  episodeStartedAt: number;
  stallTimer: ReturnType<typeof setTimeout> | null;
  readonly listeners: Set<() => void>;
  timer: ReturnType<typeof setInterval> | null;
  offReconnect: (() => void) | null;
}

const entries = new Map<string, RemoteHostEntry>();

function snapshotOf(entry: RemoteHostEntry | undefined): StatusSnapshot {
  if (!entry) return EMPTY_STATE;
  return {
    status: entry.status,
    loading: entry.loading,
    error: entry.error,
    busy: entry.busy,
    stalled: entry.stalled,
    escalation: entry.escalation,
  };
}

/**
 * True while the local server has a connect attempt in flight for this host,
 * whether the user started it (`busy`) or a proxied read did (`status`).
 */
function attemptInFlight(entry: RemoteHostEntry): boolean {
  return (
    entry.busy === "connecting" ||
    entry.busy === "restarting" ||
    entry.status?.connecting === true
  );
}

/** When the current attempt began: the server's start, or this client's action. */
function attemptStart(entry: RemoteHostEntry): number {
  return Math.max(entry.status?.connect_started_at ?? 0, entry.episodeStartedAt);
}

/**
 * Recomputes the stall state from the attempt clock. Called from notify so the
 * derived fields can never disagree with the status they were derived from, and
 * from the timer that arms the prompt.
 */
function refreshStall(entry: RemoteHostEntry) {
  if (!attemptInFlight(entry)) {
    entry.stalled = false;
    // A retry that FAILED (rather than stalled) is still a failed recovery: the
    // next step the prompt offers is the heavier one. The episode really ends
    // only when the host connects, which clears the flag below.
    if (entry.status?.connected) entry.retriedAfterStall = false;
    entry.escalation = entry.retriedAfterStall ? "restart" : "none";
    return;
  }
  // An in-flight attempt always has a clock. Without an episode start the
  // deadline could never be reached, and the timer below would re-arm forever.
  if (attemptStart(entry) === 0) entry.episodeStartedAt = Date.now();
  if (entry.stalled) return;
  if (Date.now() - attemptStart(entry) >= REMOTE_STALL_AFTER_MS) {
    entry.stalled = true;
    entry.escalation = entry.retriedAfterStall ? "restart" : "reconnect";
  }
}

/** Arms exactly one timer for the moment the attempt crosses the stall deadline. */
function scheduleStallCheck(entry: RemoteHostEntry) {
  if (entry.stallTimer !== null) {
    clearTimeout(entry.stallTimer);
    entry.stallTimer = null;
  }
  if (!attemptInFlight(entry) || entry.stalled) return;
  const wait = Math.max(0, REMOTE_STALL_AFTER_MS - (Date.now() - attemptStart(entry)));
  entry.stallTimer = setTimeout(() => {
    entry.stallTimer = null;
    notify(entry);
  }, wait);
}

function notify(entry: RemoteHostEntry) {
  refreshStall(entry);
  scheduleStallCheck(entry);
  for (const listener of entry.listeners) listener();
}

function fetchStatus(entry: RemoteHostEntry, showLoading: boolean): Promise<boolean> {
  if (showLoading) {
    entry.loading = true;
    notify(entry);
  }
  return api
    .getRemoteHostStatus(entry.host)
    .then((s) => {
      entry.status = s;
      entry.error = null;
      // A connected status read is proof the host is back: close any retry
      // window a transient failure armed. Without this, the window blocks the
      // very reads that could observe recovery, so a host that comes back can
      // stay "unreachable" for the whole retry window.
      if (s.connected) noteRemoteHostReachable(entry.host);
      return true;
    })
    .catch((err) => {
      console.error(`remote host status for ${entry.host} failed:`, err);
      entry.error = errorMessage(err);
      // Only an unreachable host opens the retry window; the read itself
      // answered for any other error.
      return !isHostUnreachable(err);
    })
    .finally(() => {
      entry.loading = false;
      notify(entry);
    });
}

/**
 * A periodic read of a remote host. It goes through the shared per-host queue
 * (see remoteHostGate.ts) so an unreachable host is asked once at a time and
 * then left alone for the retry window.
 *
 * The read's OWN outcome is what classifies it as ok/failed — never
 * entry.error, which is shared state that a concurrent user action (a failed
 * connect or restart) also writes, and which would then blacklist a host that
 * just answered fine.
 */
function pollStatus(entry: RemoteHostEntry): Promise<void> {
  // The status endpoint is an in-memory registry read, so it is safe to probe
  // even while the host is inside its retry window. That is the only way the
  // window can close before it expires: every OTHER read is skipped while it is
  // armed, so nothing else would ever observe the host recovering.
  return withRemoteRead(entry.host, "status", () => fetchStatus(entry, false), {
    probeWhileRetrying: true,
  }).then(() => undefined);
}

function ensureEntry(host: string): RemoteHostEntry {
  const existing = entries.get(host);
  if (existing) return existing;
  const entry: RemoteHostEntry = {
    host,
    status: null,
    loading: false,
    error: null,
    busy: "idle",
    stalled: false,
    escalation: "none",
    retriedAfterStall: false,
    episodeStartedAt: 0,
    stallTimer: null,
    listeners: new Set(),
    timer: null,
    offReconnect: null,
  };
  entries.set(host, entry);
  // The first ask also goes through the queue: a host that is already known to
  // be down (from a previous entry) must not be asked again on mount, and a
  // first ask that FAILS must open the retry window — otherwise the 30s poll
  // would re-ask a dead host every tick, which is exactly the connection-hold
  // the queue exists to prevent.
  void withRemoteRead(host, "status", () => fetchStatus(entry, true)).then(
    () => undefined,
  );
  entry.timer = setInterval(() => {
    void pollStatus(entry);
  }, REMOTE_STATUS_POLL_MS);
  // The bus reconnects on every SSE drop (and on a seq gap), which is not
  // user-triggered: it goes through the same queue, or a down server would
  // have the bus re-ask the host on every reconnect and hold its connection
  // for the whole ssh timeout — the exact thing the queue exists to prevent.
  entry.offReconnect = eventBus.onReconnect(() => void pollStatus(entry));
  return entry;
}

/** Tear an entry down once its last subscriber unsubscribes. */
function releaseEntry(host: string, entry: RemoteHostEntry) {
  if (entries.get(host) !== entry || entry.listeners.size > 0) return;
  if (entry.timer !== null) clearInterval(entry.timer);
  if (entry.stallTimer !== null) clearTimeout(entry.stallTimer);
  entry.offReconnect?.();
  entries.delete(host);
}

/**
 * Store a failed connect/restart: refresh the status FIRST, then re-assert the
 * action error, so a successful refresh (which clears `error`) cannot hide the
 * user-action failure. Mirrors the previous per-hook behavior.
 */
function actionFailed(entry: RemoteHostEntry, err: unknown) {
  console.error(`remote host action for ${entry.host} failed:`, err);
  const message = errorMessage(err);
  void fetchStatus(entry, false).then(() => {
    entry.error = message;
    notify(entry);
  });
}

/**
 * Run `fn` against the host's entry, creating it on demand, and release it
 * again once `fn` settles. With a subscribed hook the release is a no-op (the
 * subscriber still holds it); from an unsubscribed hook (`enabled=false`, or
 * after unmount) this is what stops the on-demand entry's poll timer and
 * reconnect listener from leaking for the rest of the process.
 */
function withEntry(host: string, fn: (entry: RemoteHostEntry) => Promise<void>): void {
  const entry = ensureEntry(host);
  void fn(entry).finally(() => releaseEntry(host, entry));
}

function runAction(
  host: string,
  busy: RemoteHostBusy,
  call: (host: string) => Promise<RemoteHostStatus>,
  opts?: { afterStall?: boolean },
) {
  withEntry(host, (entry) => {
    entry.busy = busy;
    entry.error = null;
    // A user action starts a NEW episode: restart the stall clock from now and
    // drop the previous verdict. `afterStall` is what makes a second episode
    // escalate to a host restart instead of offering reconnect again.
    entry.stalled = false;
    entry.escalation = "none";
    entry.episodeStartedAt = Date.now();
    if (opts?.afterStall) entry.retriedAfterStall = true;
    // An explicit action also clears the 2-minute read backoff, or every read
    // that follows would be skipped and the retry would look like it did
    // nothing.
    resetRemoteHostGate(host);
    notify(entry);
    return call(host)
      .then((s) => {
        entry.status = s;
        noteRemoteHostReachable(host);
      })
      .catch((err) => actionFailed(entry, err))
      .finally(() => {
        entry.busy = "idle";
        notify(entry);
      });
  });
}

/**
 * Test-only: drop every cached host entry (timers + reconnect listeners).
 * Production entries are reference-counted and removed on the last
 * unsubscribe; this exists so tests that mount the hook twice for the same
 * host do not observe a previous test's entry.
 */
export function __resetRemoteHostStatusForTests() {
  for (const entry of entries.values()) {
    if (entry.timer !== null) clearInterval(entry.timer);
    if (entry.stallTimer !== null) clearTimeout(entry.stallTimer);
    entry.offReconnect?.();
  }
  entries.clear();
  __resetRemoteReadGatesForTests();
}

/**
 * useRemoteHostStatus — per-host remote server status, shared across every
 * consumer of the same host.
 *
 * Fetches GET /api/remote/{host}/status while enabled (once, then every 30s,
 * and on every eventBus reconnect). `connect` and `restart` set `busy`, call
 * the matching endpoint, and store the returned status; on error they store
 * the message and re-read status so every surface reflects reality.
 */
export function useRemoteHostStatus(host: string | undefined, enabled: boolean): RemoteHostStatusState {
  const [state, setState] = useState<StatusSnapshot>(() => snapshotOf(host ? entries.get(host) : undefined));

  useEffect(() => {
    if (!host || !enabled) {
      setState(EMPTY_STATE);
      return;
    }
    const entry = ensureEntry(host);
    const update = () => setState(snapshotOf(entry));
    entry.listeners.add(update);
    update();
    return () => {
      entry.listeners.delete(update);
      releaseEntry(host, entry);
    };
  }, [host, enabled]);

  const refresh = useCallback(() => {
    if (host) withEntry(host, (entry) => fetchStatus(entry, true).then(() => undefined));
  }, [host]);
  const connect = useCallback(() => {
    if (host) runAction(host, "connecting", (h) => api.connectRemoteHost(h));
  }, [host]);
  const reconnect = useCallback(() => {
    if (host) {
      runAction(host, "connecting", (h) => api.connectRemoteHost(h, { force: true }), {
        afterStall: true,
      });
    }
  }, [host]);
  const restart = useCallback(() => {
    if (host) {
      runAction(host, "restarting", (h) => api.restartRemoteHost(h), { afterStall: true });
    }
  }, [host]);

  return { ...state, refresh, connect, reconnect, restart };
}
