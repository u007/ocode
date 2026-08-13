import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { useChatState } from "../../stores/chatStore";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Loader2 } from "lucide-react";
import ModelDialog from "../Layout/ModelDialog";

export default function AdvisorForm() {
  const [model, setModel] = useState("");
  const [provider, setProvider] = useState("");
  const [claudeCode, setClaudeCode] = useState(false);
  const [checkpoints, setCheckpoints] = useState<string[]>([]);
  const [runtimeEnabled, setRuntimeEnabled] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // The advisor model is picked through the ModelDialog (which persists
  // immediately on select); the chat store is the live value, with the
  // API-loaded copy as fallback so the display/save never revert a fresh pick.
  const { advisorModel } = useChatState();
  const currentModel = advisorModel || model;

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [full, enabled] = await Promise.all([api.getAdvisorFull(), api.getAdvisorEnabled()]);
      setModel(full.model);
      setProvider(full.provider);
      setClaudeCode(full.claude_code);
      setCheckpoints(full.checkpoints ?? []);
      setRuntimeEnabled(enabled.enabled);
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
      await api.setAdvisorFull({ model: currentModel, provider, claude_code: claudeCode, checkpoints });
      await api.setAdvisorEnabled(runtimeEnabled);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggleCheckpoint = (name: string) => {
    setCheckpoints((prev) => (prev.includes(name) ? prev.filter((c) => c !== name) : [...prev, name]));
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
      <h2 className="text-sm font-semibold text-zinc-200">Advisor</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}

      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={runtimeEnabled} onChange={(e) => setRuntimeEnabled(e.target.checked)} />
        Enabled for this session (not persisted)
      </label>

      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Provider</label>
        <Input value={provider} onChange={(e) => setProvider(e.target.value)} className="h-8 text-xs" />
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-zinc-500">Model</label>
        <div className="flex items-center gap-2">
          <div className="flex-1 h-8 px-3 rounded-md bg-zinc-800 border border-zinc-700 text-xs text-zinc-300 flex items-center truncate" title={currentModel || undefined}>
            {currentModel || "Not set"}
          </div>
          <Button size="sm" variant="outline" type="button" onClick={() => setDialogOpen(true)} className="h-8 text-xs">
            Change…
          </Button>
        </div>
        <ModelDialog
          open={dialogOpen}
          onClose={() => setDialogOpen(false)}
          purpose="advisor"
          onPick={(_, selectedModel, modelInfo) => {
            setModel(selectedModel);
            if (modelInfo) setProvider(modelInfo.provider);
          }}
        />
      </div>
      <label className="flex items-center gap-2 text-xs text-zinc-400">
        <input type="checkbox" checked={claudeCode} onChange={(e) => setClaudeCode(e.target.checked)} />
        Use Claude Code CLI as advisor backend
      </label>

      <div className="space-y-1.5">
        <div className="text-xs text-zinc-500">Checkpoints</div>
        {["done", "plan"].map((cp) => (
          <label key={cp} className="flex items-center gap-2 text-xs text-zinc-400">
            <input type="checkbox" checked={checkpoints.includes(cp)} onChange={() => toggleCheckpoint(cp)} />
            {cp}
          </label>
        ))}
      </div>

      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
