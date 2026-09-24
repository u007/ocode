import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import StatusBar from "./StatusBar";

// StatusBar reads two stores and the speech provider; stub all three so the
// test drives only the per-session token segment (mirrors the collapse suite).
const state: {
  spendingUSD: number;
  slice: {
    isStreaming: boolean;
    error: string | null;
    live: unknown[];
    tuiStatus: Record<string, unknown> | null;
    turnActive: boolean;
  };
} = {
  spendingUSD: 0,
  slice: { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false },
};

vi.mock("../../stores/projectStore", () => ({
  useProjectState: () => ({ activeTabId: "session-1" }),
}));
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: unknown) => unknown) => sel(state),
  getSessionSlice: (s: typeof state) => s.slice,
}));
vi.mock("../../components/Speech/SpeechProvider", () => ({
  useSpeech: () => ({ toolbarVisible: false, toggleToolbar: vi.fn() }),
}));

describe("StatusBar per-session token segment", () => {
  beforeEach(() => {
    state.spendingUSD = 0;
    state.slice = { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false };
  });

  it("shows in/cache/out token counts when the snapshot provides them", () => {
    state.slice.tuiStatus = {
      input_tokens: 1000,
      cached_tokens: 300,
      output_tokens: 200,
      total_tokens: 1500,
    };
    render(<StatusBar />);
    expect(screen.getByText(/in 1\.0k · cache 300 · out 200/)).toBeTruthy();
  });

  it("hides the token segment when no counts are present", () => {
    state.slice.tuiStatus = { context_current_tokens: 1000, context_max_tokens: 200000 };
    render(<StatusBar />);
    expect(screen.queryByText(/in \d/)).toBeNull();
    expect(screen.queryByText(/cache /)).toBeNull();
  });
});
