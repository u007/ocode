import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function LimitsForm() {
  const [maxSteps, setMaxSteps] = useState(0);
  const [maxImageDim, setMaxImageDim] = useState(0);
  const [maxConcurrentAgents, setMaxConcurrentAgents] = useState(0);
  const [undoMaxAgeDelta, setUndoMaxAgeDelta] = useState(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getLimitsConfig();
      setMaxSteps(cfg.max_steps);
      setMaxImageDim(cfg.image_max_dim);
      setMaxConcurrentAgents(cfg.max_concurrent_agents);
      setUndoMaxAgeDelta(cfg.undo_max_age_delta);
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
      await api.setLimitsConfig({
        max_steps: maxSteps, image_max_dim: maxImageDim,
        max_concurrent_agents: maxConcurrentAgents, undo_max_age_delta: undoMaxAgeDelta,
      });
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
      <h2 className="text-sm font-semibold text-foreground">Limits</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Max steps (0 = default cap of 100)</label>
        <Input
          type="number"
          value={maxSteps}
          onChange={(e) => setMaxSteps(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Max image dimension (px)</label>
        <Input
          type="number"
          value={maxImageDim}
          onChange={(e) => setMaxImageDim(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Max concurrent agents</label>
        <Input
          type="number"
          value={maxConcurrentAgents}
          onChange={(e) => setMaxConcurrentAgents(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Undo max age delta</label>
        <Input
          type="number"
          value={undoMaxAgeDelta}
          onChange={(e) => setUndoMaxAgeDelta(Number(e.target.value))}
          className="h-8 text-xs"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
