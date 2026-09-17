import { memo, useEffect, useRef } from "react";
import { ArrowLeft, Bot } from "lucide-react";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode, { elapsed, statusStyles } from "./RunNode";
import ScrollNavButtons from "../common/ScrollNavButtons";
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

function AgentsPanel({ sessionId, selectedRunId, onSelectRun }: AgentsPanelProps) {
  const { runs, loaded } = useAgentRuns(sessionId);
  const selected = selectedRunId ? findRun(runs, selectedRunId) : undefined;
  const listScrollRef = useRef<HTMLDivElement>(null);
  const detailScrollRef = useRef<HTMLDivElement>(null);

  // Opening a different run must start at its top — without this the new run
  // inherits the previous run's scroll offset (the container is not remounted).
  useEffect(() => {
    const el = detailScrollRef.current;
    if (!el) return;
    if (typeof el.scrollTo === "function") el.scrollTo({ top: 0 });
    else el.scrollTop = 0;
  }, [selectedRunId]);

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
        {/* The run detail scrolls as one surface: RunNode's own bounded
            sub-lists (thinking / messages / result) keep their inner caps, but
            the tree as a whole can now be taller than the panel without being
            clipped — and the nav buttons give it explicit top/bottom edges. */}
        <div className="relative min-h-0 flex-1">
          <div
            ref={detailScrollRef}
            className="h-full overflow-y-auto overscroll-contain p-3"
          >
            <RunNode run={selected} depth={0} />
          </div>
          <ScrollNavButtons scrollRef={detailScrollRef} watch={selectedRunId} />
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
    <div className="relative h-full">
      <div ref={listScrollRef} className="h-full overflow-y-auto overscroll-contain p-3">
        <div className="space-y-1">
          {/* Each run (and its nested sub-agents) is independently expandable,
              so any number can be open at once instead of the single-run
              drill-in swap. Rows start collapsed so a freshly spawned crew
              doesn't balloon the list; the focused full-screen view is still
              one click away on the run name. */}
          {runs.map((run) => (
            <RunNode
              key={run.id}
              run={run}
              depth={0}
              defaultOpen={false}
              onOpenDetail={onSelectRun}
            />
          ))}
        </div>
      </div>
      <ScrollNavButtons scrollRef={listScrollRef} watch={runs} />
    </div>
  );
}

/** `sessionId` is stable per instance and `onSelectRun` is a stable setState, so
 *  a parent re-render — e.g. another tab becoming active — is a no-op here. */
export default memo(AgentsPanel);
