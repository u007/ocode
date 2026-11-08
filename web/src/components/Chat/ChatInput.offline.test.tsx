import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { CHAT_INPUT_DEBOUNCE_MS } from "../../lib/chatInputBatch";
import { clearQueue } from "../../lib/tabQueue";
import { clearDraft } from "../../lib/tabDrafts";
import { clearCompaction } from "../../lib/compactionState";

const chat = vi.hoisted(() => ({
  sendMessage: vi.fn().mockResolvedValue(true),
}));
vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: chat.sendMessage,
    executeShell: vi.fn().mockResolvedValue({ output: "ok", exitCode: 0 }),
    stop: vi.fn(),
    resume: vi.fn(),
    retryLastTurn: vi.fn(),
    isStreaming: false,
    wasInterrupted: false,
    pendingPermission: null,
    hasConversation: true,
    transcriptScrolledUp: false,
    recentInputs: [],
  }),
}));
vi.mock("./SlashCommandMenu", () => ({ default: () => null }));

const TAB = "offline-gate-tab";
const onSlashCommand = vi.fn(() => ({ handled: true, accepted: true, startedTurn: false }));

function composer(isOffline: boolean) {
  return <ChatInput sessionTabId={TAB} onSlashCommand={onSlashCommand} isActive isOffline={isOffline} />;
}

describe("composer offline send gate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearQueue(TAB);
    clearDraft(TAB);
    clearCompaction(TAB);
  });
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("refuses Enter while offline and keeps the typed text", async () => {
    render(composer(true));
    const ta = screen.getByRole("textbox") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "written offline" } });

    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });
    // A plain message is debounced before it is sent, so wait past the debounce:
    // an ungated submit would reach sendMessage inside this window.
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, CHAT_INPUT_DEBOUNCE_MS + 200));
    });

    expect(chat.sendMessage).not.toHaveBeenCalled();
    expect(ta.value).toBe("written offline");
  }, CHAT_INPUT_DEBOUNCE_MS + 5000);

  it("disables the Send button while offline", () => {
    render(composer(true));
    const ta = screen.getByRole("textbox") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "written offline" } });
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("still sends on Enter when online", async () => {
    render(composer(false));
    const ta = screen.getByRole("textbox") as HTMLTextAreaElement;
    fireEvent.change(ta, { target: { value: "hello online" } });

    await act(async () => {
      fireEvent.keyDown(ta, { key: "Enter" });
    });

    await waitFor(
      () => expect(chat.sendMessage).toHaveBeenCalledWith("hello online"),
      { timeout: CHAT_INPUT_DEBOUNCE_MS + 2000 },
    );
  }, CHAT_INPUT_DEBOUNCE_MS + 5000);
});
