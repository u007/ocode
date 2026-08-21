import { useEffect, useState } from "react";
import {
  getLogPrefs,
  LOG_PREFS_CHANGE_EVENT,
  type LogViewPrefs,
} from "../lib/logViewPrefs";

/**
 * useLogPrefs — reactive view of the Logs-tab preferences.
 *
 * Re-reads on the prefs CustomEvent (same window: settings form writes) and
 * on localStorage `storage` events (other same-origin windows). LogPanel
 * consumes this so pref changes apply to every mounted panel live.
 */
export function useLogPrefs(): LogViewPrefs {
  const [prefs, setPrefs] = useState<LogViewPrefs>(() => getLogPrefs());

  useEffect(() => {
    const sync = () => setPrefs(getLogPrefs());
    window.addEventListener(LOG_PREFS_CHANGE_EVENT, sync);
    window.addEventListener("storage", sync);
    return () => {
      window.removeEventListener(LOG_PREFS_CHANGE_EVENT, sync);
      window.removeEventListener("storage", sync);
    };
  }, []);

  return prefs;
}
