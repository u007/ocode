import { useState } from "react";
import { ChevronRight, Bot } from "lucide-react";
import { useChatState } from "../../stores/chatStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import type { AgentRun, AgentRunMessage } from "../../api/types";

// statusStyles maps a run status to its dot, glow and accent-bar treatment.
function statusStyles(status: string): { dot: string; bar: string; text: string } {
  switch (status) {
    case "running":
      return {
        dot: "bg-amber-400 shadow-[0_0_0_3px_rgba(251,191,36,0.18)] animate-pulse",
        bar: "bg-amber-400/70",
        text: "text-amber-300/90",
      };
    case "done":
      return { dot: "bg-emerald-400", bar: "bg-emerald-500/40", text: "text-emerald-300/80" };
    case "failed":
      return { dot: "bg-red-400", bar: "bg-red-500/50", text: "text-red-300/90" };
    default:
      return { dot: "bg-zinc-500", bar: "bg-zinc-700", text: "text-zinc-400" };
  }
}

// elapsed renders a compact run duration like "1.4s" or "2m" from ISO stamps.
function elapsed(startedAt: string, endedAt?: string): string {
  const start = Date.parse(startedAt);
  if (Number.isNaN(start)) return "";
  const end = endedAt ? Date.parse(endedAt) : Date.now();
  const ms = Math.max(0, end - start);
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m${Math.round(s % 60)}s`;
}

// childSummary mirrors the TUI's "N sub · M running" badge.
function childSummary(children: AgentRun[]): string {
  if (children.length === 0) return "";
  let running = 0;
  let done = 0;
  let failed = 0;
  for (const c of children) {
    if (c.status === "running") running++;
    else if (c.status === "done") done++;
    else if (c.status === "failed") failed++;
  }
  const parts = [`${children.length} sub`];
  if (running) parts.push(`${running}·run`);
  if (done) parts.push(`${done}·ok`);
  if (failed) parts.push(`${failed}·err`);
  return parts.join(" ");
}

const roleChip: Record<string, string> = {
  user: "bg-blue-500/15 text-blue-300",
  assistant: "bg-emerald-500/15 text-emerald-300",
  tool: "bg-amber-500/15 text-amber-300",
  system: "bg-zinc-700/40 text-zinc-400",
};

// messageLine renders one transcript entry as a chip-prefixed row.
function messageLine(msg: AgentRunMessage, i: number) {
  const label = msg.role === "user" ? "task" : msg.role === "assistant" ? "agent" : msg.role;
  return (
    <div key={i} className="flex gap-2 text-xs leading-relaxed">
      <span
        className={`mt-px h-fit shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide ${
          roleChip[msg.role] ?? "bg-zinc-700/40 text-zinc-400"
        }`}
      >
        {label}
      </span>
      <div className="min-w-0 flex-1 text-zinc-300">
        {msg.content && <span className="whitespace-pre-wrap break-words">{msg.content}</span>}
        {msg.toolCalls?.map((tc, j) => (
          <div key={j} className="font-mono text-[11px] text-zinc-400">
            <span className="text-zinc-500">→</span> {tc.name}
            {tc.arguments ? (
              <span className="text-zinc-600">({tc.arguments.slice(0, 120)})</span>
            ) : (
              <span className="text-zinc-600">()</span>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

interface RunNodeProps {
  run: AgentRun;
  depth: number;
}

// RunNode is one run row, individually expandable to reveal its messages and
// nested sub-agent runs (recursively).
function RunNode({ run, depth }: RunNodeProps) {
  const [open, setOpen] = useState(false);
  const summary = childSummary(run.children);
  const hasDetail = run.messages.length > 0 || run.children.length > 0;
  const s = statusStyles(run.status);
  const dur = elapsed(run.startedAt, run.endedAt);

  return (
    <div className={depth > 0 ? "border-l border-zinc-800/80 pl-2.5" : ""}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="group relative flex w-full items-center gap-2 overflow-hidden rounded-md py-1 pl-2.5 pr-2 text-left text-sm transition-colors hover:bg-zinc-800/70"
      >
        {/* status accent bar */}
        <span className={`absolute left-0 top-1 bottom-1 w-0.5 rounded-full ${s.bar}`} />

        <ChevronRight
          className={`h-3.5 w-3.5 shrink-0 text-zinc-600 transition-transform ${
            hasDetail ? "group-hover:text-zinc-400" : "opacity-0"
          } ${open ? "rotate-90" : ""}`}
        />
        <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
        <span className="shrink-0 truncate font-medium text-zinc-100">{run.name}</span>
        {run.model && (
          <span className="shrink-0 truncate font-mono text-[11px] text-zinc-500">{run.model}</span>
        )}
        <span className={`shrink-0 text-[11px] ${s.text}`}>{run.status}</span>

        <span className="ml-auto flex shrink-0 items-center gap-2">
          {summary && (
            <span className="rounded-full bg-zinc-800 px-2 py-0.5 font-mono text-[10px] text-zinc-400 ring-1 ring-inset ring-zinc-700/60">
              {summary}
            </span>
          )}
          {dur && <span className="font-mono text-[10px] tabular-nums text-zinc-600">{dur}</span>}
        </span>
      </button>

      {open && hasDetail && (
        <div className="ml-[1.15rem] mt-1 mb-2 space-y-2 border-l border-zinc-800/60 pl-3">
          {run.err && (
            <div className="rounded-md bg-red-950/40 px-2 py-1 text-xs text-red-300 ring-1 ring-inset ring-red-900/40">
              {run.err}
            </div>
          )}
          {run.messages.length > 0 && (
            <div className="space-y-1.5 rounded-md bg-zinc-900/70 p-2 ring-1 ring-inset ring-zinc-800/80">
              {run.messages.map((m, i) => messageLine(m, i))}
            </div>
          )}
          {run.children.map((child) => (
            <RunNode key={child.id} run={child} depth={depth + 1} />
          ))}
        </div>
      )}
    </div>
  );
}

// AgentPreview is the live "agent preview" rail above the chat input: top-level
// agent runs, each clickable to expand its messages and nested sub-agents
// inline. Renders nothing when no runs are active.
export default function AgentPreview() {
  const { sessionId } = useChatState();
  const runs = useAgentRuns(sessionId);

  if (runs.length === 0) return null;

  const running = runs.filter((r) => r.status === "running").length;

  return (
    <div className="max-h-52 shrink-0 overflow-y-auto border-t border-zinc-800 bg-gradient-to-b from-zinc-900 to-zinc-950/80 px-3 py-2">
      <div className="mb-1.5 flex items-center gap-2">
        <Bot className="h-3.5 w-3.5 text-blue-400" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-zinc-400">
          Agents
        </span>
        <span className="rounded-full bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400 ring-1 ring-inset ring-zinc-700/60">
          {runs.length}
        </span>
        {running > 0 && (
          <span className="flex items-center gap-1 text-[10px] text-amber-300/80">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />
            {running} running
          </span>
        )}
      </div>
      <div className="space-y-0.5">
        {runs.map((run) => (
          <RunNode key={run.id} run={run} depth={0} />
        ))}
      </div>
    </div>
  );
}
