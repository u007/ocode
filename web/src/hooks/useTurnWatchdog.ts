import { useEffect, useRef } from "react";
import {
  useChatDispatch,
  useChatStateRef,
  getTurnState,
  type ChatAction,
  type ChatState,
} from "../stores/chatStore";
import { api } from "../api/client";
import { RECONCILE_PAGE_SIZE } from "../lib/sessionEvents";

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

async function reconcileSession(
  dispatch: (a: ChatAction) => void,
  getState: () => ChatState,
  sessionId: string,
): Promise<void> {
  try {
    const wasActive = getTurnState(getState(), sessionId).turnActive;
    const state = await api.getSessionState(sessionId);
    applyReconcileState(dispatch, sessionId, state);
    if (!state.turn_active && wasActive) {
      // The turn finished server-side but its terminal events were lost
      // (missed turn_done + turn-boundary messages broadcast). Recovery is
      // refetch, never replay: pull the committed transcript. The merge is
      // dispatched after SET_TURN_STATE flipped turnActive, so the reducer's
      // mid-turn guard does not hold it back.
      const detail = await api.getSession(sessionId, { limit: RECONCILE_PAGE_SIZE });
      dispatch({
        type: "MERGE_SNAPSHOT",
        sessionId,
        messages: detail.messages,
        total: detail.total,
      });
    }
  } catch (err) {
    console.warn(`turn watchdog: reconcile failed for ${sessionId}`, err);
  }
}

/** One stall-detection pass over every session currently held open (any
 *  project, active or backgrounded) — exported for unit tests. */
export function runWatchdogTick(
  sessionIds: Iterable<string>,
  getState: () => ChatState,
  dispatch: (a: ChatAction) => void,
): void {
  const now = Date.now();
  for (const sessionId of sessionIds) {
    if (!sessionId || sessionId.startsWith("new-")) continue;
    const turn = getTurnState(getState(), sessionId);
    if (!turn.turnActive) continue; // idle — nothing to watch
    const lastBeat = turn.lastHeartbeatAt ?? now;
    if (now - lastBeat > STALL_THRESHOLD_MS) {
      if (!turn.turnStalled) {
        console.warn(
          `turn watchdog: session ${sessionId} stalled — no heartbeat for ${STALL_THRESHOLD_MS / 1000}s; reconciling`,
        );
        dispatch({ type: "SET_TURN_STALLED", sessionId, stalled: true });
      }
      void reconcileSession(dispatch, getState, sessionId);
    }
  }
}

/**
 * useTurnWatchdogAll — streaming watchdog for every open session at once.
 *
 * A single interval covers every session id in `openSessionIds` (all open
 * tabs, any project) instead of only the currently active tab — a turn
 * stalling in a backgrounded tab must still get reconciled, not just one the
 * user happens to be looking at. `openSessionIds` is read through a ref so
 * newly opened/closed tabs are picked up without resetting the interval.
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
 * Bus-reconnect reconcile for open sessions is already handled by
 * SessionTabSync's `reconcileOpenSessions`, so this hook only owns the
 * periodic stall check.
 */
export function useTurnWatchdogAll(openSessionIds: ReadonlySet<string>): void {
  const dispatch = useChatDispatch();
  // Purely imperative (read inside the interval tick, not JSX) — must not
  // re-render this always-mounted hook's owner on every dispatch.
  const stateRef = useChatStateRef();
  const idsRef = useRef(openSessionIds);
  idsRef.current = openSessionIds;

  useEffect(() => {
    const tick = () => runWatchdogTick(idsRef.current, () => stateRef.current, dispatch);
    tick(); // cover sessions that were already stalled/turn-active at mount
    const interval = window.setInterval(tick, WATCHDOG_INTERVAL_MS);
    return () => clearInterval(interval);
  }, [dispatch]);
}
