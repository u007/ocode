import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { useChatSelector } from "../../stores/chatStore";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

export default function ModelDefaultsForm() {
  // The small model itself is picked through the ModelDialog (which persists
  // immediately on select), so its value is read from the chat store — seeded
  // at boot, refreshed on every dialog open/select. This form only owns the
  // enabled toggle and the recap config.
  const smallModel = useChatSelector((s) => s.smallModel);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [recapDialogOpen, setRecapDialogOpen] = useState(false);
  const [explorerDialogOpen, setExplorerDialogOpen] = useState(false);
  const [contextDialogOpen, setContextDialogOpen] = useState(false);
  const [smallModelEnabled, setSmallModelEnabled] = useState(false);
  const [recapModel, setRecapModel] = useState("");
  const [recapEnabled, setRecapEnabled] = useState(false);
  const [recapTimeout, setRecapTimeout] = useState(120);
  // Explorer (explore/scout) and context (context/doc-sync) agent models. Off
  // or unset falls back to the small model, then the main model.
  const [explorerModel, setExplorerModel] = useState("");
  const [explorerEnabled, setExplorerEnabled] = useState(false);
  const [contextModel, setContextModel] = useState("");
  const [contextEnabled, setContextEnabled] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [sm, recap, explorer, context] = await Promise.all([
        api.getSmallModelWithEnabled(),
        api.getRecapConfig(),
        api.getExplorerModel(),
        api.getContextModel(),
      ]);
      setSmallModelEnabled(sm.enabled);
      setRecapModel(recap.recap_model);
      setRecapEnabled(recap.recap_model_enabled);
      setRecapTimeout(recap.recap_timeout_seconds);
      setExplorerModel(explorer.model);
      setExplorerEnabled(explorer.enabled);
      setContextModel(context.model);
      setContextEnabled(context.enabled);
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
      // The small model is persisted by the ModelDialog on select — writing
      // our own copy here would revert a pick made just before Save.
      await api.setSmallModelEnabled(smallModelEnabled);
      await api.setRecapConfig(recapModel, recapEnabled, recapTimeout);
      // "auto" clears the override (fall back to small model, then main model).
      await api.setExplorerModel(explorerModel || "auto");
      await api.setExplorerModelEnabled(explorerEnabled);
      await api.setContextModel(contextModel || "auto");
      await api.setContextModelEnabled(contextEnabled);
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
      <h2 className="text-sm font-semibold text-foreground">Model Defaults, Recap & Agent Models</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Small model</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={smallModel || undefined}>
            {smallModel || "Not set"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog open={dialogOpen} onClose={() => setDialogOpen(false)} purpose="small" />
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={smallModelEnabled} onChange={(e) => setSmallModelEnabled(e.target.checked)} />
        Small model enabled
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Recap model</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={recapModel || undefined}>
            {recapModel || "Not set"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setRecapDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={recapDialogOpen}
          onClose={() => setRecapDialogOpen(false)}
          purpose="recap"
          onPick={(_, m) => setRecapModel(m)}
          currentValues={{ recap: recapModel }}
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={recapEnabled} onChange={(e) => setRecapEnabled(e.target.checked)} />
        Recap model enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Recap timeout (seconds)</label>
        <Input
          type="number"
          value={recapTimeout}
          onChange={(e) => setRecapTimeout(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Explorer model (explore/scout)</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={explorerModel || undefined}>
            {explorerModel || "Not set — falls back to small model, then main model"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setExplorerDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={explorerDialogOpen}
          onClose={() => setExplorerDialogOpen(false)}
          purpose="explorer"
          onPick={(_, m) => setExplorerModel(m)}
          currentValues={{ explorer: explorerModel }}
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={explorerEnabled} onChange={(e) => setExplorerEnabled(e.target.checked)} />
        Explorer model enabled
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Context model (context/doc-sync)</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate" title={contextModel || undefined}>
            {contextModel || "Not set — falls back to small model, then main model"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setContextDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={contextDialogOpen}
          onClose={() => setContextDialogOpen(false)}
          purpose="context"
          onPick={(_, m) => setContextModel(m)}
          currentValues={{ context: contextModel }}
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={contextEnabled} onChange={(e) => setContextEnabled(e.target.checked)} />
        Context model enabled
      </label>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
