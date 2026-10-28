import { useEffect } from "react";
import { api } from "../api/client";
import { eventBus } from "../lib/eventBus";
import type { TodoUpdatedEvent } from "../api/types";
import { getSessionPlan, setSessionPlan, useSessionPlan } from "../lib/sessionPlan";

// The chat's plan panel and the sidebar can be mounted for the same session at
// the same time. Without this cache both would seed with their own GET on
// mount; the key is host-qualified so a remote session and a local session that
// share an id never seed each other's plan.
const inflight = new Map<string, Promise<void>>();

// A malformed body must not reach the store: consumers index `plan.items`, and
// an unvalidated `{}` (a proxy-mocked or half-deployed server response) would
// throw during render and unmount the whole app. Same boundary rule as the
// Pulse store.
function isTodoUpdatedEvent(value: unknown): value is TodoUpdatedEvent {
  if (!value || typeof value !== "object") return false;
  return Array.isArray((value as { items?: unknown }).items);
}

function seedPlan(sessionId: string, host: string | undefined, replace: boolean): void {
  const key = `${host ?? ""}\u0000${sessionId}`;
  if (inflight.has(key)) return;
  const request = api
    .getSessionTodo(sessionId, host)
    .then((next) => {
      if (!isTodoUpdatedEvent(next)) {
        console.warn("session plan response was not a plan; keeping the last good one");
        return;
      }
      // A frame that arrived while the request was in flight is newer than the
      // response; only a reconnect reseed may overwrite it.
      if (replace || getSessionPlan(sessionId) === undefined) setSessionPlan(sessionId, next);
    })
    .catch((err) => {
      console.warn("failed to fetch session plan", err);
    })
    .finally(() => {
      if (inflight.get(key) === request) inflight.delete(key);
    });
  inflight.set(key, request);
}

/**
 * The session's todo plan, seeded from GET /api/sessions/{id}/todo and kept
 * live by the `todo_updated` event (routed into the same store by
 * sessionEvents.ts). A `new-*` id is a temp tab with nothing on the server.
 *
 * Shared by PlanPanel and SidebarTodoSection so both surfaces read one store
 * and one seed request.
 */
export function useSessionPlanLive(
  sessionId: string | null | undefined,
  host?: string,
): TodoUpdatedEvent | undefined {
  const plan = useSessionPlan(sessionId);
  useEffect(() => {
    if (!sessionId || sessionId.startsWith("new-")) return;
    seedPlan(sessionId, host, false);
    return eventBus.onReconnect(() => seedPlan(sessionId, host, true));
  }, [sessionId, host]);
  return plan;
}
