export type ActiveView = "files" | "git" | "cron" | "assets" | "sessions" | "settings";
export type FocusedKind = "chat" | "terminal" | "browser";

const STORAGE_KEY = "ocode.ui.view-state.v1";

interface PersistedViewState {
  version: 1;
  projects: Record<string, { view: ActiveView; focusedKind: FocusedKind }>;
}

const validViews: Set<ActiveView> = new Set(["files", "git", "cron", "assets", "sessions", "settings"]);
const validKinds: Set<FocusedKind> = new Set(["chat", "terminal", "browser"]);

function isValidView(v: unknown): v is ActiveView {
  return typeof v === "string" && validViews.has(v as ActiveView);
}

function isValidKind(k: unknown): k is FocusedKind {
  return typeof k === "string" && validKinds.has(k as FocusedKind);
}

function loadAll(): Record<string, { view: ActiveView; focusedKind: FocusedKind }> {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return Object.create(null);
    const parsed = JSON.parse(raw) as PersistedViewState;
    if (!parsed || parsed.version !== 1 || typeof parsed.projects !== "object" || parsed.projects === null) {
      return Object.create(null);
    }
    const out: Record<string, { view: ActiveView; focusedKind: FocusedKind }> = Object.create(null);
    for (const [path, entry] of Object.entries(parsed.projects)) {
      // Guard against prototype pollution keys.
      if (path === "__proto__" || path === "constructor" || path === "prototype") continue;
      if (!entry || typeof entry !== "object") continue;
      const view = (entry as { view: unknown }).view;
      const kind = (entry as { focusedKind: unknown }).focusedKind;
      if (!isValidView(view) || !isValidKind(kind)) continue;
      out[path] = { view, focusedKind: kind };
    }
    return out;
  } catch {
    return Object.create(null);
  }
}

function persistAll(projects: Record<string, { view: ActiveView; focusedKind: FocusedKind }>) {
  try {
    // Use a null-prototype copy to avoid polluted keys leaking into JSON.
    const safe: Record<string, { view: ActiveView; focusedKind: FocusedKind }> = Object.create(null);
    for (const [k, v] of Object.entries(projects)) {
      if (k === "__proto__" || k === "constructor" || k === "prototype") continue;
      if (!isValidView(v.view) || !isValidKind(v.focusedKind)) continue;
      safe[k] = v;
    }
    const payload: PersistedViewState = { version: 1, projects: safe };
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  } catch (err) {
    console.error("Failed to persist view state:", err);
  }
}

export function loadViewStateForProject(projectPath: string): { view: ActiveView; focusedKind: FocusedKind } | null {
  if (!projectPath) return null;
  if (projectPath === "__proto__" || projectPath === "constructor" || projectPath === "prototype") return null;
  const all = loadAll();
  return all[projectPath] ?? null;
}

export function saveViewStateForProject(projectPath: string, state: { view: ActiveView; focusedKind: FocusedKind }) {
  if (!projectPath) return;
  if (projectPath === "__proto__" || projectPath === "constructor" || projectPath === "prototype") return;
  if (!isValidView(state.view) || !isValidKind(state.focusedKind)) return;
  const all = loadAll();
  all[projectPath] = { view: state.view, focusedKind: state.focusedKind };
  persistAll(all);
}

export function getPersistedViewState(): Record<string, { view: ActiveView; focusedKind: FocusedKind }> {
  return loadAll();
}

export function persistViewStates(projects: Record<string, { view: ActiveView; focusedKind: FocusedKind }>) {
  persistAll(projects);
}

/** The fallback for a project with no stored state. A jump and a first visit
 *  both land on the chat surface, so this is the shared "nothing to restore"
 *  answer rather than a value duplicated at each call site. */
export const DEFAULT_VIEW_STATE: { view: ActiveView; focusedKind: FocusedKind } = {
  view: "sessions",
  focusedKind: "chat",
};

export interface PendingJumpView {
  /** The project being landed on — NOT the one the user came from. */
  path: string;
  view: ActiveView;
}

/**
 * Consume a Pulse jump's armed destination on a project switch.
 *
 * The slot is cleared on EVERY call, whether or not the arm applies. That is
 * the whole point: a same-project jump never re-runs the switch effect, so its
 * arm is still sitting there when the user later switches to some unrelated
 * project. Clearing unconditionally is what stops a stale arm from dictating
 * that later switch.
 *
 * Mutating `pending` (rather than returning a value App assigns) is deliberate —
 * it makes "the clear" inseparable from "the read", so a future edit cannot
 * honour an arm and forget to drop it.
 *
 * Takes the ref object rather than its value so App cannot accidentally do the
 * read and the clear in two steps.
 */
export function consumePendingJumpView(
  pending: { current: PendingJumpView | null },
  path: string,
): PendingJumpView | null {
  const armed = pending.current;
  pending.current = null;
  return armed && armed.path === path ? armed : null;
}

/**
 * Decide which view a project switch lands on.
 *
 * `forced` is a Pulse card jump: the user clicked a SESSION card, so the
 * destination is the chat surface regardless of what the target project was
 * last left showing. It is honoured only when it names THIS project — a
 * same-project jump never reaches the switch effect at all, and a stale arm
 * from an earlier jump must not silently dictate a later, unrelated switch.
 *
 * Extracted as a pure function so the precedence is testable without booting
 * App and driving a real project switch; App only owns the ref lifecycle.
 */
export function resolveViewOnProjectSwitch(
  saved: { view: ActiveView; focusedKind: FocusedKind } | null,
  forced: PendingJumpView | null,
  path: string,
): { view: ActiveView; focusedKind: FocusedKind } {
  if (forced && forced.path === path) {
    return { view: forced.view, focusedKind: "chat" };
  }
  return saved ?? DEFAULT_VIEW_STATE;
}
