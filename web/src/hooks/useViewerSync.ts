import { useEffect } from "react";
import { api } from "../api/client";
import { readViewerState, subscribeViewerState, type ViewerState } from "../lib/viewerContext";
import { throttleTrailing } from "../lib/viewerDom";

const VIEWER_SYNC_MS = 300;

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Mirrors a chat session's viewer record to the server, so a Go tool can read
 * what the user is looking at mid-turn. A record change is pushed as a PUT with
 * a 300 ms trailing throttle; a cleared record is DELETEd at once and cancels
 * any pending PUT. Draft ("new-") sessions have no server session yet, so they
 * are skipped. The mount-time record is pushed once.
 *
 * Mount this after the effect that calls `setViewerDoc(sessionId, …)`: on
 * unmount that effect clears the record first, and this hook must still be
 * subscribed to send the DELETE.
 */
export function useViewerSync(sessionId: string | null, host?: string): void {
  useEffect(() => {
    if (!sessionId || sessionId.startsWith("new-")) return;
    const id = sessionId;

    const put = throttleTrailing((state: ViewerState) => {
      api.putSessionViewer(id, state, host).catch((err: unknown) => {
        console.warn("viewer sync: PUT failed", { sessionId: id, error: errorMessage(err) });
      });
    }, VIEWER_SYNC_MS);

    const onChange = () => {
      const state = readViewerState(id);
      if (state) {
        put(state);
        return;
      }
      put.cancel();
      api.deleteSessionViewer(id, host).catch((err: unknown) => {
        console.warn("viewer sync: DELETE failed", { sessionId: id, error: errorMessage(err) });
      });
    };

    const unsubscribe = subscribeViewerState(id, onChange);
    const initial = readViewerState(id);
    if (initial) put(initial);

    return () => {
      unsubscribe();
      put.cancel();
    };
  }, [sessionId, host]);
}
