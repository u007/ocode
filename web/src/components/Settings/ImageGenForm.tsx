import { useCallback, useEffect, useState } from "react";
import { api, type ImageGenConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: ImageGenConfig = { enabled: false, provider: "gemini", model: "", output_path: "", timeout: 0 };

export default function ImageGenForm() {
  const [cfg, setCfg] = useState<ImageGenConfig>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getImageGenConfig());
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
      setCfg(await api.setImageGenConfig(cfg));
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
      <h2 className="text-sm font-semibold text-foreground">Image Generation</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={cfg.enabled} onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Provider (gemini / openai / novita / deepinfra)</label>
        <Input value={cfg.provider} onChange={(e) => setCfg({ ...cfg, provider: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Model (blank = provider default)</label>
        <Input value={cfg.model} onChange={(e) => setCfg({ ...cfg, model: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Output path (blank = working directory)</label>
        <Input value={cfg.output_path ?? ""} onChange={(e) => setCfg({ ...cfg, output_path: e.target.value })} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Timeout (s, 0 = default)</label>
        <Input
          type="number"
          value={cfg.timeout ?? 0}
          onChange={(e) => setCfg({ ...cfg, timeout: Number(e.target.value) })}
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
