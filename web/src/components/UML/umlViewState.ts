/**
 * Per-session UML tab view state: which view is active, zoom/pan, the
 * selected symbol and the list's search/filter text. Persisted to
 * localStorage under `ocode.ui.umlViewState.v1`, keyed by
 * `host::projectRoot::sessionId` — the same scheme previewViewState uses.
 * Opened-tab persistence (sub_tab: "uml") is tabs.json's job, not this one.
 */

const KEY = "ocode.ui.umlViewState.v1";

export interface UmlViewStateV1 {
  view: "map" | "classes";
  zoom: number;
  panX: number;
  panY: number;
  selectedId: string | null;
  query: string;
  langs: string[];
  kinds: string[];
  pkg: string;
}

type Store = Record<string, UmlViewStateV1>;

function readStore(): Store {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Store;
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export function umlViewStateKey(host: string | undefined, root: string, sessionId: string): string {
  return `${host ?? "local"}::${root}::${sessionId}`;
}

export function loadUmlViewState(scope: string): UmlViewStateV1 | undefined {
  const s = readStore()[scope];
  if (!s) return undefined;
  return {
    view: s.view === "classes" ? "classes" : "map",
    zoom: typeof s.zoom === "number" && Number.isFinite(s.zoom) ? s.zoom : 1,
    panX: typeof s.panX === "number" && Number.isFinite(s.panX) ? s.panX : 0,
    panY: typeof s.panY === "number" && Number.isFinite(s.panY) ? s.panY : 0,
    selectedId: typeof s.selectedId === "string" ? s.selectedId : null,
    query: typeof s.query === "string" ? s.query : "",
    langs: Array.isArray(s.langs) ? s.langs.filter((l) => typeof l === "string") : ["Go"],
    kinds: Array.isArray(s.kinds) ? s.kinds.filter((k) => typeof k === "string") : ["struct", "interface", "func", "method"],
    pkg: typeof s.pkg === "string" ? s.pkg : "all",
  };
}

export function saveUmlViewState(scope: string, s: UmlViewStateV1): void {
  try {
    const store = readStore();
    store[scope] = s;
    localStorage.setItem(KEY, JSON.stringify(store));
  } catch {
    // Quota or privacy mode: view state is a nicety, never fail a click.
  }
}
