// Persists the sidebar PreviewHost shell state PER SESSION SURFACE so each
// chat session (and each terminal) remembers the file/page/surface it was last
// previewing independently. The panel is mounted with `key={sideStateKey}`
// (`side:chat:<sessionId>` / `side:term:<terminalId>`), so switching sessions
// unmounts and remounts it — without this the file, page, and active tab would
// reset, and without per-session scoping one chat's preview would leak into
// every other chat in the same project.
//
// Keyed by the side surface key (NOT project identity): a chat session is the
// unit the pane accompanies, so two sessions in the same project must not
// share a preview slot. Session/terminal ids are globally unique, so the raw
// stateKey is a sufficient map key.

import type { PreviewKind } from "../../lib/previewKind";

// v2: keys changed from `host::projectRoot` (per project) to the side
// stateKey (per session surface). Old v1 entries are intentionally orphaned.
const STORAGE_KEY = "ocode.ui.sidebarPreview.v2";

export type SidebarSurface = "browser" | "preview";

export interface SidebarPreviewState {
  surface: SidebarSurface;
  /** Previewed file, when the Preview tab held one. */
  path?: string;
  kind?: PreviewKind;
  projectRoot?: string;
  projectHost?: string;
  /** 1-based page (PDF) / slide index (PPTX) at last use. */
  page?: number;
  /** Legacy .doc/.ppt fallback path (no in-browser renderer). */
  unsupportedPath?: string;
}

type Store = Record<string, SidebarPreviewState>;

/** Persistence key for one side surface. Returns null when there is no
 *  stateKey (the pane is not mounted / no session), so nothing is persisted. */
export function sidebarPreviewKey(stateKey?: string | null): string | null {
  if (!stateKey) return null;
  return stateKey;
}

function isSurface(v: unknown): v is SidebarSurface {
  return v === "browser" || v === "preview";
}

function loadStore(): Store {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return Object.create(null);
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object") return Object.create(null);
    const out: Store = Object.create(null);
    for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
      if (k === "__proto__" || k === "constructor" || k === "prototype") continue;
      if (!v || typeof v !== "object") continue;
      const rec = v as Record<string, unknown>;
      if (!isSurface(rec.surface)) continue;
      const entry: SidebarPreviewState = { surface: rec.surface };
      if (typeof rec.path === "string") entry.path = rec.path;
      if (typeof rec.kind === "string") entry.kind = rec.kind as PreviewKind;
      if (typeof rec.projectRoot === "string") entry.projectRoot = rec.projectRoot;
      if (typeof rec.projectHost === "string") entry.projectHost = rec.projectHost;
      if (typeof rec.page === "number" && Number.isFinite(rec.page) && rec.page > 0) entry.page = rec.page;
      if (typeof rec.unsupportedPath === "string") entry.unsupportedPath = rec.unsupportedPath;
      out[k] = entry;
    }
    return out;
  } catch (err) {
    console.error("Failed to load sidebar preview state:", err);
    return Object.create(null);
  }
}

/** Read the persisted shell state for a side surface, or null when none. */
export function loadSidebarPreviewState(stateKey?: string | null): SidebarPreviewState | null {
  const key = sidebarPreviewKey(stateKey);
  if (!key) return null;
  return loadStore()[key] ?? null;
}

/** Write the shell state for a side surface. No-op without a stateKey. */
export function saveSidebarPreviewState(stateKey: string | null | undefined, state: SidebarPreviewState) {
  const key = sidebarPreviewKey(stateKey);
  if (!key) return;
  try {
    const store = loadStore();
    store[key] = state;
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch (err) {
    console.error("Failed to persist sidebar preview state:", err);
  }
}

/** Move a side surface's persisted preview state to a new key when its owning
 *  tab is rekeyed (a `new-*` chat tab becoming its real session id, or
 *  `/reset-id`). No-op when the source has no entry or the destination already
 *  has one (the live target wins). */
export function rekeySidebarPreviewState(oldKey: string | null | undefined, newKey: string | null | undefined) {
  const from = sidebarPreviewKey(oldKey);
  const to = sidebarPreviewKey(newKey);
  if (!from || !to || from === to) return;
  try {
    const store = loadStore();
    const moved = store[from];
    if (!moved || store[to]) return;
    store[to] = moved;
    delete store[from];
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch (err) {
    console.error("Failed to rekey sidebar preview state:", err);
  }
}
