import { useEffect, useState } from "react";
import {
  getLogPrefs,
  setLogPrefs,
  resetLogPrefs,
  LOG_PREFS_MIN_ENTRIES,
  LOG_PREFS_MAX_ENTRIES,
  LOG_PREFS_DEFAULT_ENTRIES,
} from "../../lib/logViewPrefs";
import { Button } from "../ui/button";
import { Input } from "../ui/input";

/**
 * LogsForm — Settings → Logs.
 *
 * Controls the per-tab Logs panel's client-side behavior. Unlike most forms
 * here this is a pure UI preference (localStorage via logViewPrefs, applied
 * instantly — no server round-trip and no Save button): whether hidden
 * session tabs keep buffering live entries, and how many entries each tab
 * retains before the oldest are dropped.
 */
export default function LogsForm() {
  const [backgroundBuffering, setBackgroundBuffering] = useState(false);
  const [maxEntries, setMaxEntries] = useState(String(LOG_PREFS_DEFAULT_ENTRIES));

  useEffect(() => {
    const prefs = getLogPrefs();
    setBackgroundBuffering(prefs.backgroundBuffering);
    setMaxEntries(String(prefs.maxEntries));
  }, []);

  const toggleBuffering = (checked: boolean) => {
    setBackgroundBuffering(checked);
    setLogPrefs({ backgroundBuffering: checked });
  };

  const commitEntries = () => {
    // Clamp through setLogPrefs (single source of truth for bounds), then
    // reflect the clamped value back into the input.
    const n = Number(maxEntries);
    const saved = setLogPrefs({ maxEntries: Number.isFinite(n) ? n : LOG_PREFS_DEFAULT_ENTRIES });
    setMaxEntries(String(saved.maxEntries));
  };

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Logs</h2>
      <p className="text-xs text-zinc-500">
        Per-session Logs tab behavior. Stored in this browser only — no server round-trip.
      </p>

      <label className="flex items-start gap-2 cursor-pointer">
        <input
          type="checkbox"
          checked={backgroundBuffering}
          onChange={(e) => toggleBuffering(e.target.checked)}
          className="mt-0.5"
        />
        <span>
          <span className="block text-xs text-zinc-300">Buffer logs in background tabs</span>
          <span className="block text-xs text-zinc-500 mt-0.5">
            Keep collecting log entries for hidden session tabs. Off (recommended) drops live
            entries while a tab is hidden and refetches recent history when it is reopened —
            bounded memory in long sessions. On keeps complete history in every tab at the cost of
            memory.
          </span>
        </span>
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">
          Retained entries per tab ({LOG_PREFS_MIN_ENTRIES.toLocaleString()}–
          {LOG_PREFS_MAX_ENTRIES.toLocaleString()})
        </label>
        <Input
          type="number"
          min={LOG_PREFS_MIN_ENTRIES}
          max={LOG_PREFS_MAX_ENTRIES}
          value={maxEntries}
          onChange={(e) => setMaxEntries(e.target.value)}
          onBlur={commitEntries}
          onKeyDown={(e) => {
            if (e.key === "Enter") commitEntries();
          }}
          className="h-8 text-xs w-40"
        />
        <p className="text-xs text-zinc-600">
          Oldest entries are dropped first once the cap is reached. Default{" "}
          {LOG_PREFS_DEFAULT_ENTRIES.toLocaleString()}.
        </p>
      </div>

      <Button
        size="sm"
        variant="ghost"
        className="h-8 text-xs"
        onClick={() => {
          const prefs = resetLogPrefs();
          setBackgroundBuffering(prefs.backgroundBuffering);
          setMaxEntries(String(prefs.maxEntries));
        }}
      >
        Reset to defaults
      </Button>
    </div>
  );
}
