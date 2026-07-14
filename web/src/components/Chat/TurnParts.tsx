import { useState } from "react";
import { highlightMatches } from "./ChatSearchBar";

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
  const lineCount = output ? output.split("\n").length : 0;
  const [open, setOpen] = useState(lineCount <= 50);
  const pending = output === undefined;
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
                {highlight.trim() ? highlightMatches(command, highlight) : command}
              </pre>
            )}
            {output !== undefined && output !== "" && (
              <pre className="whitespace-pre-wrap break-words rounded bg-zinc-900/70 p-2 font-mono text-[11px] text-zinc-400">
                {highlight.trim() ? highlightMatches(output, highlight) : output}
              </pre>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
