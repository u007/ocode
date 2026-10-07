import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2, Power, Square } from "lucide-react";

/** Auto-share-on-start plus the live share status.
 *
 *  When on, the desktop app starts the tailscale share exposure at launch so
 *  the instance is reachable on your tailnet without opening the Share dialog.
 *  Off by default: turning this on makes the instance reachable by other devices
 *  on the tailnet, authenticated by the durable share token.
 *
 *  The status block below the toggle is the LIVE exposure and is independent of
 *  the toggle: it can be started/stopped right here, and stopping it never
 *  changes the persisted "start at launch" preference.
 */
export default function AutoShareForm() {
  const [enabled, setEnabled] = useState(false);
  const [running, setRunning] = useState(false);
  const [available, setAvailable] = useState(true);
  const [kind, setKind] = useState<string | null>(null);
  const [url, setUrl] = useState<string | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const apply = useCallback(
    (cfg: {
      enabled: boolean;
      running: boolean;
      available: boolean;
      kind?: string;
      url?: string;
      hint?: string;
    }) => {
      setEnabled(cfg.enabled);
      setRunning(cfg.running);
      setAvailable(cfg.available);
      setKind(cfg.kind ?? null);
      setUrl(cfg.url ?? null);
      setHint(cfg.hint ?? null);
    },
    [],
  );

  // applyStatus updates ONLY the exposure fields, so a start/stop response
  // (which does not carry the auto-share toggle) cannot clobber the persisted
  // preference the user is editing.
  const applyStatus = useCallback(
    (st: { running: boolean; available: boolean; kind?: string; url?: string; hint?: string }) => {
      setRunning(st.running);
      setAvailable(st.available);
      setKind(st.kind ?? null);
      setUrl(st.url ?? null);
      setHint(st.hint ?? null);
    },
    [],
  );

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      apply(await api.getAutoShareConfig());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, [apply]);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      // Echo what the server actually stored, not what was requested.
      apply(await api.setAutoShareConfig(enabled));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const start = async () => {
    setStarting(true);
    setError(null);
    try {
      applyStatus(await api.startShare());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setStarting(false);
    }
  };

  const stop = async () => {
    setStopping(true);
    setError(null);
    try {
      applyStatus(await api.stopShare());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setStopping(false);
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
      <h2 className="text-sm font-semibold text-foreground">Auto share on start</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      {/* Live exposure status + start/stop. Independent of the persisted
          toggle below. */}
      <div className="rounded-lg border p-3 space-y-2" data-testid="auto-share-status">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0">
            <span
              className={`inline-block w-2 h-2 rounded-full shrink-0 ${
                running ? "bg-emerald-500" : "bg-muted-foreground/40"
              }`}
              aria-hidden="true"
            />
            <span className="text-xs font-medium" data-testid="auto-share-status-text">
              {running ? "Sharing" : "Not sharing"}
            </span>
            {running ? (
              <span
                className={`text-[11px] ${kind === "funnel" ? "text-amber-500" : "text-muted-foreground"}`}
                data-testid="auto-share-kind"
              >
                {kind === "funnel" ? "Public on the internet" : "Tailnet only"}
              </span>
            ) : null}
          </div>
          {running ? (
            <Button
              size="sm"
              variant="outline"
              onClick={stop}
              disabled={stopping}
              className="h-8 text-xs gap-1 shrink-0"
              data-testid="auto-share-stop"
            >
              {stopping ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Square className="w-3.5 h-3.5" />}
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          ) : (
            <Button
              size="sm"
              onClick={start}
              disabled={starting || !available}
              className="h-8 text-xs gap-1 shrink-0"
              data-testid="auto-share-start"
            >
              {starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Power className="w-3.5 h-3.5" />}
              {starting ? "Starting…" : "Start sharing"}
            </Button>
          )}
        </div>

        {running && url ? (
          <p className="text-[11px] text-muted-foreground" data-testid="auto-share-url">
            Currently shared at <span className="font-mono break-all">{url}</span>
          </p>
        ) : !available ? (
          <p className="text-[11px] text-muted-foreground" data-testid="auto-share-unavailable">
            Tailscale isn&apos;t available or isn&apos;t serving yet — the app still
            starts normally and you can share manually from the Share dialog.
            {hint ? ` (${hint})` : ""}
          </p>
        ) : (
          <p className="text-[11px] text-muted-foreground" data-testid="auto-share-stopped">
            Nothing is exposed right now.
            {enabled
              ? " Auto share is on — sharing will start again the next time ocode launches."
              : ""}
          </p>
        )}
      </div>

      <label className="flex items-start gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          data-testid="auto-share-toggle"
        />
        <span>
          Share this instance over Tailscale when the app starts
          <span className="block mt-1 text-[11px] text-muted-foreground/80">
            When enabled, other devices on your tailnet can reach this instance at
            launch using the saved share token. It uses Tailscale only, is not
            published to the public internet, and takes effect the next time the
            app starts. Turning this off does not stop a share that is already
            running — use Stop above.
          </span>
        </span>
      </label>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
