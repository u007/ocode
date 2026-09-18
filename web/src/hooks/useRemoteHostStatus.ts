import { useCallback, useEffect, useState } from "react";
import { api } from "../api/client";
import type { RemoteHostStatus } from "../api/types";
import { eventBus } from "../lib/eventBus";

export type RemoteHostBusy = "idle" | "connecting" | "restarting";

export interface RemoteHostStatusState {
  status: RemoteHostStatus | null;
  loading: boolean;
  error: string | null;
  busy: RemoteHostBusy;
  refresh: () => void;
  connect: () => void;
  restart: () => void;
}

/** How often a remote host's status is re-read while it has subscribers. */
const REMOTE_STATUS_POLL_MS = 30_000;

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

type StatusSnapshot = Pick<RemoteHostStatusState, "status" | "loading" | "error" | "busy">;

const EMPTY_STATE: StatusSnapshot = { status: null, loading: false, error: null, busy: "idle" };

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
  readonly listeners: Set<() => void>;
  timer: ReturnType<typeof setInterval> | null;
  offReconnect: (() => void) | null;
}

const entries = new Map<string, RemoteHostEntry>();

function snapshotOf(entry: RemoteHostEntry | undefined): StatusSnapshot {
  if (!entry) return EMPTY_STATE;
  return { status: entry.status, loading: entry.loading, error: entry.error, busy: entry.busy };
}

function notify(entry: RemoteHostEntry) {
  for (const listener of entry.listeners) listener();
}

function fetchStatus(entry: RemoteHostEntry, showLoading: boolean): Promise<void> {
  if (showLoading) {
    entry.loading = true;
    notify(entry);
  }
  return api
    .getRemoteHostStatus(entry.host)
    .then((s) => {
      entry.status = s;
      entry.error = null;
    })
    .catch((err) => {
      console.error(`remote host status for ${entry.host} failed:`, err);
      entry.error = errorMessage(err);
    })
    .finally(() => {
      entry.loading = false;
      notify(entry);
    });
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
    listeners: new Set(),
    timer: null,
    offReconnect: null,
  };
  entries.set(host, entry);
  void fetchStatus(entry, true);
  entry.timer = setInterval(() => void fetchStatus(entry, false), REMOTE_STATUS_POLL_MS);
  entry.offReconnect = eventBus.onReconnect(() => void fetchStatus(entry, false));
  return entry;
}

/** Tear an entry down once its last subscriber unsubscribes. */
function releaseEntry(host: string, entry: RemoteHostEntry) {
  if (entries.get(host) !== entry || entry.listeners.size > 0) return;
  if (entry.timer !== null) clearInterval(entry.timer);
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

function runAction(host: string, busy: RemoteHostBusy, call: (host: string) => Promise<RemoteHostStatus>) {
  withEntry(host, (entry) => {
    entry.busy = busy;
    entry.error = null;
    notify(entry);
    return call(host)
      .then((s) => {
        entry.status = s;
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
    entry.offReconnect?.();
  }
  entries.clear();
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
    if (host) withEntry(host, (entry) => fetchStatus(entry, true));
  }, [host]);
  const connect = useCallback(() => {
    if (host) runAction(host, "connecting", (h) => api.connectRemoteHost(h));
  }, [host]);
  const restart = useCallback(() => {
    if (host) runAction(host, "restarting", (h) => api.restartRemoteHost(h));
  }, [host]);

  return { ...state, refresh, connect, restart };
}
