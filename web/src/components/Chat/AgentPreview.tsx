import { useEffect, useState } from "react";
import { Bot, ChevronRight } from "lucide-react";
import { useProjectState } from "../../stores/projectStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode from "../Agents/RunNode";
import {
  loadAgentPreviewCollapsed,
  saveAgentPreviewCollapsed,
  subscribeAgentPreviewCollapsed,
} from "./agentPreviewPersistence";

interface AgentPreviewProps {
  onOpenDetail?: (runId: string) => void;
}

// AgentPreview is the live "agent preview" rail above the chat input: top-level
// agent runs, each clickable to expand its messages and nested sub-agents
// inline. Runs start collapsed to a single summary row (the rail must not
// balloon when a sub-agent spawns); clicking a row/chevron expands it.
//
// The whole rail is also collapsible from its header (the count + "running"
// signal stay visible) so it can be tucked away without hiding agent activity.
// The user's choice is persisted and shared across every mounted chat tab.
// Renders nothing when no runs are active.
export default function AgentPreview({ onOpenDetail }: AgentPreviewProps) {
  const { activeTabId: sessionId } = useProjectState();
  const { runs } = useAgentRuns(sessionId);
  const [collapsed, setCollapsed] = useState(loadAgentPreviewCollapsed);

  // Keep every mounted rail (one per open chat tab, plus other browser tabs) in
  // sync when the preference changes elsewhere.
  useEffect(() => subscribeAgentPreviewCollapsed(setCollapsed), []);

  if (runs.length === 0) return null;

  const running = runs.filter((r) => r.status === "running").length;

  const toggleCollapsed = () => {
    const next = !collapsed;
    setCollapsed(next);
    saveAgentPreviewCollapsed(next);
  };

  return (
    <div
      className={`shrink-0 border-t border-border bg-gradient-to-b from-card to-background/80 px-3 py-2 ${
        collapsed ? "" : "max-h-52 overflow-y-auto"
      }`}
    >
      <button
        type="button"
        onClick={toggleCollapsed}
        aria-expanded={!collapsed}
        aria-label={collapsed ? "Expand agents" : "Collapse agents"}
        className="flex w-full items-center gap-2 rounded-md text-left hover:opacity-90"
      >
        <ChevronRight
          className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${
            collapsed ? "" : "rotate-90"
          }`}
        />
        <Bot className="h-3.5 w-3.5 text-blue-400" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
          Agents
        </span>
        <span className="rounded-full bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground ring-1 ring-inset ring-ring/60">
          {runs.length}
        </span>
        {running > 0 && (
          <span className="flex items-center gap-1 text-[10px] text-amber-300/80">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />
            {running} running
          </span>
        )}
      </button>
      {!collapsed && (
        <div className="mt-1.5 space-y-0.5">
          {runs.map((run) => (
            <RunNode key={run.id} run={run} depth={0} onOpenDetail={onOpenDetail} defaultOpen={false} />
          ))}
        </div>
      )}
    </div>
  );
}
