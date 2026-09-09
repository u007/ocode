import { useCallback, useEffect, useState } from "react";
import { api, type AutoPermissionConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

const EMPTY_AUTO: AutoPermissionConfig = {
  enabled: false, allow_destructive: false, prompt: "",
  max_context_bytes: 0, max_context_sources: 0, max_context_lines_per_source: 0, min_confidence: 0,
};

const DEFAULT_MODE_OPTIONS: { value: string; auto: boolean; label: string }[] = [
  { value: "normal", auto: false, label: "Normal" },
  { value: "normal", auto: true, label: "Normal + auto-approve" },
  { value: "yolo", auto: false, label: "Yolo" },
  { value: "sandbox", auto: false, label: "Sandbox" },
];

export default function PermissionsForm() {
  // Live permission mode string (normal|yolo|locked|sandbox). Loaded from GET
  // /api/permissions and saved via the mode endpoint so a session-scoped
  // sandbox toggle is preserved (never reverted by the "yolo" boolean path).
  const [mode, setMode] = useState<string>("normal");
  const [loadedMode, setLoadedMode] = useState<string>("normal");
  const [sandboxSupported, setSandboxSupported] = useState(true);
  // The persisted default a new TUI/web/RC session starts in — distinct from
  // the live mode above. Loaded from/saved to GET|PUT
  // /api/config/ocode/permissions-mode.
  const [defaultMode, setDefaultMode] = useState<string>("normal");
  const [loadedDefaultMode, setLoadedDefaultMode] = useState<string>("normal");
  const [auto, setAuto] = useState<AutoPermissionConfig>(EMPTY_AUTO);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [permDialogOpen, setPermDialogOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [perms, autoCfg, defaultModeCfg] = await Promise.all([
        api.getPermissions(),
        api.getAutoPermissionConfig(),
        api.getPermissionModeConfig(),
      ]);
      setMode(perms.mode || "normal");
      setLoadedMode(perms.mode || "normal");
      setSandboxSupported(perms.sandbox_supported ?? true);
      setDefaultMode(defaultModeCfg.mode || "normal");
      setLoadedDefaultMode(defaultModeCfg.mode || "normal");
      setAuto({ ...EMPTY_AUTO, ...autoCfg });
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
      // Only flip the mode if the user changed it in this form; otherwise a
      // session-scoped sandbox/yolo toggle stays untouched.
      if (mode !== loadedMode) {
        await api.setPermissionMode(mode);
      }
      if (defaultMode !== loadedDefaultMode) {
        await api.setPermissionModeConfig(defaultMode);
      }
      // Model is persisted separately via SavePermissionModel; preserve separation
      // so setAutoPermissionConfig doesn't clobber the model (it preserves it).
      const { model, ...rest } = auto;
      await api.setAutoPermissionConfig(rest as AutoPermissionConfig);
      if (model !== undefined) {
        await api.setPermissionModel(model ?? "");
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
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-foreground">Permissions</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={mode === "yolo"}
          onChange={(e) => setMode(e.target.checked ? "yolo" : "normal")}
        />
        Yolo mode (auto-approve all tool calls, current session only)
      </label>
      {(mode === "sandbox" || mode === "locked") && (
        <div className="text-xs text-muted-foreground">
          Current live permission mode: <span className="font-mono text-foreground">{mode}</span> (preserved on save)
        </div>
      )}

      <div className="border-t border-border pt-4 space-y-2">
        <div className="text-xs font-semibold text-foreground">Default on startup</div>
        <p className="text-xs text-muted-foreground">
          The permission mode a new TUI, web, or remote session starts in.
        </p>
        <div className="space-y-1.5">
          {DEFAULT_MODE_OPTIONS.map((opt) => {
            const disabled = opt.value === "sandbox" && !sandboxSupported;
            const checked = defaultMode === opt.value && Boolean(auto.enabled) === opt.auto;
            return (
              <label
                key={`${opt.value}-${opt.auto}`}
                className={`flex items-center gap-2 text-xs ${disabled ? "text-muted-foreground/50" : "text-muted-foreground"}`}
              >
                <input
                  type="radio"
                  name="default-permission-mode"
                  disabled={disabled}
                  checked={checked}
                  onChange={() => {
                    setDefaultMode(opt.value);
                    setAuto({ ...auto, enabled: opt.auto });
                  }}
                />
                {opt.label}
                {disabled && " (not supported on this OS)"}
              </label>
            );
          })}
        </div>
      </div>

      <div className="border-t border-border pt-4 space-y-4">
        <div className="text-xs font-semibold text-foreground">Auto-approval (LLM-assisted)</div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={Boolean(auto.enabled)}
            onChange={(e) => setAuto({ ...auto, enabled: e.target.checked })}
          />
          Enabled
        </label>
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input
            type="checkbox"
            checked={Boolean(auto.allow_destructive)}
            onChange={(e) => setAuto({ ...auto, allow_destructive: e.target.checked })}
          />
          Allow destructive actions
        </label>
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Permission model</label>
          <div className="flex items-center gap-2">
            <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={auto.model || undefined}>
              {auto.model || "(not set — falls back to small model)"}
            </div>
            <Button size="sm" variant="outline" type="button" onClick={() => setPermDialogOpen(true)} className="h-8 text-xs">
              Change…
            </Button>
          </div>
          <ModelDialog
            open={permDialogOpen}
            onClose={() => setPermDialogOpen(false)}
            purpose="permission"
            currentValues={{ permission: auto.model ?? "" }}
            onPick={(_, selectedModel) => {
              setAuto({ ...auto, model: selectedModel });
            }}
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">Prompt</label>
          <textarea
            value={auto.prompt ?? ""}
            onChange={(e) => setAuto({ ...auto, prompt: e.target.value })}
            rows={3}
            className="w-full rounded-md border border-border bg-muted px-2 py-1.5 text-xs text-foreground"
          />
        </div>
        <div className="grid grid-cols-3 gap-2">
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Max context bytes</label>
            <Input
              type="number"
              value={auto.max_context_bytes ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_bytes: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Max sources</label>
            <Input
              type="number"
              value={auto.max_context_sources ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_sources: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Max lines/source</label>
            <Input
              type="number"
              value={auto.max_context_lines_per_source ?? 0}
              onChange={(e) => setAuto({ ...auto, max_context_lines_per_source: Number(e.target.value) })}
              className="h-8 text-xs"
            />
          </div>
        </div>
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
