/**
 * logViewPrefs — client-side Logs-tab view preferences.
 *
 * Pure frontend behavior (how each session tab's hidden LogPanel buffers and
 * how many entries it retains), so this is localStorage-backed rather than a
 * server config round-trip — same tier as sidebar width / open tabs. Changes
 * dispatch a window CustomEvent so mounted LogPanels and the settings form
 * update without a reload; the `storage` event covers other same-origin
 * windows (desktop multi-window / second browser tab).
 */

export interface LogViewPrefs {
  /**
   * true  = hidden session tabs keep buffering live log entries (pre-fix
   *         behavior; complete history, higher memory).
   * false = default. Hidden panels drop entries; re-opening the tab refetches
   *         the backlog from the server's bounded ring buffer.
   */
  backgroundBuffering: boolean;
  /** Retained entries per tab, clamped to [MIN, MAX]. */
  maxEntries: number;
}

export const LOG_PREFS_MIN_ENTRIES = 100;
export const LOG_PREFS_MAX_ENTRIES = 10_000;
export const LOG_PREFS_DEFAULT_ENTRIES = 1000;

const DEFAULTS: LogViewPrefs = {
  backgroundBuffering: false,
  maxEntries: LOG_PREFS_DEFAULT_ENTRIES,
};

const STORAGE_KEY = "ocode.ui.logs.v1";
/** Window CustomEvent fired after every successful write. */
export const LOG_PREFS_CHANGE_EVENT = "ocode:log-prefs-changed";

function clampEntries(n: unknown): number {
  const v = typeof n === "number" ? n : Number(n);
  if (!Number.isFinite(v)) return DEFAULTS.maxEntries;
  return Math.min(LOG_PREFS_MAX_ENTRIES, Math.max(LOG_PREFS_MIN_ENTRIES, Math.round(v)));
}

export function getLogPrefs(): LogViewPrefs {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS };
    const parsed = JSON.parse(raw) as Partial<LogViewPrefs>;
    return {
      backgroundBuffering: parsed.backgroundBuffering === true,
      maxEntries: clampEntries(parsed.maxEntries),
    };
  } catch {
    // Corrupt entry — fall back to defaults rather than breaking the panel.
    return { ...DEFAULTS };
  }
}

export function setLogPrefs(patch: Partial<LogViewPrefs>): LogViewPrefs {
  const next = getLogPrefs();
  if (patch.backgroundBuffering !== undefined) next.backgroundBuffering = patch.backgroundBuffering === true;
  if (patch.maxEntries !== undefined) next.maxEntries = clampEntries(patch.maxEntries);
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch (err) {
    console.error("Failed to persist log view prefs:", err);
  }
  window.dispatchEvent(new CustomEvent(LOG_PREFS_CHANGE_EVENT));
  return next;
}

export function resetLogPrefs(): LogViewPrefs {
  return setLogPrefs({ ...DEFAULTS });
}
