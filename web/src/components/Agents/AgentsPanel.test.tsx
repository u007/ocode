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

// The panel resolves its session from the active project tab, like
// AgentPreview does.
vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ activeTabId: "session-1" }),
}));

const mockUseAgentRuns = vi.fn<() => { runs: AgentRun[]; loaded: boolean }>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentsPanel", () => {
  it("shows the empty state when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: true });
    render(<AgentsPanel selectedRunId={null} onSelectRun={vi.fn()} />);

    expect(screen.getByText("No agent runs yet in this session.")).toBeInTheDocument();
  });

  it("renders the run list and opens a run's detail view on click", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel selectedRunId={null} onSelectRun={onSelectRun} />);

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    fireEvent.click(screen.getByText("code-reviewer"));
    expect(onSelectRun).toHaveBeenCalledWith("run-1");
  });

  it("renders the selected run's full tree, including nested children, in detail view", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    render(<AgentsPanel selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("reviewed")).toBeInTheDocument();
    expect(screen.getByText("sub-linter")).toBeInTheDocument();
  });

  it("calls onSelectRun(null) when the back button is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel selectedRunId="run-1" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Agents"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });

  it("shows a loading state while the tree has not arrived yet instead of falling through", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: false });
    render(<AgentsPanel selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("Loading agent run…")).toBeInTheDocument();
    expect(
      screen.queryByText("No agent runs yet in this session."),
    ).not.toBeInTheDocument();
  });

  it("surfaces a selected run that is missing from the loaded tree instead of silently showing the list", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    render(<AgentsPanel selectedRunId="gone-run" onSelectRun={vi.fn()} />);

    expect(
      screen.getByText("This agent run is no longer available in the current session's run list."),
    ).toBeInTheDocument();
    // Must not silently fall through to the unrelated run list.
    expect(screen.queryByText("code-reviewer")).not.toBeInTheDocument();
  });

  it("back button in the missing-run view clears the selection", () => {
    mockUseAgentRuns.mockReturnValue({ runs, loaded: true });
    const onSelectRun = vi.fn();
    render(<AgentsPanel selectedRunId="gone-run" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Back to all runs"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });
});
