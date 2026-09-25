import { useEffect, useState } from "react";
import type { ChatDisplayOverride, ChatVerbosityPreset } from "@/api/types";
import {
  DEFAULT_CHAT_VERBOSITY_CONFIG,
  saveChatVerbosityConfig,
  useChatVerbosity,
} from "@/lib/chatVerbosity";

const PRESETS: Array<{ id: ChatVerbosityPreset; label: string; description: string }> = [
  { id: "full", label: "Full", description: "Show everything (current default)." },
  {
    id: "balanced",
    label: "Balanced",
    description: "Hide older thinking and tool-call details; keep tool output with a short preview.",
  },
  {
    id: "quiet",
    label: "Quiet",
    description: "Show only headers; tool output and activity notices stay collapsed until opened.",
  },
];

const OVERRIDE_CATEGORIES: Array<{
  key: keyof typeof DEFAULT_CHAT_VERBOSITY_CONFIG.overrides;
  label: string;
}> = [
  { key: "older_thinking", label: "Older thinking" },
  { key: "tool_calls", label: "Tool call details" },
  { key: "tool_output", label: "Tool output" },
  { key: "activity_notices", label: "Activity notices" },
];

const OVERRIDE_OPTIONS: Array<{ value: ChatDisplayOverride; label: string }> = [
  { value: "preset", label: "Follow preset" },
  { value: "expanded", label: "Always expanded" },
  { value: "collapsed", label: "Always collapsed" },
];

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function presetOverrides(): typeof DEFAULT_CHAT_VERBOSITY_CONFIG.overrides {
  return { ...DEFAULT_CHAT_VERBOSITY_CONFIG.overrides };
}

export default function ChatDisplayForm() {
  const { config, loading, error: loadError } = useChatVerbosity();
  const [preset, setPreset] = useState<ChatVerbosityPreset>(config.preset);
  const [overrides, setOverrides] = useState(config.overrides);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // The shared store is the single source of truth: re-sync the draft when a
  // fetch or another client's save publishes a new config.
  useEffect(() => {
    setPreset(config.preset);
    setOverrides(config.overrides);
  }, [config]);

  const save = async () => {
    setSaving(true);
    setSaveError(null);
    try {
      await saveChatVerbosityConfig({ preset, overrides });
    } catch (error) {
      setSaveError(`Couldn't save chat display settings: ${errorText(error)}`);
    } finally {
      setSaving(false);
    }
  };

  const resetOverrides = () => {
    // Reset only the category selects; the preset radio is deliberately kept.
    setOverrides(presetOverrides());
    setSaveError(null);
  };

  const displayError = saveError ?? loadError;

  if (loading && !displayError) {
    return <div className="p-6 text-sm text-muted-foreground">Loading chat display settings…</div>;
  }

  return (
    <div className="space-y-6 p-6">
      <div>
        <h2 className="text-sm font-semibold">Chat display</h2>
        <p className="mt-1 text-xs text-muted-foreground">
          Controls how much detail the rendered chat shows — thinking, tool calls, tool output, and
          activity notices. Display only: the stored transcript, search, and what the model sees are
          unchanged.
        </p>
        <p className="mt-1 text-xs text-muted-foreground">
          The latest thinking block always stays expanded.
        </p>
      </div>

      {displayError && (
        <div role="alert" className="rounded border border-red-800/60 bg-red-950/30 px-3 py-2 text-xs text-red-300">
          {displayError}
        </div>
      )}

      <fieldset className="space-y-2">
        <legend className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Preset
        </legend>
        {PRESETS.map((option) => (
          <label
            key={option.id}
            className="flex cursor-pointer items-start gap-2 rounded border border-border px-3 py-2 text-xs hover:bg-muted"
          >
            <input
              type="radio"
              name="chat-verbosity-preset"
              value={option.id}
              checked={preset === option.id}
              onChange={() => setPreset(option.id)}
              onClick={() => setPreset(option.id)}
              className="mt-0.5"
            />
            <span>
              <span className="font-medium text-foreground">{option.label}</span>
              <span className="ml-2 text-muted-foreground">{option.description}</span>
            </span>
          </label>
        ))}
      </fieldset>

      <div className="space-y-3">
        <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Category overrides
        </div>
        {OVERRIDE_CATEGORIES.map(({ key, label }) => (
          <label key={key} className="flex items-center justify-between gap-3 text-xs">
            <span className="text-foreground">{label}</span>
            <select
              aria-label={label}
              className="rounded border border-border bg-background px-2 py-1 text-xs text-foreground"
              value={overrides[key]}
              onChange={(event) =>
                setOverrides((current) => ({
                  ...current,
                  [key]: event.target.value as ChatDisplayOverride,
                }))
              }
            >
              {OVERRIDE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          </label>
        ))}
      </div>

      <div className="flex items-center justify-between gap-3 border-t border-border pt-4">
        <button
          type="button"
          onClick={resetOverrides}
          className="rounded border border-border px-3 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          Reset overrides
        </button>
        <button
          type="button"
          onClick={() => void save()}
          disabled={saving}
          className="rounded bg-primary px-3 py-1 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          {saving ? "Saving…" : "Save changes"}
        </button>
      </div>
    </div>
  );
}
