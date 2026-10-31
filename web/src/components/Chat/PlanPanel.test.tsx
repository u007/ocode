import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PlanPanel from "./PlanPanel";
import { PLAN_PANEL_COLLAPSED_STORAGE_KEY } from "./planPanelPersistence";
import { setSessionPlan } from "../../lib/sessionPlan";
import type { TodoUpdatedEvent } from "../../api/types";

const getSessionTodo = vi.fn<(id: string, host?: string) => Promise<TodoUpdatedEvent>>();
vi.mock("../../api/client", () => ({
  api: { getSessionTodo: (id: string, host?: string) => getSessionTodo(id, host) },
}));
let planPanelSetting = "5";
vi.mock("../../lib/chatVerbosity", () => ({
  useChatVerbosity: () => ({ config: { overrides: { plan_panel: planPanelSetting } } }),
}));
vi.mock("../../lib/eventBus", () => ({
  eventBus: { onReconnect: () => () => {} },
}));

const plan = (id: string, items: TodoUpdatedEvent["items"]): TodoUpdatedEvent => ({
  session_id: id,
  revision: 1,
  done: items.filter((i) => i.state === "done").length,
  total: items.length,
  current: "",
  items,
});

describe("PlanPanel", () => {
  beforeEach(() => {
    window.localStorage.removeItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY);
    getSessionTodo.mockReset();
    planPanelSetting = "5";
  });

  it("keeps the header row non-shrinking and makes only the body shrinkable", async () => {
    getSessionTodo.mockResolvedValue(
      plan("ses_shrink", [
        { id: "t1", text: "build it", state: "done" },
        { id: "t2", text: "test it", state: "in_progress" },
      ]),
    );
    render(<PlanPanel sessionId="ses_shrink" />);
    await screen.findByText("1/2");
    // Token-exact: the substring "shrink" also matches "shrink-0", so split on
    // whitespace and assert membership.
    const classesOf = (testId: string) => screen.getByTestId(testId).className.split(/\s+/);

    // The header is a flex sibling of the transcript and never gives way, so
    // the toggle stays visible however small the column gets.
    expect(classesOf("plan-panel-header")).toContain("shrink-0");

    // Only the body yields height: it shrinks, drops below its content, caps
    // at max-h-52 and scrolls. An expanded plan therefore takes at most its
    // cap from the column, and the transcript gives way to it first.
    const body = classesOf("plan-panel-body");
    expect(body).toContain("shrink");
    expect(body).not.toContain("shrink-0");
    expect(body).toContain("min-h-0");
    expect(body).toContain("overflow-y-auto");
    expect(body).toContain("max-h-52");

    // The body exists only while expanded; collapsed leaves the header alone.
    fireEvent.click(await screen.findByRole("button", { name: "Collapse plan" }));
    expect(screen.queryByTestId("plan-panel-body")).toBeNull();
    expect(classesOf("plan-panel-header")).toContain("shrink-0");
  });

  it("renders nothing when the session has no plan", async () => {
    getSessionTodo.mockResolvedValue(plan("ses_empty", []));
    const { container } = render(<PlanPanel sessionId="ses_empty" />);
    await waitFor(() => expect(getSessionTodo).toHaveBeenCalledWith("ses_empty", undefined));
    expect(container).toBeEmptyDOMElement();
  });

  it("loads the plan and shows blocked, running and finished details", async () => {
    getSessionTodo.mockResolvedValue(
      plan("ses_a", [
        { id: "t1", text: "build it", state: "done", result: "built ok" },
        { id: "t2", text: "test it", state: "in_progress", run_id: "agent-run-2" },
        { id: "t3", text: "ship it", state: "pending", deps: ["t2"], blocked: true },
      ]),
    );
    const onOpenRun = vi.fn();
    render(<PlanPanel sessionId="ses_a" host="box" onOpenRun={onOpenRun} />);

    expect(await screen.findByText("1/3")).toBeInTheDocument();
    expect(getSessionTodo).toHaveBeenCalledWith("ses_a", "box");
    expect(screen.getByText("build it")).toHaveClass("line-through");
    expect(screen.getByText("built ok")).toBeInTheDocument();
    expect(screen.getByText("blocked by t2")).toBeInTheDocument();
    fireEvent.click(screen.getByText("running as agent-run-2"));
    expect(onOpenRun).toHaveBeenCalledWith("agent-run-2");
  });

  it("updates live from the store without refetching", async () => {
    getSessionTodo.mockResolvedValue(plan("ses_b", [{ id: "t1", text: "first", state: "pending" }]));
    render(<PlanPanel sessionId="ses_b" />);
    expect(await screen.findByText("0/1")).toBeInTheDocument();

    act(() => setSessionPlan("ses_b", plan("ses_b", [{ id: "t1", text: "first", state: "done" }])));
    expect(screen.getByText("1/1")).toBeInTheDocument();
    expect(getSessionTodo).toHaveBeenCalledTimes(1);
  });

  it("persists the collapsed state and hides the items", async () => {
    getSessionTodo.mockResolvedValue(plan("ses_c", [{ id: "t1", text: "only item", state: "in_progress" }]));
    render(<PlanPanel sessionId="ses_c" />);
    expect(await screen.findByText("only item")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Collapse plan" }));
    expect(screen.queryByText("only item")).toBeNull();
    expect(JSON.parse(window.localStorage.getItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY) ?? "{}")).toEqual({ ses_c: true });
  });

  it("starts collapsed when the stored preference says so", async () => {
    window.localStorage.setItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY, JSON.stringify({ ses_d: true }));
    getSessionTodo.mockResolvedValue(plan("ses_d", [{ id: "t1", text: "hidden item", state: "pending" }]));
    render(<PlanPanel sessionId="ses_d" />);
    expect(await screen.findByRole("button", { name: "Expand plan" })).toBeInTheDocument();
    expect(screen.queryByText("hidden item")).toBeNull();
  });
  describe("plan_panel display setting", () => {
    const eight = (): TodoUpdatedEvent["items"] => [
      { id: "t1", text: "item one", state: "done" },
      { id: "t2", text: "item two", state: "pending" },
      { id: "t3", text: "item three", state: "in_progress" },
      { id: "t4", text: "item four", state: "done" },
      { id: "t5", text: "item five", state: "pending" },
      { id: "t6", text: "item six", state: "pending" },
      { id: "t7", text: "item seven", state: "pending" },
      { id: "t8", text: "item eight", state: "pending" },
    ];

    it("5 starts open with five rows, open items first, and a +N more toggle", async () => {
      getSessionTodo.mockResolvedValue(plan("ses_e", eight()));
      render(<PlanPanel sessionId="ses_e" />);
      expect(await screen.findByText("item two")).toBeInTheDocument();

      const rows = screen.getAllByRole("listitem").filter((li) => li.textContent?.startsWith("☐") || li.textContent?.startsWith("▸") || li.textContent?.startsWith("☑"));
      expect(rows.map((li) => li.textContent)).toEqual([
        "☐ item two",
        "▸ item three",
        "☐ item five",
        "☐ item six",
        "☐ item seven",
      ]);
      expect(screen.queryByText("item one")).toBeNull();

      fireEvent.click(screen.getByText("+3 more"));
      expect(screen.getByText("item one")).toBeInTheDocument();
      expect(screen.getByText("item eight")).toBeInTheDocument();
      fireEvent.click(screen.getByText("Show less"));
      expect(screen.queryByText("item eight")).toBeNull();
    });

    it("all shows every row with no toggle", async () => {
      planPanelSetting = "all";
      getSessionTodo.mockResolvedValue(plan("ses_f", eight()));
      render(<PlanPanel sessionId="ses_f" />);
      expect(await screen.findByText("item eight")).toBeInTheDocument();
      expect(screen.getByText("item one")).toBeInTheDocument();
      expect(screen.queryByText(/more$/)).toBeNull();
    });

    it("collapsed starts folded, and a remembered choice overrides the setting", async () => {
      planPanelSetting = "collapsed";
      getSessionTodo.mockResolvedValue(plan("ses_g", eight()));
      const { unmount } = render(<PlanPanel sessionId="ses_g" />);
      fireEvent.click(await screen.findByRole("button", { name: "Expand plan" }));
      expect(screen.getByText("item two")).toBeInTheDocument();
      unmount();

      // The user's unfold wins over the "collapsed" setting on the next mount.
      render(<PlanPanel sessionId="ses_g" />);
      expect(await screen.findByText("item two")).toBeInTheDocument();
    });

    it("a remembered fold wins over an open setting", async () => {
      window.localStorage.setItem(PLAN_PANEL_COLLAPSED_STORAGE_KEY, JSON.stringify({ ses_h: true }));
      getSessionTodo.mockResolvedValue(plan("ses_h", eight()));
      render(<PlanPanel sessionId="ses_h" />);
      expect(await screen.findByRole("button", { name: "Expand plan" })).toBeInTheDocument();
      expect(screen.queryByText("item two")).toBeNull();
    });
  });
});
