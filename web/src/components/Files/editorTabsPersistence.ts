// Persists which editor tabs are open in the Files view, each tab's scroll
// position, and — for dirty tabs — the unsaved draft content, across page
// reloads. Clean content is always re-fetched from the server on restore;
// only edits that differ from the last-saved state are stored as drafts.

export interface PersistedEditorTab {
  path: string;
  projectRoot?: string;
}

interface PersistedEditorTabsFile {
  version: 1;
  tabs: PersistedEditorTab[];
  activeId: string | null;
}

const TABS_KEY = "ocode.ui.editorTabs.v1";
const SCROLL_KEY_PREFIX = "ocode.editor.scroll.";
const DRAFT_KEY_PREFIX = "ocode.editor.draft.";

export function loadEditorTabs(): PersistedEditorTabsFile | null {
  try {
    const raw = window.localStorage.getItem(TABS_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as PersistedEditorTabsFile;
    if (!parsed || parsed.version !== 1 || !Array.isArray(parsed.tabs)) return null;
    return parsed;
  } catch (err) {
    console.error("Failed to load persisted editor tabs:", err);
    return null;
  }
}

/** Saves the open tab list and garbage-collects scroll positions and drafts
 *  for any tab id no longer open. */
export function saveEditorTabs(tabs: PersistedEditorTab[], activeId: string | null, liveIds: string[]) {
  try {
    window.localStorage.setItem(TABS_KEY, JSON.stringify({ version: 1, tabs, activeId } satisfies PersistedEditorTabsFile));
  } catch (err) {
    console.error("Failed to persist editor tabs:", err);
    return;
  }
  const live = new Set(liveIds);
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const key = window.localStorage.key(i);
    if (!key) continue;
    const prefix = key.startsWith(SCROLL_KEY_PREFIX)
      ? SCROLL_KEY_PREFIX
      : key.startsWith(DRAFT_KEY_PREFIX)
        ? DRAFT_KEY_PREFIX
        : null;
    if (prefix && !live.has(key.slice(prefix.length))) window.localStorage.removeItem(key);
  }
}

export function loadEditorScroll(tabId: string): number {
  try {
    const raw = window.localStorage.getItem(SCROLL_KEY_PREFIX + tabId);
    if (!raw) return 0;
    const top = Number(raw);
    return Number.isFinite(top) && top > 0 ? top : 0;
  } catch (err) {
    console.error("Failed to load editor scroll position:", err);
    return 0;
  }
}

export function saveEditorScroll(tabId: string, scrollTop: number) {
  try {
    window.localStorage.setItem(SCROLL_KEY_PREFIX + tabId, String(scrollTop));
  } catch (err) {
    console.error("Failed to persist editor scroll position:", err);
  }
}

export interface EditorDraft {
  content: string;
  /** hashContent() of the on-disk content the draft was edited against, so a
   *  restore can tell "disk unchanged, just reapply" from "disk moved under
   *  the draft" without storing the full base text. */
  baseHash: string;
}

/** Cheap stable content fingerprint (djb2, base36). Collision risk is
 *  irrelevant here — a false "unchanged" only skips a conflict banner. */
export function hashContent(text: string): string {
  let h = 5381;
  for (let i = 0; i < text.length; i++) {
    h = ((h << 5) + h + text.charCodeAt(i)) | 0;
  }
  return (h >>> 0).toString(36);
}

/** Returns the unsaved draft for a tab, or null when none exists. */
export function loadEditorDraft(tabId: string): EditorDraft | null {
  try {
    const raw = window.localStorage.getItem(DRAFT_KEY_PREFIX + tabId);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as EditorDraft;
    if (!parsed || typeof parsed.content !== "string" || typeof parsed.baseHash !== "string") return null;
    return parsed;
  } catch (err) {
    console.error("Failed to load editor draft:", err);
    return null;
  }
}

/** Persists unsaved content. Can hit the localStorage quota on very large
 *  files — logged, and the tab simply restores as clean server content. */
export function saveEditorDraft(tabId: string, draft: EditorDraft) {
  try {
    window.localStorage.setItem(DRAFT_KEY_PREFIX + tabId, JSON.stringify(draft));
  } catch (err) {
    console.error("Failed to persist editor draft:", err);
  }
}

export function clearEditorDraft(tabId: string) {
  try {
    window.localStorage.removeItem(DRAFT_KEY_PREFIX + tabId);
  } catch (err) {
    console.error("Failed to clear editor draft:", err);
  }
}
