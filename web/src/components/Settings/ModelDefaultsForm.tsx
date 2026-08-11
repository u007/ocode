import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function ModelDefaultsForm() {
  const [smallModel, setSmallModel] = useState("");
  const [smallModelEnabled, setSmallModelEnabled] = useState(false);
  const [recapModel, setRecapModel] = useState("");
  const [recapEnabled, setRecapEnabled] = useState(false);
  const [recapTimeout, setRecapTimeout] = useState(120);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [sm, recap] = await Promise.all([api.getSmallModelWithEnabled(), api.getRecapConfig()]);
      setSmallModel(sm.model);
      setSmallModelEnabled(sm.enabled);
      setRecapModel(recap.recap_model);
      setRecapEnabled(recap.recap_model_enabled);
      setRecapTimeout(recap.recap_timeout_seconds);
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
      await api.setSmallModel(smallModel);
      await api.setSmallModelEnabled(smallModelEnabled);
      await api.setRecapConfig(recapModel, recapEnabled, recapTimeout);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">Model Defaults & Recap</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Small model</label>
        <Input value={smallModel} onChange={(e) => setSmallModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={smallModelEnabled} onChange={(e) => setSmallModelEnabled(e.target.checked)} />
        Small model enabled
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Recap model</label>
        <Input value={recapModel} onChange={(e) => setRecapModel(e.target.value)} className="h-8 text-xs" />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={recapEnabled} onChange={(e) => setRecapEnabled(e.target.checked)} />
        Recap model enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Recap timeout (seconds)</label>
        <Input
          type="number"
          value={recapTimeout}
          onChange={(e) => setRecapTimeout(Number(e.target.value))}
          className="h-8 text-xs w-32"
        />
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
