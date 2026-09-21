// Persisted "show hidden files" preference for the Files tab.
//
// The preference is per project (and remote host): a project where you routinely
// work with dotfiles can show them while others stay tidy. Keys are derived from
// `showHiddenFilesProjectKey(projectPath, projectHost)`.
//
// SHOW_HIDDEN_FILES_STORAGE_KEY is the pre-per-project *global* key. It is
// still read as a fallback so an existing choice seeds each project until the
// user overrides it there; writes only ever go to the per-project key.

export const SHOW_HIDDEN_FILES_STORAGE_KEY = "ocode.ui.showHiddenFiles.v1";

const PER_PROJECT_PREFIX = `${SHOW_HIDDEN_FILES_STORAGE_KEY}:`;

// `storage` events only fire in *other* documents, so same-tab listeners
// (FileTree <-> FilePicker) are notified via this custom event instead.
const CHANGE_EVENT = "ocode:showHiddenFilesChange";

interface ChangeDetail {
  projectKey: string;
  showHidden: boolean;
}

/**
 * Stable persistence key for one project. Includes the remote host because two
 * hosts can expose the same path (e.g. `~/www/app`), and both must remember
 * their own choice. Empty when there is no project (a shared bucket).
 */
export function showHiddenFilesProjectKey(
  projectPath: string | undefined,
  projectHost: string | undefined,
): string {
  const base = projectPath ? projectPath.replace(/[\\/]+$/, "") || projectPath : "";
  return projectHost ? `${projectHost}\u0000${base}` : base;
}

function projectStorageKey(projectKey: string): string {
  return `${PER_PROJECT_PREFIX}${projectKey}`;
}

export function loadShowHiddenFiles(projectKey: string): boolean {
  try {
    const raw = window.localStorage.getItem(projectStorageKey(projectKey));
    if (raw != null) return JSON.parse(raw) === true;
    // Backward compat: builds before per-project keys stored one global flag;
    // use it as the initial value for a project that has no override yet.
    const legacy = window.localStorage.getItem(SHOW_HIDDEN_FILES_STORAGE_KEY);
    if (legacy != null) return JSON.parse(legacy) === true;
  } catch {
    // ignore — blocked/quota storage must not break the tree
  }
  return false;
}

export function saveShowHiddenFiles(projectKey: string, showHidden: boolean): void {
  try {
    window.localStorage.setItem(projectStorageKey(projectKey), JSON.stringify(showHidden));
  } catch {
    // ignore
  }
  window.dispatchEvent(
    new CustomEvent<ChangeDetail>(CHANGE_EVENT, { detail: { projectKey, showHidden } }),
  );
}

/**
 * Subscribe to the active project's preference (same tab or another tab).
 * Returns unsubscribe.
 */
export function subscribeShowHiddenFiles(
  projectKey: string,
  onChange: (showHidden: boolean) => void,
): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key !== projectStorageKey(projectKey)) return;
    if (e.newValue == null) return;
    onChange(e.newValue === "true");
  };
  const handleLocal = (e: Event) => {
    const detail = (e as CustomEvent<ChangeDetail>).detail;
    if (!detail || detail.projectKey !== projectKey) return;
    onChange(detail.showHidden === true);
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(CHANGE_EVENT, handleLocal);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(CHANGE_EVENT, handleLocal);
  };
}
