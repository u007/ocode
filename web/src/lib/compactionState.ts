import { useSyncExternalStore } from "react";

export type CompactionState =
  | { status: "active"; startedAt: number }
  | { status: "error"; error: string };

// Like tabQueue/tabDrafts, this is session-owned UI state, not transcript state.
// It survives SET_MESSAGES and composer remounts, but not a page reload.
//
// There is deliberately no "complete" state: a finished compaction is reported
// by the persisted compaction-summary notice in the transcript (see
// CompactionNotice), so the composer bar is simply dropped on success instead
// of retaining a dismissible banner.
const states = new Map<string, CompactionState>();
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

export function getCompactionState(sessionId: string | null | undefined) {
  return sessionId ? states.get(sessionId) : undefined;
}

export function setCompactionState(sessionId: string, state: CompactionState) {
  states.set(sessionId, state);
  listeners.forEach((listener) => listener());
}

export function dismissCompaction(sessionId: string) {
  // An active operation cannot be hidden (or unlocked) by dismissal.
  if (states.get(sessionId)?.status === "active") return;
  states.delete(sessionId);
  listeners.forEach((listener) => listener());
}

/**
 * clearCompaction unconditionally drops a session's compaction state, including
 * an active one. Used when a manual /compact succeeds: completion is reported
 * by the persisted compaction-summary notice in the transcript, so the composer
 * bottom bar must not be retained (mirrors the TUI, which keeps no completion
 * bar). Still fires listeners so the queue drain sees the busy→idle edge.
 */
export function clearCompaction(sessionId: string) {
  if (!states.delete(sessionId)) return;
  listeners.forEach((listener) => listener());
}

export function useCompactionState(sessionId: string | null | undefined) {
  return useSyncExternalStore(subscribe, () => getCompactionState(sessionId));
}

export function isCompactCommand(text: string) {
  return text.trim().split(/\s+/, 1)[0].toLowerCase() === "/compact";
}
