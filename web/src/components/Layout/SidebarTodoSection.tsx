import { useEffect, useState } from "react";
import { useSessionPlanLive } from "../../hooks/useSessionPlanLive";
import { visibleTodoItems } from "../../lib/todoPlan";
import { TodoPlanRow } from "../common/TodoItem";

interface Props {
  sessionId?: string | null;
  host?: string;
}

// A long plan must not push the sidebar's other sections away; the same
// open-first truncation as the chat panel, with its own "show more" toggle.
const SIDEBAR_TODO_LIMIT = 8;

// The cowork sidebar's TODO section. It reads the same session plan store as
// the chat's PlanPanel (`todo_updated` + GET /api/sessions/{id}/todo), so a
// plan written by the agent and every later status change are visible here
// without a hard-coded empty state.
export default function SidebarTodoSection({ sessionId, host }: Props) {
  const plan = useSessionPlanLive(sessionId, host);
  const [showAll, setShowAll] = useState(false);

  useEffect(() => {
    setShowAll(false);
  }, [sessionId]);

  if (!plan || plan.items.length === 0) {
    return (
      <div className="text-xs text-muted-foreground">
        <div>No TODO items</div>
        <div className="mt-2 text-foreground">Agent will add items during execution</div>
      </div>
    );
  }

  const running = plan.items.some((item) => item.state === "in_progress");
  const { items, hidden } = visibleTodoItems(plan.items, SIDEBAR_TODO_LIMIT, showAll);

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-[11px] text-muted-foreground">
        <span className="font-mono">
          {plan.done}/{plan.total} done
        </span>
        {running && (
          <span className="flex items-center gap-1 text-amber-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-400" />
            running
          </span>
        )}
      </div>
      <ul className="space-y-1">
        {items.map((item, i) => (
          <TodoPlanRow key={item.id ?? `${i}-${item.text}`} item={item} />
        ))}
        {(hidden > 0 || showAll) && plan.items.length > SIDEBAR_TODO_LIMIT && (
          <li>
            <button
              type="button"
              onClick={() => setShowAll((value) => !value)}
              className="text-[11px] text-muted-foreground hover:text-foreground"
            >
              {showAll ? "Show less" : `+${hidden} more`}
            </button>
          </li>
        )}
      </ul>
    </div>
  );
}
