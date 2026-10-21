import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearQueue, getQueue, pushQueued } from "../../lib/tabQueue";

// The composer after a user Stop: `wasInterrupted` is true, and the user's
// next message must RESUME the conversation (be sent), not sit in the queue
// forever waiting for an explicit "Resume" click.
//
// This is the web/desktop half of the auto-resume contract. The server half
// (a message dispatched after a cancelled turn actually runs) is pinned by
// internal/server/handler_auto_resume_test.go.
const chat = vi.hoisted(() => ({
  streaming: false,
  interrupted: false,
  permission: null as object | null,
  turnError: false,
}));
const sendMessage = vi.fn().mockResolvedValue(true);
const resume = vi.fn();

vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell: vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 }),
    stop: vi.fn(),
    resume,
    retryLastTurn: vi.fn().mockResolvedValue(true),
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    turnError: chat.turnError,
    pendingPermission: chat.permission,
    hasConversation: true,
  }),
}));

vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const TAB = "auto-resume-tab";

function composer() {
  // The real hook subscribes to chat context; the stub needs a changed prop to
  // get through ChatInput's memo boundary when its simulated state changes.
  return <ChatInput sessionTabId={TAB} isActive={!chat.streaming && !chat.interrupted && !chat.permission} />;
}

async function submit(text: string) {
  fireEvent.change(screen.getByRole("textbox"), { target: { value: text } });
  await act(async () => {
    fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter" });
  });
}

describe("composer auto-resume after a user Stop", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    chat.turnError = false;
    sendMessage.mockResolvedValue(true);
    clearQueue(TAB);
  });
  afterEach(() => {
    clearQueue(TAB);
  });

  // `resume()` in the real hook clears `wasInterrupted` in the store; the stub
  // mirrors that so the drain effect sees the barrier release on re-render.
  function releaseBarrierOnResume() {
    resume.mockImplementation(() => {
      chat.interrupted = false;
    });
  }

  it("sends a message submitted after a Stop instead of queueing it forever", async () => {
    chat.interrupted = true;
    releaseBarrierOnResume();
    const view = render(composer());

    await submit("continue please");
    // The interrupt barrier must be released, otherwise the composer stays
    // blocked and the conversation never continues.
    expect(resume).toHaveBeenCalled();
    await act(async () => {
      view.rerender(composer());
    });

    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(sendMessage).toHaveBeenCalledWith("continue please");
    // Nothing is left sitting in the queue.
    expect(getQueue(TAB)).toEqual([]);
  });

  it("keeps the message queued when the resumed send is rejected", async () => {
    chat.interrupted = true;
    releaseBarrierOnResume();
    sendMessage.mockResolvedValue(false);
    const view = render(composer());

    await submit("continue please");
    await act(async () => {
      view.rerender(composer());
    });

    expect(sendMessage).toHaveBeenCalledTimes(1);
    // The message must not be silently dropped: the drain puts it back at the
    // head of the queue so the user can retry it.
    expect(getQueue(TAB)).toEqual([{ kind: "message", text: "continue please" }]);
  });

  it("sends a message queued before the Stop first, then the new one, each once", async () => {
    // A message queued while a turn was running, then the user stops, then
    // types a new message instead of pressing Resume. The new message goes
    // BEHIND the queued one and nothing is sent twice: the submit path used to
    // call sendMessage itself while resume() also triggered the drain, so both
    // reached the server at once.
    chat.interrupted = true;
    releaseBarrierOnResume();
    pushQueued(TAB, { kind: "message", text: "first" });
    const view = render(composer());

    await submit("second");
    await act(async () => {
      view.rerender(composer());
    });

    // The drain stops after a message starts a turn; "second" waits its turn.
    expect(sendMessage.mock.calls.map((c) => c[0])).toEqual(["first"]);
    expect(getQueue(TAB)).toEqual([{ kind: "message", text: "second" }]);

    // The first turn runs and ends: the next drain sends "second".
    chat.streaming = true;
    await act(async () => {
      view.rerender(composer());
    });
    chat.streaming = false;
    await act(async () => {
      view.rerender(composer());
    });

    expect(sendMessage.mock.calls.map((c) => c[0])).toEqual(["first", "second"]);
    expect(getQueue(TAB)).toEqual([]);
  });

  it("still queues a message while a permission ask is pending", async () => {
    // The permission barrier is NOT the interrupt barrier: a pending ask must
    // keep the message queued so it is not sent on top of an unresolved ask.
    chat.permission = {};
    render(composer());

    await submit("hello");

    expect(sendMessage).not.toHaveBeenCalled();
    expect(getQueue(TAB)).toEqual([{ kind: "message", text: "hello" }]);
  });

  it("still queues a message while a turn is streaming", async () => {
    chat.streaming = true;
    render(composer());

    await submit("hello");

    // A streaming turn injects the message into the live loop (TUI parity),
    // so it IS sent — but it is also kept flagged `dispatched` for recall.
    expect(sendMessage).toHaveBeenCalledTimes(1);
    expect(getQueue(TAB)).toEqual([{ kind: "message", text: "hello", dispatched: true }]);
  });
});
