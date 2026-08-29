import { Bot } from "lucide-react";
import { useProjectState } from "../../stores/projectStore";
import { useAgentRuns } from "../../hooks/useAgentRuns";
import RunNode from "../Agents/RunNode";

interface AgentPreviewProps {
  onOpenDetail?: (runId: string) => void;
}

// AgentPreview is the live "agent preview" rail above the chat input: top-level
// agent runs, each clickable to expand its messages and nested sub-agents
// inline. Renders nothing when no runs are active.
export default function AgentPreview({ onOpenDetail }: AgentPreviewProps) {
  const { activeTabId: sessionId } = useProjectState();
  const { runs } = useAgentRuns(sessionId);

  if (runs.length === 0) return null;

  const running = runs.filter((r) => r.status === "running").length;

  return (
    <div className="max-h-52 shrink-0 overflow-y-auto border-t border-border bg-gradient-to-b from-card to-background/80 px-3 py-2">
      <div className="mb-1.5 flex items-center gap-2">
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
      </div>
      <div className="space-y-0.5">
        {runs.map((run) => (
          <RunNode key={run.id} run={run} depth={0} onOpenDetail={onOpenDetail} />
        ))}
      </div>
    </div>
  );
}
