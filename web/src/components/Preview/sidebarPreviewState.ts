// Persists the sidebar PreviewHost shell state PER PROJECT so switching
// projects (which remounts the whole panel — `key={sideStateKey}` tracks the
// active session tab) restores the file that project was last previewing and
// its page/slide, instead of dropping back to the Browser tab with no file.
//
// Keyed by project identity (`host::projectRoot`), the same scope the editor
// tabs use, so a remote project's preview never bleeds into a local one.

import type { PreviewKind } from "../../lib/previewKind";

const STORAGE_KEY = "ocode.ui.sidebarPreview.v1";

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

/** Project-scoped storage key. Returns null when there is no project identity
 *  (boot, before the project list resolves) — nothing is persisted then. */
export function sidebarPreviewProjectKey(projectRoot?: string, projectHost?: string): string | null {
  const host = projectHost ?? "";
  const root = projectRoot ?? "";
  if (!host && !root) return null;
  return host ? `${host}::${root}` : root;
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

/** Read the persisted shell state for a project, or null when none. */
export function loadSidebarPreviewState(projectRoot?: string, projectHost?: string): SidebarPreviewState | null {
  const key = sidebarPreviewProjectKey(projectRoot, projectHost);
  if (!key) return null;
  return loadStore()[key] ?? null;
}

/** Write the shell state for a project. No-op without a project identity. */
export function saveSidebarPreviewState(projectRoot: string | undefined, projectHost: string | undefined, state: SidebarPreviewState) {
  const key = sidebarPreviewProjectKey(projectRoot, projectHost);
  if (!key) return;
  try {
    const store = loadStore();
    store[key] = state;
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(store));
  } catch (err) {
    console.error("Failed to persist sidebar preview state:", err);
  }
}
