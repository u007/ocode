import { useState } from "react";
import { highlightMatches } from "./ChatSearchBar";
import HighlightedCode from "./HighlightedCode";

const TOOL_OUTPUT_PREVIEW_LINES = 20;

// Mirrors internal/tui/tool_render.go's stripTruncationFooter: the Go layer
// appends this marker (see internal/agent/truncate.go TruncationMarkerPrefix)
// to the tool result sent to the LLM so it knows how to fetch the rest. The
// footer must stay in the LLM-facing content but is noise for the human
// reading the chat, so strip it before rendering.
const TRUNCATION_MARKER_PREFIX = "[output truncated:";

function stripTruncationFooter(content: string): string {
  const marker = "\n\n" + TRUNCATION_MARKER_PREFIX;
  const idx = content.indexOf(marker);
  if (idx >= 0) return content.slice(0, idx);
  if (content.startsWith(TRUNCATION_MARKER_PREFIX)) return "";
  return content;
}

// Language to highlight a tool's raw result with. Keyed on what the Go tools in
// internal/tool actually return, not on the file they touched:
// the write/edit family returns FormatDiff output, bash returns raw combined
// stdout/stderr. `read`'s output is source code, so its language is derived
// per-call from the file extension in its command params (see readLang
// below) rather than being a fixed entry here. Everything else (grep, glob,
// list, todo*) has no grammar that renders it correctly — those stay plain.
// Only the live stream (ChatPanel) supplies a real tool name alongside output;
// replayed history arrives as a role-"tool" message with no name, so it is
// rendered plain.
const TOOL_OUTPUT_LANG: Record<string, string> = {
  bash: "shellsession",
  write: "diff",
  edit: "diff",
  multiedit: "diff",
  replace_lines: "diff",
};

// readLang derives a Shiki language hint from the `path` field of a `read`
// tool call's JSON command, e.g. {"path":"worker/protocol/messages.go"} ->
// "go". Returns "" when the command isn't parseable JSON, has no path, or
// the path has no extension — HighlightedCode falls back to plain text.
function readLang(command: string | undefined): string {
  if (!command) return "";
  try {
    const parsed = JSON.parse(command) as Record<string, unknown>;
    const path = parsed.path ?? parsed.file_path ?? parsed.filePath;
    if (typeof path !== "string") return "";
    const ext = path.split(".").pop();
    return ext && ext !== path ? ext : "";
  } catch {
    return "";
  }
}

// ThinkingBlock renders reasoning tokens in a muted panel. The content is shown
// expanded by default so reasoning is visible immediately in the web UI.
export function ThinkingBlock({
  text,
  highlight = "",
}: {
  text: string;
  highlight?: string;
}) {
  const [open, setOpen] = useState(true);
  if (!text) return null;
  return (
    <div className="mb-3 flex justify-start">
      <div className="max-w-[95%] md:max-w-[80%] w-full rounded-lg border border-zinc-700/60 bg-zinc-900/40 px-3 py-2">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-1.5 text-xs font-medium text-zinc-400 hover:text-zinc-200"
        >
          <span>{open ? "▾" : "▸"}</span>
          <span>🧠 Thinking</span>
        </button>
        {open && (
          <pre className="mt-2 whitespace-pre-wrap break-words font-mono text-xs text-zinc-400">
            {highlight.trim() ? highlightMatches(text, highlight) : text}
          </pre>
        )}
      </div>
    </div>
  );
}

// StatusBlock renders a transient one-line status (e.g. an in-flight
// auto-permission judge consult) so the chat shows activity instead of
// looking stalled while the turn is legitimately busy off-screen.
export function StatusBlock({ text }: { text: string }) {
  if (!text) return null;
  return (
    <div className="mb-3 flex justify-start">
      <div className="flex items-center gap-1.5 rounded-lg border border-zinc-700/60 bg-zinc-900/40 px-3 py-2 text-xs text-zinc-400">
        <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-zinc-500" />
        <span>{text}</span>
      </div>
    </div>
  );
}

// ToolBlock renders a single tool call and (optionally) its result. The details
// are expanded by default so tool output is visible immediately.
export function ToolBlock({
  tool,
  command,
  output,
  highlight = "",
}: {
  tool: string;
  command?: string;
  output?: string;
  highlight?: string;
}) {
  const displayOutput =
    output !== undefined ? stripTruncationFooter(output) : output;
  const lineCount = displayOutput ? displayOutput.split("\n").length : 0;
  const [open, setOpen] = useState(lineCount <= 50);
  const [expanded, setExpanded] = useState(false);
  const pending = output === undefined;
  const outputLines = displayOutput ? displayOutput.split("\n") : [];
  const collapsible = outputLines.length > TOOL_OUTPUT_PREVIEW_LINES;
  const visibleOutput =
    collapsible && !expanded
      ? outputLines.slice(-TOOL_OUTPUT_PREVIEW_LINES).join("\n")
      : displayOutput;
  const hiddenLineCount = outputLines.length - TOOL_OUTPUT_PREVIEW_LINES;
  return (
    <div className="mb-3 flex justify-start">
      <div className="max-w-[95%] md:max-w-[80%] w-full rounded-lg border border-amber-700/40 bg-amber-950/20 px-3 py-2">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-1.5 text-xs font-medium text-amber-300/90 hover:text-amber-200"
        >
          <span>{open ? "▾" : "▸"}</span>
          <span>🔧 {tool || "tool"}{lineCount > 0 ? ` · ${lineCount} lines` : ""}</span>
          {pending && <span className="ml-1 animate-pulse text-amber-400/70">running…</span>}
        </button>
        {open && (
          <div className="mt-2 space-y-2">
            {command && (
              <pre className="whitespace-pre-wrap break-words rounded bg-zinc-900/70 p-2 font-mono text-[11px] text-zinc-300">
                {highlight.trim() ? (
                  highlightMatches(command, highlight)
                ) : (
                  <HighlightedCode code={command} lang="json" />
                )}
              </pre>
            )}
            {output !== undefined && output !== "" && (
              <div className="rounded bg-zinc-900/70 p-2">
                <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-zinc-400">
                  {highlight.trim() ? (
                    highlightMatches(visibleOutput ?? "", highlight)
                  ) : (
                    <HighlightedCode
                      code={visibleOutput ?? ""}
                      lang={
                        TOOL_OUTPUT_LANG[tool] ??
                        (tool === "read" ? readLang(command) : "")
                      }
                    />
                  )}
                </pre>
                {collapsible && (
                  <button
                    type="button"
                    onClick={() => setExpanded((v) => !v)}
                    className="mt-1 text-[11px] text-zinc-500 hover:text-zinc-300"
                  >
                    {expanded
                      ? "▲ click to collapse"
                      : `… ${hiddenLineCount} earlier lines · click to expand`}
                  </button>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
