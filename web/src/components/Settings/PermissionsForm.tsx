import { useCallback, useEffect, useState } from "react";
import { api, type AutoPermissionConfig, type RelaxableConcern } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

const EMPTY_AUTO: AutoPermissionConfig = {
  enabled: false, allow_destructive: false, prompt: "",
  max_context_bytes: 0, max_context_sources: 0, max_context_lines_per_source: 0, min_confidence: 0,
  relaxed_concerns: [],
};

const DEFAULT_MODE_OPTIONS: { value: string; auto: boolean; label: string }[] = [
  { value: "normal", auto: false, label: "Normal" },
  { value: "normal", auto: true, label: "Normal + auto-approve" },
  { value: "yolo", auto: false, label: "Yolo" },
  { value: "sandbox", auto: false, label: "Sandbox" },
];

export default function PermissionsForm() {
  // This form is process-wide settings. The live permission mode is now PER
  // CHAT SESSION (toggled from the sidebar pill, /yolo, /sandbox), so it is
  // deliberately not edited here: a session-less live-mode write would be the
  // process-global footgun this design removes. Settings owns the persisted
  // default a brand-new session starts in.
  const [sandboxSupported, setSandboxSupported] = useState(true);
  // The persisted default a new TUI/web/RC session starts in. Loaded from and
  // saved to GET|PUT /api/config/ocode/permissions-mode.
  const [defaultMode, setDefaultMode] = useState<string>("normal");
  const [loadedDefaultMode, setLoadedDefaultMode] = useState<string>("normal");
  const [auto, setAuto] = useState<AutoPermissionConfig>(EMPTY_AUTO);
  // The checkbox catalog is served by Go (next to the Jev rubric) so the list,
  // the rubric and the chat judge prompt share one vocabulary.
  const [concerns, setConcerns] = useState<RelaxableConcern[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [permDialogOpen, setPermDialogOpen] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [perms, autoCfg, defaultModeCfg, concernsCfg] = await Promise.all([
        api.getPermissions(),
        api.getAutoPermissionConfig(),
        api.getPermissionModeConfig(),
        api.getPermissionConcerns(),
      ]);
      setSandboxSupported(perms.sandbox_supported ?? true);
      setDefaultMode(defaultModeCfg.mode || "normal");
      setLoadedDefaultMode(defaultModeCfg.mode || "normal");
      setAuto({ ...EMPTY_AUTO, ...autoCfg });
      setConcerns(concernsCfg?.concerns ?? []);
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

  // Enforcement is stored inverted: a ticked box means "enforce", so it is the
  // ABSENCE of the key from relaxed_concerns. Saving writes the unticked keys in
  // catalog order, which keeps the persisted list stable and diffable.
  const relaxedSet = new Set(auto.relaxed_concerns ?? []);
  const toggleConcern = (key: string, enforced: boolean) => {
    const next = new Set(auto.relaxed_concerns ?? []);
    if (enforced) next.delete(key);
    else next.add(key);
    setAuto({ ...auto, relaxed_concerns: concerns.filter((c) => next.has(c.key)).map((c) => c.key) });
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

      <p className="text-xs text-muted-foreground">
        The live permission mode (normal / yolo / locked / sandbox) is set per
        chat session — use the Permission pill in the chat sidebar or
        <span className="font-mono"> /yolo</span> and
        <span className="font-mono"> /sandbox</span>. It never affects other
        chats or projects.
      </p>

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

        <div className="border-t border-border pt-4 space-y-2">
          <div className="flex items-center justify-between">
            <div className="text-xs font-semibold text-foreground">Categories the judge must enforce</div>
            <div className="flex gap-2">
              <button
                type="button"
                className="text-[11px] text-muted-foreground hover:text-foreground"
                onClick={() => setAuto({ ...auto, relaxed_concerns: [] })}
              >
                All
              </button>
              <button
                type="button"
                className="text-[11px] text-muted-foreground hover:text-foreground"
                onClick={() => setAuto({ ...auto, relaxed_concerns: concerns.map((c) => c.key) })}
              >
                None
              </button>
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            Ticked categories are enforced by the LLM judge. Untick one to let it auto-approve a
            call whose ONLY concern is that category. Go's own guards — hard blocks, dangerous rm,
            out-of-scope paths — always apply, so an unticked box cannot auto-grant those.
          </p>
          {concerns.length === 0 && (
            <p className="text-xs text-muted-foreground/70">No categories reported by the server.</p>
          )}
          <div className="space-y-2">
            {concerns.map((c) => (
              <label key={c.key} className="flex items-start gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  className="mt-0.5"
                  checked={!relaxedSet.has(c.key)}
                  aria-label={`Enforce ${c.key}`}
                  onChange={(e) => toggleConcern(c.key, e.target.checked)}
                />
                <span className="min-w-0">
                  <span className="font-mono text-foreground">{c.key}</span>
                  <span className="block">{c.label}</span>
                  {c.note && <span className="block text-muted-foreground/70">{c.note}</span>}
                </span>
              </label>
            ))}
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
