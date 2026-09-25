import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import ChatInput from "./ChatInput";
import { RESTORE_EVENT, type RestoreDetail } from "../../lib/inputRestore";
import {
  __resetPendingRewindForTests,
  loadPendingRewind,
  savePendingRewind,
  PENDING_REWIND_STORAGE_PREFIX,
} from "../../lib/pendingRewindStore";

const mocks = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  prepareRewind: vi.fn(),
  getRewind: vi.fn(),
  cancelRewind: vi.fn(),
}));

const { prepareRewind, getRewind, cancelRewind } = mocks;

vi.mock("../../hooks/useChat", () => ({
  useChat: () => ({
    sendMessage: mocks.sendMessage,
    executeShell: vi.fn(),
    stop: vi.fn(),
    resume: vi.fn(),
    retryLastTurn: vi.fn(),
    wasInterrupted: false,
    turnError: false,
    isStreaming: false,
    pendingPermission: null,
    pendingQuestion: null,
    hasConversation: true,
    projectHost: "devbox",
  }),
}));

vi.mock("../../api/client", () => ({
  api: {
    prepareRewind: mocks.prepareRewind,
    getRewind: mocks.getRewind,
    cancelRewind: mocks.cancelRewind,
  },
  apiPath: (path: string) => path,
  authHeaders: () => ({}),
}));

function dispatchRestore(detail: RestoreDetail) {
  act(() => {
    window.dispatchEvent(new CustomEvent<RestoreDetail>(RESTORE_EVENT, { detail }));
  });
}

function textarea(): HTMLTextAreaElement {
  return screen.getByPlaceholderText(/Type a message/i) as HTMLTextAreaElement;
}

describe("ChatInput pending rewind", () => {
  beforeEach(() => {
    __resetPendingRewindForTests();
    window.localStorage.clear();
    prepareRewind.mockReset();
    getRewind.mockReset();
    cancelRewind.mockReset();
    prepareRewind.mockResolvedValue({
      token: "a".repeat(64),
      session_id: "ses-1",
      status: "armed",
      expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
      target_index: 4,
      committed_user_seq: 0,
    });
    cancelRewind.mockResolvedValue({ cancelled: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("arms a server resource and shows the pending banner without touching history", async () => {
    render(<ChatInput sessionTabId="ses-1" isActive />);

    dispatchRestore({ sessionId: "ses-1", text: "restore me", targetIndex: 4, userSeq: 2 });

    await waitFor(() => {
      expect(screen.getByText("Pending rewind")).toBeInTheDocument();
    });
    expect(prepareRewind).toHaveBeenCalledWith(
      "ses-1",
      { targetIndex: 4, targetContent: "restore me", userSeq: 2 },
      "devbox",
    );
    expect(textarea().value).toBe("restore me");
    expect(loadPendingRewind("devbox", "ses-1")?.draft).toBe("restore me");
  });

  it("Cancel restores the exact pre-Restore draft only after server success", async () => {
    render(<ChatInput sessionTabId="ses-1" isActive />);
    fireEvent.change(textarea(), { target: { value: "my prior draft" } });

    dispatchRestore({ sessionId: "ses-1", text: "restore me", targetIndex: 4, userSeq: 2 });
    await waitFor(() => expect(screen.getByText("Pending rewind")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Cancel restore" }));

    await waitFor(() => expect(textarea().value).toBe("my prior draft"));
    expect(cancelRewind).toHaveBeenCalledWith("ses-1", "a".repeat(64), "devbox");
    expect(loadPendingRewind("devbox", "ses-1")).toBeNull();
  });

  it("clears an armed banner when the server reports the rewind already committed", async () => {
    savePendingRewind({
      version: 1,
      host: "devbox",
      sessionId: "ses-1",
      token: "b".repeat(64),
      draft: "recovered draft",
      previousDraft: "",
      targetPreview: "recovered",
      expiresAt: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
    });
    getRewind.mockResolvedValue({ status: "committed" });

    render(<ChatInput sessionTabId="ses-1" isActive />);

    await waitFor(() => expect(loadPendingRewind("devbox", "ses-1")).toBeNull());
    expect(screen.queryByText("Pending rewind")).toBeNull();
    expect(textarea().value).toBe("recovered draft");
  });

  it("rolls back a server resource when the local record cannot be persisted", async () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementationOnce(() => {
      throw new Error("quota");
    });
    render(<ChatInput sessionTabId="ses-1" isActive />);

    dispatchRestore({ sessionId: "ses-1", text: "restore me", targetIndex: 4, userSeq: 2 });

    await waitFor(() => expect(cancelRewind).toHaveBeenCalledWith("ses-1", "a".repeat(64), "devbox"));
    expect(textarea().value).not.toBe("restore me");
    setItem.mockRestore();
  });

  it("does not arm a resource for a client-only draft tab", () => {
    render(<ChatInput sessionTabId="new-9" isActive />);
    dispatchRestore({ sessionId: "new-9", text: "restore me", targetIndex: 0 });
    expect(prepareRewind).not.toHaveBeenCalled();
    expect(window.localStorage.length).toBe(0);
    expect(
      Array.from({ length: window.localStorage.length }, (_, i) => window.localStorage.key(i)).some((key) =>
        key?.startsWith(PENDING_REWIND_STORAGE_PREFIX),
      ),
    ).toBe(false);
  });
});
