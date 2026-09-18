import { useCallback, useEffect, useRef, useState } from "react";
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

/** How often the sidebar row re-reads a remote host's status while visible. */
const REMOTE_STATUS_POLL_MS = 30_000;

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * useRemoteHostStatus — per-host remote server status for the sidebar row.
 *
 * Fetches GET /api/remote/{host}/status on mount (when enabled), every 30s
 * while enabled, and on every eventBus reconnect. `connect` and `restart` set
 * `busy`, call the matching endpoint, and store the returned status; on error
 * they store the message and re-read status so the line reflects reality.
 */
export function useRemoteHostStatus(host: string | undefined, enabled: boolean): RemoteHostStatusState {
  const [status, setStatus] = useState<RemoteHostStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<RemoteHostBusy>("idle");
  const cancelledRef = useRef(false);

  const fetchStatus = useCallback(
    (showLoading: boolean): Promise<void> => {
      if (!host) return Promise.resolve();
      if (showLoading) setLoading(true);
      return api
        .getRemoteHostStatus(host)
        .then((s) => {
          if (cancelledRef.current) return;
          setStatus(s);
          setError(null);
        })
        .catch((err) => {
          if (cancelledRef.current) return;
          setError(errorMessage(err));
        })
        .finally(() => {
          if (!cancelledRef.current) setLoading(false);
        });
    },
    [host],
  );

  useEffect(() => {
    cancelledRef.current = false;
    if (!enabled || !host) return;
    void fetchStatus(true);
    const timer = setInterval(() => void fetchStatus(false), REMOTE_STATUS_POLL_MS);
    const off = eventBus.onReconnect(() => void fetchStatus(false));
    return () => {
      cancelledRef.current = true;
      clearInterval(timer);
      off();
    };
  }, [enabled, host, fetchStatus]);

  const connect = useCallback(() => {
    if (!host) return;
    setBusy("connecting");
    setError(null);
    api
      .connectRemoteHost(host)
      .then((s) => {
        if (!cancelledRef.current) setStatus(s);
      })
      .catch((err) => {
        if (cancelledRef.current) return;
        const message = errorMessage(err);
        // Refresh first, then re-assert the action error: a successful refresh
        // clears the error, and a user-action failure must stay visible.
        void fetchStatus(false).finally(() => {
          if (!cancelledRef.current) setError(message);
        });
      })
      .finally(() => {
        if (!cancelledRef.current) setBusy("idle");
      });
  }, [host, fetchStatus]);

  const restart = useCallback(() => {
    if (!host) return;
    setBusy("restarting");
    setError(null);
    api
      .restartRemoteHost(host)
      .then((s) => {
        if (!cancelledRef.current) setStatus(s);
      })
      .catch((err) => {
        if (cancelledRef.current) return;
        const message = errorMessage(err);
        // Refresh first, then re-assert the action error: a successful refresh
        // clears the error, and a user-action failure must stay visible.
        void fetchStatus(false).finally(() => {
          if (!cancelledRef.current) setError(message);
        });
      })
      .finally(() => {
        if (!cancelledRef.current) setBusy("idle");
      });
  }, [host, fetchStatus]);

  const refresh = useCallback(() => {
    void fetchStatus(true);
  }, [fetchStatus]);

  return { status, loading, error, busy, refresh, connect, restart };
}
