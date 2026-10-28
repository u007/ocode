import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ToolBlock } from "./TurnParts";
import { ChatDisplayTestProvider } from "./chatDisplayTestUtils";
import type { AgentRun, ChatVerbosityConfig } from "../../api/types";

const stickCalls: unknown[] = [];
vi.mock("../../hooks/useStickToBottom", () => ({
  useStickToBottom: (_ref: unknown, dep: unknown) => {
    stickCalls.push(dep);
    return { onScroll: () => {}, onNavigate: () => {} };
  },
}));

const mockUseAgentRuns = vi.fn<() => { runs: AgentRun[]; loaded: boolean }>();
vi.mock("../../hooks/useAgentRuns", () => ({
  useAgentRuns: () => mockUseAgentRuns(),
}));

const run: AgentRun = {
  id: "agent-run-1",
  name: "explore",
  status: "running",
  startedAt: "2026-08-06T10:00:00.000Z",
  inputTokens: 0,
  outputTokens: 0,
  toolCallId: "call_1",
  todoItem: "t2",
  messages: [{ role: "assistant", content: "streaming transcript line" }],
  children: [],
};

function taskTree(toolCallId: string, tool = "task", callKey = "c", overrides?: Partial<ChatVerbosityConfig["overrides"]>) {
  return (
    <ChatDisplayTestProvider preset="full" overrides={overrides}>
      <ToolBlock
        tool={tool}
        command='{"prompt":"look around"}'
        output="final result"
        toolCallId={toolCallId}
        sessionId="ses_1"
        callKey={callKey}
        outputKey={`${callKey}:out`}
      />
    </ChatDisplayTestProvider>
  );
}

function renderTask(toolCallId: string, tool = "task") {
  return render(taskTree(toolCallId, tool));
}

describe("ToolBlock for task/agent calls", () => {
  beforeEach(() => {
    mockUseAgentRuns.mockReset();
    stickCalls.length = 0;
  });

  it("renders the matching run inline, collapsed, with a live summary", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
    renderTask("call_1");

    expect(screen.getByText("explore")).toBeInTheDocument();
    expect(screen.getByText("running")).toBeInTheDocument();
    expect(screen.getByText("t2")).toBeInTheDocument();
    expect(screen.getByText("1 msg")).toBeInTheDocument();
    // collapsed: no transcript, and the generic result body is replaced
    expect(screen.queryByText("streaming transcript line")).toBeNull();
    expect(screen.queryByText("final result")).toBeNull();
  });

  it("matches the agent tool name too and expands to the transcript", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
    renderTask("call_1", "agent");

    fireEvent.click(screen.getByText("running"));
    expect(screen.getByText("streaming transcript line")).toBeInTheDocument();
  });

  it("renders the plain block when no run matches the call id", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
    renderTask("call_other");

    expect(screen.queryByText("explore")).toBeNull();
    expect(screen.getByText("final result")).toBeInTheDocument();
  });

  it("does not subscribe to runs for other tools", () => {
    render(
      <ChatDisplayTestProvider preset="full">
        <ToolBlock tool="bash" command="ls" output="x" toolCallId="call_1" sessionId="ses_1" />
      </ChatDisplayTestProvider>,
    );
    expect(mockUseAgentRuns).not.toHaveBeenCalled();
  });
  it("appends streamed child messages to an open card and feeds them to the stick-to-bottom hook", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
    const { rerender } = render(taskTree("call_1"));
    fireEvent.click(screen.getByText("running"));
    expect(screen.getByText("streaming transcript line")).toBeInTheDocument();

    const grown = [...run.messages, { role: "assistant", content: "second streamed line" }];
    mockUseAgentRuns.mockReturnValue({ runs: [{ ...run, messages: grown }], loaded: true });
    rerender(taskTree("call_1", "task", "c-rerender"));

    expect(screen.getByText("second streamed line")).toBeInTheDocument();
    // The transcript box is bounded so it scrolls internally, and the hook was
    // handed the new list (it only receives the list while the card is open).
    expect(screen.getByTestId("agent-transcript").className).toContain("max-h-72");
    expect(screen.getByTestId("agent-transcript").className).toContain("overflow-y-auto");
    expect(stickCalls[stickCalls.length - 1]).toEqual(grown);
  });

  it("updates the collapsed summary live without expanding", () => {
    mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
    const { rerender } = render(taskTree("call_1"));
    expect(screen.getByText("1 msg")).toBeInTheDocument();

    mockUseAgentRuns.mockReturnValue({
      runs: [{ ...run, status: "done", messages: [...run.messages, { role: "assistant", content: "x" }] }],
      loaded: true,
    });
    rerender(taskTree("call_1", "task", "c-rerender"));
    expect(screen.getByText("2 msg")).toBeInTheDocument();
    expect(screen.getByText("done")).toBeInTheDocument();
  });

  it("advances elapsed time between run updates while running", () => {
    vi.useFakeTimers();
    try {
      vi.setSystemTime(new Date("2026-08-06T10:00:00.000Z"));
      mockUseAgentRuns.mockReturnValue({ runs: [run], loaded: true });
      renderTask("call_1");
      expect(screen.getByText("0ms")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(3000);
      });
      expect(screen.getByText("3.0s")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  describe("child thinking follows the chat thinking setting", () => {
    const withReasoning = (status: string): AgentRun => ({
      ...run,
      status,
      messages: [{ role: "assistant", content: "answer text", reasoningContent: "secret child reasoning" }],
    });

    it("shows nothing of the thinking while the card is collapsed", () => {
      mockUseAgentRuns.mockReturnValue({ runs: [withReasoning("done")], loaded: true });
      renderTask("call_1");
      expect(screen.queryByText("secret child reasoning")).toBeNull();
      expect(screen.queryByText(/Thinking/)).toBeNull();
    });

    it.each(["running", "done"])("hides the reasoning text when the chat setting is collapsed (%s run)", (status) => {
      mockUseAgentRuns.mockReturnValue({ runs: [withReasoning(status)], loaded: true });
      render(taskTree("call_1", "task", "c", { older_thinking: "collapsed", latest_thinking: "collapsed" }));
      fireEvent.click(screen.getByText(status));
      expect(screen.getByText("answer text")).toBeInTheDocument();
      expect(screen.getByText(/Thinking/)).toBeInTheDocument();
      expect(screen.queryByText("secret child reasoning")).toBeNull();
    });

    it.each(["running", "done"])("shows the reasoning text when the chat setting is expanded (%s run)", (status) => {
      mockUseAgentRuns.mockReturnValue({ runs: [withReasoning(status)], loaded: true });
      render(taskTree("call_1", "task", "c", { older_thinking: "expanded", latest_thinking: "expanded" }));
      fireEvent.click(screen.getByText(status));
      expect(screen.getByText("secret child reasoning")).toBeInTheDocument();
    });
  });
});
