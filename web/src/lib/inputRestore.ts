/**
 * inputRestore — cross-component bridge for "restore older user message to composer".
 *
 * ChatPanel/MessageBubble and ChatInput are siblings per tab under App.tsx with
 * no shared React state. The composer draft lives as local state in ChatInput
 * plus a module-level map (tabDrafts). To avoid lifting draft state into
 * App.tsx or creating a dedicated store for a single action, restoration uses a
 * typed window CustomEvent.
 *
 * Contract:
 * - Trigger: user clicks "Restore" on an older user message bubble → confirmation dialog → on confirm dispatches.
 * - Payload: selected text, its absolute server target, and the durable user
 *   sequence when the transcript provides one.
 * - Semantics: replace (not append) the current draft for that sessionId. Focus moves to textarea, cursor at end.
 * - Protection: ChatInput only applies the event when detail.sessionId === its own sessionTabId, so hidden tabs don't mutate.
 * - Lifecycle: listener is added in useEffect and removed on unmount.
 */
export const RESTORE_EVENT = "ocode:restore-draft";

export interface RestoreDetail {
  sessionId: string;
  text: string;
  /** Absolute index in the server's full transcript. */
  targetIndex: number;
  userSeq?: number;
}

/** Translate a loaded-window entry to the server's absolute transcript index.
 * An unknown window anchor must fail closed: using the window-relative index
 * would silently target the wrong user message in a partially paginated chat. */
export function absoluteRestoreTarget(windowStartServerIndex: number, entryIndex: number): number | null {
  if (!Number.isSafeInteger(windowStartServerIndex) || windowStartServerIndex < 0) return null;
  if (!Number.isSafeInteger(entryIndex) || entryIndex < 0) return null;
  const target = windowStartServerIndex + entryIndex;
  return Number.isSafeInteger(target) ? target : null;
}

export function dispatchRestore(detail: RestoreDetail) {
  window.dispatchEvent(
    new CustomEvent<RestoreDetail>(RESTORE_EVENT, {
      detail,
    }),
  );
}
