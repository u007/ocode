import { useEffect, useRef } from "react";
import { useChatDispatch, useChatState, getTurnState, type ChatAction } from "../stores/chatStore";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";

/** Stall threshold: no turn_heartbeat for this long while a turn is active
 *  means the stream (or the turn) is stuck. */
export const STALL_THRESHOLD_MS = 30_000;
/** How often the watchdog re-checks. */
const WATCHDOG_INTERVAL_MS = 5_000;

/**
 * applyReconcileState — pure application of a GET /api/sessions/:id/state
 * snapshot to the store. Exported for unit tests.
 */
export function applyReconcileState(
  dispatch: (a: ChatAction) => void,
  sessionId: string,
  state: { bootstrap_stage: string; turn_active: boolean; last_seq: number },
): void {
  if (!state.turn_active) {
    // Server-side turn is done — clear streaming + turn state.
    dispatch({ type: "SET_TURN_STATE", sessionId, turnActive: false });
    dispatch({ type: "SET_STREAMING", sessionId, isStreaming: false });
    dispatch({ type: "SET_TURN_STALLED", sessionId, stalled: false });
  } else if (state.bootstrap_stage) {
    dispatch({ type: "SET_BOOTSTRAP_STAGE", sessionId, stage: state.bootstrap_stage });
  }
}

/**
 * useTurnWatchdog — per-session streaming watchdog.
 *
 * While a session's turn is active (turn state from the store, patched by bus
 * turn_heartbeat events), no heartbeat within STALL_THRESHOLD_MS marks the
 * turn "stalled" and reconciles against GET /api/sessions/:id/state:
 * - reconcile reports turn_active: false → the streaming spinner clears (the
 *   server-side turn finished but its terminal event was lost).
 * - reconcile reports turn_active: true → the turn is still running server-
 *   side; we keep the stalled marker (a fresh heartbeat clears it) and retry
 *   on the next tick.
 *
 * Reconcile also runs on bus reconnect (eventBus.onReconnect) and on tab
 * activation of a turn-active session, per the Part 05 design.
 */
export function useTurnWatchdog(sessionId: string | null): void {
  const dispatch = useChatDispatch();
  const chatState = useChatState();
  const stateRef = useRef(chatState);
  stateRef.current = chatState;

  useEffect(() => {
    if (!sessionId || sessionId.startsWith("new-")) return;
    let cancelled = false;

    const reconcile = async () => {
      try {
        const state = await api.getSessionState(sessionId);
        if (cancelled) return;
        applyReconcileState(dispatch, sessionId, state);
      } catch (err) {
        if (!cancelled) console.warn(`turn watchdog: reconcile failed for ${sessionId}`, err);
      }
    };

    // Reconcile on reconnect and on activation of a turn-active session.
    const offReconnect = eventBus.onReconnect(() => void reconcile());
    if (getTurnState(stateRef.current, sessionId).turnActive) void reconcile();

    const interval = window.setInterval(() => {
      if (cancelled) return;
      const turn = getTurnState(stateRef.current, sessionId);
      if (!turn.turnActive) return; // idle — nothing to watch
      const now = Date.now();
      const lastBeat = turn.lastHeartbeatAt ?? now;
      if (now - lastBeat > STALL_THRESHOLD_MS) {
        if (!turn.turnStalled) {
          console.warn(
            `turn watchdog: session ${sessionId} stalled — no heartbeat for ${STALL_THRESHOLD_MS / 1000}s; reconciling`,
          );
          dispatch({ type: "SET_TURN_STALLED", sessionId, stalled: true });
        }
        void reconcile();
      }
    }, WATCHDOG_INTERVAL_MS);

    return () => {
      cancelled = true;
      clearInterval(interval);
      offReconnect();
    };
  }, [sessionId, dispatch]);
}
