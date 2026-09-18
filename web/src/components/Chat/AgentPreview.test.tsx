import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AgentPreview from "./AgentPreview";
import { AGENT_PREVIEW_COLLAPSED_STORAGE_KEY } from "./agentPreviewPersistence";
import type { AgentRun } from "../../api/types";

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ activeTabId: "session-1" }),
}));

const runningRun: AgentRun = {
  id: "run-1",
  name: "code-reviewer",
  status: "running",
  startedAt: "2026-08-06T10:00:00.000Z",
  inputTokens: 0,
  outputTokens: 0,
  messages: [],
  children: [],
};

const mockUseAgentRuns = vi.fn<() => { runs: AgentRun[]; loaded: boolean }>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

describe("AgentPreview", () => {
  beforeEach(() => {
    window.localStorage.removeItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY);
  });

  it("calls onOpenDetail with the run id when a run name is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    const onOpenDetail = vi.fn();
    render(<AgentPreview onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("code-reviewer"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-1");
  });

  it("renders spawned runs collapsed until the user expands a row", () => {
    mockUseAgentRuns.mockReturnValue({
      runs: [{ ...runningRun, messages: [{ role: "assistant", content: "agent thinking out loud" }] }],
      loaded: true,
    });
    const { container } = render(<AgentPreview onOpenDetail={vi.fn()} />);

    // the rail shows the summary row but not the transcript by default
    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(container.textContent).not.toContain("agent thinking out loud");

    // clicking the row's status area (not the name link) expands it
    fireEvent.click(screen.getByText("running"));
    expect(screen.getByText("agent thinking out loud")).toBeInTheDocument();
  });

  it("renders nothing when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: true });
    const { container } = render(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("collapses to just the header when the header toggle is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    render(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Collapse agents" }));

    // run rows are hidden, but the header signal (label + count) stays visible
    expect(screen.queryByText("code-reviewer")).not.toBeInTheDocument();
    expect(screen.getByText("Agents")).toBeInTheDocument();
    expect(screen.getByText("1")).toBeInTheDocument();
    expect(window.localStorage.getItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY)).toBe("true");
  });

  it("starts collapsed when the stored preference says so", () => {
    window.localStorage.setItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY, "true");
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    render(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(screen.queryByText("code-reviewer")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand agents" })).toHaveAttribute("aria-expanded", "false");
  });

  it("re-expands from the collapsed header and persists the choice", () => {
    window.localStorage.setItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY, "true");
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    render(<AgentPreview onOpenDetail={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Expand agents" }));

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(window.localStorage.getItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY)).toBe("false");
  });
});
