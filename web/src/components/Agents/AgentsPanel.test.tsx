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

const mockUseAgentRuns = vi.fn<() => AgentRun[]>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentsPanel", () => {
  it("shows the empty state when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue([]);
    render(<AgentsPanel session="session-1" selectedRunId={null} onSelectRun={vi.fn()} />);

    expect(screen.getByText("No agent runs yet in this session.")).toBeInTheDocument();
  });

  it("renders the run list and opens a run's detail view on click", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    const onSelectRun = vi.fn();
    render(<AgentsPanel session="session-1" selectedRunId={null} onSelectRun={onSelectRun} />);

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    fireEvent.click(screen.getByText("code-reviewer"));
    expect(onSelectRun).toHaveBeenCalledWith("run-1");
  });

  it("renders the selected run's full tree, including nested children, in detail view", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    render(<AgentsPanel session="session-1" selectedRunId="run-1" onSelectRun={vi.fn()} />);

    expect(screen.getByText("reviewed")).toBeInTheDocument();
    expect(screen.getByText("sub-linter")).toBeInTheDocument();
  });

  it("calls onSelectRun(null) when the back button is clicked", () => {
    mockUseAgentRuns.mockReturnValue(runs);
    const onSelectRun = vi.fn();
    render(<AgentsPanel session="session-1" selectedRunId="run-1" onSelectRun={onSelectRun} />);

    fireEvent.click(screen.getByText("Agents"));
    expect(onSelectRun).toHaveBeenCalledWith(null);
  });
});
