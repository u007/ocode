import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import type { OcrConfig } from "../../api/types";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

export default function OcrForm() {
  const [cfg, setCfg] = useState<OcrConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getOcrConfig());
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
    if (!cfg) return;
    setSaving(true);
    setError(null);
    try {
      await api.setOcrConfig(cfg);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  if (loading || !cfg) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-zinc-500 animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-lg space-y-4">
      <h2 className="text-sm font-semibold text-zinc-200">OCR</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input
          type="checkbox"
          checked={Boolean(cfg.enabled)}
          onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })}
        />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Backend</label>
        <Input
          value={String(cfg.backend ?? "")}
          onChange={(e) => setCfg({ ...cfg, backend: e.target.value as OcrConfig["backend"] })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">OpenAI base URL</label>
        <Input
          value={String(cfg.openai?.base_url ?? "")}
          onChange={(e) => setCfg({ ...cfg, openai: { ...cfg.openai, base_url: e.target.value } })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">OpenAI model</label>
        <Input
          value={String(cfg.openai?.model ?? "")}
          onChange={(e) => setCfg({ ...cfg, openai: { ...cfg.openai, model: e.target.value } })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Paddle endpoint</label>
        <Input
          value={String(cfg.paddle?.endpoint ?? "")}
          onChange={(e) => setCfg({ ...cfg, paddle: { ...cfg.paddle, endpoint: e.target.value } })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Paddle variant</label>
        <Input
          value={String(cfg.paddle?.variant ?? "")}
          onChange={(e) => setCfg({ ...cfg, paddle: { ...cfg.paddle, variant: e.target.value } })}
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
