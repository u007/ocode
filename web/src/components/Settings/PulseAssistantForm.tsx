import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { api } from "../../api/client";
import { shortModelName } from "../../lib/pulseAssistant";
import { Button } from "../ui/button";
import ModelDialog from "../Layout/ModelDialog";
import type { PulseSystemPrompt } from "../../api/types";

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Settings for the Pulse assistant: its model slot and its system prompt.
 *
 * Nothing is optimistic. A save, a reset and a model change each round-trip and
 * then re-read what the server holds, so the form only ever shows a value the
 * server accepted. The model pick goes through the existing chooser's `pulse`
 * purpose, which hands the id back instead of writing anything itself, so this
 * form is the only writer. Both settings apply on the assistant's NEXT turn.
 */
export default function PulseAssistantForm() {
  const [saved, setSaved] = useState<PulseSystemPrompt | null>(null);
  const [draft, setDraft] = useState("");
  const [model, setModel] = useState("");
  const [loading, setLoading] = useState(true);
  const [pending, setPending] = useState<"save" | "reset" | "model" | null>(null);
  const [justSaved, setJustSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [modelDialogOpen, setModelDialogOpen] = useState(false);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [prompt, slot] = await Promise.all([api.getPulseSystemPrompt(), api.getPulseModel()]);
      setSaved(prompt);
      setDraft(prompt.prompt);
      setModel(slot.model);
    } catch (err) {
      console.error("PulseAssistantForm: loading the assistant settings failed:", err);
      setError(`Loading failed: ${errorText(err)}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const writePrompt = async (prompt: string, kind: "save" | "reset") => {
    setPending(kind);
    setError(null);
    setJustSaved(false);
    try {
      await api.setPulseSystemPrompt(prompt);
      const fresh = await api.getPulseSystemPrompt();
      setSaved(fresh);
      setDraft(fresh.prompt);
      setJustSaved(true);
    } catch (err) {
      console.error(`PulseAssistantForm: ${kind} of the system prompt failed:`, err);
      setError(`${kind === "save" ? "Saving" : "Resetting"} failed: ${errorText(err)}`);
    } finally {
      setPending(null);
    }
  };

  const writeModel = async (next: string) => {
    setPending("model");
    setError(null);
    try {
      await api.setPulseModel(next);
      setModel((await api.getPulseModel()).model);
    } catch (err) {
      console.error("PulseAssistantForm: changing the assistant model failed:", err);
      setError(`Changing the model failed: ${errorText(err)}`);
    } finally {
      setPending(null);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  const dirty = saved !== null && draft !== saved.prompt;
  const busy = pending !== null;

  return (
    <div className="p-6 max-w-2xl space-y-4">
      <h2 className="text-sm font-semibold text-foreground">Pulse assistant</h2>
      <p className="text-[11px] text-muted-foreground/80">
        The helper in the Pulse dashboard&apos;s chat drawer. Changes apply from its next turn.
      </p>
      {error && (
        <div role="alert" data-testid="pulse-assistant-form-error" className="text-xs text-red-400">
          {error}
        </div>
      )}

      <div className="space-y-1.5">
        <label className="text-xs text-muted-foreground">Model</label>
        <div className="flex items-center gap-2">
          <div
            className="flex-1 h-8 px-3 rounded-md bg-muted border border-border text-xs text-foreground flex items-center truncate"
            title={model || undefined}
            data-testid="pulse-assistant-form-model"
          >
            {model ? shortModelName(model) : "Not set — uses the server default model"}
          </div>
          <Button
            size="sm"
            variant="outline"
            type="button"
            className="h-8 text-xs"
            disabled={busy}
            onClick={() => setModelDialogOpen(true)}
          >
            Change…
          </Button>
          <Button
            size="sm"
            variant="ghost"
            type="button"
            className="h-8 text-xs"
            disabled={busy || model === ""}
            onClick={() => void writeModel("")}
          >
            Use default
          </Button>
        </div>
        <ModelDialog
          open={modelDialogOpen}
          onClose={() => setModelDialogOpen(false)}
          purpose="pulse"
          currentValues={{ pulse: model }}
          onPick={(_purpose, modelId) => void writeModel(modelId)}
        />
      </div>

      <div className="space-y-1.5">
        <label htmlFor="pulse-assistant-prompt" className="text-xs text-muted-foreground">
          System prompt
        </label>
        <textarea
          id="pulse-assistant-prompt"
          rows={12}
          value={draft}
          disabled={busy}
          onChange={(e) => {
            setDraft(e.target.value);
            setJustSaved(false);
          }}
          placeholder="Empty uses the built-in prompt (see below)."
          className="min-h-[12lh] w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
        />
        <div className="flex items-center gap-2">
          <Button size="sm" className="h-8 text-xs" disabled={busy || !dirty} onClick={() => void writePrompt(draft, "save")}>
            {pending === "save" ? "Saving…" : "Save"}
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-8 text-xs"
            disabled={busy || (saved !== null && saved.prompt === "")}
            onClick={() => void writePrompt("", "reset")}
          >
            {pending === "reset" ? "Resetting…" : "Reset to default"}
          </Button>
          {justSaved && (
            <span role="status" className="text-xs text-muted-foreground">
              Saved
            </span>
          )}
        </div>
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">Show default</summary>
          <pre
            data-testid="pulse-assistant-default-prompt"
            className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted p-3 font-mono text-xs text-foreground"
          >
            {saved ? saved.default : ""}
          </pre>
        </details>
      </div>
    </div>
  );
}
