import { useEffect, useRef } from "react";
import { useChatDispatch, useChatStateRef } from "../stores/chatStore";
import { useProjectDispatch } from "../stores/projectStore";
import { revalidateSession, type SessionEventRouter } from "../lib/sessionEvents";
import { onWake } from "../lib/wakeSignal";

/**
 * How often an open session is revalidated against its stored revision. Slow
 * on purpose: this is the safety net for writes made by ANOTHER ocode process
 * sharing the project (desktop + `make dev` server + TUI), not the in-process
 * live path, which is already push-based over the shared event bus.
 */
export const REVISION_POLL_MS = 15_000;

/**
 * Cross-process session sync. Every open tab (any project, active or
 * backgrounded) is polled against `GET /api/sessions/:id/state`; when the
 * stored transcript revision moved since that session's transcript was last
 * fetched — an out-of-process `/compact` or turn — the transcript is refetched
 * and merged (see `revalidateSession`).
 *
 * Also runs an immediate pass on wake (`online` / `visibilitychange`), so
 * returning to the desktop window converges without waiting out the interval.
 * A Wails/WKWebView window does not always fire `visibilitychange` on OS focus
 * changes, so the interval is the reliable path and the wake pass is the
 * fast-follow.
 *
 * In-process activity is unaffected: it arrives over the event bus, and the
 * revision check then finds nothing to do.
 */
export function useSessionRevisionSync(
  openSessionIds: ReadonlySet<string>,
  hostById?: ReadonlyMap<string, string | undefined>,
): void {
  const dispatch = useChatDispatch();
  const projectDispatch = useProjectDispatch();
  // Read through refs so the interval/wake closures always see the current
  // tabs without re-subscribing on every tab change. Both are purely
  // imperative (read inside the tick, not JSX), so they must not re-render
  // this always-mounted hook's owner.
  const stateRef = useChatStateRef();
  const idsRef = useRef(openSessionIds);
  idsRef.current = openSessionIds;
  const hostsRef = useRef(hostById);
  hostsRef.current = hostById;

  useEffect(() => {
    const router: Pick<SessionEventRouter, "dispatch" | "getState" | "hostFor" | "projectDispatch"> = {
      dispatch,
      getState: () => stateRef.current,
      hostFor: (sessionId) => hostsRef.current?.get(sessionId),
      // The authoritative title from the refetched detail is applied to the tab
      // strip here too, so a title written by another ocode process relabels
      // background/inactive tabs (see revalidateSession / applySessionTabTitle).
      projectDispatch,
    };
    // One pass over all open sessions, deduped per session so a slow /state
    // can't stack up if the interval and a wake fire close together.
    const inFlight = new Set<string>();
    const pass = () => {
      for (const sessionId of idsRef.current) {
        if (!sessionId || sessionId.startsWith("new-") || inFlight.has(sessionId)) continue;
        inFlight.add(sessionId);
        void revalidateSession(sessionId, router)
          .catch((err) => {
            console.warn(`session revision sync: revalidate failed for ${sessionId}`, err);
          })
          .finally(() => inFlight.delete(sessionId));
      }
    };
    const interval = window.setInterval(pass, REVISION_POLL_MS);
    const offWake = onWake(pass);
    return () => {
      clearInterval(interval);
      offWake();
    };
  }, [dispatch, projectDispatch]);
}
