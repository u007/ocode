import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import AgentsPanel from "./AgentsPanel";
import type { AgentRun } from "../../api/types";

const runs: AgentRun[] = [
  {
    id: "run-1",
    name: "code-reviewer",
    status: "done",
    startedAt: "2026-08-06T10:00:00.000Z",
    endedAt: "2026-08-06T10:00:02.000Z",
    inputTokens: 0,
    outputTokens: 0,
    messages: [{ role: "assistant", content: "reviewed" }],
    children: [
      {
        id: "run-2",
        name: "sub-linter",
        status: "done",
        startedAt: "2026-08-06T10:00:00.000Z",
        endedAt: "2026-08-06T10:00:01.000Z",
        inputTokens: 0,
        outputTokens: 0,
        messages: [],
        children: [],
      },
    ],
  },
];

const mockUseAgentRuns = vi.fn<() => { runs: AgentRun[]; loaded: boolean }>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentsPanel", () => {
  it("shows the empty state when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: true });
    render(<AgentsPanel sessionId="session-1" selectedRunId={null} onSelectRun={vi.fn()} />);

    expect(screen.getByText("No agent runs yet in this session.")).toBeInTheDocument();
  });

  it("renders the run list and opens a run's detail view on click", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel sessionId="session-1" selectedRunId={null} onSelectRun={onSelectRun} />);

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    fireEvent.click(screen.getByText("code-reviewer"));
    expect(onSelectRun).toHaveBeenCalledWith("run-1");
  });

  it("keeps more than one run expanded at the same time in the list", () => {
    const twoRuns: AgentRun[] = [
      {
        ...runs[0],
        id: "run-a",
        name: "agent-a",
        children: [],
        messages: [{ role: "assistant", content: "alpha output" }],
      },
      {
        ...runs[0],
        id: "run-b",
        name: "agent-b",
        children: [],
        messages: [{ role: "assistant", content: "beta output" }],
      },
    ];
    mockUseAgentRuns.mockReturnValue({ runs: twoRuns, loaded: true });
    render(<AgentsPanel sessionId="session-1" selectedRunId={null} onSelectRun={vi.fn()} />);

    // Rows start collapsed so a spawned crew does not balloon the list.
    expect(screen.queryByText("alpha output")).toBeNull();
    expect(screen.queryByText("beta output")).toBeNull();

    // The name span opens the single-run drill-in, so the row button (the name
    // span's ancestor) is the expand toggle.
    const rowFor = (name: string) => screen.getByText(name).closest("button")!;
    fireEvent.click(rowFor("agent-a"));
    fireEvent.click(rowFor("agent-b"));

    // Both stay open — this is not a single-view swap.
    expect(screen.getByText("alpha output")).toBeInTheDocument();
    expect(screen.getByText("beta output")).toBeInTheDocument();

    // Collapsing one leaves the other open.
    fireEvent.click(rowFor("agent-a"));
    expect(screen.queryByText("alpha output")).toBeNull();
    expect(screen.getByText("beta output")).toBeInTheDocument();
  });

  it("renders the selected run's full tree, including nested children, in detail view", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    render(<AgentsPanel sessionId="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("reviewed")).toBeInTheDocument();
    expect(screen.getByText("sub-linter")).toBeInTheDocument();
  });

  it("calls onSelectRun(null) when the back button is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel sessionId="session-1" selectedRunId="run-1" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Agents"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });

  it("shows a loading state while the tree has not arrived yet instead of falling through", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: false });
    render(<AgentsPanel sessionId="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("Loading agent run…")).toBeInTheDocument();
    expect(
      screen.queryByText("No agent runs yet in this session."),
    ).not.toBeInTheDocument();
  });

  it("surfaces a selected run that is missing from the loaded tree instead of silently showing the list", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    render(<AgentsPanel sessionId="session-1" selectedRunId="gone-run" onSelectRun={vi.fn()} />);

    expect(
      screen.getByText("This agent run is no longer available in the current session's run list."),
    ).toBeInTheDocument();
    // Must not silently fall through to the unrelated run list.
    expect(screen.queryByText("code-reviewer")).not.toBeInTheDocument();
  });

  it("back button in the missing-run view clears the selection", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel sessionId="session-1" selectedRunId="gone-run" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Back to all runs"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });

  it("exposes scroll-to-top and scroll-to-bottom affordances in a long run detail", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const { container } = render(
      <AgentsPanel sessionId="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />,
    );

    const scroller = container.querySelector<HTMLElement>(".overflow-y-auto")!;
    expect(scroller).toBeTruthy();
    let top = 0;
    Object.defineProperty(scroller, "scrollHeight", { configurable: true, value: 2000 });
    Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 300 });
    Object.defineProperty(scroller, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v;
      },
    });

    // Parked at the top of a long detail: only "scroll to bottom" is offered.
    fireEvent.scroll(scroller);
    expect(screen.getByRole("button", { name: /scroll to bottom/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /scroll to top/i })).toBeNull();

    // Scrolled into the middle: both edges are reachable.
    top = 800;
    fireEvent.scroll(scroller);
    expect(screen.getByRole("button", { name: /scroll to top/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /scroll to bottom/i })).toBeInTheDocument();
  });

  it("resets a freshly selected run's detail scroll offset to the top", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const { rerender } = render(
      <AgentsPanel sessionId="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />,
    );
    const scroller = document.querySelector<HTMLElement>(".overflow-y-auto")!;
    let top = 900;
    Object.defineProperty(scroller, "scrollHeight", { configurable: true, value: 2000 });
    Object.defineProperty(scroller, "clientHeight", { configurable: true, value: 300 });
    Object.defineProperty(scroller, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v;
      },
    });

    rerender(<AgentsPanel sessionId="session-1" selectedRunId="run-2" onSelectRun={vi.fn()} />);

    expect(top).toBe(0);
  });
});
