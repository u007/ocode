import { useCallback, useEffect, useState } from "react";
import { api, type HtrStatus, type HtrTab } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

// How often the HTR daemon status is re-probed while the form is mounted. The
// daemon can be started/stopped by another ocode process or the tray icon, so
// the panel stays live instead of only reflecting its own actions.
const HTR_POLL_MS = 10_000;

export default function BrowserForm() {
  const [chromePath, setChromePath] = useState("");
  const [idleTimeoutMinutes, setIdleTimeoutMinutes] = useState(10);
  const [screencastQuality, setScreencastQuality] = useState(85);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [htr, setHtr] = useState<HtrStatus | null>(null);
  const [htrBusy, setHtrBusy] = useState(false);
  const [htrError, setHtrError] = useState<string | null>(null);
  const [tabs, setTabs] = useState<HtrTab[] | null>(null);
  const [tabsBusy, setTabsBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getBrowserConfig();
      setChromePath(cfg.chrome_path);
      setIdleTimeoutMinutes(cfg.idle_timeout_minutes);
      setScreencastQuality(cfg.screencast_quality);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshHtr = useCallback(async () => {
    try {
      setHtr(await api.getHtrStatus());
    } catch (err) {
      setHtrError(err instanceof Error ? err.message : String(err));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    refreshHtr();
    const id = setInterval(refreshHtr, HTR_POLL_MS);
    return () => clearInterval(id);
  }, [refreshHtr]);

  // applyHtr runs a start/stop action and adopts its returned status. The
  // server reports operational failures in status.error (HTTP 200), so both
  // paths surface through htrError.
  const applyHtr = async (action: () => Promise<HtrStatus>) => {
    setHtrBusy(true);
    setHtrError(null);
    try {
      const next = await action();
      setHtr(next);
      if (next.error) setHtrError(next.error);
    } catch (err) {
      setHtrError(err instanceof Error ? err.message : String(err));
    } finally {
      setHtrBusy(false);
    }
  };

  const listTabs = async () => {
    setTabsBusy(true);
    setHtrError(null);
    try {
      const res = await api.listHtrTabs();
      setTabs(res.tabs ?? []);
      if (res.error) setHtrError(res.error);
    } catch (err) {
      setHtrError(err instanceof Error ? err.message : String(err));
    } finally {
      setTabsBusy(false);
    }
  };

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const saved = await api.setBrowserConfig({
        chrome_path: chromePath,
        idle_timeout_minutes: idleTimeoutMinutes,
        screencast_quality: screencastQuality,
      });
      setScreencastQuality(saved.screencast_quality);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-foreground">Browser</h2>
      <p className="text-xs text-muted-foreground">
        Embedded Chrome tab (CDP screencast) settings. Quality applies live — open tabs pick it up on their
        next resize or navigation.
      </p>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">
          Screencast quality ({screencastQuality}) — higher is sharper text, more bandwidth
        </label>
        <input
          type="range"
          min={1}
          max={100}
          value={screencastQuality}
          onChange={(e) => setScreencastQuality(Number(e.target.value))}
          className="w-full"
          data-testid="browser-quality"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Chrome binary path (empty = auto-discover)</label>
        <Input
          type="text"
          value={chromePath}
          onChange={(e) => setChromePath(e.target.value)}
          placeholder="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Idle timeout (minutes, 0 = default)</label>
        <Input
          type="number"
          value={idleTimeoutMinutes}
          onChange={(e) => setIdleTimeoutMinutes(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>

      <div className="border-t border-border pt-4 space-y-3">
        <div>
          <h3 className="text-sm font-semibold text-foreground">HTR NControl daemon</h3>
          <p className="text-xs text-muted-foreground">
            Supervised <code>htrcli serve</code> daemon backing browser automation. Enabling starts a single
            managed instance; Stop also turns off auto-start.
          </p>
        </div>
        <div className="flex items-center gap-2 text-xs" data-testid="htr-status">
          <span
            className={`inline-block h-2 w-2 shrink-0 rounded-full ${
              htr?.running ? "bg-emerald-500" : "bg-muted-foreground/40"
            }`}
          />
          <span className="text-muted-foreground">
            {htr?.running ? `Running — ${htr.addr}` : "Stopped"}
          </span>
          {htr?.running && htr.binary && (
            <span className="truncate text-muted-foreground/70" title={htr.binary}>
              {htr.binary}
            </span>
          )}
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={!!htr?.enabled}
            disabled={htrBusy}
            onChange={(e) => applyHtr(e.target.checked ? api.startHtr : api.stopHtr)}
            data-testid="htr-enabled"
          />
          Enabled (auto-start with ocode)
        </label>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            className="h-8 text-xs"
            disabled={htrBusy || !!htr?.running}
            onClick={() => applyHtr(api.startHtr)}
            data-testid="htr-start"
          >
            {htrBusy && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
            Start
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={htrBusy || !htr?.running}
            onClick={() => applyHtr(api.stopHtr)}
            data-testid="htr-stop"
          >
            Stop
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={tabsBusy}
            onClick={listTabs}
            data-testid="htr-list-tabs"
          >
            {tabsBusy && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
            List tabs
          </Button>
        </div>
        {htrError && (
          <div className="text-xs text-red-400" data-testid="htr-error">
            {htrError}
          </div>
        )}
        {tabs && (
          <div className="space-y-1 text-xs text-muted-foreground" data-testid="htr-tabs">
            {tabs.length === 0 ? (
              <div>No connected tabs.</div>
            ) : (
              <>
                <div>
                  {tabs.length} connected tab{tabs.length === 1 ? "" : "s"}:
                </div>
                <ul className="space-y-0.5">
                  {tabs.map((tab) => (
                    <li key={tab.id} className="truncate" title={`${tab.title} — ${tab.url}`}>
                      <span className="text-foreground">
                        {tab.active ? "● " : ""}
                        {tab.title || "(untitled)"}
                      </span>
                      {" — "}
                      <span>{tab.url}</span>
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
