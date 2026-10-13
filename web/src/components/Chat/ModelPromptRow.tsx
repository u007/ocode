import type { ModelPromptInfo } from "../../api/types";
import { basename } from "@/lib/utils";

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

/**
 * ModelPromptRow — the web/desktop mirror of the TUI's "◆ Model prompt" bottom
 * chrome row (internal/tui/model.go renderModelContextRow + the Kaizen
 * "directives active" notice) and the loaded-briefing segment (CLAUDE.md /
 * AGENTS.md, root-relative). Renders nothing when there is no custom prompt,
 * no force-injected Kaizen directives and no loaded briefing file. Single-line
 * and clamped like the TUI's MaxHeight(1).
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
  const briefing = prompt?.briefing_file ? `${prompt.briefing_file} loaded` : "";

  const line = source ? `◆ Model prompt · ${source} ${tok} ${modelPart}`.replace(/\s+/g, " ").trim() : "";
  if (!line && !kz && !briefing) return null;

  return (
    <div
      data-testid="model-prompt-row"
      className="border-t border-border px-4 py-1 text-xs text-muted-foreground/90 truncate"
      title={[briefing, line, kz].filter(Boolean).join(" | ")}
    >
      {briefing && <span data-testid="briefing-file">◆ {briefing}</span>}
      {briefing && (line || kz) && <span className="mx-2 select-none text-muted-foreground/40">|</span>}
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