import type { ModelPromptInfo } from "../../api/types";

interface Props {
  prompt?: ModelPromptInfo;
  model?: string;
}

// Token estimate formatting, mirroring the TUI's formatTok (1234 → "1.2k").
function formatTok(n: number): string {
  if (!isFinite(n) || n < 0) return "0";
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(1)}M`;
}

// Base filename of the prompt source, mirroring the TUI's filepath.Base.
function basename(path: string): string {
  return path.split(/[\\/]/).pop() || path;
}

/**
 * ModelPromptRow — the web/desktop mirror of the TUI's "◆ Model prompt" bottom
 * chrome row (internal/tui/model.go renderModelContextRow + the Kaizen
 * "directives active" notice). Renders nothing when the active model has no
 * custom prompt and no force-injected Kaizen directives, so untuned sessions
 * stay visually clean. Single-line and clamped like the TUI's MaxHeight(1).
 *
 * Data source: TUIStatus.model_prompt, populated by the shared status pipeline
 * (server buildStatusSnapshot for headless/desktop, the TUI's
 * buildTUIStatusSnapshot in bridged mode).
 */
export default function ModelPromptRow({ prompt, model }: Props) {
  const source = prompt?.kind ? `${basename(prompt.path || "")} (${prompt.kind})` : "";
  const tok = prompt?.tokens ? `· ~${formatTok(prompt.tokens)} tok` : "";
  const modelPart = model ? `· ${model}` : "";
  const kz = (prompt?.kaizen ?? [])
    .map((k) => `${k.name} → ${k.tuned_for}${k.stack ? ` (${k.stack})` : ""}`)
    .join(", ");

  const line = source ? `◆ Model prompt · ${source} ${tok} ${modelPart}`.replace(/\s+/g, " ").trim() : "";
  if (!line && !kz) return null;

  return (
    <div
      data-testid="model-prompt-row"
      className="border-t border-border px-4 py-1 text-xs text-muted-foreground/90 truncate"
      title={kz || line}
    >
      {line && <span>{line}</span>}
      {line && kz && <span className="mx-2 select-none text-muted-foreground/40">|</span>}
      {kz && (
        <span className="text-blue-400/90">
          Kaizen directives active (force-injected): {kz}
        </span>
      )}
    </div>
  );
}