import { useCallback, useEffect, useState } from "react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import { Loader2 } from "lucide-react";

export default function FeaturesForm() {
  const [memoryEnabled, setMemoryEnabled] = useState(false);
  const [docPromptEnabled, setDocPromptEnabled] = useState(false);
  const [suggestedResponsesEnabled, setSuggestedResponsesEnabled] = useState(false);
  const [communicationStyleEnabled, setCommunicationStyleEnabled] = useState(true);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const cfg = await api.getFeaturesConfig();
      setMemoryEnabled(cfg.memory_enabled);
      setDocPromptEnabled(cfg.doc_prompt_enabled);
      // Absent on an older server: default to ON so the toggle reflects the
      // server's actual default rather than silently reading as "off".
      setSuggestedResponsesEnabled(cfg.suggested_responses_enabled ?? true);
      // Same default-ON rule for the communication-style standard.
      setCommunicationStyleEnabled(cfg.communication_style_enabled ?? true);
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
      await api.setFeaturesConfig(
        memoryEnabled,
        docPromptEnabled,
        suggestedResponsesEnabled,
        communicationStyleEnabled,
      );
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
      <h2 className="text-sm font-semibold text-foreground">Features</h2>
      {error && <div className="text-xs text-red-400">{error}</div>}
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={memoryEnabled} onChange={(e) => setMemoryEnabled(e.target.checked)} />
        Memory injection enabled
      </label>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={docPromptEnabled} onChange={(e) => setDocPromptEnabled(e.target.checked)} />
        Documentation-first prompt enabled
      </label>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={suggestedResponsesEnabled}
          onChange={(e) => setSuggestedResponsesEnabled(e.target.checked)}
        />
        Suggested responses enabled
      </label>
      <p className="text-xs text-muted-foreground/80">
        When on, the agent ends each reply with up to five suggested follow-ups.
        They appear as clickable options under the chat input — clicking one
        fills the composer.
      </p>
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input
          type="checkbox"
          checked={communicationStyleEnabled}
          onChange={(e) => setCommunicationStyleEnabled(e.target.checked)}
        />
        Communication style standard enabled
      </label>
      <p className="text-xs text-muted-foreground/80">
        When on, the agent follows a plain-language standard: short active
        sentences (ASD-STE100), conditions before actions, one action per
        numbered step, consistent names, and code or commands before
        explanations. You can append your own rules under Prompts.
      </p>
      <Button size="sm" onClick={save} disabled={saving} className="h-8 text-xs">
        {saving && <Loader2 className="w-3.5 h-3.5 animate-spin mr-1.5" />}
        Save
      </Button>
    </div>
  );
}
