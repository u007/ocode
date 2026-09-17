import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

export default function ComputerUseForm() {
  const [enabled, setEnabled] = useState(false);
  const [statusLines, setStatusLines] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [requesting, setRequesting] = useState(false);
  const [permissionLines, setPermissionLines] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getComputerUseConfig();
      setEnabled(cfg.enabled);
      setStatusLines(cfg.status_lines ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const saved = await api.setComputerUseConfig(enabled);
      setEnabled(saved.enabled);
      setStatusLines(saved.status_lines ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const requestPermissions = async () => {
    setRequesting(true);
    setError(null);
    setPermissionLines([]);
    try {
      const report = await api.requestComputerUsePermissions();
      setPermissionLines(report.lines ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRequesting(false);
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
      <h2 className="text-sm font-semibold text-foreground">Computer Use</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        Enable the desktop-control <code>computer</code> tool
      </label>
      <p className="text-xs text-muted-foreground">
        Takes effect in new sessions. The tool is not advertised to the model while
        disabled.
      </p>
      {statusLines.length > 0 && (
        <div
          data-testid="computer-use-status"
          className="space-y-1 rounded-md border border-border bg-muted/40 p-3 text-xs text-muted-foreground"
        >
          {statusLines.map((line, i) => (
            <div key={i}>{line}</div>
          ))}
        </div>
      )}
      <div className="flex items-center gap-2">
        <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
          {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
          Save
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={requestPermissions}
          disabled={requesting}
          className="h-8 text-xs"
        >
          {requesting && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
          Request permissions
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Triggers the operating-system permission dialogs the computer tool needs
        (on macOS: Accessibility, Screen Recording, and Automation). On Windows
        and Linux this only reports that no explicit grant is required.
      </p>
      {permissionLines.length > 0 && (
        <div
          data-testid="computer-use-permission-result"
          className="space-y-1 rounded-md border border-border bg-muted/40 p-3 text-xs text-muted-foreground"
        >
          {permissionLines.map((line, i) => (
            <div key={i}>{line}</div>
          ))}
        </div>
      )}
    </div>
  );
}
