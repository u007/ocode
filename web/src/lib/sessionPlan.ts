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

/** Frames can reach the client out of order (the server publishes from a goroutine per write): keep the highest revision. */
export function setSessionPlan(sessionId: string, plan: TodoUpdatedEvent): void {
  const held = plans.get(sessionId);
  if (held && plan.revision < held.revision) return;
  plans.set(sessionId, plan);
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
