import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { PulseTerminal } from "../../api/types";

/** How often the terminal list refreshes while the dashboard is open. */
export const PULSE_TERMINALS_POLL_MS = 3000;

/** Rows per page. "Load more" grows the window; each poll refetches all of it. */
const PULSE_TERMINALS_PAGE = 50;

/**
 * usePulseTerminals: the live terminals for the Pulse dashboard, running
 * programs first. It polls the first `limit` rows and skips a tick while the
 * document is hidden, so a background tab costs nothing.
 */
export function usePulseTerminals() {
  const [limit, setLimit] = useState(PULSE_TERMINALS_PAGE);
  const [terminals, setTerminals] = useState<PulseTerminal[]>([]);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
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
          if (!stale) setError(`Listing terminals failed: ${err instanceof Error ? err.message : String(err)}`);
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
  }, [limit]);

  const loadMore = useCallback(() => setLimit((n) => n + PULSE_TERMINALS_PAGE), []);
  return { terminals, total, error, hasMore: terminals.length < total, loadMore };
}
