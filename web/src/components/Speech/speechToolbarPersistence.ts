export const SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY = "ocode.ui.speech_toolbar_visible.v1";

export function loadSpeechToolbarVisible(): boolean {
  try {
    const raw = window.localStorage.getItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY);
    if (raw != null) return JSON.parse(raw) === false ? false : true;
  } catch {
    // intentionally not logged: SSR or blocked storage. The stored value is a
    // UI preference with a working default, so there is nothing to report and
    // logging would spam the debug log on every render in those environments.
  }
  return true;
}

export function saveSpeechToolbarVisible(visible: boolean): void {
  try {
    window.localStorage.setItem(SPEECH_TOOLBAR_VISIBLE_STORAGE_KEY, JSON.stringify(visible));
  } catch {
    // intentionally not logged: see loadSpeechToolbarVisible.
  }
}

/** How the speech funnel renders text before handing it to the engine. */
export type SpeechSpeakMode = "summarised" | "full";

export const SPEECH_SPEAK_MODE_STORAGE_KEY = "ocode.ui.speech_speak_mode.v1";

/**
 * Read the per-user preference for summarising before speaking.
 *
 * Default is "summarised": the whole point of the feature is that reading
 * aloud does not recite fenced code blocks. "full" is the opt-out, kept
 * client-side and separate from the server's `speech_summary_enabled` gate so
 * a user can read one message verbatim without turning the feature off for
 * every session.
 *
 * Any unrecognised stored value falls back to the default rather than throwing
 * — a corrupted key must not be able to break speaking entirely.
 */
export function loadSpeechSpeakMode(): SpeechSpeakMode {
  try {
    const raw = window.localStorage.getItem(SPEECH_SPEAK_MODE_STORAGE_KEY);
    if (raw == null) return "summarised";
    // JSON.parse, matching saveSpeechSpeakMode's JSON.stringify and
    // loadSpeechToolbarVisible. Comparing the raw string would never match a
    // stored value, because stringify quotes it. A corrupt value throws here
    // and lands on the default in the catch below.
    if (JSON.parse(raw) === "full") return "full";
    return "summarised";
  } catch {
    // intentionally not logged: see loadSpeechToolbarVisible.
  }
  return "summarised";
}

export function saveSpeechSpeakMode(mode: SpeechSpeakMode): void {
  try {
    window.localStorage.setItem(SPEECH_SPEAK_MODE_STORAGE_KEY, JSON.stringify(mode));
  } catch {
    // intentionally not logged: see loadSpeechToolbarVisible.
  }
}
