import { useCallback, useEffect, useState } from "react";
import { api, type BrowserConfig, type HtrStatus, type HtrTab } from "../../api/client";
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

  // The HTR half of the browser config. Kept as one object because the
  // shared-daemon fields are provenance for a daemon whose coordinates live in
  // htrcli's own config, not ocode's.
  const [browserCfg, setBrowserCfg] = useState<BrowserConfig | null>(null);
  const [htrPort, setHtrPort] = useState("");
  const [htrSocketPath, setHtrSocketPath] = useState("");

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
      setBrowserCfg(cfg);
      setHtrPort(cfg.htr_port ? String(cfg.htr_port) : "");
      setHtrSocketPath(cfg.htr_socket_path ?? "");
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
        // Shared mode ignores both of these, so sending them while shared is
        // just noise. The fields are disabled then, so their state is whatever
        // was loaded, not something the user just typed.
        ...(browserCfg?.htr_shared
          ? {}
          : {
              htr_port: htrPort.trim() ? Number(htrPort) : 0,
              htr_socket_path: htrSocketPath.trim(),
            }),
      });
      setScreencastQuality(saved.screencast_quality);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  // Shared mode is read-only provenance, never an input: browser.htr_shared and
  // browser.htr_token are config-file-only (config.SaveOcodeHTRConfig has no
  // parameter for either), so the form shows what they produced instead of
  // offering a control that would silently fail to persist.
  const shared = browserCfg?.htr_shared === true;

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
        {shared && (
          <div className="space-y-1 rounded-md border border-border bg-muted/20 p-2 text-xs" data-testid="htr-effective">
            <div className="font-medium text-foreground">Shared daemon</div>
            <div className="text-muted-foreground">
              ocode attaches to the one <code>htrcli serve</code> you already run. These are the
              coordinates in use, not values set here:
            </div>
            <div className="text-muted-foreground">
              Port <span className="font-mono text-foreground">{browserCfg?.effective_port ?? "—"}</span>
              {browserCfg?.effective_socket && (
                <>
                  {" · socket "}
                  <span className="font-mono break-all text-foreground">{browserCfg.effective_socket}</span>
                </>
              )}
            </div>
            <div className="text-muted-foreground">
              Token from{" "}
              {/* Rendered on its own node so a test can pin the source label
                  itself: the config path below also contains "htrcli", so a
                  substring check on the whole row would pass even if this were
                  dropped. */}
              <span className="font-mono text-foreground" data-testid="htr-token-source">
                {browserCfg?.token_source || "unknown"}
              </span>
              {browserCfg?.config_path && (
                <>
                  {" in "}
                  <span className="font-mono break-all text-foreground">{browserCfg.config_path}</span>
                </>
              )}
            </div>
            <div className="text-muted-foreground">
              {browserCfg?.htr_token_set
                ? "An htrcli token is configured."
                : "No htrcli token is configured."}
            </div>
            <div className="text-muted-foreground">
              <code>browser.htr_shared</code> and <code>browser.htr_token</code> are set in
              ocodeconfig.json — this form cannot write them.
            </div>
          </div>
        )}
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground" htmlFor="htr-port">
            HTR port — {shared ? "legacy private-daemon mode only; shared mode takes the port from htrcli" : "legacy private daemon (0 = managed default)"}
          </label>
          <Input
            id="htr-port"
            type="number"
            value={htrPort}
            onChange={(e) => setHtrPort(e.target.value)}
            placeholder="3846"
            disabled={shared}
            className="h-8 text-xs"
            data-testid="htr-port"
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground" htmlFor="htr-socket">
            HTR socket path — {shared ? "legacy private-daemon mode only" : "legacy private daemon; an empty value is left unchanged on save"}
          </label>
          <Input
            id="htr-socket"
            type="text"
            value={htrSocketPath}
            onChange={(e) => setHtrSocketPath(e.target.value)}
            placeholder="(managed default)"
            disabled={shared}
            className="h-8 text-xs"
            data-testid="htr-socket"
          />
        </div>
        {htr?.adopt_only && (
          <div className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-200" data-testid="htr-notice">
            {htr.notice}
          </div>
        )}
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
          {/* ocode stops only a daemon it spawned; a refused stop comes back
              as stopped:false with a reason and nothing happens. Disabling here
              is what keeps the button from looking like it worked. */}
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={htrBusy || !htr?.running || htr?.started_by_ocode !== true}
            title={
              htr?.running && htr.started_by_ocode !== true
                ? "ocode did not start this daemon, so it will not stop it. Start it from the terminal with `htrcli serve`, or let the ocode instance that owns it exit."
                : undefined
            }
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
        {htr?.running && htr.started_by_ocode !== true && (
          // A visible line, not just the button's title: a disabled control
          // does not fire mouse events, so its native tooltip never appears.
          // Without this the button is simply dead and the reason is invisible.
          <div className="text-xs text-muted-foreground" data-testid="htr-stop-unavailable">
            ocode did not start this daemon, so it will not stop it. Start it yourself with{" "}
            <code>htrcli serve</code>, or let the ocode instance that owns it exit.
          </div>
        )}
        {htr?.stopped === false && htr.reason && (
          <div className="text-xs text-amber-200" data-testid="htr-stop-refused">
            {htr.reason}
          </div>
        )}
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
