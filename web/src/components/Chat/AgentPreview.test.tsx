import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import AgentPreview from "./AgentPreview";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";
import { AGENT_PREVIEW_COLLAPSED_STORAGE_KEY } from "./agentPreviewPersistence";
import type { AgentRun } from "../../api/types";

vi.mock("../../stores/projectStore", () => ({
  // resolveSessionHost (useSessionHost.ts) imports this directly, so the
  // real implementation runs against the stub state in these tests.
  findTabForSession: () => undefined,
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
  // Run cards render the chat's ThinkingBlock/ToolBlock, so the rail needs the
  // Chat display context. In the app it comes from AgentRunDisplayScope (or the
  // ChatPanel provider when a run card is inline in the transcript).
  function renderPreview(
    element: ReactNode,
    preset: "full" | "balanced" | "quiet" = "full",
  ) {
    return render(<ChatDisplayTestProvider preset={preset}>{element}</ChatDisplayTestProvider>);
  }

  beforeEach(() => {
    window.localStorage.removeItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY);
  });

  it("calls onOpenDetail with the run id when a run name is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    const onOpenDetail = vi.fn();
    renderPreview(<AgentPreview onOpenDetail={onOpenDetail} />);

    fireEvent.click(screen.getByText("code-reviewer"));

    expect(onOpenDetail).toHaveBeenCalledWith("run-1");
  });

  it("renders spawned runs collapsed until the user expands a row", () => {
    mockUseAgentRuns.mockReturnValue({
      runs: [{ ...runningRun, messages: [{ role: "assistant", content: "agent thinking out loud" }] }],
      loaded: true,
    });
    const { container } = renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);

    // the rail shows the summary row but not the transcript by default
    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(container.textContent).not.toContain("agent thinking out loud");

    // clicking the row's status area (not the name link) expands it
    fireEvent.click(screen.getByText("running"));
    expect(screen.getByText("agent thinking out loud")).toBeInTheDocument();
  });

  it("yields height to the transcript instead of crushing it", () => {
    mockUseAgentRuns.mockReturnValue({
      runs: [{ ...runningRun, messages: [{ role: "assistant", content: "agent thinking out loud" }] }],
      loaded: true,
    });
    renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);
    // Token-exact: the substring "shrink" also matches "shrink-0", so a
    // `toContain("shrink")` check alone cannot fail when the regression is
    // restored. Split on whitespace and assert membership instead.
    const classesOf = (testId: string) => screen.getByTestId(testId).className.split(/\s+/);

    // The header row is a flex sibling of the chat transcript and never gives
    // way, so the "Agents" toggle stays reachable at any column height.
    expect(classesOf("agent-preview-header")).toContain("shrink-0");

    // Only the body yields: it shrinks, drops below its content (`min-h-0`),
    // caps at max-h-52 and scrolls. Expanding a run takes at most that cap
    // from the column, and the transcript gives way to it first.
    const body = classesOf("agent-preview-body");
    expect(body).toContain("shrink");
    expect(body).not.toContain("shrink-0");
    expect(body).toContain("min-h-0");
    expect(body).toContain("overflow-y-auto");
    expect(body).toContain("max-h-52");

    // The body exists only while the rail is expanded (the default); collapsed
    // leaves the header row alone.
    fireEvent.click(screen.getByRole("button", { name: "Collapse agents" }));
    expect(screen.queryByTestId("agent-preview-body")).toBeNull();
    expect(classesOf("agent-preview-header")).toContain("shrink-0");
    fireEvent.click(screen.getByRole("button", { name: "Expand agents" }));
    expect(classesOf("agent-preview-body")).toContain("max-h-52");
  });

  it("renders nothing when there are no runs", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [], loaded: true });
    const { container } = renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("collapses to just the header when the header toggle is clicked", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);

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
    renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);

    expect(screen.queryByText("code-reviewer")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand agents" })).toHaveAttribute("aria-expanded", "false");
  });

  it("re-expands from the collapsed header and persists the choice", () => {
    window.localStorage.setItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY, "true");
    mockUseAgentRuns.mockReturnValue({ runs: [runningRun], loaded: true });
    renderPreview(<AgentPreview onOpenDetail={vi.fn()} />);

    fireEvent.click(screen.getByRole("button", { name: "Expand agents" }));

    expect(screen.getByText("code-reviewer")).toBeInTheDocument();
    expect(window.localStorage.getItem(AGENT_PREVIEW_COLLAPSED_STORAGE_KEY)).toBe("false");
  });

  it("renders run messages through the chat transcript components and its display setting", () => {
    const toolRun: AgentRun = {
      ...runningRun,
      messages: [
        {
          role: "assistant",
          content: "",
          toolCalls: [{ id: "call-1", name: "bash", arguments: '{"command":"echo hi"}' }],
        },
        { role: "tool", toolCallId: "call-1", content: "alpha output" },
      ],
    };
    mockUseAgentRuns.mockReturnValue({ runs: [toolRun], loaded: true });

    // Quiet collapses tool output, so the run card shows the result gate only.
    renderPreview(<AgentPreview onOpenDetail={vi.fn()} />, "quiet");
    fireEvent.click(screen.getByText("running"));

    const gate = screen.getByRole("button", { name: /show output/i });
    expect(screen.queryByText("alpha output")).toBeNull();
    fireEvent.click(gate);
    expect(screen.getByText("alpha output")).toBeInTheDocument();
  });
});
