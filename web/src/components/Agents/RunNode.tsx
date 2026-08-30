import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { AgentRun, AgentRunMessage } from "../../api/types";

// statusStyles maps a run status to its dot, glow and accent-bar treatment.
export function statusStyles(status: string): { dot: string; bar: string; text: string } {
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
      return { dot: "bg-muted", bar: "bg-accent", text: "text-muted-foreground" };
  }
}

// elapsed renders a compact run duration like "1.4s" or "2m" from ISO stamps.
export function elapsed(startedAt: string, endedAt?: string): string {
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
export function childSummary(children: AgentRun[]): string {
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
  system: "bg-accent/40 text-muted-foreground",
};

// messageLine renders one transcript entry as a chip-prefixed row.
function messageLine(msg: AgentRunMessage, i: number) {
  const label = msg.role === "user" ? "task" : msg.role === "assistant" ? "agent" : msg.role;
  return (
    <div key={i} className="space-y-1 text-xs leading-relaxed">
      <div className="flex gap-2">
        <span
          className={`mt-px h-fit shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide ${
            roleChip[msg.role] ?? "bg-accent/40 text-muted-foreground"
          }`}
        >
          {label}
        </span>
        <div className="min-w-0 flex-1 text-foreground">
          {msg.content && <span className="whitespace-pre-wrap break-words">{msg.content}</span>}
          {msg.toolCalls?.map((tc, j) => (
            <div key={j} className="font-mono text-[11px] text-muted-foreground">
              <span className="text-muted-foreground">→</span> {tc.name}
              {tc.arguments ? (
                <span className="text-foreground">({tc.arguments.slice(0, 120)})</span>
              ) : (
                <span className="text-foreground">()</span>
              )}
            </div>
          ))}
        </div>
      </div>
      {msg.reasoningContent && (
        <div className="ml-[1.75rem] max-h-40 overflow-y-auto overscroll-contain rounded-md border border-border/60 bg-card/50 px-2 py-1.5">
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">
            Thinking
          </div>
          <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-muted-foreground">
            {msg.reasoningContent}
          </pre>
        </div>
      )}
    </div>
  );
}

interface RunNodeProps {
  run: AgentRun;
  depth: number;
  // onOpenDetail, when given, makes the run's name its own clickable
  // element that opens a full transcript view instead of just toggling
  // this row's inline expand/collapse.
  onOpenDetail?: (runId: string) => void;
  // defaultOpen controls the row's initial expand state; it propagates to
  // nested children. The chat-bottom AgentPreview rail passes false so a
  // spawned agent starts as one compact summary row until the user expands
  // it; detail views (AgentsPanel) keep the expanded default.
  defaultOpen?: boolean;
}

// RunNode is one run row, individually expandable to reveal its messages and
// nested sub-agent runs (recursively).
export default function RunNode({ run, depth, onOpenDetail, defaultOpen = true }: RunNodeProps) {
  const [open, setOpen] = useState(defaultOpen);
  const summary = childSummary(run.children);
  const hasResult = Boolean(run.result?.trim());
  const hasDetail = run.messages.length > 0 || run.children.length > 0 || hasResult;
  const s = statusStyles(run.status);
  const dur = elapsed(run.startedAt, run.endedAt);

  return (
    <div className={depth > 0 ? "border-l border-border/80 pl-2.5" : ""}>
      <button
        onClick={() => setOpen((v) => !v)}
        className="group relative flex w-full items-center gap-2 overflow-hidden rounded-md py-1 pl-2.5 pr-2 text-left text-sm transition-colors hover:bg-muted/70"
      >
        {/* status accent bar */}
        <span className={`absolute left-0 top-1 bottom-1 w-0.5 rounded-full ${s.bar}`} />

        <ChevronRight
          className={`h-3.5 w-3.5 shrink-0 text-foreground transition-transform ${
            hasDetail ? "group-hover:text-muted-foreground" : "opacity-0"
          } ${open ? "rotate-90" : ""}`}
        />
        <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
        {onOpenDetail ? (
          <span
            role="button"
            onClick={(e) => {
              e.stopPropagation();
              onOpenDetail(run.id);
            }}
            className="shrink-0 truncate font-medium text-foreground hover:text-blue-400 hover:underline"
          >
            {run.name}
          </span>
        ) : (
          <span className="shrink-0 truncate font-medium text-foreground">{run.name}</span>
        )}
        {run.model && (
          <span className="shrink-0 truncate font-mono text-[11px] text-muted-foreground">{run.model}</span>
        )}
        <span className={`shrink-0 text-[11px] ${s.text}`}>{run.status}</span>
        {run.contract && run.contract.checked && !run.contract.satisfied && (
          <span
            title={
              run.contract.deficiency
                ? `Output contract not met: ${run.contract.deficiency}`
                : "Output contract not met"
            }
            className="shrink-0 rounded bg-red-500/15 px-1.5 py-0.5 font-mono text-[10px] text-red-300 ring-1 ring-inset ring-red-800/50"
          >
            contract ✗
          </span>
        )}

        <span className="ml-auto flex shrink-0 items-center gap-2">
          {summary && (
            <span className="rounded-full bg-muted px-2 py-0.5 font-mono text-[10px] text-muted-foreground ring-1 ring-inset ring-ring/60">
              {summary}
            </span>
          )}
          {dur && <span className="font-mono text-[10px] tabular-nums text-foreground">{dur}</span>}
        </span>
      </button>

      {open && hasDetail && (
        <div className="ml-[1.15rem] mt-1 mb-2 space-y-2 border-l border-border/60 pl-3">
          {run.err && (
            <div className="rounded-md bg-red-950/40 px-2 py-1 text-xs text-red-300 ring-1 ring-inset ring-red-900/40">
              {run.err}
            </div>
          )}
          {run.messages.length > 0 && (
            <div className="max-h-72 overflow-y-auto overscroll-contain space-y-1.5 rounded-md bg-card/70 p-2 ring-1 ring-inset ring-ring/80">
              {run.messages.map((m, i) => messageLine(m, i))}
            </div>
          )}
          {hasResult && (
            <div className="max-h-48 overflow-y-auto overscroll-contain rounded-md bg-emerald-950/25 px-2 py-1.5 ring-1 ring-inset ring-emerald-900/30">
              <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-emerald-300/75">
                Result
              </div>
              <pre className="whitespace-pre-wrap break-words font-mono text-[11px] text-emerald-100/90">
                {run.result}
              </pre>
            </div>
          )}
          {run.children.map((child) => (
            <RunNode key={child.id} run={child} depth={depth + 1} defaultOpen={defaultOpen} />
          ))}
        </div>
      )}
    </div>
  );
}
