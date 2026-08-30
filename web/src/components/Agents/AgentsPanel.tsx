import { ArrowLeft, Bot } from "lucide-react";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode, { childSummary, elapsed, statusStyles } from "./RunNode";
import type { AgentRun } from "../../api/types";

interface AgentsPanelProps {
  sessionId: string | null;
  selectedRunId: string | null;
  onSelectRun: (runId: string | null) => void;
}

function findRun(runs: AgentRun[], id: string): AgentRun | undefined {
  for (const r of runs) {
    if (r.id === id) return r;
    const child = findRun(r.children, id);
    if (child) return child;
  }
  return undefined;
}

function AgentListRow({ run, onOpen }: { run: AgentRun; onOpen: () => void }) {
  const s = statusStyles(run.status);
  const summary = childSummary(run.children);
  const dur = elapsed(run.startedAt, run.endedAt);

  return (
    <button
      onClick={onOpen}
      className="group relative flex w-full items-center gap-2 overflow-hidden rounded-md py-2 pl-3 pr-2 text-left text-sm transition-colors hover:bg-muted/70"
    >
      <span className={`absolute left-0 top-1 bottom-1 w-0.5 rounded-full ${s.bar}`} />
      <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
      <span className="shrink-0 truncate font-medium text-foreground">{run.name}</span>
      {run.model && (
        <span className="shrink-0 truncate font-mono text-[11px] text-muted-foreground">{run.model}</span>
      )}
      <span className={`shrink-0 text-[11px] ${s.text}`}>{run.status}</span>
      <span className="ml-auto flex shrink-0 items-center gap-2">
        {summary && (
          <span className="rounded-full bg-muted px-2 py-0.5 font-mono text-[10px] text-muted-foreground ring-1 ring-inset ring-ring/60">
            {summary}
          </span>
        )}
        {dur && <span className="font-mono text-[10px] tabular-nums text-foreground">{dur}</span>}
      </span>
    </button>
  );
}

export default function AgentsPanel({ sessionId, selectedRunId, onSelectRun }: AgentsPanelProps) {
  const { runs, loaded } = useAgentRuns(sessionId);
  const selected = selectedRunId ? findRun(runs, selectedRunId) : undefined;

  if (selected) {
    const s = statusStyles(selected.status);
    const dur = elapsed(selected.startedAt, selected.endedAt);
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-3">
          <button
            onClick={() => onSelectRun(null)}
            className="flex items-center gap-1.5 rounded-md px-2 py-1 text-sm text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <ArrowLeft className="h-4 w-4" />
            Agents
          </button>
          <span className={`h-2 w-2 shrink-0 rounded-full ${s.dot}`} />
          <span className="font-medium text-foreground">{selected.name}</span>
          {selected.model && (
            <span className="font-mono text-[11px] text-muted-foreground">{selected.model}</span>
          )}
          <span className={`text-[11px] ${s.text}`}>{selected.status}</span>
          {dur && <span className="font-mono text-[10px] tabular-nums text-foreground">{dur}</span>}
        </div>
        <div className="flex-1 min-h-0 overflow-hidden p-3">
          <RunNode run={selected} depth={0} />
        </div>
      </div>
    );
  }

  // A run id is selected but the tree hasn't loaded yet (the SSE stream sends
  // its first snapshot after connect). Don't fall through to the list or the
  // empty state yet — that would make the click appear to have done nothing.
  if (selectedRunId && !loaded) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <Bot className="h-8 w-8 animate-pulse" />
        <p className="text-sm">Loading agent run…</p>
      </div>
    );
  }

  // The tree is loaded but the selected run is not in it (e.g. the registry
  // pruned it, or the session changed underneath the selection). Surface this
  // instead of silently showing an unrelated list.
  if (selectedRunId && loaded) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center text-muted-foreground">
        <Bot className="h-8 w-8" />
        <p className="max-w-sm text-sm">
          This agent run is no longer available in the current session's run list.
        </p>
        <button
          onClick={() => onSelectRun(null)}
          className="rounded-md bg-muted px-3 py-1.5 text-sm text-foreground hover:bg-accent hover:text-accent-foreground"
        >
          Back to all runs
        </button>
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <Bot className="h-8 w-8" />
        <p className="text-sm">No agent runs yet in this session.</p>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-3">
      <div className="space-y-1">
        {runs.map((run) => (
          <AgentListRow key={run.id} run={run} onOpen={() => onSelectRun(run.id)} />
        ))}
      </div>
    </div>
  );
}
