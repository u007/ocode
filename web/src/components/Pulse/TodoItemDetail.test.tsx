import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { TodoItemDetail } from "./PulseCard";
import type { PulseTodoItem } from "../../api/types";

const item = (over: Partial<PulseTodoItem>): PulseTodoItem => ({
  text: "build",
  state: "in_progress",
  ...over,
});

describe("TodoItemDetail", () => {
  it("shows running as for an in-progress item with a run and no result", () => {
    render(<TodoItemDetail item={item({ run_id: "agent-run-3" })} />);
    expect(screen.getByTestId("pulse-todo-detail")).toHaveTextContent("running as agent-run-3");
  });

  it.each(["FAILED: provider exploded", "CANCELLED: stopped by user"])(
    "shows the result, not running as, for %s",
    (result) => {
      render(<TodoItemDetail item={item({ run_id: "agent-run-3", result })} />);
      const el = screen.getByTestId("pulse-todo-detail");
      expect(el).toHaveTextContent(result);
      expect(el).not.toHaveTextContent("running as");
      expect(el.className).toContain("text-destructive");
    },
  );

  it("keeps a plain in-progress result muted", () => {
    render(<TodoItemDetail item={item({ run_id: "agent-run-3", result: "halfway there" })} />);
    const el = screen.getByTestId("pulse-todo-detail");
    expect(el).toHaveTextContent("halfway there");
    expect(el.className).toContain("text-muted-foreground");
  });
});
