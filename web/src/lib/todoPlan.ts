import type { PulseTodoItem } from "../api/types";

/**
 * The rows a plan surface starts with. A limited view shows open items first
 * (file order within each group) so the work that still matters is visible; a
 * finished plan or "show all" keeps plain file order. Shared by the chat
 * PlanPanel and the cowork sidebar so their truncation rules cannot drift.
 */
export function visibleTodoItems<T extends Pick<PulseTodoItem, "state">>(
  items: T[],
  limit: number,
  showAll: boolean,
): { items: T[]; hidden: number } {
  if (showAll || limit >= items.length) return { items, hidden: 0 };
  const capped = Math.max(0, limit);
  if (capped === 0) return { items: [], hidden: items.length };
  const open = items.filter((item) => item.state !== "done");
  const done = items.filter((item) => item.state === "done");
  return { items: [...open, ...done].slice(0, capped), hidden: items.length - capped };
}
