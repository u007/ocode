import { useCallback, useEffect, useState } from "react";
import { ApiError, api } from "../../api/client";
import type { PulseTerminal } from "../../api/types";

/** How often the terminal list refreshes while the dashboard is open. */
export const PULSE_TERMINALS_POLL_MS = 3000;

/** Rows per page. "Load more" grows the window; each poll refetches all of it. */
const PULSE_TERMINALS_PAGE = 50;

/**
 * Statuses that mean the list can never load from this server: 403 when the bind
 * is non-loopback without auth, 501 on Windows (no pty). Nothing clears them
 * without a restart, so polling stops instead of repeating the same failure.
 */
const PERMANENT_STATUSES = new Set([403, 501]);

/**
 * usePulseTerminals: the live terminals for the Pulse dashboard, running
 * programs first. It polls the first `limit` rows and skips a tick while the
 * document is hidden, so a background tab costs nothing. On a permanent status
 * it stops polling for good and reports `unavailable`.
 */
export function usePulseTerminals() {
  const [limit, setLimit] = useState(PULSE_TERMINALS_PAGE);
  const [terminals, setTerminals] = useState<PulseTerminal[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [unavailable, setUnavailable] = useState(false);

  useEffect(() => {
    if (unavailable) return;
    let stale = false;
    const load = () => {
      api
        .listPulseTerminals(0, limit)
        .then((page) => {
          if (stale) return;
          setTerminals(page.terminals);
          setTotal(page.total);
          setError(null);
        })
        .catch((err: unknown) => {
          console.error("usePulseTerminals: listing terminals failed:", err);
          if (stale) return;
          if (err instanceof ApiError && PERMANENT_STATUSES.has(err.status)) {
            setUnavailable(true);
            return;
          }
          setError(`Listing terminals failed: ${err instanceof Error ? err.message : String(err)}`);
        });
    };
    load();
    const timer = window.setInterval(() => {
      if (!document.hidden) load();
    }, PULSE_TERMINALS_POLL_MS);
    return () => {
      stale = true;
      window.clearInterval(timer);
    };
  }, [limit, unavailable]);

  const loadMore = useCallback(() => setLimit((n) => n + PULSE_TERMINALS_PAGE), []);
  return { terminals, total, error, unavailable, hasMore: terminals.length < total, loadMore };
}
