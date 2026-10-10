import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { PulseFocusPane } from "./PulseFocusPane";
import type { PulseTail } from "./usePulseTail";
import type { PulseRow } from "../../api/types";

const mockTail = vi.fn<(id: string, enabled: boolean, status: string) => PulseTail>();
vi.mock("./usePulseTail", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./usePulseTail")>();
  return { ...actual, usePulseTail: (...a: Parameters<typeof actual.usePulseTail>) => mockTail(...a) };
});

const mockGetSessionState = vi.fn();
const mockResolvePermission = vi.fn();
const mockAnswerQuestion = vi.fn();
const mockCancelQuestion = vi.fn();
const mockSendMessage = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    resolvePermission: (...a: unknown[]) => mockResolvePermission(...a),
    answerQuestion: (...a: unknown[]) => mockAnswerQuestion(...a),
    cancelQuestion: (...a: unknown[]) => mockCancelQuestion(...a),
    sendMessage: (...a: unknown[]) => mockSendMessage(...a),
  },
}));

const mockJump = vi.fn();
vi.mock("../../lib/jumpToSession", () => ({
  useJumpToSession: () => mockJump,
  useJumpToPendingAsk: () => vi.fn(),
}));

function makeRow(over: Partial<PulseRow> = {}): PulseRow {
  return {
    session_id: "ses_1",
    project_path: "/Users/james/www/ocode",
    title: "Fix the pulse dashboard",
    status: "idle",
    current_task: null,
    todo: null,
    pending_ask: null,
    turn_started_at: "2020-01-01T00:00:00Z",
    updated_at: new Date().toISOString(),
    child_count: 0,
    ...over,
  };
}

const permissionAsk = {
  request_id: "req_perm",
  tool: "bash",
  command: "rm -rf build",
  scope: "tool",
};

function loggedErrors(): string {
  return vi
    .mocked(console.error)
    .mock.calls.map((c) => c.map((a) => String(a)).join(" "))
    .join("\n");
}

beforeEach(() => {
  for (const m of [mockGetSessionState, mockResolvePermission, mockAnswerQuestion, mockCancelQuestion, mockSendMessage, mockJump, mockTail]) {
    m.mockReset();
  }
  mockTail.mockReturnValue({ entries: [], error: null, loading: false });
  mockGetSessionState.mockResolvedValue({});
  vi.spyOn(console, "error").mockImplementation(() => {});
});
afterEach(() => vi.restoreAllMocks());

describe("PulseFocusPane header", () => {
  it("shows project, title and status, opens the session, and closes", () => {
    const onClose = vi.fn();
    render(<PulseFocusPane row={makeRow()} onClose={onClose} />);

    expect(screen.getByText("ocode")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Fix the pulse dashboard" })).toBeInTheDocument();
    expect(screen.getByLabelText("idle")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /open session/i }));
    expect(mockJump).toHaveBeenCalledWith({
      projectPath: "/Users/james/www/ocode",
      host: "",
      sessionId: "ses_1",
      title: "Fix the pulse dashboard",
    });

    fireEvent.click(screen.getByRole("button", { name: "Close focus" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("lists the full plan with state marks", () => {
    render(
      <PulseFocusPane
        row={makeRow({
          todo: {
            done: 1,
            total: 2,
            current: "second",
            items: [
              { text: "first", state: "done" },
              { text: "second", state: "in_progress" },
            ],
          },
        })}
        onClose={() => {}}
      />,
    );

    const todo = screen.getByTestId("pulse-focus-todo");
    expect(within(todo).getByText("first")).toBeInTheDocument();
    expect(within(todo).getByText("☑")).toBeInTheDocument();
    expect(within(todo).getByText("▸")).toBeInTheDocument();
  });
});

describe("PulseFocusPane activity feed", () => {
  it("renders text and tool entries from the tail hook, enabled for any status", () => {
    mockTail.mockReturnValue({
      entries: [
        { kind: "text", text: "looking around" },
        { kind: "tool", text: "▸ bash ls ✓" },
      ],
      error: null,
      loading: false,
    });
    render(<PulseFocusPane row={makeRow({ status: "running" })} onClose={() => {}} />);

    const feed = screen.getByTestId("pulse-focus-stream");
    expect(within(feed).getByText("looking around")).toBeInTheDocument();
    expect(within(feed).getByText("▸ bash ls ✓")).toBeInTheDocument();
    expect(mockTail).toHaveBeenLastCalledWith("ses_1", true, "running");
    // No height cap on the pane's feed, unlike the card's.
    expect(feed.className).toContain("overflow-y-auto");
    expect(feed.className).not.toContain("max-h-");
  });

  it("says when there is nothing to show, or that it is loading", () => {
    const { rerender } = render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);
    expect(screen.getByText("No activity to show")).toBeInTheDocument();

    mockTail.mockReturnValue({ entries: [], error: null, loading: true });
    rerender(<PulseFocusPane row={makeRow()} onClose={() => {}} />);
    expect(screen.getByText("Loading…")).toBeInTheDocument();
  });
});

describe("PulseFocusPane pending permission", () => {
  const askRow = () =>
    makeRow({ status: "needs_permission", pending_ask: { kind: "permission", summary: "run rm -rf build" } });

  it("renders the permission dialog inside the pane and resolves it by request id", async () => {
    mockGetSessionState.mockResolvedValue({ pending_asks: { permissions: [permissionAsk] } });
    mockResolvePermission.mockResolvedValue({});
    render(<PulseFocusPane row={askRow()} onClose={() => {}} />);

    const dialog = await screen.findByRole("dialog");
    // Confined to the pane, not portalled to the viewport.
    expect(screen.getByTestId("pulse-focus-pane").contains(dialog)).toBe(true);
    expect(within(dialog).getByText("rm -rf build")).toBeInTheDocument();

    fireEvent.click(within(dialog).getByText("Allow once"));

    await waitFor(() => expect(mockResolvePermission).toHaveBeenCalledWith("req_perm", "ses_1", "allow"));
    expect(mockGetSessionState).toHaveBeenCalledWith("ses_1");
  });

  it("shows a failed decision inline and logs it with the session id", async () => {
    mockGetSessionState.mockResolvedValue({ pending_asks: { permissions: [permissionAsk] } });
    mockResolvePermission.mockRejectedValue(new Error("409 gone"));
    render(<PulseFocusPane row={askRow()} onClose={() => {}} />);

    fireEvent.click(within(await screen.findByRole("dialog")).getByText("Allow once"));

    await waitFor(() => expect(screen.getByTestId("pulse-focus-error")).toHaveTextContent("409 gone"));
    expect(screen.getByTestId("pulse-focus-error")).toHaveTextContent("ses_1");
    expect(loggedErrors()).toContain("ses_1");
  });

  it("surfaces a failed ask lookup instead of showing nothing", async () => {
    mockGetSessionState.mockRejectedValue(new Error("state boom"));
    render(<PulseFocusPane row={askRow()} onClose={() => {}} />);

    await waitFor(() => expect(screen.getByTestId("pulse-focus-error")).toHaveTextContent("state boom"));
    expect(loggedErrors()).toContain("ses_1");
  });

  it("fetches no ask state, and shows no dialog, when nothing is pending", async () => {
    render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);
    await act(async () => {});

    expect(mockGetSessionState).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("PulseFocusPane pending question", () => {
  it("answers the first question through answerQuestion", async () => {
    mockGetSessionState.mockResolvedValue({
      pending_asks: {
        questions: [
          {
            request_id: "req_q",
            questions: [
              { header: "Scope", question: "Pick one?", options: [{ label: "Alpha" }, { label: "Beta" }] },
            ],
          },
        ],
      },
    });
    mockAnswerQuestion.mockResolvedValue({});
    render(
      <PulseFocusPane
        row={makeRow({ status: "needs_question", pending_ask: { kind: "question", summary: "Pick one?" } })}
        onClose={() => {}}
      />,
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByText("Alpha"));
    fireEvent.click(within(dialog).getByRole("button", { name: /submit/i }));

    await waitFor(() =>
      expect(mockAnswerQuestion).toHaveBeenCalledWith("req_q", "ses_1", expect.any(Array)),
    );
  });
});

describe("PulseFocusPane reply", () => {
  const textarea = () => screen.getByTestId("pulse-focus-reply") as HTMLTextAreaElement;

  it("sends on Enter, clears the box, and does not send on Shift+Enter", async () => {
    mockSendMessage.mockResolvedValue({});
    render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);

    fireEvent.change(textarea(), { target: { value: "please continue" } });
    fireEvent.keyDown(textarea(), { key: "Enter", shiftKey: true });
    expect(mockSendMessage).not.toHaveBeenCalled();

    fireEvent.keyDown(textarea(), { key: "Enter" });

    await waitFor(() => expect(mockSendMessage).toHaveBeenCalledWith("ses_1", "please continue"));
    await waitFor(() => expect(textarea().value).toBe(""));
  });

  it("sends from the Send button", async () => {
    mockSendMessage.mockResolvedValue({});
    render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);

    fireEvent.change(textarea(), { target: { value: "hello" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(mockSendMessage).toHaveBeenCalledWith("ses_1", "hello"));
  });

  it("keeps the text and shows the error when sending fails", async () => {
    mockSendMessage.mockRejectedValue(new Error("offline"));
    render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);

    fireEvent.change(textarea(), { target: { value: "keep me" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => expect(screen.getByTestId("pulse-focus-reply-error")).toHaveTextContent("offline"));
    expect(textarea().value).toBe("keep me");
    expect(loggedErrors()).toContain("ses_1");
  });

  it("does not send an empty reply", () => {
    render(<PulseFocusPane row={makeRow()} onClose={() => {}} />);

    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    fireEvent.keyDown(textarea(), { key: "Enter" });
    expect(mockSendMessage).not.toHaveBeenCalled();
  });

  it("is disabled while a turn is running, saying why", () => {
    render(<PulseFocusPane row={makeRow({ status: "running" })} onClose={() => {}} />);

    expect(textarea()).toBeDisabled();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    expect(screen.getByTestId("pulse-focus-disabled-reason")).toHaveTextContent("Turn in progress");
  });

  it("is disabled while an ask is pending, saying why", () => {
    render(
      <PulseFocusPane
        row={makeRow({ status: "needs_question", pending_ask: { kind: "question", summary: "q" } })}
        onClose={() => {}}
      />,
    );

    expect(textarea()).toBeDisabled();
    expect(screen.getByTestId("pulse-focus-disabled-reason")).toHaveTextContent(
      "Answer the pending request first",
    );
  });
});
