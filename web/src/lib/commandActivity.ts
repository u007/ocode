import { useSyncExternalStore } from "react";

/**
 * What the composer bar reports as currently running, for one session.
 *
 * `command` — a client-side slash command whose handler is still awaiting the
 * server (`/recap`, `/share`, `/mask`, `/btw`, …). Set by App.handleCommand
 * around its single `await dispatchCommand(...)`.
 *
 * `skill` — the skill the model loaded this turn (`skill` / `load_skill`). Its
 * lifetime is deliberately the TURN, not the tool call: a skill tool returns
 * instantly (it only reads SKILL.md), and the work that actually matters is the
 * bash/edit calls that follow it. So the bar answers "which skill is driving
 * this turn", which is the thing the status bar's per-tool `⚙` row cannot say.
 */
export type SessionActivity =
  | { kind: "command"; label: string; startedAt: number }
  | { kind: "skill"; name: string; startedAt: number };

// Like compactionState, this is session-owned UI state rather than transcript
// state: it survives SET_MESSAGES and composer remounts, and a reload starts
// empty (nothing is running yet).
const states = new Map<string, SessionActivity>();
const listeners = new Set<() => void>();

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
};

function emit() {
  listeners.forEach((listener) => listener());
}

export function setCommandActivity(sessionId: string, label: string): SessionActivity {
  const entry: SessionActivity = { kind: "command", label, startedAt: Date.now() };
  states.set(sessionId, entry);
  emit();
  return entry;
}

export function setSkillActivity(sessionId: string, name: string) {
  states.set(sessionId, { kind: "skill", name, startedAt: Date.now() });
  emit();
}

/**
 * Drop this session's activity. Called on turn_started / turn_done /
 * turn_error and on abort — the turn is the natural lifetime for both kinds.
 *
 * Session frames on one SSE stream are ordered, so a late event from a finished
 * turn cannot resurrect a bar after the next turn_started has already cleared
 * it; that is why no generation counter is needed here.
 */
export function clearSessionActivity(sessionId: string) {
  if (!states.delete(sessionId)) return;
  emit();
}

/**
 * Clear only if `entry` is still the live one. A slow command (`/recap` can
 * burn a minute) resolves after the user has already sent a new message and the
 * model loaded a skill; its `finally` must not erase that newer, live
 * indicator. Reference equality is the whole check — no counter to keep in sync.
 */
export function clearCommandActivity(sessionId: string, entry: SessionActivity) {
  if (states.get(sessionId) !== entry) return;
  states.delete(sessionId);
  emit();
}

/**
 * Move this session's activity to a new id, for `/reset-id`.
 *
 * Per CLAUDE.md any session-keyed map must move when a chat is re-keyed or it
 * is stranded under the deleted id. Usually there is nothing to move (the
 * command wrapper clears before the rekey lands), but a skill bar set during a
 * turn would otherwise survive under an id nothing reads.
 */
export function rekeySessionActivity(oldId: string, newId: string) {
  const current = states.get(oldId);
  if (!current) return;
  states.delete(oldId);
  states.set(newId, current);
  emit();
}

export function getSessionActivity(sessionId: string | null | undefined) {
  return sessionId ? states.get(sessionId) : undefined;
}

export function useSessionActivity(sessionId: string | null | undefined) {
  return useSyncExternalStore(
    subscribe,
    () => getSessionActivity(sessionId),
    () => undefined,
  );
}

/** Test-only reset. Mirrors compactionState's escape hatch. */
export function __resetSessionActivityForTests() {
  states.clear();
  listeners.clear();
}