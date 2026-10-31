import { useCallback, useEffect, useState } from "react";
import { api, type OfficeAddinPath } from "../../api/client";
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

export default function PathsForm() {
  const [pathsText, setPathsText] = useState("");
  const [uploadDir, setUploadDir] = useState("");
  const [platform, setPlatform] = useState("");
  const [officePaths, setOfficePaths] = useState<OfficeAddinPath[]>([]);
  const [selected, setSelected] = useState<Record<string, boolean>>({});
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
      await api.setPathsConfig(merged, uploadDir);
      // The server normalizes roots; reflect the grant state we just sent.
      setOfficePaths((prev) => prev.map((o) => ({ ...o, allowed: !!selected[o.app] })));
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
            macOS still requires Full Disk Access for ocode (or the terminal running it) before a
            write into another app's container can succeed. Grant it in System Settings → Privacy
            &amp; Security → Full Disk Access, then restart ocode.
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
                  </span>
                </label>
              );
            })}
          </div>
        )}
      </div>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Other extra allowed paths (comma-separated)</label>
        <Input value={pathsText} onChange={(e) => setPathsText(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Upload directory</label>
        <Input value={uploadDir} onChange={(e) => setUploadDir(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
