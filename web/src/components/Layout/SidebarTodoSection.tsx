import { useSessionPlanLive } from "../../hooks/useSessionPlanLive";
import { cn } from "../../lib/utils";
import { TODO_MARK, TodoItemDetail } from "../Pulse/PulseCard";

interface Props {
  sessionId?: string | null;
  host?: string;
}

// The cowork sidebar's TODO section. It reads the same session plan store as
// the chat's PlanPanel (`todo_updated` + GET /api/sessions/{id}/todo), so a
// plan written by the agent and every later status change are visible here
// without a hard-coded empty state.
export default function SidebarTodoSection({ sessionId, host }: Props) {
  const plan = useSessionPlanLive(sessionId, host);
  const items = plan?.items ?? [];

  if (items.length === 0) {
    return (
      <div className="text-xs text-muted-foreground">
        <div>No TODO items</div>
        <div className="mt-2 text-foreground">Agent will add items during execution</div>
      </div>
    );
  }

  const running = items.some((item) => item.state === "in_progress");

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-[11px] text-muted-foreground">
        <span className="font-mono">
          {plan?.done ?? 0}/{plan?.total ?? items.length} done
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
          <li
            key={item.id ?? `${i}-${item.text}`}
            className={cn("min-w-0 text-xs", item.state === "done" && "text-muted-foreground")}
          >
            <span aria-hidden>{TODO_MARK[item.state]} </span>
            <span
              className={cn(
                "break-words",
                item.state === "done" && "line-through",
                item.state === "in_progress" && "font-medium text-foreground",
              )}
            >
              {item.text}
            </span>
            <TodoItemDetail item={item} />
          </li>
        ))}
      </ul>
    </div>
  );
}
