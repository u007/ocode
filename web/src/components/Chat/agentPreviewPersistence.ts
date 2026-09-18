export const AGENT_PREVIEW_COLLAPSED_STORAGE_KEY = "ocode.ui.agentPreviewCollapsed.v1";

// `storage` events only fire in *other* documents, so same-tab listeners
// (multiple mounted chat tabs each render an AgentPreview rail) are notified
// via this custom event instead.
const CHANGE_EVENT = "ocode:agentPreviewCollapsedChange";

export function loadAgentPreviewCollapsed(): boolean {
  try {
    const raw = window.localStorage.getItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY);
    if (raw != null) return JSON.parse(raw) === true;
  } catch {
    // ignore
  }
  return false;
}

export function saveAgentPreviewCollapsed(collapsed: boolean): void {
  try {
    window.localStorage.setItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY, JSON.stringify(collapsed));
  } catch {
    // ignore
  }
  window.dispatchEvent(new CustomEvent<boolean>(CHANGE_EVENT, { detail: collapsed }));
}

/** Subscribe to changes from any surface (same tab or other tabs). Returns unsubscribe. */
export function subscribeAgentPreviewCollapsed(onChange: (collapsed: boolean) => void): () => void {
  const handleStorage = (e: StorageEvent) => {
    if (e.key === AGENT_PREVIEW_COLLAPSED_STORAGE_KEY) onChange(e.newValue === "true");
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
