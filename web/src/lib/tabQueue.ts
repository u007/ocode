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

/** Pop the most recently queued item (LIFO) — used to recall it into the input box. */
export function popLastQueued(tabId: string | null | undefined): QueuedItem | undefined {
  if (!tabId) return undefined;
  const list = queues.get(tabId);
  if (!list || list.length === 0) return undefined;
  return list.pop();
}

export function clearQueue(tabId: string | null | undefined) {
  if (!tabId) return;
  queues.delete(tabId);
}
