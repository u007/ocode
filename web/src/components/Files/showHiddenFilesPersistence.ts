export const SHOW_HIDDEN_FILES_STORAGE_KEY = "ocode.ui.showHiddenFiles.v1";

// `storage` events only fire in *other* documents, so same-tab listeners
// (FileTree <-> FilePicker) are notified via this custom event instead.
const CHANGE_EVENT = "ocode:showHiddenFilesChange";

export function loadShowHiddenFiles(): boolean {
  try {
    const raw = window.localStorage.getItem(SHOW_HIDDEN_FILES_STORAGE_KEY);
    if (raw != null) return JSON.parse(raw) === true;
  } catch {
    // ignore
  }
  return false;
}

export function saveShowHiddenFiles(showHidden: boolean): void {
  try {
    window.localStorage.setItem(SHOW_HIDDEN_FILES_STORAGE_KEY, JSON.stringify(showHidden));
  } catch {
    // ignore
  }
  window.dispatchEvent(new CustomEvent<boolean>(CHANGE_EVENT, { detail: showHidden }));
}

/** Subscribe to changes from any surface (same tab or other tabs). Returns unsubscribe. */
export function subscribeShowHiddenFiles(onChange: (showHidden: boolean) => void): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key === SHOW_HIDDEN_FILES_STORAGE_KEY) onChange(e.newValue === "true");
  };
  const handleLocal = (e: Event) => {
    onChange((e as CustomEvent<boolean>).detail === true);
  };
  window.addEventListener("storage", handleStorage);
  window.addEventListener(CHANGE_EVENT, handleLocal);
  return () => {
    window.removeEventListener("storage", handleStorage);
    window.removeEventListener(CHANGE_EVENT, handleLocal);
  };
}
