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
