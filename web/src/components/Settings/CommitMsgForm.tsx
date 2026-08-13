import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

export default function CommitMsgForm() {
  const [model, setModel] = useState("");
  const [modelDialogOpen, setModelDialogOpen] = useState(false);
  const [prompt, setPrompt] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getCommitMsgConfig();
      setModel(cfg.commit_msg_model);
      setPrompt(cfg.commit_msg_prompt);
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
      await api.setCommitMsgConfig(model, prompt);
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
      <h2 className="text-sm font-semibold text-zinc-200">Commit Message</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-zinc-800 border border-zinc-700 text-xs text-zinc-300 flex items-center truncate" title={model || undefined}>
            {model || "Not set"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setModelDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={modelDialogOpen}
          onClose={() => setModelDialogOpen(false)}
          purpose="commit"
          onPick={(_, m) => setModel(m)}
          currentValues={{ commit: model }}
        />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Prompt</label>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          rows={4}
          className="w-full rounded-md border border-zinc-700 bg-zinc-800 px-2 py-1.5 text-xs text-zinc-200"
        />
      </div>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
