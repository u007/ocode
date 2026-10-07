import { useSyncExternalStore } from "react";
import { eventBus } from "./eventBus";

/**
 * Per-session state for the docked /btw side-query panel.
 *
 * The server runs an INDEPENDENT side query and streams its progress as
 * session-scoped `btw` bus frames (started / activity / delta / done / error).
 * Nothing is written to the transcript, so this store is the only place the
 * aside, its tool activity and its answer live on the client.
 *
 * Keyed `host\0sessionId`: the same session id can exist on the local server
 * and a remote host, and their frames must not collide.
 */
export interface BtwState {
  sessionId: string;
  host?: string;
  question: string;
  generation: number;
  /** Tool-activity lines ("→ read …"), in arrival order. */
  activity: string[];
  /** The streamed answer, replaced by the authoritative text on `done`. */
  answer: string;
  loading: boolean;
  error?: string;
  /** Whether the panel is shown for this session. */
  open: boolean;
}

interface BtwFrame {
  generation: number;
  phase: "started" | "activity" | "delta" | "done" | "error";
  question?: string;
  text?: string;
  error?: string;
}

const states = new Map<string, BtwState>();
const listeners = new Set<() => void>();

function key(host: string | undefined, sessionId: string): string {
  return `${host ?? ""}\u0000${sessionId}`;
}

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

function emit() {
  listeners.forEach((listener) => listener());
}

/**
 * Reduce one bus frame. A `started` frame is the only one that can create
 * state (so a stray frame after the panel closed cannot resurrect it), and a
 * frame from an older generation is ignored so a superseded run cannot
 * overwrite the current one.
 */
function applyFrame(host: string | undefined, sessionId: string, f: BtwFrame) {
  const k = key(host, sessionId);
  const cur = states.get(k);
  if (cur && f.generation < cur.generation) return;

  if (f.phase === "started") {
    states.set(k, {
      sessionId,
      host,
      question: f.question ?? "",
      generation: f.generation,
      activity: [],
      answer: "",
      loading: true,
      open: true,
    });
    emit();
    return;
  }

  // Every other phase needs a live panel at the SAME generation.
  if (!cur || f.generation !== cur.generation) return;

  if (f.phase === "activity") {
    states.set(k, { ...cur, activity: [...cur.activity, f.text ?? ""] });
  } else if (f.phase === "delta") {
    states.set(k, { ...cur, answer: cur.answer + (f.text ?? "") });
  } else if (f.phase === "done") {
    states.set(k, {
      ...cur,
      loading: false,
      error: undefined,
      answer: f.text !== undefined ? f.text : cur.answer,
    });
  } else if (f.phase === "error") {
    states.set(k, { ...cur, loading: false, error: f.error || "side query failed" });
  } else {
    return;
  }
  emit();
}

// One subscription for the app lifetime. The handler is registered at module
// load so frames are never missed, matching compactionState.
eventBus.on("btw", (env) => {
  if (!env.session_id) return;
  applyFrame(env.host || undefined, env.session_id, env.data as BtwFrame);
});

/** Open (or reset) the panel for a session. Called from the `/btw` effect. */
export function startBtw(sessionId: string, host: string | undefined, question: string) {
  const k = key(host, sessionId);
  const cur = states.get(k);
  // A TERMINAL frame can arrive before this call: the server publishes
  // `started` — and a fast `error`/`done` — before it writes the 202, and the
  // SSE stream is delivered first. Do not clobber that state: resetting to
  // `loading` would leave the panel spinning on "Thinking…" forever with no
  // error and no retry (the exact failure this feature set out to avoid).
  if (cur && !cur.loading) {
    states.set(k, { ...cur, sessionId, host, question, open: true });
    emit();
    return;
  }
  states.set(k, {
    sessionId,
    host,
    question,
    // Keep the last generation so a late frame from the previous run cannot be
    // mistaken for the new one before its `started` frame arrives.
    generation: cur?.generation ?? 0,
    activity: [],
    answer: "",
    loading: true,
    open: true,
  });
  emit();
}

/** Dismiss the panel and drop its state. The caller also cancels the run. */
export function closeBtw(sessionId: string, host?: string) {
  if (!states.delete(key(host, sessionId))) return;
  emit();
}

/**
 * Move a session's panel to a new id on `/reset-id`. The server cancels the
 * old run, so the moved panel simply stops receiving frames; the user closes
 * it. Mirrors rekeySessionActivity — a session-keyed map must move with the
 * chat or it is stranded under the deleted id.
 */
export function rekeyBtw(oldId: string, newId: string) {
  let changed = false;
  for (const [k, st] of [...states]) {
    if (st.sessionId !== oldId) continue;
    states.delete(k);
    // Reset the generation: the server DELETES the run entry on /reset-id, so
    // the new id's first run starts at generation 1. Carrying the old
    // generation would make the new run's frames look stale and the rekeyed
    // panel would never update.
    states.set(key(st.host, newId), { ...st, sessionId: newId, generation: 0 });
    changed = true;
  }
  if (changed) emit();
}

export function useBtwState(
  sessionId: string | null | undefined,
  host?: string,
): BtwState | undefined {
  return useSyncExternalStore(
    subscribe,
    () => getBtwState(sessionId, host),
    () => undefined,
  );
}

/** Non-hook read (also used by the hook's snapshot). */
export function getBtwState(
  sessionId: string | null | undefined,
  host?: string,
): BtwState | undefined {
  return sessionId ? states.get(key(host, sessionId)) : undefined;
}

/** Test-only escape hatch, mirroring __resetSessionActivityForTests. */
export function __resetBtwStoreForTests() {
  states.clear();
  listeners.clear();
}

/** Test-only: drive one frame through the reducer without the bus. */
export function __applyBtwFrameForTests(
  host: string | undefined,
  sessionId: string,
  frame: BtwFrame,
) {
  applyFrame(host, sessionId, frame);
}
