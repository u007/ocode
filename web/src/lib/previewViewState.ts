// Persists per-file viewer state (PDF/PPTX page or slide, PDF zoom and scroll
// offset) so a project switch — which unmounts the Files-tab pane and the
// sidebar PreviewHost — does not drop the reader back to page 1 / zoom 100%.
//
// Monaco already persists its own scroll via editorTabsPersistence; this covers
// the *other* viewers, whose state used to live only in component state and
// therefore died on unmount. State is keyed by the file's full identity
// (host::projectRoot::path) so a remote project's `report.pdf` never shares a
// position with a local one.

const STORAGE_KEY = "ocode.ui.previewViewState.v1";
/** Bound the map so a long-lived app can't grow it without limit. Oldest
 *  written entry is evicted first (string-key insertion order is stable). */
const MAX_ENTRIES = 200;

export interface PreviewViewState {
  /** 1-based visible page (PDF) or slide index (PPTX). */
  page?: number;
  /** PDF zoom multiplier (1 = 100%). */
  zoom?: number;
  /** PDF scroll container offset in px. More precise than `page` when the
   *  reader stopped mid-page. */
  scrollTop?: number;
}

type Store = Record<string, PreviewViewState>;

/** Stable identity of a previewed file. Mirrors `editorTabProjectKey` in
 *  editorTabsPersistence (`host::projectRoot`); the two must stay in sync so a
 *  remote (host, path) and a local path never share viewer state. */
export function previewViewKey(path: string, projectRoot?: string, projectHost?: string): string {
  const host = projectHost ?? "";
  const root = projectRoot ?? "";
  const scope = host ? `${host}::${root}` : root;
  return scope ? `${scope}::${path}` : path;
}

function loadStore(): Store {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return Object.create(null);
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object") return Object.create(null);
    const out: Store = Object.create(null);
    for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
      // Guard against prototype-pollution keys reaching JSON.parse output.
      if (k === "__proto__" || k === "constructor" || k === "prototype") continue;
      if (!v || typeof v !== "object") continue;
      const entry: PreviewViewState = {};
      const rec = v as Record<string, unknown>;
      if (typeof rec.page === "number" && Number.isFinite(rec.page) && rec.page > 0) entry.page = rec.page;
      if (typeof rec.zoom === "number" && Number.isFinite(rec.zoom) && rec.zoom > 0) entry.zoom = rec.zoom;
      if (typeof rec.scrollTop === "number" && Number.isFinite(rec.scrollTop) && rec.scrollTop >= 0) {
        entry.scrollTop = rec.scrollTop;
      }
      if (Object.keys(entry).length) out[k] = entry;
    }
    return out;
  } catch (err) {
    console.error("Failed to load preview view state:", err);
    return Object.create(null);
  }
}

function persist(next: Store) {
  try {
    // Evict oldest entries past the cap (insertion order is stable), keeping
    // the most recently touched ones.
    const keys = Object.keys(next);
    if (keys.length > MAX_ENTRIES) {
      for (const k of keys.slice(0, keys.length - MAX_ENTRIES)) delete next[k];
    }
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch (err) {
    console.error("Failed to persist preview view state:", err);
  }
}

/** Merge `patch` into the stored state for `key`. No-op when every patched
 *  field already matches, so a debounced saver can call this freely.
 *
 *  Values are validated with the SAME rules `loadStore` applies on read
 *  (finite; page/zoom > 0, scrollTop >= 0), so a caller cannot write a value
 *  that would be silently dropped on the next load — which, if it were the
 *  entry's only field, would make the entry disappear. */
export function savePreviewViewState(key: string, patch: PreviewViewState) {
  try {
    const store = loadStore();
    const prev = store[key] ?? {};
    let changed = false;
    for (const [k, v] of Object.entries(patch) as [keyof PreviewViewState, number | undefined][]) {
      if (v === undefined) continue;
      if (typeof v !== "number" || !Number.isFinite(v)) continue;
      if (k === "scrollTop" ? v < 0 : v <= 0) continue;
      if (prev[k] !== v) changed = true;
      prev[k] = v;
    }
    if (!changed) return;
    // Re-insert to move this key to the end (most-recently-used ordering).
    delete store[key];
    store[key] = prev;
    persist(store);
  } catch (err) {
    console.error("Failed to persist preview view state:", err);
  }
}

/** Read the stored state for `key`, or null when nothing is recorded. */
export function loadPreviewViewState(key: string): PreviewViewState | null {
  const state = loadStore()[key];
  return state && Object.keys(state).length ? state : null;
}
