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
 * - Payload: { sessionId, text } where text is the raw message.content to replace the composer with.
 * - Semantics: replace (not append) the current draft for that sessionId. Focus moves to textarea, cursor at end.
 * - Protection: ChatInput only applies the event when detail.sessionId === its own sessionTabId, so hidden tabs don't mutate.
 * - Lifecycle: listener is added in useEffect and removed on unmount.
 */
export const RESTORE_EVENT = "ocode:restore-draft";

export interface RestoreDetail {
  sessionId: string;
  text: string;
  /** Index in the messages array of the restored message. Truncation keeps messages[0 .. index-1]. */
  index?: number;
}

export function dispatchRestore(sessionId: string, text: string, index?: number) {
  window.dispatchEvent(
    new CustomEvent<RestoreDetail>(RESTORE_EVENT, {
      detail: { sessionId, text, index },
    }),
  );
}
