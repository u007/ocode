import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

/** Auto-share-on-start.
 *
 *  When on, the desktop app starts the tailscale share exposure at launch so
 *  the instance is reachable on your tailnet without opening the Share dialog.
 *  Off by default: turning this on makes the instance reachable by other devices
 *  on the tailnet, authenticated by the durable share token.
 */
export default function AutoShareForm() {
  const [enabled, setEnabled] = useState(false);
  const [url, setUrl] = useState<string | null>(null);
  const [hint, setHint] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // `available` is intentionally not stored: a non-empty url already implies it,
  // and an empty one is reported by the "not available" branch below.
  const apply = useCallback(
    (cfg: { enabled: boolean; url?: string; hint?: string }) => {
      setEnabled(cfg.enabled);
      setUrl(cfg.url ?? null);
      setHint(cfg.hint ?? null);
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
            app starts.
          </span>
        </span>
      </label>

      {url ? (
        <p className="text-[11px] text-muted-foreground" data-testid="auto-share-url">
          Currently shared at <span className="font-mono">{url}</span>
        </p>
      ) : enabled ? (
        <p className="text-[11px] text-muted-foreground" data-testid="auto-share-unavailable">
          Tailscale isn&apos;t available or isn&apos;t serving yet — the app still
          starts normally and you can share manually from the Share dialog.
          {hint ? ` (${hint})` : ""}
        </p>
      ) : null}

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}