export const STATUS_BAR_COLLAPSED_STORAGE_KEY = "ocode.ui.statusBarCollapsed.v1";

// `storage` events only fire in *other* documents, so same-document listeners
// are notified via this custom event instead (multi-window desktop / second
// browser tab still sync through the `storage` event below).
const CHANGE_EVENT = "ocode:statusBarCollapsedChange";

export function loadStatusBarCollapsed(): boolean {
  try {
    const raw = window.localStorage.getItem(STATUS_BAR_COLLAPSED_STORAGE_KEY);
    if (raw != null) return JSON.parse(raw) === true;
  } catch {
    // ignore
  }
  return false;
}

export function saveStatusBarCollapsed(collapsed: boolean): void {
  try {
    window.localStorage.setItem(STATUS_BAR_COLLAPSED_STORAGE_KEY, JSON.stringify(collapsed));
  } catch {
    // ignore
  }
  window.dispatchEvent(new CustomEvent<boolean>(CHANGE_EVENT, { detail: collapsed }));
}

/** Subscribe to changes from any surface (same tab or other tabs). Returns unsubscribe. */
export function subscribeStatusBarCollapsed(onChange: (collapsed: boolean) => void): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key === STATUS_BAR_COLLAPSED_STORAGE_KEY) onChange(e.newValue === "true");
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
