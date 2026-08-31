export type FileTreeViewMode = "tree" | "columns";

const STORAGE_KEY = "ocode.ui.filetree_view.v1";
const VALID: ReadonlySet<FileTreeViewMode> = new Set(["tree", "columns"]);

export function loadFileTreeView(): FileTreeViewMode {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (raw && VALID.has(raw as FileTreeViewMode)) return raw as FileTreeViewMode;
  } catch {
    // ignore — SSR or blocked storage
  }
  return "tree";
}

export function saveFileTreeView(mode: FileTreeViewMode): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // ignore
  }
}
