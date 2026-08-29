import { useCallback, useEffect, useState } from "react";
import { api, type DiscoveryConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";

const EMPTY: DiscoveryConfig = {
  enabled: false, embedding_model: "", embedding_backend: "http", local_model_status: "none",
  local_server_url: "", pinned_skills: [], ignore_paths: [],
};

export default function DiscoveryForm() {
  const [cfg, setCfg] = useState<DiscoveryConfig>(EMPTY);
  const [pinnedText, setPinnedText] = useState("");
  const [ignoreText, setIgnoreText] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const c = await api.getDiscoveryConfig();
      setCfg(c);
      setPinnedText((c.pinned_skills ?? []).join(", "));
      setIgnoreText((c.ignore_paths ?? []).join(", "));
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
      const next: DiscoveryConfig = {
        ...cfg,
        pinned_skills: pinnedText.split(",").map((s) => s.trim()).filter(Boolean),
        ignore_paths: ignoreText.split(",").map((s) => s.trim()).filter(Boolean),
      };
      setCfg(await api.setDiscoveryConfig(next));
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
      <h2 className="text-sm font-semibold text-foreground">Discovery</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={cfg.enabled} onChange={(e) => setCfg({ ...cfg, enabled: e.target.checked })} />
        Enabled
      </label>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Embedding model</label>
        <Input
          value={cfg.embedding_model}
          onChange={(e) => setCfg({ ...cfg, embedding_model: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Embedding backend (http / local)</label>
        <Input
          value={cfg.embedding_backend}
          onChange={(e) => setCfg({ ...cfg, embedding_backend: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Local server URL</label>
        <Input
          value={cfg.local_server_url}
          onChange={(e) => setCfg({ ...cfg, local_server_url: e.target.value })}
          className="h-8 text-xs"
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Pinned skills (comma-separated)</label>
        <Input value={pinnedText} onChange={(e) => setPinnedText(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Ignore paths (comma-separated)</label>
        <Input value={ignoreText} onChange={(e) => setIgnoreText(e.target.value)} className="h-8 text-xs" />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
