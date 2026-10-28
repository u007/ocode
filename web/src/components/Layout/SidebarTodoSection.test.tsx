import { act, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { TodoUpdatedEvent } from "../../api/types";
import { setSessionPlan } from "../../lib/sessionPlan";
import SidebarTodoSection from "./SidebarTodoSection";

const getSessionTodo = vi.fn();
vi.mock("../../api/client", () => ({
  api: { getSessionTodo: (...args: unknown[]) => getSessionTodo(...args) },
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { onReconnect: () => () => {} },
}));

function plan(id: string, overrides: Partial<TodoUpdatedEvent> = {}): TodoUpdatedEvent {
  return {
    session_id: id,
    revision: 1,
    done: 0,
    total: 2,
    current: "first step",
    items: [
      { id: "t1", text: "first step", state: "in_progress", started: "2026-10-29T00:00:00Z" },
      { id: "t2", text: "second step", state: "pending" },
    ],
    ...overrides,
  };
}

describe("SidebarTodoSection", () => {
  beforeEach(() => {
    getSessionTodo.mockReset();
  });

  it("renders the seeded plan with counts and the running item", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-sidebar-seed"));
    render(<SidebarTodoSection sessionId="ses-sidebar-seed" />);

    expect(await screen.findByText("first step")).toBeTruthy();
    expect(screen.getByText("0/2 done")).toBeTruthy();
    expect(screen.getByText("running")).toBeTruthy();
  });

  it("updates when a todo_updated frame lands", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-sidebar-live"));
    render(<SidebarTodoSection sessionId="ses-sidebar-live" />);
    await screen.findByText("first step");

    act(() =>
      setSessionPlan(
        "ses-sidebar-live",
        plan("ses-sidebar-live", {
          revision: 2,
          done: 1,
          total: 2,
          current: "second step",
          items: [
            { id: "t1", text: "first step", state: "done", result: "shipped" },
            { id: "t2", text: "second step", state: "in_progress" },
          ],
        }),
      ),
    );

    expect(screen.getByText("1/2 done")).toBeTruthy();
    expect(screen.getByText("shipped")).toBeTruthy();
    expect(screen.getByText("second step")).toBeTruthy();
  });

  it("shows the empty state when the session has no plan", async () => {
    getSessionTodo.mockResolvedValue({
      session_id: "ses-sidebar-empty",
      revision: 0,
      done: 0,
      total: 0,
      current: "",
      items: [],
    });
    render(<SidebarTodoSection sessionId="ses-sidebar-empty" />);

    expect(await screen.findByText("No TODO items")).toBeTruthy();
  });

  it("does not fetch for a draft tab", () => {
    render(<SidebarTodoSection sessionId="new-123" />);
    expect(getSessionTodo).not.toHaveBeenCalled();
  });

  it("ignores a malformed plan response instead of crashing", async () => {
    getSessionTodo.mockResolvedValue({});
    render(<SidebarTodoSection sessionId="ses-sidebar-malformed" />);

    expect(await screen.findByText("No TODO items")).toBeTruthy();
  });
});
