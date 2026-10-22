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
 * Settings for the Pulse assistant: its model slot, system prompt, auto-receive
 * of stop indicators from other sessions, and auto-compaction of its own
 * context.
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

  // Auto-receive of stop indicators from other sessions.
  const [autoReceive, setAutoReceive] = useState(false);
  const [autoGapMs, setAutoGapMs] = useState(2000);
  const [autoReceivePending, setAutoReceivePending] = useState<"save" | null>(null);
  const [autoReceiveError, setAutoReceiveError] = useState<string | null>(null);

  // Auto-compaction of the assistant's own context.
  const [autoCompact, setAutoCompact] = useState(false);
  const [compactTokenThreshold, setCompactTokenThreshold] = useState(250000);
  const [compactPctThreshold, setCompactPctThreshold] = useState(80);
  const [autoCompactPending, setAutoCompactPending] = useState<"save" | null>(null);
  const [autoCompactError, setAutoCompactError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const [prompt, slot, autoReceive, autoGap, autoCompact, compactThresholds] =
        await Promise.all([
          api.getPulseSystemPrompt(),
          api.getPulseModel(),
          api.getPulseAutoReceive(),
          api.getPulseAutoGapMs(),
          api.getPulseAutoCompact(),
          api.getPulseCompactThresholds(),
        ]);
      setSaved(prompt);
      setDraft(prompt.prompt);
      setModel(slot.model);
      setAutoReceive(autoReceive.enabled);
      setAutoGapMs(autoGap.gap_ms);
      setAutoCompact(autoCompact.enabled);
      setCompactTokenThreshold(compactThresholds.token_threshold);
      setCompactPctThreshold(compactThresholds.pct_threshold);
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

  const writeAutoReceive = async (enabled: boolean) => {
    setAutoReceivePending("save");
    setAutoReceiveError(null);
    try {
      await api.setPulseAutoReceive(enabled);
      const fresh = await api.getPulseAutoReceive();
      setAutoReceive(fresh.enabled);
    } catch (err) {
      console.error("PulseAssistantForm: changing auto-receive failed:", err);
      setAutoReceiveError(`Changing auto-receive failed: ${errorText(err)}`);
    } finally {
      setAutoReceivePending(null);
    }
  };

  const writeAutoGapMs = async (gapMs: number) => {
    setAutoReceivePending("save");
    setAutoReceiveError(null);
    try {
      await api.setPulseAutoGapMs(gapMs);
      const fresh = await api.getPulseAutoGapMs();
      setAutoGapMs(fresh.gap_ms);
    } catch (err) {
      console.error("PulseAssistantForm: changing auto gap failed:", err);
      setAutoReceiveError(`Changing auto gap failed: ${errorText(err)}`);
    } finally {
      setAutoReceivePending(null);
    }
  };

  const writeAutoCompact = async (enabled: boolean) => {
    setAutoCompactPending("save");
    setAutoCompactError(null);
    try {
      await api.setPulseAutoCompact(enabled);
      const fresh = await api.getPulseAutoCompact();
      setAutoCompact(fresh.enabled);
    } catch (err) {
      console.error("PulseAssistantForm: changing auto-compact failed:", err);
      setAutoCompactError(`Changing auto-compact failed: ${errorText(err)}`);
    } finally {
      setAutoCompactPending(null);
    }
  };

  const writeCompactThresholds = async () => {
    setAutoCompactPending("save");
    setAutoCompactError(null);
    try {
      await api.setPulseCompactThresholds(compactTokenThreshold, compactPctThreshold);
      const fresh = await api.getPulseCompactThresholds();
      setCompactTokenThreshold(fresh.token_threshold);
      setCompactPctThreshold(fresh.pct_threshold);
    } catch (err) {
      console.error("PulseAssistantForm: changing compact thresholds failed:", err);
      setAutoCompactError(`Changing compact thresholds failed: ${errorText(err)}`);
    } finally {
      setAutoCompactPending(null);
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

      <div className="space-y-1.5 border-t border-border pt-2">
        <h3 className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
          Auto-receive stop indicators
        </h3>
        <p className="text-xs text-muted-foreground">
          When on, the Pulse assistant automatically receives stop indicators
          (turn done, permission ask, question ask, task complete) from other
          sessions. Off by default.
        </p>
        <div className="flex items-center gap-2">
          <label className="text-xs text-foreground flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={autoReceive}
              onChange={(e) => void writeAutoReceive(e.target.checked)}
              disabled={autoReceivePending !== null}
              className="h-4 w-4 rounded border-border text-primary accent-primary focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            />
            Auto-receive stop indicators
          </label>
          {autoReceivePending === "save" && (
            <span className="text-xs text-muted-foreground">Saving…</span>
          )}
          {autoReceiveError && (
            <span role="alert" className="text-xs text-red-400">{autoReceiveError}</span>
          )}
        </div>
        <div className="ml-6 flex items-center gap-2">
          <label className="text-xs text-muted-foreground">Quiet gap (ms)</label>
          <input
            type="number"
            min="0"
            value={autoGapMs}
            onChange={(e) => setAutoGapMs(Number(e.target.value))}
            disabled={autoReceivePending !== null || !autoReceive}
            className="h-7 w-20 rounded-md border border-border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
          />
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={autoReceivePending !== null || !autoReceive}
            onClick={() => void writeAutoGapMs(autoGapMs)}
          >
            {autoReceivePending === "save" ? "Saving…" : "Save gap"}
          </Button>
        </div>
      </div>

      <div className="space-y-1.5 border-t border-border pt-2">
        <h3 className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">
          Auto-compact context
        </h3>
        <p className="text-xs text-muted-foreground">
          When on, auto-comacts the assistant&rsquo;s own context when it hits the
          token count or percent of model context, whichever comes first. Off by
          default.
        </p>
        <div className="flex items-center gap-2">
          <label className="text-xs text-foreground flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={autoCompact}
              onChange={(e) => void writeAutoCompact(e.target.checked)}
              disabled={autoCompactPending !== null}
              className="h-4 w-4 rounded border-border text-primary accent-primary focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            />
            Auto-compact context
          </label>
          {autoCompactPending === "save" && (
            <span className="text-xs text-muted-foreground">Saving…</span>
          )}
          {autoCompactError && (
            <span role="alert" className="text-xs text-red-400">{autoCompactError}</span>
          )}
        </div>
        <div className="ml-6 space-y-1">
          <div className="flex items-center gap-2">
            <label className="text-xs text-muted-foreground">Token threshold</label>
            <input
              type="number"
              min="0"
              value={compactTokenThreshold}
              onChange={(e) => setCompactTokenThreshold(Number(e.target.value))}
              disabled={autoCompactPending !== null || !autoCompact}
              className="h-7 w-24 rounded-md border border-border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            />
          </div>
          <div className="flex items-center gap-2">
            <label className="text-xs text-muted-foreground">Pct threshold</label>
            <input
              type="number"
              min="0"
              max="100"
              value={compactPctThreshold}
              onChange={(e) => setCompactPctThreshold(Number(e.target.value))}
              disabled={autoCompactPending !== null || !autoCompact}
              className="h-7 w-20 rounded-md border border-border bg-background px-2 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            />
          </div>
          <Button
            size="sm"
            variant="outline"
            className="h-7 text-xs"
            disabled={autoCompactPending !== null || !autoCompact}
            onClick={() => void writeCompactThresholds()}
          >
            {autoCompactPending === "save" ? "Saving…" : "Save thresholds"}
          </Button>
        </div>
      </div>

      <div className="space-y-1.5">
        <label htmlFor="pulse-assistant-prompt" className="text-xs text-muted-foreground">
          Extra instructions
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
        <p className="text-xs text-muted-foreground">
          These are appended to the built-in prompt, which keeps the tool list, the
          per-line stop-report format and the activation-gate rules. Use them to
          change how the assistant answers (length, tone), not to delete those rules.
        </p>
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
            {pending === "reset" ? "Resetting…" : "Clear instructions"}
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
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">
            Show the full prompt the assistant receives
          </summary>
          <pre
            data-testid="pulse-assistant-effective-prompt"
            className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-muted p-3 font-mono text-xs text-foreground"
          >
            {saved ? saved.effective : ""}
          </pre>
        </details>
      </div>
    </div>
  );
}
