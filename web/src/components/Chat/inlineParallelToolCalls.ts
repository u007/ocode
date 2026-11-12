// Persisted "inline grouping of parallel tool calls" preference for the chat.
//
// Global rather than per project: it only changes how a turn's parallel tool
// calls are laid out. The stored value keeps its original "1"/"0" encoding so
// existing users keep the choice they already made.

import { useSyncExternalStore } from "react";

export const INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY = "ocode:inlineParallelToolCalls";

// `storage` events only fire in *other* documents, so same-tab subscribers are
// notified via this custom event instead.
const CHANGE_EVENT = "ocode:inlineParallelToolCallsChange";

// Inline items shrink-wrap to their content, so each one is capped at the column
// width (min-w-0 + max-w-full stops a long call from overflowing horizontally).
// The card's own 80% cap is lifted with [&>*>*]:!max-w-full: inside a
// shrink-wrapped item it would resolve to 80% of that item's content width and
// truncate every title.
export const PARALLEL_TOOL_CALLS_INLINE_CLASS =
  "flex flex-wrap items-start gap-2 [&>*]:min-w-0 [&>*]:max-w-full [&>*>*]:!max-w-full";
export const PARALLEL_TOOL_CALLS_STACKED_CLASS = "flex flex-col gap-2";

// The choice the user made while storage refused the write (quota, blocked site
// data), plus what storage held for the key at that moment. null means no write
// has failed since the last successful one, so storage is the source of truth.
//
// `baseline` is the raw stored string, `null` for "nothing stored", or
// `undefined` when that read also threw and the baseline is unknown. An unknown
// baseline is adopted by the first `load` that can read storage (see there).
// Module-level so every reader in this tab sees the same choice, and so it
// survives the component that made it.
interface UnpersistedChoice {
  value: boolean;
  baseline: string | null | undefined;
}
let unpersisted: UnpersistedChoice | null = null;

function readBaseline(): string | null | undefined {
  try {
    return window.localStorage.getItem(INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY);
  } catch {
    return undefined;
  }
}

export function loadInlineParallelToolCalls(): boolean {
  let stored: string | null;
  try {
    stored = window.localStorage.getItem(INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY);
  } catch {
    // Blocked or unavailable storage (private mode, disabled site data) must not
    // break rendering. Read the in-memory choice for this session, or off if the
    // user has not chosen one.
    return unpersisted?.value ?? false;
  }
  if (unpersisted !== null) {
    // The baseline was unreadable when the write failed, so storage could not be
    // compared against it. This is the first readable load since then: adopt what
    // storage holds now as the baseline. Nothing is known to have changed under the
    // user, so the choice made during the outage keeps applying, and a later change
    // by another tab is still caught by the comparison below. Adopting is idempotent
    // for useSyncExternalStore: the next call sees stored === baseline and returns
    // the same value.
    if (unpersisted.baseline === undefined) unpersisted.baseline = stored;
    // Storage still holds what it held when the write failed, so nothing has
    // changed under the user: the choice that could not be saved still applies.
    if (stored === unpersisted.baseline) return unpersisted.value;
    // Storage now holds something else, so another tab wrote it after the
    // failure. Storage wins from here on, and the fallback is dropped for good:
    // if storage later returns to the old baseline, the stale choice stays gone.
    // Clearing here is safe for useSyncExternalStore because the value returned
    // is the stored one, and the next call with no fallback returns the same.
    unpersisted = null;
  }
  return stored === "1";
}

export function saveInlineParallelToolCalls(value: boolean): void {
  try {
    window.localStorage.setItem(INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY, value ? "1" : "0");
    unpersisted = null;
  } catch {
    // Quota or blocked storage: keep the choice in memory so it still applies to
    // this open tab, and record what storage held so a later change made in
    // another tab can be told apart from the value this write failed to replace.
    // If that read also failed, the baseline stays unknown and the first readable
    // load adopts it. A later successful write clears the fallback, which is also
    // how a choice made during an outage is persisted once storage recovers: no
    // write is attempted from `load`.
    unpersisted = { value, baseline: readBaseline() };
  }
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

/**
 * Test-only: forget any in-memory fallback, including one whose baseline was
 * unknown, so each test starts from storage.
 */
export function resetInlineParallelToolCallsFallbackForTests(): void {
  unpersisted = null;
}

/**
 * Subscribe to the preference (same tab or another tab). Returns unsubscribe.
 */
export function subscribeInlineParallelToolCalls(onChange: () => void): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key !== INLINE_PARALLEL_TOOL_CALLS_STORAGE_KEY) return;
    // A storage event only fires for a write from another document, so another
    // tab has written this key. That write is newer than any failed write made
    // here, so drop the fallback and let storage win. This does not depend on
    // the baseline, so it also covers an unknown one that was never adopted.
    unpersisted = null;
    onChange();
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(CHANGE_EVENT, onChange);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(CHANGE_EVENT, onChange);
  };
}

export function useInlineParallelToolCalls(): boolean {
  return useSyncExternalStore(
    subscribeInlineParallelToolCalls,
    loadInlineParallelToolCalls,
    () => false,
  );
}
