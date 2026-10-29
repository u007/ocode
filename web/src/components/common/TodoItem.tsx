import { cn } from "../../lib/utils";
import type { PulseTodoItem, PulseTodoState } from "../../api/types";

export const TODO_MARK: Record<PulseTodoState, string> = {
  pending: "☐",
  in_progress: "▸",
  done: "☑",
};

/** The muted line under a plan item: what blocks it, the run working on it, or
 *  its outcome. Renders nothing for an item with none of them. With `onOpenRun`
 *  the "running as" line is a button that opens that run's detail. */
export function TodoItemDetail({ item, onOpenRun }: { item: PulseTodoItem; onOpenRun?: (runId: string) => void }) {
  let text: string | null = null;
  let runId: string | null = null;
  let failed = false;
  if (item.state === "done") {
    text = item.result ?? null;
  } else if (item.blocked && item.deps?.length) {
    text = `blocked by ${item.deps.join(", ")}`;
  } else if (item.result) {
    // A failed or cancelled run leaves its item in_progress with the run kept
    // and the outcome as the result; that is not "running".
    text = item.result;
    failed = item.result.startsWith("FAILED: ") || item.result.startsWith("CANCELLED: ");
  } else if (item.state === "in_progress" && item.run_id) {
    text = `running as ${item.run_id}`;
    runId = item.run_id;
  }
  if (!text) return null;
  const className = cn(
    "block max-w-full truncate text-left text-[11px] font-normal",
    failed ? "text-destructive/80" : "text-muted-foreground",
  );
  if (runId && onOpenRun) {
    return (
      <button
        type="button"
        data-testid="pulse-todo-detail"
        onClick={() => onOpenRun(runId)}
        className={cn(className, "hover:text-foreground hover:underline")}
      >
        {text}
      </button>
    );
  }
  return (
    <span data-testid="pulse-todo-detail" className={className}>
      {text}
    </span>
  );
}

/** One plan row, shared by the chat PlanPanel and the cowork sidebar so the two
 *  surfaces cannot drift in glyphs, strikethrough, emphasis or detail lines. */
export function TodoPlanRow({ item, onOpenRun }: { item: PulseTodoItem; onOpenRun?: (runId: string) => void }) {
  return (
    <li className={cn("min-w-0 text-xs", item.state === "done" && "text-muted-foreground")}>
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
      <TodoItemDetail item={item} onOpenRun={onOpenRun} />
    </li>
  );
}
