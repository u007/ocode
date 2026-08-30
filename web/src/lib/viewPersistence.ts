export type ActiveView = "files" | "git" | "cron" | "assets" | "sessions" | "settings";
export type FocusedKind = "chat" | "terminal";

const STORAGE_KEY = "ocode.ui.view-state.v1";

interface PersistedViewState {
  version: 1;
  projects: Record<string, { view: ActiveView; focusedKind: FocusedKind }>;
}

const validViews: Set<ActiveView> = new Set(["files", "git", "cron", "assets", "sessions", "settings"]);
const validKinds: Set<FocusedKind> = new Set(["chat", "terminal"]);

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
