import { memo, useState } from "react";
import { CheckCircle2, Volume2 } from "lucide-react";
import type { QuestionAnswerPayload } from "@/api/types";
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
// stdout/stderr. `read`'s output is source code, rendered plain here (the
// editor surfaces the language). Everything else (grep, glob,
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

// ThinkingBlock renders reasoning tokens in a muted panel. The content is shown
// expanded by default so reasoning is visible immediately in the web UI.
export function ThinkingBlock({
  text,
  highlight = "",
  onSpeak,
}: {
  text: string;
  highlight?: string;
  onSpeak?: () => void;
}) {
  const [open, setOpen] = useState(true);
  if (!text) return null;
  return (
    <div className="mb-3 flex justify-start">
      <div className="max-w-[95%] md:max-w-[80%] w-full rounded-lg border border-border/60 bg-card/40 px-3 py-2">
        <div className="flex items-center justify-between">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"
          >
            <span>{open ? "▾" : "▸"}</span>
            <span>🧠 Thinking</span>
          </button>
          {onSpeak && (
            <button
              type="button"
              aria-label="Speak thinking"
              title="Speak thinking"
              onClick={onSpeak}
              className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-muted-foreground hover:bg-background hover:text-foreground"
            >
              <Volume2 className="h-3.5 w-3.5" /> Speak
            </button>
          )}
        </div>
        {open && (
          <pre className="mt-2 whitespace-pre-wrap break-words font-mono text-xs text-muted-foreground">
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
      <div className="flex items-center gap-1.5 rounded-lg border border-border/60 bg-card/40 px-3 py-2 text-xs text-muted-foreground">
        <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-muted" />
        <span>{text}</span>
      </div>
    </div>
  );
}

// Parses an answered `question` tool result. The server replaces the pending
// QUESTION_PROMPT sentinel in place with the shape-compatible payload parsed
// from the browser's answers (handler_questions.go questionAnswerPayload — the
// same payload the model receives as the tool result; optional fields are
// omitempty, so it is not necessarily byte-identical to what the client sent).
// Recognizing it lets the transcript show
// the questions and the selected answers instead of a raw JSON blob. Returns
// null for the unanswered sentinel and for anything that is not an answer
// payload, so callers fall back to the generic rendering.
export function parseQuestionAnswers(
  output?: string,
): QuestionAnswerPayload[] | null {
  if (!output) return null;
  const trimmed = output.trim();
  if (!trimmed.startsWith("[")) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return null;
  }
  if (!Array.isArray(parsed) || parsed.length === 0) return null;
  for (const entry of parsed) {
    if (!entry || typeof entry !== "object") return null;
    const e = entry as Record<string, unknown>;
    if (typeof e.question !== "string" || !Array.isArray(e.answers)) return null;
  }
  return parsed as QuestionAnswerPayload[];
}

// QuestionAnswerBlock renders the answered `question` result as the questions
// the model asked plus the answer(s) the user selected — i.e. what was sent to
// the LLM. Shown in place of the generic tool block once a question is answered.
export function QuestionAnswerBlock({
  answers,
}: {
  answers: QuestionAnswerPayload[];
}) {
  return (
    <div className="mb-3 flex justify-start">
      <div className="w-full max-w-[95%] rounded-lg border border-emerald-700/40 bg-emerald-950/20 px-3 py-2 md:max-w-[80%]">
        <div className="flex items-center gap-1.5 text-xs font-medium text-emerald-300/90">
          <CheckCircle2 className="h-3.5 w-3.5" />
          Answered question prompt
        </div>
        <div className="mt-2 space-y-2">
          {answers.map((entry, i) => (
            <div key={i} className="text-xs">
              {entry.header ? (
                <div className="font-medium text-foreground">{entry.header}</div>
              ) : null}
              <div className="text-muted-foreground">{entry.question}</div>
              <ul className="mt-0.5 space-y-0.5">
                {entry.answers.length === 0 ? (
                  <li className="text-muted-foreground">→ (no selection)</li>
                ) : (
                  entry.answers.map((a, j) => (
                    <li key={j} className="text-emerald-300/90">
                      → {a.custom && a.text ? `${a.label}: “${a.text}”` : a.label}
                    </li>
                  ))
                )}
              </ul>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ToolBlock renders a single tool call and (optionally) its result. The details
// are expanded by default so tool output is visible immediately.
//
// Memoized: in the live stream, every thinking/text delta replaces the whole
// `live` array reference, which re-renders ChatPanel's `live.map(...)` and
// would otherwise re-invoke every already-completed ToolBlock in the current
// turn (each re-running its HighlightedCode effect) on every single token.
export const ToolBlock = memo(function ToolBlock({
  tool,
  command,
  output,
  stream,
  highlight = "",
  onOpenQuestion,
}: {
  tool: string;
  command?: string;
  output?: string;
  /** Incremental output while the tool is still running. Shown only until the
   *  authoritative `output` arrives, which then replaces it. */
  stream?: string;
  highlight?: string;
  /** Set for a `question` call still waiting on the user: re-opens its dialog
   *  (the dialog can be lost to a reload/reconcile while the ask is pending). */
  onOpenQuestion?: () => void;
}) {
  // Never render the raw QUESTION_PROMPT sentinel. The grouped transcript path
  // already filters the sentinel tool message, but the live stream can carry it
  // as the tool output until the authoritative snapshot lands.
  const suppressSentinel =
    tool === "question" && (output ?? "").startsWith("QUESTION_PROMPT:");
  const displayOutput =
    output !== undefined && !suppressSentinel
      ? stripTruncationFooter(output)
      : undefined;
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
  // Diff coloring only for FormatDiff output: known by tool name on the live
  // stream, or by the "DIFF:" first line on replayed history (no tool name).
  const isDiffOutput =
    TOOL_OUTPUT_LANG[tool] === "diff" || (displayOutput ?? "").startsWith("DIFF:");
  // Answered `question` call: render the questions + the selected answers (what
  // was sent to the LLM) instead of the raw result JSON. Placed after every
  // hook so the early return cannot change the hook order across renders.
  if (tool === "question") {
    const answers = parseQuestionAnswers(output);
    if (answers) return <QuestionAnswerBlock answers={answers} />;
  }
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
          {pending && !onOpenQuestion && <span className="ml-1 animate-pulse text-amber-400/70">running…</span>}
          {onOpenQuestion && <span className="ml-1 text-amber-400/70">awaiting your answer</span>}
        </button>
        {onOpenQuestion && (
          <button
            type="button"
            onClick={onOpenQuestion}
            className="mt-2 rounded border border-amber-500/50 px-2 py-0.5 text-[11px] text-amber-200 hover:bg-amber-500/15"
          >
            Open question
          </button>
        )}
        {open && (
          <div className="mt-2 space-y-2">
            {command && (
              <pre className="whitespace-pre-wrap break-words rounded bg-card/70 p-2 font-mono text-[11px] text-foreground">
                {highlight.trim() ? (
                  highlightMatches(command, highlight)
                ) : (
                  <HighlightedCode code={command} lang="json" />
                )}
              </pre>
            )}
            {pending && stream && (
              <div className="rounded bg-card/70 p-2">
                <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">
                  {stream.split("\n").slice(-TOOL_OUTPUT_PREVIEW_LINES).join("\n")}
                </pre>
              </div>
            )}
            {output !== undefined && output !== "" && (
              <div className="rounded bg-card/70 p-2">
                <div className="font-mono text-[11px] text-muted-foreground whitespace-pre">
                  {(visibleOutput ?? "").split("\n").map((line, i) => {
                    const colorClass = !isDiffOutput
                      ? "text-muted-foreground"
                      : line.startsWith("+") && !line.startsWith("+++") ? "text-green-400"
                      : line.startsWith("-") && !line.startsWith("---") ? "text-red-400"
                      : line.startsWith("@@") ? "text-blue-400"
                      : line.startsWith("DIFF:") ? "text-amber-400 font-bold"
                      : "text-muted-foreground";
                    return (
                      <div key={i} className={`flex ${colorClass}`}>
                        <span className="select-none text-neutral-600 w-10 text-right pr-2 shrink-0 text-[10px] leading-4">{isDiffOutput ? String(i + 1) : ""}</span>
                        <span className="whitespace-pre-wrap break-words">{highlight.trim() ? highlightMatches(line, highlight) : line}</span>
                      </div>
                    );
                  })
                }</div>
                {collapsible && (
                  <button
                    type="button"
                    onClick={() => setExpanded((v) => !v)}
                    className="mt-1 text-[11px] text-muted-foreground hover:text-foreground"
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
});
