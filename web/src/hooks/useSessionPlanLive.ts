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
// throw during render and unmount the whole app. The check lives in
// setSessionPlan, the single writer for both the seed and the event path.
function seedPlan(sessionId: string, host: string | undefined, replace: boolean): void {
  const key = `${host ?? ""}\u0000${sessionId}`;
  // Expanding the sidebar after the chat seeded must not fetch again.
  if (!replace && getSessionPlan(sessionId) !== undefined) return;
  if (inflight.has(key) && !replace) return;
  // A reconnect reseed supersedes an in-flight request: a stalled GET (fetchJSON
  // has no timeout) must not disable reseeding for the SPA's lifetime.
  const request = api
    .getSessionTodo(sessionId, host)
    .then((next) => {
      setSessionPlan(sessionId, next, { force: replace });
    })
    .catch((err) => {
      console.warn("failed to fetch session plan", err);
    })
    .finally(() => {
      if (inflight.get(key) === request) inflight.delete(key);
    });
  inflight.set(key, request);
}

/** Drop in-flight seeds for a session id that was closed or rekeyed. */
export function forgetPlanSeed(sessionId: string): void {
  const suffix = `\u0000${sessionId}`;
  for (const key of inflight.keys()) {
    if (key.endsWith(suffix)) inflight.delete(key);
  }
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
