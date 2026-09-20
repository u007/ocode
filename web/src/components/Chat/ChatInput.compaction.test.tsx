import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { dispatchCommand, type CommandContext } from "./commands";
import { clearQueue, dispatchQueueChanged, getQueue } from "../../lib/tabQueue";
import { clearCompaction, getCompactionState, setCompactionState } from "../../lib/compactionState";
import { clearDraft } from "../../lib/tabDrafts";
import { ChatProvider, useChatDispatch, type ChatAction } from "../../stores/chatStore";
import type { Dispatch } from "react";

let transcriptDispatch: Dispatch<ChatAction>;
function TranscriptHarness() {
  transcriptDispatch = useChatDispatch();
  return composer();
}

const chat = vi.hoisted(() => ({ streaming: false, interrupted: false, permission: null as object | null }));
const sendMessage = vi.fn().mockResolvedValue(true);
const executeShell = vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 });
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({ sendMessage, executeShell, stop: vi.fn(), resume: vi.fn(), isStreaming: chat.streaming, wasInterrupted: chat.interrupted, pendingPermission: chat.permission }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const compactSession = vi.fn<CommandContext["api"]["compactSession"]>();
const onSlashCommand = vi.fn((text: string, id?: string | null) => dispatchCommand(text, {
  commandName: "compact", args: "", api: { compactSession } as unknown as CommandContext["api"],
  getSessionId: () => id ?? null, host: "user@remote",
}));
const A = "compact-test-a";
const B = "compact-test-b";

function deferred() {
  let resolve!: (value: { original_len: number; compacted_len: number }) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<{ original_len: number; compacted_len: number }>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function submit(text: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
  await act(async () => { fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" }); });
}

function composer(id = A) {
  // The real hook subscribes to chat context; the stub needs a changed prop to
  // get through ChatInput's memo boundary when its simulated state changes.
  return <ChatInput sessionTabId={id} onSlashCommand={onSlashCommand} isActive={!chat.streaming && !chat.interrupted && !chat.permission} />;
}

describe("composer compaction lifecycle", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    compactSession.mockResolvedValue({ original_len: 24, compacted_len: 6 });
    for (const id of [A, B]) {
      clearQueue(id);
      clearDraft(id);
      clearCompaction(id);
    }
  });
  afterEach(() => { vi.useRealTimers(); });

  it.each(["streaming", "permission", "interrupted"])("queues /compact while %s, then runs it only when idle, never sending it to the model", async (barrier) => {
    if (barrier === "streaming") chat.streaming = true;
    if (barrier === "permission") chat.permission = {};
    if (barrier === "interrupted") chat.interrupted = true;
    const view = render(composer());
    await submit("/compact ");
    expect(screen.getByText(/Compaction queued/)).toBeInTheDocument();
    expect(compactSession).not.toHaveBeenCalled();
    expect(onSlashCommand).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "command", text: "/compact" }]);
    chat.streaming = false;
    chat.permission = null;
    chat.interrupted = false;
    await act(async () => { view.rerender(composer()); });
    expect(compactSession).toHaveBeenCalledExactlyOnceWith(A, "user@remote", undefined);
    expect(sendMessage).not.toHaveBeenCalled();
    // Completion feedback now lives in the transcript (the persisted
    // compaction-summary notice), so the composer bottom bar is dropped.
    expect(screen.queryByText(/Compacted:/)).not.toBeInTheDocument();
    expect(getCompactionState(A)).toBeUndefined();
    expect(screen.queryByText(/Compaction queued/)).not.toBeInTheDocument();
  });

  it("recalling /compact clears queued feedback and prevents execution", async () => {
    chat.streaming = true;
    const view = render(composer());
    await submit("/compact ");
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "ArrowUp" });
    expect(screen.getByRole("textbox")).toHaveValue("/compact");
    expect(screen.queryByText(/Compaction queued/)).not.toBeInTheDocument();
    expect(getQueue(A)).toEqual([]);
    chat.streaming = false;
    await act(async () => { view.rerender(composer()); });
    expect(compactSession).not.toHaveBeenCalled();
  });

  it("external queue removal clears queued feedback", async () => {
    chat.streaming = true;
    render(composer());
    await submit("/compact ");
    act(() => { clearQueue(A); dispatchQueueChanged(A); });
    expect(screen.queryByText(/Compaction queued/)).not.toBeInTheDocument();
  });

  it("keeps active spinner and elapsed past 8s, survives remounts, and isolates tabs", async () => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    const view = render(composer());
    await submit("/compact ");
    act(() => { vi.advanceTimersByTime(12000); });
    expect(screen.getByText(/12s elapsed/)).toBeInTheDocument();
    expect(screen.getByRole("status").querySelector(".animate-spin")).not.toBeNull();
    expect(screen.queryByLabelText("Dismiss compaction status")).not.toBeInTheDocument();
    view.rerender(composer(B));
    expect(screen.queryByText(/Compacting conversation/)).not.toBeInTheDocument();
    view.unmount();
    render(composer());
    expect(screen.getByText(/12s elapsed/)).toBeInTheDocument();
    await act(async () => { request.resolve({ original_len: 42, compacted_len: 7 }); });
    expect(screen.queryByText(/Compacted:/)).not.toBeInTheDocument();
    expect(getCompactionState(A)).toBeUndefined();
    // The composer bar is not retained after completion — no lingering notice
    // and no dismiss button (previously it stayed until clicked).
    act(() => { vi.advanceTimersByTime(60000); });
    expect(screen.queryByText(/Compacted:/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Dismiss compaction status")).not.toBeInTheDocument();
    expect(getCompactionState(B)).toBeUndefined();
  });

  it("drops the composer bar after completion even if the transcript is replaced", async () => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    render(<ChatProvider><TranscriptHarness /></ChatProvider>);
    await submit("/compact ");
    act(() => { transcriptDispatch({ type: "SET_MESSAGES", sessionId: A, messages: [] }); });
    expect(screen.getAllByText(/Compacting conversation/)).toHaveLength(1);
    await act(async () => { request.resolve({ original_len: 80, compacted_len: 5 }); });
    act(() => { transcriptDispatch({ type: "SET_MESSAGES", sessionId: A, messages: [{ role: "system", content: "[ocode:compaction-summary]\nCompacted summary covering 80 messages\n\nbody" }] }); });
    expect(screen.queryByText(/Compacted:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Compacting conversation/)).not.toBeInTheDocument();
    expect(getCompactionState(A)).toBeUndefined();
  });

  it.each([false, true])("does not block a different tab while compaction is active (started via queue: %s)", async (queued) => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    chat.streaming = queued;
    const view = render(composer());
    await submit("/compact ");
    if (queued) {
      chat.streaming = false;
      await act(async () => { view.rerender(composer()); });
    }
    view.rerender(composer(B));
    await submit("other session message");
    await act(async () => { vi.advanceTimersByTime(1500); });
    expect(sendMessage).toHaveBeenCalledExactlyOnceWith("other session message");
    expect(getCompactionState(A)?.status).toBe("active");
    await act(async () => { request.resolve({ original_len: 20, compacted_len: 4 }); });
  });

  it("runs queued work after a failed compaction without dismissing the error", async () => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    render(composer());
    await submit("/compact ");
    await submit("!echo ok");
    expect(executeShell).not.toHaveBeenCalled();
    await act(async () => { request.reject(new Error("failed request")); });
    expect(executeShell).toHaveBeenCalledExactlyOnceWith("echo ok");
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("alert")).toHaveTextContent("failed request");
  });

  it("holds an existing debounce if compaction starts before its timer fires", async () => {
    render(composer());
    await submit("delayed message");
    act(() => { setCompactionState(A, { status: "active", startedAt: Date.now() }); });
    await act(async () => { vi.advanceTimersByTime(1500); });
    expect(sendMessage).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "message", text: "delayed message" }]);
    await act(async () => { clearCompaction(A); });
    expect(sendMessage).toHaveBeenCalledExactlyOnceWith("delayed message");
  });

  it("retains rejected API errors until a new attempt or dismissal, including completion off-tab", async () => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    const view = render(composer());
    await submit("/compact ");
    view.rerender(composer(B));
    await act(async () => { request.reject(new Error("503 unavailable")); });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    view.rerender(composer());
    expect(screen.getByRole("alert")).toHaveTextContent("Compaction failed: 503 unavailable");
    act(() => { vi.advanceTimersByTime(120000); });
    expect(screen.getByRole("alert")).toBeInTheDocument();
    const retry = deferred();
    compactSession.mockReturnValueOnce(retry.promise);
    await submit("/compact ");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByText(/Compacting conversation/)).toBeInTheDocument();
    await act(async () => { retry.reject(new Error("retry failed")); });
    fireEvent.click(screen.getByLabelText("Dismiss compaction status"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it.each([false, true])("serializes subsequent commands and messages during compaction (started via queue: %s)", async (queued) => {
    const request = deferred();
    compactSession.mockReturnValueOnce(request.promise);
    chat.streaming = queued;
    const view = render(composer());
    await submit("/compact ");
    if (queued) {
      chat.streaming = false;
      await act(async () => { view.rerender(composer()); });
    }
    await submit("/compact ");
    await submit("after compact");
    expect(compactSession).toHaveBeenCalledTimes(1);
    expect(sendMessage).not.toHaveBeenCalled();
    expect(getQueue(A)).toEqual([{ kind: "command", text: "/compact" }, { kind: "message", text: "after compact" }]);
    await act(async () => { request.resolve({ original_len: 20, compacted_len: 4 }); });
    expect(compactSession).toHaveBeenCalledTimes(2);
    expect(sendMessage).toHaveBeenCalledExactlyOnceWith("after compact");
    expect(getQueue(A)).toEqual([]);
  });

  it("queues behind debounced input rather than claiming compaction is active", async () => {
    const view = render(composer());
    await submit("first message");
    await submit("/compact ");
    expect(sendMessage).toHaveBeenCalledExactlyOnceWith("first message");
    expect(screen.getByText(/Compaction queued/)).toBeInTheDocument();
    expect(compactSession).not.toHaveBeenCalled();
    chat.streaming = true;
    view.rerender(composer());
    chat.streaming = false;
    await act(async () => { view.rerender(composer()); });
    expect(compactSession).toHaveBeenCalledTimes(1);
  });
});
