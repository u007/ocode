import type { ReminderItem } from "@/api/types";

/**
 * How the Due column reads for one item.
 *
 * Three cases, and the distinction between them matters to the user:
 *   - no due date      → "No due date". A task without one is a plain
 *                        checklist entry and will never fire on its own.
 *   - due, not fired   → the absolute time.
 *   - due and overdue  → the absolute time, flagged, because a pending item
 *                        whose time has passed while the app was closed is the
 *                        case the user most needs to notice.
 */
export function describeDueLabel(
  item: Pick<ReminderItem, "due_at_ms" | "fired_at_ms" | "status">,
  // Injectable clock, for the same reason isOverdue takes one: a caller that
  // already has a snapshot timestamp (and every test) must be able to render
  // against it instead of against wall-clock time, which would make the label
  // depend on when the test happened to run.
  nowMs: number = Date.now(),
): string {
  if (!item.due_at_ms) return "No due date";
  const when = new Date(item.due_at_ms).toLocaleString();
  if (isOverdue(item, nowMs)) return `${when} (overdue)`;
  return when;
}

/** True when the item's time has passed but it is still active and unfired. */
export function isOverdue(
  item: Pick<ReminderItem, "due_at_ms" | "fired_at_ms" | "status">,
  nowMs: number = Date.now(),
): boolean {
  if (!item.due_at_ms || item.due_at_ms > nowMs) return false;
  if (item.fired_at_ms) return false;
  return item.status === "pending" || item.status === "in_progress";
}
