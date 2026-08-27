import { useEffect, useRef } from "react";
import { useChatDispatch, useChatStateRef } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { eventBus } from "../../lib/eventBus";
import { api } from "../../api/client";
import { applyReconcileState } from "../../hooks/useTurnWatchdog";
import {
  routeBusEnvelope,
  reconcileOpenSessions,
  ROUTABLE_EVENTS,
  type SessionEventRouter,
} from "../../lib/sessionEvents";

/**
 * SessionTabSync owns the cross-cutting behaviors of the multi-session tab
 * bar on the Home view:
 *
 * 1. Live event routing — one subscription on the shared event bus (the
 *    single `/api/events` EventSource). Every event is routed to its own
 *    session's slice in chatStore as long as that session has an open tab (in
 *    any project) — not just the currently active one, so background tabs
 *    keep streaming. The routing logic lives in `lib/sessionEvents.ts`.
 * 2. Tab title replacement — live status keeps tab labels current.
 * 3. Reconcile on reconnect — when the bus re-establishes (or detects a seq
 *    gap), every open session's turn state + transcript are refetched.
 * 4. Load-time reconcile — once per page load, as soon as restored tabs
 *    first appear, every open session's turn state + transcript are
 *    reconciled. A fresh page missed all prior events (including
 *    turn_started/turn_heartbeat), so without this turnActive would stay
 *    false and neither the stall watchdog nor the MERGE_SNAPSHOT mid-turn
 *    guard would arm until the next turn.
 */
export default function SessionTabSync() {
  const chatDispatch = useChatDispatch();
  const { state: projectState, dispatch: projectDispatch } = useProjectState();

  // All open tab ids across every project, recomputed each render so the bus
  // handler (a stable closure inside the effect below) always sees the
  // current set via this ref. `routeBusEnvelope` mutates it directly during a
  // session_started rekey so the first user_message echo isn't dropped.
  const openSessionIdsRef = useRef<Set<string>>(new Set());
  // Mutate the existing Set in place rather than reassigning — the router
  // closure below (installed once by the effect) holds a reference to this
  // exact Set instance. Reassigning it here would leave that closure's
  // "openSessionIds" stuck on whatever tabs existed at mount, permanently
  // dropping every event for tabs opened afterward.
  const liveTabIds = Object.values(projectState.tabsByProject).flat().map((tab) => tab.id);
  openSessionIdsRef.current.clear();
  liveTabIds.forEach((id) => openSessionIdsRef.current.add(id));

  // The router below is a stable closure (effect deps don't include chat
  // state); read current slices through this ref so it never sees stale
  // per-session state from the render that installed the listener. Also
  // purely imperative — this component renders nothing (returns null) — so
  // useChatStateRef must not force a re-render on every dispatch.
  const chatStateRef = useChatStateRef();

  useEffect(() => {
    const router: SessionEventRouter = {
      openSessionIds: openSessionIdsRef.current,
      dispatch: chatDispatch,
      projectDispatch,
      getState: () => chatStateRef.current,
    };
    // The bus dispatches per-event-type (no wildcard) — subscribe to every
    // event routeBusEnvelope handles individually.
    const offEnvelope = ROUTABLE_EVENTS.map((event) =>
      eventBus.on(event, (env) => routeBusEnvelope(env, router)),
    );
    const offReconnect = eventBus.onReconnect(() => {
      // Reconcile every open session: turn state + transcript refetch.
      void reconcileOpenSessions(openSessionIdsRef.current, chatDispatch);
    });
    return () => {
      offEnvelope.forEach((off) => off());
      offReconnect();
    };
  }, [chatDispatch, projectDispatch]);

  // Load-time reconcile (see docblock item 4). Tab hydration is itself a
  // mount effect (projectStore RESTORE_TABS), so this cannot simply run on
  // mount: the sorted-id key below is the trigger that fires when hydration
  // lands. The ref guard keeps re-renders and StrictMode double-mounts from
  // refetching.
  const realTabKey = liveTabIds.filter((id) => !id.startsWith("new-")).sort().join(",");
  const didLoadReconcileRef = useRef(false);
  useEffect(() => {
    if (didLoadReconcileRef.current || realTabKey === "") return;
    didLoadReconcileRef.current = true;
    void reconcileOpenSessions(new Set(openSessionIdsRef.current), chatDispatch);
  }, [chatDispatch, realTabKey]);

  // Activation state-sync: when the active tab lands on a real session,
  // fetch its authoritative turn state once. The routing gate only maintains
  // slices for tracked sessions, so a tab opened mid-turn may have missed
  // turn_started; this makes the streaming indicator correct on arrival.
  // Transcript catch-up stays ChatPanel's job — pulling a page here would
  // collapse scrollback-paginated history on every tab switch.
  const activeTabId = projectState.activeProject
    ? projectState.activeTabByProject[projectState.activeProject.path] ?? null
    : null;
  const prevActiveRef = useRef<string | null>(null);
  useEffect(() => {
    const prev = prevActiveRef.current;
    prevActiveRef.current = activeTabId;
    if (!activeTabId || activeTabId === prev || activeTabId.startsWith("new-")) return;
    let cancelled = false;
    api
      .getSessionState(activeTabId)
      .then((state) => {
        if (!cancelled) applyReconcileState(chatDispatch, activeTabId, state);
      })
      .catch(() => {
        // Server unreachable / session gone — the watchdog retries while the
        // tab is open and turn-active.
      });
    return () => {
      cancelled = true;
    };
  }, [activeTabId, chatDispatch]);

  return null;
}
