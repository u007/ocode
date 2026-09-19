import { useSyncExternalStore } from "react";

/**
 * actionErrors — a tiny app-wide surface for "a settings action failed" so the
 * web/desktop can show the user an error instead of only logging it.
 *
 * Context: every sidebar toggle and every model pick is a fire-and-forget API
 * write whose failure was swallowed by `.catch(console.error)`. That is
 * invisible in the UI — a 404 (e.g. a remote session whose write reached the
 * wrong server) or a 500 left the control silently unchanged, so the user
 * could not tell an ignored click from a rejected one. The ModelDialog also
 * closes on pick, so a local `useState` error there would unmount with the
 * dialog before it could be read.
 *
 * This mirrors the module-level store pattern already used by
 * `compactionState.ts` / `tabQueue.ts` (listeners + useSyncExternalStore): the
 * error outlives whichever component raised it, and one mounted toast renders
 * it. It holds only the most recent error — standard toast semantics — and is
 * deliberately sticky until dismissed or replaced, matching the Git panel's
 * error contract (a failure stays visible; successes auto-clear).
 */

export interface ActionError {
  /** Human-readable, already includes the failing action and the reason. */
  message: string;
  /** `Date.now()` when reported — lets a test assert replacement ordering. */
  at: number;
}

let current: ActionError | null = null;
const listeners = new Set<() => void>();

function emit(): void {
  listeners.forEach((listener) => listener());
}

/** Formats a failure for display, naming the action and surfacing the HTTP
 *  status for a non-2xx response (the 404 case in particular is otherwise
 *  indistinguishable from a network error in the message).
 *
 *  The status is detected structurally rather than with `instanceof ApiError`
 *  so this module has no import on `api/client` — test suites that mock that
 *  module (without re-exporting ApiError) can still report an error. */
export function describeActionError(e: unknown, what: string): string {
  const message = e instanceof Error && e.message ? e.message : "";
  const status =
    typeof e === "object" && e !== null && typeof (e as { status?: unknown }).status === "number"
      ? (e as { status: number }).status
      : 0;
  if (!message) return `${what} failed`;
  return status ? `${what} failed: ${message} (HTTP ${status})` : `${what} failed: ${message}`;
}

/** Report a failed settings action. Sticky until dismissed or superseded. */
export function reportActionError(e: unknown, what: string): void {
  current = { message: describeActionError(e, what), at: Date.now() };
  emit();
}

/** Report an already-formatted message (callers that built their own text). */
export function reportActionErrorMessage(message: string): void {
  current = { message, at: Date.now() };
  emit();
}

export function dismissActionError(): void {
  if (!current) return;
  current = null;
  emit();
}

export function getActionError(): ActionError | null {
  return current;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Subscribe to the latest action error. Returns null when there is none. */
export function useActionError(): ActionError | null {
  return useSyncExternalStore(subscribe, getActionError, () => null);
}

/** Test-only: drop the current error without rendering the toast. */
export function resetActionErrors(): void {
  current = null;
  emit();
}
