import { useCallback, useEffect, useState } from "react";
import { api, type CompactConfig } from "../../api/client";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

const EMPTY: CompactConfig = {
  enabled: false, summary_provider: "", summary_model: "", token_threshold: 0,
  keep_recent_turns: 0, keep_recent_tokens: 0, min_messages: 0,
  summary_timeout_seconds: 0, summary_max_retries: 0, max_summary_input_tokens: 0,
};

const FIELDS: { key: keyof CompactConfig; label: string; type: "text" | "number" | "checkbox" }[] = [
  { key: "enabled", label: "Enabled", type: "checkbox" },
  { key: "summary_provider", label: "Summary provider", type: "text" },
  { key: "summary_model", label: "Summary model", type: "text" },
  { key: "token_threshold", label: "Token threshold (0-1)", type: "number" },
  { key: "keep_recent_turns", label: "Keep recent turns", type: "number" },
  { key: "keep_recent_tokens", label: "Keep recent tokens", type: "number" },
  { key: "min_messages", label: "Min messages", type: "number" },
  { key: "summary_timeout_seconds", label: "Summary timeout (s)", type: "number" },
  { key: "summary_max_retries", label: "Summary max retries", type: "number" },
  { key: "max_summary_input_tokens", label: "Max summary input tokens", type: "number" },
];

export default function CompactForm() {
  const [cfg, setCfg] = useState<CompactConfig>(EMPTY);
  const [summaryDialogOpen, setSummaryDialogOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setCfg(await api.getCompactConfig());
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
      setCfg(await api.setCompactConfig(cfg));
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
      <h2 className="text-sm font-semibold text-zinc-200">Compact</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      {FIELDS.map((f) =>
        f.key === "summary_model" ? (
          <div key={f.key} className="space-y-1.5">
            <label className="text-xs text-zinc-500">{f.label}</label>
            <div className="flex items-center gap-2">
              <div className="flex-1 h-8 px-3 rounded-md bg-zinc-800 border border-zinc-700 text-xs text-zinc-300 flex items-center truncate" title={cfg.summary_model || undefined}>
                {cfg.summary_model || "Not set"}
              </div>
              <Button size="sm" variant="outline" type="button" onClick={() => setSummaryDialogOpen(true)} className="h-8 text-xs">
                Change…
              </Button>
            </div>
            <ModelDialog
              open={summaryDialogOpen}
              onClose={() => setSummaryDialogOpen(false)}
              purpose="summary"
              onPick={(_, m) => setCfg({ ...cfg, summary_model: m })}
              currentValues={{ summary: cfg.summary_model }}
            />
          </div>
        ) : f.type === "checkbox" ? (
          <label key={f.key} className="flex items-center gap-2 text-xs text-zinc-400">
            <input
              type="checkbox"
              checked={Boolean(cfg[f.key])}
              onChange={(e) => setCfg({ ...cfg, [f.key]: e.target.checked })}
            />
            {f.label}
          </label>
        ) : (
          <div key={f.key} className="space-y-1.5">
            <label className="text-xs text-zinc-500">{f.label}</label>
            <Input
              type={f.type}
              value={String(cfg[f.key])}
              onChange={(e) =>
                setCfg({ ...cfg, [f.key]: f.type === "number" ? Number(e.target.value) : e.target.value })
              }
              className="h-8 text-xs"
            />
          </div>
        ),
      )}
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
