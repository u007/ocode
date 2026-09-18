import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { RemoteTerminalEntry } from "../api/types";
import { eventBus } from "../lib/eventBus";

export interface RemoteTerminalsState {
  terminals: RemoteTerminalEntry[];
  loading: boolean;
  error: string | null;
  refresh: () => void;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * useRemoteTerminals — the live terminal inventory for one remote project.
 *
 * Fetches GET /api/terminal on the host (through the local reverse proxy) when
 * `enabled` becomes true and again on every eventBus reconnect. Used by the
 * sidebar row's expanded reattach list when localStorage no longer knows the
 * terminal ids.
 */
export function useRemoteTerminals(
  host: string | undefined,
  projectPath: string,
  enabled: boolean,
): RemoteTerminalsState {
  const [terminals, setTerminals] = useState<RemoteTerminalEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cancelledRef = useRef(false);

  const fetchTerminals = useCallback(() => {
    if (!host) return;
    setLoading(true);
    return api
      .listRemoteTerminals(host, projectPath)
      .then((list) => {
        if (cancelledRef.current) return;
        setTerminals(list);
        setError(null);
      })
      .catch((err) => {
        if (cancelledRef.current) return;
        setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelledRef.current) setLoading(false);
      });
  }, [host, projectPath]);

  useEffect(() => {
    cancelledRef.current = false;
    if (!enabled || !host) return;
    void fetchTerminals();
    const off = eventBus.onReconnect(() => void fetchTerminals());
    return () => {
      cancelledRef.current = true;
      off();
    };
  }, [enabled, host, fetchTerminals]);

  return { terminals, loading, error, refresh: () => void fetchTerminals() };
}
