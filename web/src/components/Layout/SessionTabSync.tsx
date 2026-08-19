import { useEffect, useRef } from "react";
import { useChatDispatch, useChatState } from "../../stores/chatStore";
import { useProjectState } from "../../stores/projectStore";
import { eventBus } from "../../lib/eventBus";
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
 */
export default function SessionTabSync() {
  const chatState = useChatState();
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

  // The router below is a stable closure (effect deps don't include
  // chatState); read current slices through this ref so it never sees stale
  // per-session state from the render that installed the listener.
  const chatStateRef = useRef(chatState);
  chatStateRef.current = chatState;

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

  return null;
}
