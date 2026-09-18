import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2, Plus, Trash2 } from "lucide-react";
import type {
  SystemPermissionEntry,
  SystemPermissionsResponse,
} from "../../api/types";

const STATUS_LABEL: Record<string, string> = {
  granted: "Granted",
  denied: "Denied",
  not_determined: "Not requested",
  unknown: "Unknown",
  not_required: "Not required",
};

const STATUS_CLASS: Record<string, string> = {
  granted: "text-emerald-400 border-emerald-500/40",
  denied: "text-red-400 border-red-500/40",
  not_determined: "text-amber-400 border-amber-500/40",
  unknown: "text-muted-foreground border-border",
  not_required: "text-muted-foreground border-border",
};

export default function SystemPermissionsForm() {
  const [data, setData] = useState<SystemPermissionsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [requestingAll, setRequestingAll] = useState(false);
  const [customPath, setCustomPath] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  // apply folds a response into state and surfaces any per-entry messages.
  const apply = useCallback((res: SystemPermissionsResponse) => {
    setData(res);
    const msgs: string[] = [];
    if (res.result?.message) msgs.push(res.result.message);
    if (res.results) {
      for (const r of res.results) if (r.message) msgs.push(r.message);
    }
    if (msgs.length > 0) setMessage(Array.from(new Set(msgs)).join(" "));
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setData(await api.getSystemPermissions());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = async (entry: SystemPermissionEntry, enabled: boolean) => {
    setBusyId(entry.id);
    setError(null);
    setMessage(null);
    try {
      apply(await api.setSystemPermission({ id: entry.id, enabled }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const requestOne = async (entry: SystemPermissionEntry) => {
    setBusyId(entry.id);
    setError(null);
    setMessage(null);
    try {
      apply(await api.requestSystemPermissions(entry.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const addPath = async () => {
    const path = customPath.trim();
    if (!path) return;
    setBusyId("__add__");
    setError(null);
    setMessage(null);
    try {
      apply(await api.setSystemPermission({ path, enabled: true }));
      setCustomPath("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const removePath = async (id: string) => {
    setBusyId(id);
    setError(null);
    setMessage(null);
    try {
      apply(await api.deleteSystemPermission(id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusyId(null);
    }
  };

  const requestAll = async () => {
    setRequestingAll(true);
    setError(null);
    setMessage(null);
    try {
      apply(await api.requestSystemPermissions());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRequestingAll(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  const entries = data?.entries ?? [];
  const supported = data?.supported ?? false;

  return (
    <div className="p-6 max-w-2xl space-y-4" data-testid="system-permissions-form">
      <h2 className="text-sm font-semibold text-foreground">System Permissions</h2>
      <p className="text-xs text-muted-foreground">
        {supported
          ? "Operating-system grants ocode may need. Toggle one on to request it, or off to stop requesting it. On macOS these grants are tied to the app's code signature, so every rebuild forgets them — ocode re-requests the ones you enabled when the desktop app starts."
          : "This platform has no per-application permission grants — the list is informational."}
      </p>

      {error && (
        <div className="text-xs text-red-400" data-testid="system-permissions-error">
          {error}
        </div>
      )}
      {message && (
        <div
          className="rounded-md border border-border bg-muted/40 p-3 text-xs text-muted-foreground"
          data-testid="system-permissions-message"
        >
          {message}
        </div>
      )}

      <div className="space-y-2">
        {entries.map((entry) => (
          <div
            key={entry.id}
            data-testid={`sysperm-row-${entry.id}`}
            className="rounded-md border border-border p-3 space-y-1.5"
          >
            <div className="flex items-center gap-2">
              <label className="flex items-center gap-2 text-sm text-foreground">
                <input
                  type="checkbox"
                  checked={entry.enabled}
                  disabled={!entry.supported || busyId === entry.id}
                  onChange={(e) => void toggle(entry, e.target.checked)}
                />
                {entry.label}
              </label>
              <span
                data-testid={`sysperm-status-${entry.id}`}
                className={`ml-auto rounded border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${
                  STATUS_CLASS[entry.status] ?? "text-muted-foreground border-border"
                }`}
              >
                {STATUS_LABEL[entry.status] ?? entry.status}
              </span>
            </div>
            {entry.detail && (
              <div className="text-xs text-muted-foreground break-all">{entry.detail}</div>
            )}
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                className="h-7 text-xs"
                disabled={!entry.supported || busyId === entry.id}
                onClick={() => void requestOne(entry)}
              >
                Request
              </Button>
              {entry.source === "custom" && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-7 text-xs"
                  disabled={busyId === entry.id}
                  onClick={() => void removePath(entry.id)}
                >
                  <Trash2 className="w-3.5 h-3.5 mr-1" />
                  Remove
                </Button>
              )}
              {entry.source === "discovered" && (
                <span className="text-[10px] text-muted-foreground">from your projects</span>
              )}
            </div>
          </div>
        ))}
      </div>

      <div className="flex items-center gap-2">
        <Input
          value={customPath}
          onChange={(e) => setCustomPath(e.target.value)}
          placeholder="/Users/you/Projects/acme"
          className="h-8 text-xs"
          data-testid="system-permissions-path-input"
        />
        <Button
          size="sm"
          variant="outline"
          className="h-8 text-xs shrink-0"
          disabled={busyId === "__add__" || customPath.trim() === ""}
          onClick={() => void addPath()}
        >
          {busyId === "__add__" ? (
            <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />
          ) : (
            <Plus className="w-3.5 h-3.5 mr-1.5" />
          )}
          Add path
        </Button>
      </div>

      {supported && (
        <Button
          size="sm"
          variant="outline"
          className="h-8 text-xs"
          disabled={requestingAll}
          onClick={() => void requestAll()}
        >
          {requestingAll && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
          Request all enabled
        </Button>
      )}
    </div>
  );
}
