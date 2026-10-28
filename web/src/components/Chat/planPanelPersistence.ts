export const PLAN_PANEL_COLLAPSED_STORAGE_KEY = "ocode.ui.planPanelCollapsed.v1";

// Most recent sessions whose fold/unfold choice is remembered. Keeps the stored
// map from growing with every session ever opened.
const MAX_REMEMBERED_SESSIONS = 200;

// `storage` events only fire in *other* documents, so same-tab listeners
// (multiple mounted chat tabs each render a PlanPanel) are notified
// via this custom event instead.
const CHANGE_EVENT = "ocode:planPanelCollapsedChange";

interface ChangeDetail {
  sessionId: string;
  collapsed: boolean;
}

function readAll(): Record<string, boolean> {
  try {
    const raw = window.localStorage.getItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY);
    if (raw != null) {
      const parsed: unknown = JSON.parse(raw);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return parsed as Record<string, boolean>;
    }
  } catch (err) {
    console.warn("plan panel: read fold state from localStorage failed", err);
  }
  return {};
}

/** The user's own fold choice for this session, or null when they never made one. */
export function loadPlanPanelCollapsed(sessionId: string): boolean | null {
  const value = readAll()[sessionId];
  return typeof value === "boolean" ? value : null;
}

export function savePlanPanelCollapsed(sessionId: string, collapsed: boolean): void {
  const all = readAll();
  delete all[sessionId]; // re-insert last so the oldest entries are dropped first
  all[sessionId] = collapsed;
  const ids = Object.keys(all);
  for (const id of ids.slice(0, Math.max(0, ids.length - MAX_REMEMBERED_SESSIONS))) delete all[id];
  try {
    window.localStorage.setItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY, JSON.stringify(all));
  } catch (err) {
    console.warn("plan panel: save fold state to localStorage failed", err);
  }
  window.dispatchEvent(new CustomEvent<ChangeDetail>(CHANGE_EVENT, { detail: { sessionId, collapsed } }));
}

/** Subscribe to changes from any surface (same tab or other tabs). Returns unsubscribe. */
export function subscribePlanPanelCollapsed(
  onChange: (sessionId: string, collapsed: boolean) => void,
): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key !== PLAN_PANEL_COLLAPSED_STORAGE_KEY) return;
    for (const [sessionId, collapsed] of Object.entries(readAll())) onChange(sessionId, collapsed);
  };
  const handleLocal = (e: Event) => {
    const { sessionId, collapsed } = (e as CustomEvent<ChangeDetail>).detail;
    onChange(sessionId, collapsed);
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(CHANGE_EVENT, handleLocal);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(CHANGE_EVENT, handleLocal);
  };
}
