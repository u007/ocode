import { useSyncExternalStore } from "react";

export type CompactionState =
  | { status: "active"; startedAt: number }
  | { status: "complete"; originalLen: number; compactedLen: number }
  | { status: "error"; error: string };

// Like tabQueue/tabDrafts, this is session-owned UI state, not transcript state.
// It survives SET_MESSAGES and composer remounts, but not a page reload.
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

export function useCompactionState(sessionId: string | null | undefined) {
  return useSyncExternalStore(subscribe, () => getCompactionState(sessionId));
}

export function isCompactCommand(text: string) {
  return text.trim().split(/\s+/, 1)[0].toLowerCase() === "/compact";
}
