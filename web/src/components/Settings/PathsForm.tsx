import { useCallback, useEffect, useState } from "react";
import { api, type OfficeAddinAccessResult, type OfficeAddinPath } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

/** Compare paths the way the user sees them: ignore a trailing separator, but
 *  never reduce a root to nothing ("/" → "") or to a drive ("C:\\" → "C:"). */
function canonicalPath(p: string): string {
  const trimmed = p.trim();
  const stripped = trimmed.replace(/[\\/]+$/, "");
  if (stripped === "" || /^[A-Za-z]:$/.test(stripped)) return trimmed;
  return stripped;
}

function statusLabel(preset: OfficeAddinPath, selected: boolean): string {
  if (!preset.available) return "Not installed";
  if (!preset.exists) return selected ? "Will be granted — folder not created yet" : "Folder not created yet";
  return "Folder found";
}

const ACCESS_LABEL: Record<OfficeAddinAccessResult["status"], string> = {
  granted: "Access granted",
  denied: "Access denied",
  not_determined: "Folder not found",
  missing: "Folder not created yet",
  not_required: "Folder readable",
  unknown: "Access not verifiable",
};

const ACCESS_CLASS: Record<OfficeAddinAccessResult["status"], string> = {
  granted: "text-emerald-400",
  denied: "text-red-400",
  not_determined: "text-amber-400",
  missing: "text-amber-400",
  not_required: "text-muted-foreground",
  unknown: "text-muted-foreground",
};

export default function PathsForm() {
  const [pathsText, setPathsText] = useState("");
  const [uploadDir, setUploadDir] = useState("");
  const [platform, setPlatform] = useState("");
  const [officePaths, setOfficePaths] = useState<OfficeAddinPath[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [access, setAccess] = useState<Record<string, OfficeAddinAccessResult>>({});
  const [requestingApps, setRequestingApps] = useState<string[]>([]);
  const [accessError, setAccessError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getPathsConfig();
      const office = cfg.office_addin_paths ?? [];
      // Office presets get their own checkboxes, so keep them out of the
      // free-text field to avoid showing the same path twice.
      // Include allowed_path: the server reports the RAW matched entry, which
      // can be a "~/..." or trailing-slash form that does not equal `path`.
      const officeSet = new Set(
        office.flatMap((o) => [
          canonicalPath(o.path),
          ...(o.allowed_path ? [canonicalPath(o.allowed_path)] : []),
        ]),
      );
      setOfficePaths(office);
      setSelected(Object.fromEntries(office.map((o) => [o.app, o.allowed])));
      // Access reports describe the folder as it was at the last save; a
      // reload starts clean so a stale "granted" cannot outlive the page.
      setAccess({});
      setAccessError(null);
      setPathsText(
        (cfg.extra_allowed_paths ?? [])
          .filter((p) => !officeSet.has(canonicalPath(p)))
          .join(", "),
      );
      setUploadDir(cfg.upload_dir);
      setPlatform(cfg.platform ?? "");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // requestAccess asks the server to probe the named folders. It keeps its
  // errors separate from `save`: the config write already succeeded, so a
  // refused OS prompt is not a save failure.
  const requestAccess = useCallback(async (apps: string[]) => {
    if (apps.length === 0) return;
    setRequestingApps(apps);
    setAccessError(null);
    try {
      const res = await api.requestOfficeAddinAccess(apps);
      setAccess((prev) => {
        const next = { ...prev };
        for (const r of res.results) next[r.app] = r;
        return next;
      });
    } catch (err) {
      setAccessError(err instanceof Error ? err.message : String(err));
    } finally {
      setRequestingApps([]);
    }
  }, []);

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const otherPaths = pathsText.split(",").map((p) => p.trim()).filter(Boolean);
      const granted = officePaths.filter((o) => selected[o.app]).map((o) => o.path);
      // Preserve order and drop duplicates (an "other" path can repeat a preset).
      const seen = new Set<string>();
      const merged: string[] = [];
      for (const p of [...otherPaths, ...granted]) {
        const key = canonicalPath(p);
        if (seen.has(key)) continue;
        seen.add(key);
        merged.push(p);
      }
      // Capture this BEFORE the save: the list below marks every ticked preset
      // as allowed, which would hide which folders are newly granted.
      const newlyGranted = officePaths
        .filter((o) => selected[o.app] && !o.allowed)
        .map((o) => o.app);
      await api.setPathsConfig(merged, uploadDir);
      // The server normalizes roots; reflect the grant state we just sent.
      setOfficePaths((prev) => prev.map((o) => ({ ...o, allowed: !!selected[o.app] })));
      // Ask the OS for the folders that just became granted. The save already
      // succeeded, so a refused prompt must not surface as a save failure.
      if (newlyGranted.length > 0) {
        await requestAccess(newlyGranted);
      }
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
    <div className="p-6 max-w-lg space-y-5">
      <h2 className="text-sm font-semibold text-foreground">Paths & Uploads</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <div className="space-y-2">
        <div className="text-xs font-medium text-foreground">Microsoft Office add-in folders</div>
        <p className="text-[11px] leading-relaxed text-muted-foreground">
          Grant the agent read/write permission for the folders Office reads add-in manifests
          from. These paths apply to the machine running ocode, not to remote SSH/WSL projects.
        </p>
        {platform === "darwin" && (
          <p className="text-[11px] leading-relaxed text-amber-500">
            Saving asks macOS for access to the folders you tick. If macOS denies the prompt, grant
            Full Disk Access to ocode (or the terminal running it) in System Settings → Privacy
            &amp; Security, then restart ocode.
          </p>
        )}
        {officePaths.length === 0 ? (
          <p className="text-[11px] text-muted-foreground">
            No known Office add-in folder on this platform. Add the folder you use in the extra
            paths field below.
          </p>
        ) : (
          <div className="space-y-2">
            {officePaths.map((preset) => {
              const checked = !!selected[preset.app];
              const disabled = !preset.available && !checked;
              const report = access[preset.app];
              const requesting = requestingApps.includes(preset.app);
              return (
                <label
                  key={preset.app}
                  className={`flex items-start gap-2 rounded-md border border-border px-2.5 py-2 text-xs ${
                    disabled ? "opacity-60" : "hover:bg-muted/40"
                  }`}
                >
                  <input
                    type="checkbox"
                    className="mt-0.5"
                    checked={checked}
                    disabled={disabled}
                    aria-label={`Grant ${preset.label} add-in folder access`}
                    onChange={(e) => {
                      // An unavailable preset must never be GRANTED (its parent
                      // directory is gone, so the runtime could not resolve it).
                      // It must still be REVOCABLE: a preset that is already
                      // allowed but no longer available renders enabled for
                      // exactly that reason, and blocking the uncheck left the
                      // grant impossible to remove from this form.
                      if (!preset.available && e.target.checked) return;
                      setSelected((prev) => ({ ...prev, [preset.app]: e.target.checked }));
                    }}
                  />
                  <span className="min-w-0 flex-1 space-y-0.5">
                    <span className="flex flex-wrap items-baseline gap-x-2">
                      <span className="font-medium text-foreground">{preset.label}</span>
                      <span
                        className={`text-[10px] ${
                          preset.available ? "text-muted-foreground" : "text-red-400"
                        }`}
                      >
                        {statusLabel(preset, checked)}
                      </span>
                    </span>
                    <span
                      title={preset.path}
                      className="block truncate font-mono text-[10px] text-muted-foreground"
                    >
                      {preset.path}
                    </span>
                    {preset.note && (
                      <span className="block text-[10px] leading-relaxed text-muted-foreground">
                        {preset.note}
                      </span>
                    )}
                    {requesting && (
                      <span className="block text-[10px] text-muted-foreground">Requesting access…</span>
                    )}
                    {report && (
                      <span className="block space-y-0.5">
                        <span
                          className={`block text-[10px] font-medium ${ACCESS_CLASS[report.status]}`}
                        >
                          {ACCESS_LABEL[report.status]}
                        </span>
                        {report.message && (
                          <span className="block text-[10px] leading-relaxed text-muted-foreground">
                            {report.message}
                          </span>
                        )}
                      </span>
                    )}
                    {checked && !requesting && report?.status !== "granted" && (
                      <button
                        type="button"
                        className="w-fit text-[10px] text-muted-foreground underline underline-offset-2 hover:text-foreground"
                        onClick={(e) => {
                          // The row is a <label>: keep the click from toggling
                          // the checkbox while it re-requests access.
                          e.preventDefault();
                          e.stopPropagation();
                          void requestAccess([preset.app]);
                        }}
                      >
                        {report ? "Retry access" : "Check access"}
                      </button>
                    )}
                  </span>
                </label>
              );
            })}
          </div>
        )}
      </div>

      {accessError && (
        <div className="text-xs text-red-400">Access request failed: {accessError}</div>
      )}

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Other extra allowed paths (comma-separated)</label>
        <Input value={pathsText} onChange={(e) => setPathsText(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Upload directory</label>
        <Input value={uploadDir} onChange={(e) => setUploadDir(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button
        size="sm"
        onClick={save}
        disabled={saving || requestingApps.length > 0}
        className="h-8 text-xs"
      >
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
