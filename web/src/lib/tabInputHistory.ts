/**
 * Per-tab history of the user's own submitted chat input — the web/desktop
 * equivalent of the TUI's `inputHistory` (internal/tui/model.go). Pressing ↑ in
 * the composer walks BACK through what was sent; ↓ walks FORWARD and, past the
 * newest entry, restores the draft that was in the box when navigation began.
 *
 * Like tabDrafts/tabQueue this is a plain module-level map, not React state:
 * the composer reads it imperatively on a key press and never renders from it,
 * so no subscriber plumbing is needed. It is deliberately NOT derived from the
 * transcript — that would surface the `!shell` output wrapper and the huge
 * server-assembled command prompts the composer sends, none of which are what
 * the user actually typed.
 *
 * Append rules mirror the TUI's on-Enter handler: `!shell` commands are not
 * recorded (they live in the transcript, and recalling one would invite
 * re-running it), empty text is ignored, and an immediate repeat of the
 * previous entry is collapsed.
 */
const histories = new Map<string, string[]>();

/** Per-tab cap so a long-lived session cannot grow the list without bound. */
export const MAX_INPUT_HISTORY = 200;

export function getInputHistory(tabId: string | null | undefined): string[] {
  if (!tabId) return [];
  return histories.get(tabId) ?? [];
}

/**
 * Record a submitted input. Callers pass the raw trimmed text; `!`-prefixed
 * shell commands are skipped here (parity with the TUI), so the composer can
 * call this unconditionally at the top of its submit handler.
 */
export function pushInputHistory(tabId: string | null | undefined, text: string): void {
  if (!tabId) return;
  const trimmed = text.trim();
  if (!trimmed || trimmed.startsWith("!")) return;
  const list = histories.get(tabId) ?? [];
  if (list[list.length - 1] === trimmed) return;
  list.push(trimmed);
  if (list.length > MAX_INPUT_HISTORY) list.splice(0, list.length - MAX_INPUT_HISTORY);
  histories.set(tabId, list);
}

/**
 * Move history from one tab id to another. A temp `new-*` tab becomes a real
 * session id on first send, and `/reset-id` re-keys an existing chat — without
 * this the history would be orphaned under the old id (and a slow leak).
 * Old entries are chronologically older, so they are prepended.
 */
export function rekeyInputHistory(
  oldId: string | null | undefined,
  newId: string | null | undefined,
): void {
  if (!oldId || !newId || oldId === newId) return;
  const list = histories.get(oldId);
  if (!list || list.length === 0) return;
  histories.delete(oldId);
  const existing = histories.get(newId) ?? [];
  const merged = [...list, ...existing];
  if (merged.length > MAX_INPUT_HISTORY) merged.splice(0, merged.length - MAX_INPUT_HISTORY);
  histories.set(newId, merged);
}

export function clearInputHistory(tabId: string | null | undefined): void {
  if (!tabId) return;
  histories.delete(tabId);
}
