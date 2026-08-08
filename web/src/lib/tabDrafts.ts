/**
 * Per-tab chat input drafts.
 *
 * Deliberately a plain module-level map, NOT React state: drafts are read by
 * the "New session" button (to decide whether the active tab is "completely
 * empty") and restored when switching back to a tab. Neither consumer needs to
 * re-render on every keystroke, so storing the draft in a store would re-render
 * the whole chat tree on each character. The textarea's local state remains
 * the source of truth for rendering; this map is the durable mirror.
 */
const drafts = new Map<string, string>();

export function getDraft(tabId: string | null | undefined): string {
  if (!tabId) return "";
  return drafts.get(tabId) ?? "";
}

export function setDraft(tabId: string | null | undefined, draft: string) {
  if (!tabId) return;
  drafts.set(tabId, draft);
}

export function clearDraft(tabId: string | null | undefined) {
  if (!tabId) return;
  drafts.delete(tabId);
}

/**
 * Single source of truth for "is the active tab a completely empty new-session
 * tab" — the predicate the "New session" buttons use to decide whether to
 * reuse the tab (true) or stack a fresh one (false). A tab is empty when it is
 * still a `new-*` temp id (i.e. no session was created/restored from it yet)
 * AND it has no typed draft.
 */
export function isNewSessionTabEmpty(tabId: string | null | undefined): boolean {
  return !!tabId && tabId.startsWith("new-") && !getDraft(tabId).trim();
}
