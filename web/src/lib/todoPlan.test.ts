import { describe, expect, it } from "vitest";
import { visibleTodoItems } from "./todoPlan";

const item = (id: string, state: "pending" | "in_progress" | "done") => ({ id, text: id, state });

describe("visibleTodoItems", () => {
  const items = [
    item("t1", "done"),
    item("t2", "in_progress"),
    item("t3", "pending"),
    item("t4", "pending"),
    item("t5", "done"),
  ];

  it("returns everything when the limit covers the list or showAll is set", () => {
    expect(visibleTodoItems(items, 5, false)).toEqual({ items, hidden: 0 });
    expect(visibleTodoItems(items, 10, false)).toEqual({ items, hidden: 0 });
    expect(visibleTodoItems(items, 2, true)).toEqual({ items, hidden: 0 });
  });

  it("shows open items first and counts what it hides", () => {
    const { items: visible, hidden } = visibleTodoItems(items, 3, false);
    expect(visible.map((i) => i.id)).toEqual(["t2", "t3", "t4"]);
    expect(hidden).toBe(2);
  });

  it("keeps file order when every item is still open", () => {
    const allOpen = [item("a", "pending"), item("b", "pending"), item("c", "pending")];
    expect(visibleTodoItems(allOpen, 2, false).items.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("handles a zero limit without indexing past the array", () => {
    expect(visibleTodoItems(items, 0, false)).toEqual({ items: [], hidden: 5 });
  });
});
