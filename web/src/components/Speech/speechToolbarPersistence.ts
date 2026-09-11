export const SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY = "ocode.ui.speech_toolbar_visible.v1";

export function loadSpeechToolbarVisible(): boolean {
  try {
    const raw = window.localStorage.getItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY);
    if (raw != null) return JSON.parse(raw) === false ? false : true;
  } catch {
    // ignore — SSR or blocked storage
  }
  return true;
}

export function saveSpeechToolbarVisible(visible: boolean): void {
  try {
    window.localStorage.setItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY, JSON.stringify(visible));
  } catch {
    // ignore
  }
}
