import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { api } from "../../api/client";
import { Button } from "../ui/button";
import type { PromptOverrideEntry } from "../../api/types";

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Custom instructions for every configurable built-in system prompt.
 *
 * Nothing is optimistic. Each save or reset round-trips and then shows the
 * entry the server returned, so the card only ever displays a value the server
 * accepted.
 */
export default function PromptsForm() {
  const [entries, setEntries] = useState<PromptOverrideEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setError(null);
    try {
      const res = await api.getPromptOverrides();
      setEntries(res.prompts);
    } catch (err) {
      console.error("PromptsForm: loading the system prompts failed:", err);
      setError(`Loading failed: ${errorText(err)}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const replaceEntry = (fresh: PromptOverrideEntry) => {
    setEntries((prev) => prev.map((e) => (e.key === fresh.key ? fresh : e)));
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="w-5 h-5 text-muted-foreground animate-spin" />
      </div>
    );
  }

  return (
    <div className="p-6 max-w-2xl space-y-4">
      <h2 className="text-sm font-semibold text-foreground">System Prompts</h2>
      <p className="text-[11px] text-muted-foreground/80">
        When enabled and not empty, the text is appended to the built-in prompt.
        Disabled or empty uses the built-in prompt unchanged.
      </p>
      {error && (
        <div role="alert" className="text-xs text-red-400">
          {error}
        </div>
      )}
      {entries.map((entry) => (
        <PromptCard key={entry.key} entry={entry} onSaved={replaceEntry} />
      ))}
    </div>
  );
}

function PromptCard({
  entry,
  onSaved,
}: {
  entry: PromptOverrideEntry;
  onSaved: (fresh: PromptOverrideEntry) => void;
}) {
  const [enabled, setEnabled] = useState(entry.enabled);
  const [text, setText] = useState(entry.text);
  const [pending, setPending] = useState<"save" | "reset" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const write = async (next: { enabled: boolean; text: string }, kind: "save" | "reset") => {
    setPending(kind);
    setError(null);
    try {
      const fresh = await api.setPromptOverride(entry.key, next);
      setEnabled(fresh.enabled);
      setText(fresh.text);
      onSaved(fresh);
    } catch (err) {
      console.error(`PromptsForm: ${kind} of the "${entry.key}" prompt failed:`, err);
      setError(`${kind === "save" ? "Saving" : "Resetting"} failed: ${errorText(err)}`);
    } finally {
      setPending(null);
    }
  };

  const busy = pending !== null;
  const dirty = enabled !== entry.enabled || text !== entry.text;
  const alreadyReset = !entry.enabled && entry.text === "";
  const textareaId = `prompt-override-${entry.key}`;

  return (
    <section data-testid={`prompt-card-${entry.key}`} className="space-y-2 border-t border-border pt-3">
      <div>
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-semibold text-foreground">{entry.label}</h3>
          <span className="rounded border border-border px-1.5 py-px text-[10px] text-muted-foreground">
            {entry.kind}
          </span>
        </div>
        <p className="text-xs text-muted-foreground">{entry.description}</p>
      </div>
      <label className="text-xs text-foreground flex items-center gap-2 cursor-pointer select-none">
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
          disabled={busy}
          className="h-4 w-4 rounded border-border text-primary accent-primary focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
        />
        Enable custom instructions
      </label>
      <label htmlFor={textareaId} className="sr-only">
        Custom instructions for {entry.label}
      </label>
      <textarea
        id={textareaId}
        rows={4}
        value={text}
        disabled={busy}
        onChange={(e) => setText(e.target.value)}
        placeholder="Appended to the built-in prompt when enabled."
        className="w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
      />
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          className="h-8 text-xs"
          disabled={busy || !dirty}
          onClick={() => void write({ enabled, text }, "save")}
        >
          {pending === "save" ? "Saving…" : "Save"}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="h-8 text-xs"
          disabled={busy || alreadyReset}
          onClick={() => void write({ enabled: false, text: "" }, "reset")}
        >
          {pending === "reset" ? "Resetting…" : "Reset"}
        </Button>
      </div>
      {error && (
        <div role="alert" className="text-xs text-red-400">
          {error}
        </div>
      )}
    </section>
  );
}
