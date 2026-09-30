import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { clearQueue, getQueue, pushQueued } from "../../lib/tabQueue";
import { clearCompaction, setCompactionState } from "../../lib/compactionState";
import { clearDraft, getDraft } from "../../lib/tabDrafts";
import { ChatProvider } from "../../stores/chatStore";

const chat = vi.hoisted(() => ({ streaming: false, interrupted: false, permission: null as object | null }));
const sendMessage = vi.fn().mockResolvedValue(true);
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage,
    executeShell: vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 }),
    stop: vi.fn(),
    resume: vi.fn(),
    isStreaming: chat.streaming,
    wasInterrupted: chat.interrupted,
    pendingPermission: chat.permission,
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const TAB = "composer-restore-tab";

function composer() {
  return (
    <ChatProvider>
      <ChatInput sessionTabId={TAB} isActive />
    </ChatProvider>
  );
}

function composerValue() {
  return (screen.getByRole("textbox") as HTMLTextAreaElement).value;
}

describe("composer restores queued messages after a failed compaction", () => {
  beforeEach(() => {
    // Composer edits schedule a debounced draft save; fake timers keep that
    // callback from firing after the assertions (same reason the sibling
    // ChatInput.compaction suite fakes them).
    vi.useFakeTimers();
    vi.clearAllMocks();
    chat.streaming = false;
    chat.interrupted = false;
    chat.permission = null;
    clearQueue(TAB);
    clearDraft(TAB);
    clearCompaction(TAB);
  });

  afterEach(() => {
    vi.useRealTimers();
    clearQueue(TAB);
    clearDraft(TAB);
    clearCompaction(TAB);
  });

  it("moves queued messages into the composer ahead of the current draft", async () => {
    render(composer());
    pushQueued(TAB, { kind: "message", text: "first queued" });
    pushQueued(TAB, { kind: "message", text: "second queued" });
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "draft in progress" } });

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });

    expect(composerValue()).toBe("first queued\nsecond queued\ndraft in progress");
    // The draft map backs the composer's per-tab persistence, so it must agree
    // or a tab switch would resurrect the pre-restore text.
    expect(getDraft(TAB)).toBe("first queued\nsecond queued\ndraft in progress");
    expect(getQueue(TAB)).toEqual([]);
  });

  it("restores into an empty composer without adding a trailing separator", async () => {
    render(composer());
    pushQueued(TAB, { kind: "message", text: "only queued" });

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary cancelled" });
    });

    expect(composerValue()).toBe("only queued");
  });

  it("keeps a queued command queued rather than inlining it as prose", async () => {
    render(composer());
    pushQueued(TAB, { kind: "command", text: "/compact" });
    pushQueued(TAB, { kind: "message", text: "queued prose" });

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });

    expect(composerValue()).toBe("queued prose");
    expect(getQueue(TAB)).toEqual([{ kind: "command", text: "/compact" }]);
  });

  it("keeps the caret anchored to the user's own draft instead of jumping to the top", async () => {
    render(composer());
    pushQueued(TAB, { kind: "message", text: "queued text" });
    const box = screen.getByRole("textbox") as HTMLTextAreaElement;
    await act(async () => {
      fireEvent.change(box, { target: { value: "draft in progress" } });
    });
    // Caret mid-draft: after "draft " (6), i.e. just before "in progress".
    box.setSelectionRange(6, 6);

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });

    expect(box.value).toBe("queued text\ndraft in progress");
    // Shifted by exactly the inserted "queued text\n" (12) so the caret still
    // sits at the same point inside the user's own words.
    expect(box.selectionStart).toBe(6 + "queued text\n".length);
  });

  it("restores only once per failure, however often the error is re-published", async () => {
    render(composer());
    pushQueued(TAB, { kind: "message", text: "queued" });

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });
    expect(composerValue()).toBe("queued");

    // /state reconciliation re-publishes the same error on every poll; a second
    // merge would duplicate the text with nothing left in the queue to justify it.
    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });
    expect(composerValue()).toBe("queued");
  });

  it("leaves the composer alone when the failure carries no queued messages", async () => {
    render(composer());
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "untouched draft" } });

    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "compact: summary timed out" });
    });

    expect(composerValue()).toBe("untouched draft");
  });

  it("restores again for a NEW failure after a compaction succeeds", async () => {
    render(composer());
    pushQueued(TAB, { kind: "message", text: "first failure" });
    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "boom one" });
    });
    expect(composerValue()).toBe("first failure");

    await act(async () => {
      setCompactionState(TAB, { status: "active", startedAt: Date.now() });
    });
    await act(async () => {
      fireEvent.change(screen.getByRole("textbox"), { target: { value: "" } });
    });
    pushQueued(TAB, { kind: "message", text: "second failure" });
    await act(async () => {
      setCompactionState(TAB, { status: "error", error: "boom two" });
    });

    // The latch must reset on success, otherwise a later genuine failure would
    // be silently swallowed.
    expect(composerValue()).toBe("second failure");
  });
});
