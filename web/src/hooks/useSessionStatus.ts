import { useEffect } from "react";
import { useChatDispatch } from "../stores/chatStore";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";

/**
 * useSessionStatus — per-session status on tab activation.
 *
 * Given the active session id, fetches GET /api/sessions/:id/status into the
 * session's chatStore slice and patches from bus `status` events (which
 * SessionTabSync already routes). Switching back to a session whose slice is
 * cached refreshes it in the background instead of flashing a loader.
 *
 * Placeholder `new-*` tabs (no server session yet) skip the fetch entirely —
 * their slice is first populated by session_started/session_bootstrap events.
 *
 * Mount exactly once per active-tab scope (HomeApp) so activating a tab
 * triggers exactly one status fetch regardless of how many panels read the
 * slice.
 */
export function useSessionStatus(sessionId: string | null): void {
  const dispatch = useChatDispatch();

  useEffect(() => {
    if (!sessionId || sessionId.startsWith("new-")) return;
    let cancelled = false;
    let hadCached = false;

    const fetchStatus = (initial: boolean) => {
      // First fetch for this activation: show the loader unless the slice was
      // already populated (background refresh on tab switch-back).
      if (initial && !hadCached) {
        dispatch({ type: "SET_STATUS_LOADING", sessionId, loading: true });
      }
      api
        .getSessionStatus(sessionId)
        .then((status) => {
          if (cancelled) return;
          dispatch({ type: "SET_TUI_STATUS", sessionId, status });
        })
        .catch((err) => {
          if (!cancelled) console.warn(`failed to fetch status for session ${sessionId}`, err);
        })
        .finally(() => {
          if (!cancelled) dispatch({ type: "SET_STATUS_LOADING", sessionId, loading: false });
        });
    };

    fetchStatus(true);
    // Refresh on reconnect so status recovers after a disconnect.
    const offReconnect = eventBus.onReconnect(() => fetchStatus(false));
    return () => {
      cancelled = true;
      offReconnect();
    };
  }, [sessionId, dispatch]);
}
