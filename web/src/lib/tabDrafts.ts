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

/**
 * Prefix for the client-only temp tab id a brand-new chat gets before its
 * first message creates a real session on the server (see
 * `openNewSessionTab` in stores/projectStore.tsx). Nothing persisted ever
 * uses this prefix — a session id looks like `ses_2026-01-02-030405-abcd`.
 */
export const NEW_SESSION_TAB_PREFIX = "new-";

/**
 * True when `tabId` is a client-only temp tab id. Session-scoped API calls
 * must never be sent one: the server resolves ids against on-disk sessions
 * and 404s with "session not found".
 */
export function isTempSessionTabId(tabId: string | null | undefined): boolean {
  return !!tabId && tabId.startsWith(NEW_SESSION_TAB_PREFIX);
}

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

/** Move a draft from one tab id to another — the temp `new-*` id becomes a
 *  real session id on first send. Without this the typed draft would be
 *  orphaned under the temp id forever (a slow per-tab leak). */
export function rekeyDraft(oldId: string | null | undefined, newId: string | null | undefined) {
  if (!oldId || !newId || oldId === newId) return;
  const draft = drafts.get(oldId);
  if (draft === undefined) return;
  drafts.delete(oldId);
  if (!drafts.has(newId)) drafts.set(newId, draft);
}

/**
 * Single source of truth for "is the active tab a completely empty new-session
 * tab" — the predicate the "New session" buttons use to decide whether to
 * reuse the tab (true) or stack a fresh one (false). A tab is empty when it is
 * still a `new-*` temp id (i.e. no session was created/restored from it yet)
 * AND it has no typed draft.
 */
export function isNewSessionTabEmpty(tabId: string | null | undefined): boolean {
  return isTempSessionTabId(tabId) && !getDraft(tabId).trim();
}
