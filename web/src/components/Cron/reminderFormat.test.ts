import { describe, expect, it } from "vitest";
import { describeDueLabel, isOverdue } from "./reminderFormat";
import type { ReminderItem } from "@/api/types";

/** A minimal item; the helpers only read three fields. */
function item(over: Partial<ReminderItem> = {}): Pick<
  ReminderItem,
  "due_at_ms" | "fired_at_ms" | "status"
> {
  return { due_at_ms: 0, fired_at_ms: undefined, status: "pending", ...over };
}

const NOW = new Date("2026-03-01T12:00:00Z").getTime();
const PAST = NOW - 60 * 60 * 1000;
const FUTURE = NOW + 60 * 60 * 1000;

describe("isOverdue", () => {
  it("is true for a past-due, unfired, still-active item", () => {
    expect(isOverdue(item({ due_at_ms: PAST }), NOW)).toBe(true);
    expect(isOverdue(item({ due_at_ms: PAST, status: "in_progress" }), NOW)).toBe(true);
  });

  it("is false when the time is still in the future", () => {
    expect(isOverdue(item({ due_at_ms: FUTURE }), NOW)).toBe(false);
  });

  it("is false for an item with no due date at all", () => {
    expect(isOverdue(item({ due_at_ms: 0 }), NOW)).toBe(false);
  });

  // The three cases that make an "overdue" badge WRONG. Each of these was a way
  // to shout at the user about something that is not actually late.
  it("is false once the item has fired", () => {
    expect(isOverdue(item({ due_at_ms: PAST, fired_at_ms: PAST + 1000 }), NOW)).toBe(false);
  });

  it("is false for a completed or cancelled item", () => {
    expect(isOverdue(item({ due_at_ms: PAST, status: "completed" }), NOW)).toBe(false);
    expect(isOverdue(item({ due_at_ms: PAST, status: "cancelled" }), NOW)).toBe(false);
  });
});

describe("describeDueLabel", () => {
  it("says so plainly when there is no due date", () => {
    expect(describeDueLabel(item({ due_at_ms: 0 }), NOW)).toBe("No due date");
  });

  it("shows the absolute time for a future due date", () => {
    const label = describeDueLabel(item({ due_at_ms: FUTURE }), NOW);
    expect(label).not.toContain("overdue");
    expect(label).toContain(new Date(FUTURE).toLocaleString());
  });

  it("flags an overdue item", () => {
    const label = describeDueLabel(item({ due_at_ms: PAST, status: "pending" }), NOW);
    expect(label).toContain("overdue");
  });

  it("does not flag an overdue-but-already-fired item", () => {
    const label = describeDueLabel(item({ due_at_ms: PAST, fired_at_ms: PAST + 1 }), NOW);
    expect(label).not.toContain("overdue");
  });
});
