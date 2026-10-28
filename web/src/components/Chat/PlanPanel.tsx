import { useEffect, useState } from "react";
import { ChevronRight, ListChecks } from "lucide-react";
import { cn } from "../../lib/utils";
import { useChatVerbosity } from "../../lib/chatVerbosity";
import { useSessionPlanLive } from "../../hooks/useSessionPlanLive";
import { TODO_MARK, TodoItemDetail } from "../Pulse/PulseCard";
import {
  loadPlanPanelCollapsed,
  savePlanPanelCollapsed,
  subscribePlanPanelCollapsed,
} from "./planPanelPersistence";

interface PlanPanelProps {
  sessionId: string;
  host?: string;
  onOpenRun?: (runId: string) => void;
}

// PlanPanel shows the session's todo plan as a live checklist above the agent
// rail. The first load comes from GET /api/sessions/{id}/todo; afterwards the
// `todo_updated` session event keeps it current (see sessionEvents.ts). Like
// AgentPreview it is collapsible and renders nothing when the session has no
// plan. The chat display setting `plan_panel` only decides the INITIAL state of
// a session with no remembered fold choice: folded, or open showing the first
// 5 / 10 / all rows. The user's own fold/unfold is remembered per session and
// always wins.
export default function PlanPanel({ sessionId, host, onOpenRun }: PlanPanelProps) {
  const plan = useSessionPlanLive(sessionId, host);
  const setting = useChatVerbosity().config.overrides.plan_panel;
  const [choice, setChoice] = useState(() => loadPlanPanelCollapsed(sessionId));
  const [showAll, setShowAll] = useState(false);

  useEffect(() => {
    setChoice(loadPlanPanelCollapsed(sessionId));
    setShowAll(false);
    return subscribePlanPanelCollapsed((id, next) => {
      if (id === sessionId) setChoice(next);
    });
  }, [sessionId]);
  const collapsed = choice ?? setting === "collapsed";

  if (!plan || plan.items.length === 0) return null;

  const running = plan.items.some((it) => it.state === "in_progress");

  const toggleCollapsed = () => {
    const next = !collapsed;
    setChoice(next);
    savePlanPanelCollapsed(sessionId, next);
  };

  // At most `limit` rows start visible: open items first (file order), then
  // done ones. Showing everything keeps plain file order.
  const limit = setting === "5" || setting === "10" ? Number(setting) : plan.items.length;
  const truncated = !showAll && plan.items.length > limit;
  const items = truncated
    ? [
        ...plan.items.filter((it) => it.state !== "done"),
        ...plan.items.filter((it) => it.state === "done"),
      ].slice(0, limit)
    : plan.items;
  const hidden = plan.items.length - items.length;

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
        aria-label={collapsed ? "Expand plan" : "Collapse plan"}
        className="flex w-full items-center gap-2 rounded-md text-left hover:opacity-90"
      >
        <ChevronRight
          className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${
            collapsed ? "" : "rotate-90"
          }`}
        />
        <ListChecks className="h-3.5 w-3.5 text-emerald-400" />
        <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">Plan</span>
        <span className="rounded-full bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground ring-1 ring-inset ring-ring/60">
          {plan.done}/{plan.total}
        </span>
        {running && <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />}
      </button>
      {!collapsed && (
        <ul className="mt-1.5 space-y-1">
          {items.map((item, i) => (
            <li
              key={item.id ?? `${i}-${item.text}`}
              className={cn("min-w-0 text-xs", item.state === "done" && "text-muted-foreground")}
            >
              <span aria-hidden>{TODO_MARK[item.state]} </span>
              <span className={cn("break-words", item.state === "done" && "line-through")}>{item.text}</span>
              <TodoItemDetail item={item} onOpenRun={onOpenRun} />
            </li>
          ))}
          {(hidden > 0 || showAll) && plan.items.length > limit && (
            <li>
              <button
                type="button"
                onClick={() => setShowAll((v) => !v)}
                className="text-[11px] text-muted-foreground hover:text-foreground"
              >
                {showAll ? "Show less" : `+${hidden} more`}
              </button>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
