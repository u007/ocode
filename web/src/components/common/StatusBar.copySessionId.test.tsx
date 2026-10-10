import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import StatusBar from "./StatusBar";

const copyTextToClipboard = vi.hoisted(() => vi.fn(async (_text: string) => true));
vi.mock("../../lib/clipboard", () => ({ copyTextToClipboard }));

// StatusBar reads two stores and the speech provider; stub all three so the
// test drives only the session-id segment (mirrors the tokens suite).
const state: {
  slice: {
    isStreaming: boolean;
    error: string | null;
    live: unknown[];
    tuiStatus: Record<string, unknown> | null;
    turnActive: boolean;
    lastDispatchedModel: string | null;
  };
} = {
  slice: { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false, lastDispatchedModel: null },
};

vi.mock("../../stores/projectStore", () => ({
  findTabForSession: () => undefined,
  useProjectState: () => ({ activeTabId: "session-1" }),
}));
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: unknown) => unknown) => sel(state),
  getSessionSlice: (s: typeof state) => s.slice,
}));
vi.mock("../../components/Speech/SpeechProvider", () => ({
  useSpeech: () => ({ toolbarVisible: false, toggleToolbar: vi.fn() }),
}));

describe("StatusBar — copy session ID", () => {
  beforeEach(() => {
    copyTextToClipboard.mockClear();
    copyTextToClipboard.mockResolvedValue(true);
    state.slice = { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false, lastDispatchedModel: null };
  });

  it("copies the session id shown in the bar", async () => {
    state.slice.tuiStatus = { session_id: "ses_2026-09-30-abc123" };
    render(<StatusBar />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("statusbar-copy-session-id"));
    });
    expect(copyTextToClipboard).toHaveBeenCalledWith("ses_2026-09-30-abc123");
  });

  // The id must stay readable as text — the copy button is an addition to the
  // existing display, not a replacement for it.
  it("still renders the session id as visible text", () => {
    state.slice.tuiStatus = { session_id: "ses_2026-09-30-abc123" };
    render(<StatusBar />);
    expect(screen.getByText("ses_2026-09-30-abc123")).toBeTruthy();
  });

  it("shows no copy button when the session has no id yet", () => {
    state.slice.tuiStatus = null;
    render(<StatusBar />);
    expect(screen.queryByTestId("statusbar-copy-session-id")).toBeNull();
  });

  it("confirms the copy in place", async () => {
    state.slice.tuiStatus = { session_id: "ses_1" };
    render(<StatusBar />);
    await act(async () => {
      fireEvent.click(screen.getByTestId("statusbar-copy-session-id"));
    });
    expect(screen.getByTestId("statusbar-copy-session-id").dataset.state).toBe("copied");
  });
});
