import { useSyncExternalStore } from "react";

export type CompactionState =
  | { status: "active"; startedAt: number }
  | { status: "error"; error: string };

// Like tabQueue/tabDrafts, this is session-owned UI state, not transcript state.
// It survives SET_MESSAGES and composer remounts. Server lifecycle events and
// /state reconciliation can now hydrate it for clients that did not initiate
// the operation, but a page with no server state still starts empty.
//
// There is deliberately no "complete" state: a finished compaction is reported
// by the persisted compaction-summary notice in the transcript (see
// CompactionNotice), so the composer bar is simply dropped on success instead
// of retaining a dismissible banner.
const states = new Map<string, CompactionState>();
const listeners = new Set<() => void>();
// Monotonic per-session mutation marker used to discard a /state response that
// started before a newer lifecycle event arrived. Without this, a slow poll
// that observed compacting:false could clear an active indicator after the
// start event won the race.
const eventVersions = new Map<string, number>();
const localPending = new Set<string>();
// Highest lifecycle generation observed per session. A `compaction_done`
// from a previous group can be published after a newer `compaction_started`
// (the publish happens outside the server's session lock), so the client must
// ignore any done older than the latest start.
const compactionGenerations = new Map<string, number>();
const finishedCompactionGenerations = new Map<string, number>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

export function getCompactionState(sessionId: string | null | undefined) {
  return sessionId ? states.get(sessionId) : undefined;
}

export function setCompactionState(sessionId: string, state: CompactionState) {
  states.set(sessionId, state);
  eventVersions.set(sessionId, (eventVersions.get(sessionId) ?? 0) + 1);
  listeners.forEach((listener) => listener());
}

export function dismissCompaction(sessionId: string) {
  // An active operation cannot be hidden (or unlocked) by dismissal.
  if (states.get(sessionId)?.status === "active") return;
  states.delete(sessionId);
  eventVersions.set(sessionId, (eventVersions.get(sessionId) ?? 0) + 1);
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
  eventVersions.set(sessionId, (eventVersions.get(sessionId) ?? 0) + 1);
  listeners.forEach((listener) => listener());
}

/** Mark the initiating client's request as in flight before its API call. */
export function markLocalCompactionStart(sessionId: string) {
  localPending.add(sessionId);
  setCompactionState(sessionId, { status: "active", startedAt: Date.now() });
}

/**
 * Mark the initiating request as settled. The caller still applies the final
 * success/error state; this only prevents an older /state poll from winning a
 * race with the local request lifecycle.
 */
export function markLocalCompactionEnd(sessionId: string) {
  localPending.delete(sessionId);
  eventVersions.set(sessionId, (eventVersions.get(sessionId) ?? 0) + 1);
}

export function isLocalCompactionPending(sessionId: string) {
  return localPending.has(sessionId);
}

export function getCompactionEventVersion(sessionId: string) {
  return eventVersions.get(sessionId) ?? 0;
}

export function getCompactionGeneration(sessionId: string) {
  return compactionGenerations.get(sessionId) ?? 0;
}

export function noteCompactionGeneration(sessionId: string, generation: number) {
  if (!Number.isFinite(generation) || generation <= 0) return;
  if (generation > (compactionGenerations.get(sessionId) ?? 0)) {
    compactionGenerations.set(sessionId, generation);
  }
}

export function noteCompactionFinishedGeneration(sessionId: string, generation: number) {
  if (!Number.isFinite(generation) || generation <= 0) return;
  if (generation > (finishedCompactionGenerations.get(sessionId) ?? 0)) {
    finishedCompactionGenerations.set(sessionId, generation);
  }
}

export function compactionGenerationFinished(sessionId: string, generation: number) {
  return generation > 0 && generation <= (finishedCompactionGenerations.get(sessionId) ?? 0);
}

/**
 * A server restart resets its in-memory generation counter. The next SSE
 * reconnect is the client signal that a fresh server snapshot may be arriving,
 * so old generation watermarks must not suppress the first post-restart start.
 */
export function resetCompactionGenerations() {
  compactionGenerations.clear();
  finishedCompactionGenerations.clear();
}

export function useCompactionState(sessionId: string | null | undefined) {
  return useSyncExternalStore(subscribe, () => getCompactionState(sessionId));
}

export function isCompactCommand(text: string) {
  return text.trim().split(/\s+/, 1)[0].toLowerCase() === "/compact";
}

/** Test-only reset for the module-level maps. */
export function __resetCompactionStateForTests() {
  states.clear();
  eventVersions.clear();
  localPending.clear();
  resetCompactionGenerations();
}
