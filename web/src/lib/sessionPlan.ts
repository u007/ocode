import { useSyncExternalStore } from "react";
import type { TodoUpdatedEvent } from "../api/types";

// A session's todo plan as the chat's plan panel shows it. Session-owned UI
// state like compactionState: the `todo_updated` event (routeBusEnvelope) and
// the panel's initial GET /api/sessions/{id}/todo both write here. A session
// with no entry has not been loaded yet; one with zero items has no plan.
const plans = new Map<string, TodoUpdatedEvent>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};
const notify = () => listeners.forEach((listener) => listener());

export function getSessionPlan(sessionId: string | null | undefined): TodoUpdatedEvent | undefined {
  return sessionId ? plans.get(sessionId) : undefined;
}

/**
 * Runtime shape check for the single store boundary. Both writers (the seed GET
 * and the `todo_updated` event) go through setSessionPlan, so a half-deployed
 * or proxy-mocked `{}` can never reach a renderer that indexes plan.items.
 */
export function isTodoUpdatedEvent(value: unknown): value is TodoUpdatedEvent {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.session_id === "string" &&
    typeof v.revision === "number" &&
    Number.isFinite(v.revision) &&
    typeof v.done === "number" &&
    Number.isFinite(v.done) &&
    typeof v.total === "number" &&
    Number.isFinite(v.total) &&
    Array.isArray(v.items)
  );
}

/**
 * Frames can reach the client out of order (the server publishes from a
 * goroutine per write): keep the highest revision. A revision of 0 is the
 * server's "no plan" answer and always applies, otherwise a removed plan could
 * never clear. `force` is the reconnect reseed: the server's current snapshot is
 * authoritative even when its revision is lower (a recreated file restarts).
 */
export function setSessionPlan(
  sessionId: string,
  plan: TodoUpdatedEvent,
  opts?: { force?: boolean },
): void {
  if (!isTodoUpdatedEvent(plan)) {
    console.warn("ignoring malformed session plan", { sessionId, plan });
    return;
  }
  if (plan.session_id !== sessionId) {
    console.warn("ignoring session plan for another session", { sessionId, planSessionId: plan.session_id });
    return;
  }
  const held = plans.get(sessionId);
  if (!opts?.force && held && plan.revision > 0 && plan.revision < held.revision) return;
  plans.set(sessionId, plan);
  notify();
}

/** Drop a closed session's plan so the map does not retain every tab ever opened. */
export function forgetSessionPlan(sessionId: string): void {
  if (!plans.delete(sessionId)) return;
  notify();
}

/** Move a plan to the session's new id after /reset-id. */
export function rekeySessionPlan(oldId: string, newId: string): void {
  const plan = plans.get(oldId);
  if (!plan) return;
  plans.delete(oldId);
  plans.set(newId, { ...plan, session_id: newId });
  notify();
}

export function useSessionPlan(sessionId: string | null | undefined): TodoUpdatedEvent | undefined {
  return useSyncExternalStore(
    subscribe,
    () => getSessionPlan(sessionId),
    () => undefined,
  );
}
