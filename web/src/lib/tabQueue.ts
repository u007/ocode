/**
 * Per-tab queue of messages/commands typed while a turn is busy (streaming or
 * a `!shell` command is in flight). Mirrors the TUI's unified `queuedItems`
 * (internal/tui/model.go) so the web/desktop UI has the same behavior: typing
 * while busy queues instead of being blocked, the queue auto-drains in order
 * once the turn frees up, and the most recently queued item can be recalled
 * into the input box (web equivalent of the TUI's up-arrow restore).
 *
 * Plain module-level map, not React state, for the same reason as tabDrafts:
 * mutations are driven from ChatInput itself, which mirrors count into local
 * state after each mutation, so no separate subscriber plumbing is needed.
 */
export interface QueuedItem {
  /** "command": raw text incl. `/` or `!` prefix, replayed through the same
   *  dispatch logic as an immediate send. "message": already-resolved final
   *  text (refs/context baked in at queue time), sent as-is. */
  kind: "command" | "message";
  text: string;
  /** Set when the item was also handed straight to a running agent's live
   *  loop (e.g. a message typed while streaming, mirrored from the TUI's
   *  EnqueueInjection). Such items stay in the queue only so they can be
   *  recalled via up-arrow before the server echoes them back; the auto-drain
   *  backstop skips them so they are never sent a second time. */
  dispatched?: boolean;
}

const queues = new Map<string, QueuedItem[]>();

export function getQueue(tabId: string | null | undefined): QueuedItem[] {
  if (!tabId) return [];
  return queues.get(tabId) ?? [];
}

export function pushQueued(tabId: string | null | undefined, item: QueuedItem) {
  if (!tabId) return;
  const list = queues.get(tabId) ?? [];
  list.push(item);
  queues.set(tabId, list);
}

/** Dequeue the oldest item (FIFO) — used when auto-draining after a turn ends. */
export function shiftQueued(tabId: string | null | undefined): QueuedItem | undefined {
  if (!tabId) return undefined;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return undefined;
  return list.shift();
}

/** Dequeue the oldest item that has NOT already been handed to the running
 *  agent's live loop (FIFO). Dispatched items (messages typed while streaming,
 *  mirrored from the TUI's EnqueueInjection) are silently dropped because the
 *  server already has them — this is the drain backstop that guarantees a
 *  dispatched message is never sent a second time. Returns undefined when only
 *  dispatched items remain (all of which are discarded). */
export function shiftUndispatched(tabId: string | null | undefined): QueuedItem | undefined {
  if (!tabId) return undefined;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return undefined;
  while (list.length > 0) {
    const item = list.shift()!;
    if (!item.dispatched) return item;
  }
  return undefined;
}

/** Put a failed item back at the head of its queue so a transient submit
 * failure cannot silently drop user input. */
export function unshiftQueued(tabId: string | null | undefined, item: QueuedItem) {
  if (!tabId) return;
  const list = queues.get(tabId) ?? [];
  list.unshift(item);
  queues.set(tabId, list);
}

/** Remove a specific queued item by reference (e.g. a dispatched message whose
 *  submit was rejected, so it never reached the live loop and must not linger
 *  as a phantom entry the drain would skip). */
export function removeQueuedItem(tabId: string | null | undefined, item: QueuedItem) {
  if (!tabId) return;
  const list = queues.get(tabId);
  if (!list) return;
  const idx = list.indexOf(item);
  if (idx !== -1) list.splice(idx, 1);
}

/** Remove the first dispatched entry matching text. Called when the server's
 *  `user_message` echo confirms the agent actually picked an injected message
 *  up (web mirror of the TUI's removeQueuedInputByText). The entry existed
 *  only for up-arrow recall while the message sat in the live loop; once the
 *  agent has spliced it in, the queue must stop showing it (and the drain
 *  backstop no longer has to discard it). Undispatched entries are never
 *  matches — they have not been sent and must drain normally. Returns true
 *  when an entry was removed. */
export function removeDispatchedQueuedByText(
  tabId: string | null | undefined,
  text: string,
): boolean {
  if (!tabId) return false;
  const list = queues.get(tabId);
  if (!list) return false;
  const idx = list.findIndex((item) => item.dispatched && item.kind === "message" && item.text === text);
  if (idx === -1) return false;
  list.splice(idx, 1);
  return true;
}

/** Window event fired when a queue is mutated outside ChatInput (currently by
 *  sessionEvents when an injected message's pickup echo arrives). ChatInput
 *  listens and re-syncs its queueCount so the "N queued" line stays accurate
 *  — same sibling-bridge pattern as inputRestore's RESTORE_EVENT. */
export const QUEUE_CHANGED_EVENT = "ocode:queue-changed";

export interface QueueChangedDetail {
  tabId: string;
}

export function dispatchQueueChanged(tabId: string | null | undefined) {
  if (!tabId) return;
  window.dispatchEvent(
    new CustomEvent<QueueChangedDetail>(QUEUE_CHANGED_EVENT, {
      detail: { tabId },
    }),
  );
}

/** Pop the most recently queued item (LIFO) — used to recall it into the
 *  input box via the up-arrow restore. Mirrors the TUI's up-arrow queue
 *  restore, which restores the last queued item *regardless of kind* (command
 *  or message) and removes it from the queue so resubmitting the recalled text
 *  doesn't duplicate it. Restoring a command simply puts its raw text (incl.
 *  the `/` or `!` prefix) back into the input for editing or re-send, which is
 *  exactly what the TUI does. */
export function popLastQueued(tabId: string | null | undefined): QueuedItem | undefined {
  if (!tabId) return undefined;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return undefined;
  return list.splice(list.length - 1, 1)[0];
}

/** Restore `item` and every later queued MESSAGE into the composer draft,
 *  leaving anything still queued ahead of them (FIFO) untouched.
 *
 *  Why a suffix and not just the clicked item: the queue drains oldest-first, so
 *  pulling back a later item while an earlier one is still pending would send
 *  the restored text after the item the user clicked past. Restoring the
 *  clicked item plus everything after it keeps submission order intact —
 *  clicking the first queued item therefore restores the whole queue, and
 *  clicking the last restores only the last.
 *
 *  Commands are deliberately left queued (same rule as
 *  `drainQueuedMessagesIntoDraft`): a `/compact` or `!ls` inlined into a
 *  multi-message composer block becomes prose and would never execute. A suffix
 *  with no messages removes nothing and returns null.
 *
 *  `dispatched` messages are restored too, matching `popLastQueued`: they stay
 *  recallable until the server echoes that the agent picked them up.
 *
 *  Addressed by the item itself rather than a numeric index: the queue can drain
 *  between the render that drew the row and the click, and a stale index would
 *  then restore a DIFFERENT suffix. A reference that is no longer queued is a
 *  no-op. */
export function restoreQueuedFrom(
  tabId: string | null | undefined,
  item: QueuedItem,
  draft: string,
): MergedDraft | null {
  if (!tabId) return null;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return null;
  const index = list.indexOf(item);
  if (index === -1) return null;
  const texts = list.slice(index)
    .filter((entry) => entry.kind === "message")
    .map((entry) => entry.text);
  if (texts.length === 0) return null;
  // Keep the untouched prefix and any commands from the restored suffix, in
  // their original relative order, so they still auto-drain normally.
  queues.set(tabId, list.filter((entry, i) => i < index || entry.kind === "command"));
  return mergeQueuedIntoDraft(texts, draft);
}

/** Move a queue from one tab id to another. Used when a temp `new-*` tab becomes
 *  a real session: ChatInput queues by the temp id, and without the rekey those
 *  queued items would be orphaned under the old id and never drain. */
export function rekeyQueue(oldId: string | null | undefined, newId: string | null | undefined) {
  if (!oldId || !newId || oldId === newId) return;
  const list = queues.get(oldId);
  if (!list || list.length === 0) return;
  queues.delete(oldId);
  const existing = queues.get(newId) ?? [];
  queues.set(newId, [...existing, ...list]);
}

export function clearQueue(tabId: string | null | undefined) {
  if (!tabId) return;
  queues.delete(tabId);
}

export interface MergedDraft {
  /** The new composer text: queued messages, then the user's own draft. */
  value: string;
  /** Where the user's own draft starts in `value`. The composer shifts the
   *  caret by this many units so it stays anchored to their text. */
  draftStart: number;
}

/** Merge queued message text into the composer draft, queued first.
 *
 * A compaction pass that fails never reaches the LLM, so anything the user
 * submitted while it was running still exists ONLY in this queue. Dropping it
 * would lose input with no trace, so the failure path moves it back into the
 * composer.
 *
 * Order is deliberate: queued messages were submitted earlier, so restoring them
 * in submission order keeps the block chronological, and the user's own draft
 * stays the suffix — the text they are looking at and about to keep typing is
 * not buried under restored history.
 *
 * Blank entries are dropped rather than turned into blank lines, so a
 * whitespace-only item cannot leave a leading or doubled separator behind. */
export function mergeQueuedIntoDraft(queued: string[], draft: string): MergedDraft {
  const parts = queued.filter((q) => q.trim() !== "").map((q) => q.trim());
  if (parts.length === 0) return { value: draft, draftStart: 0 };
  const block = parts.join("\n");
  if (draft === "") return { value: block, draftStart: block.length };
  return { value: `${block}\n${draft}`, draftStart: block.length + 1 };
}

/** Remove every queued MESSAGE (leaving commands queued and dispatchable) and
 *  return the composer text they should be merged into, or null when there was
 *  nothing to restore.
 *
 * Commands are deliberately left alone: a "/compact" or "!ls" is not composer
 * prose, inlining it would corrupt the merged block, and it stays available for
 * the user to run. */
export function drainQueuedMessagesIntoDraft(
  tabId: string | null | undefined,
  draft: string,
): MergedDraft | null {
  if (!tabId) return null;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return null;
  const texts: string[] = [];
  const keep: QueuedItem[] = [];
  for (const item of list) {
    if (item.kind === "command") keep.push(item);
    else texts.push(item.text);
  }
  if (texts.length === 0) return null;
  queues.set(tabId, keep);
  return mergeQueuedIntoDraft(texts, draft);
}
