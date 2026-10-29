import { render, screen } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import StatusBar from "./StatusBar";

// jsdom ships no matchMedia; StatusBar reads the mobile breakpoint.
beforeAll(() => {
  window.matchMedia = ((media: string) => ({
    matches: false,
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

// `useSessionWorkspace` is the async store; the workspace SUITE covers its
// fetch/poll/refresh behaviour. Here it is a controllable snapshot so this
// file pins the StatusBar integration: which checkout is named, and — just as
// importantly — that no chip appears when the checkout is unknown.
const ws = vi.hoisted(() => ({ worktrees: null as unknown }));

vi.mock("../../lib/sessionWorkspace", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../lib/sessionWorkspace")>();
  return {
    ...actual,
    useSessionWorkspace: () => ({ worktrees: ws.worktrees, state: "ready" as const }),
  };
});

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
  findTabForSession: () => undefined,
  findProjectPathForTab: () => undefined,
  useProjectState: () => ({ state: {}, activeTabId: "session-1" }),
}));
vi.mock("../../stores/chatStore", () => ({
  useChatSelector: (sel: (s: unknown) => unknown) => sel(state),
  getSessionSlice: (s: typeof state) => s.slice,
}));
vi.mock("../../components/Speech/SpeechProvider", () => ({
  useSpeech: () => ({ toolbarVisible: false, toggleToolbar: vi.fn() }),
}));

const MAIN = {
  path: "/repo",
  head: "1111111aaaa",
  branch: "main",
  detached: false,
  locked: false,
  prunable: false,
  is_main: true,
  is_current: false,
  exists: true,
  dirty: 0,
  ahead: -1,
  behind: -1,
  contained_in_base: null,
  base_ref: "main",
  base_known: true,
  owner_sessions: [],
  owner_session_count: 2,
  last_commit: "2026-10-28T12:00:00+08:00",
};

const LINKED = {
  path: "/repo/.worktrees/feat-x",
  head: "2222222bbbb",
  branch: "feat/x",
  detached: false,
  locked: false,
  prunable: false,
  is_main: false,
  is_current: true,
  exists: true,
  dirty: 0,
  ahead: 2,
  behind: 0,
  contained_in_base: false,
  base_ref: "main",
  base_known: true,
  owner_sessions: [{ session_id: "ses_1" }],
  owner_session_count: 1,
  last_commit: "2026-10-28T13:00:00+08:00",
};

describe("StatusBar workspace chip", () => {
  beforeEach(() => {
    ws.worktrees = [MAIN, LINKED];
    state.spendingUSD = 0;
    state.slice = { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false };
  });

  it("names the main checkout when the session is rooted there", () => {
    state.slice.tuiStatus = { cwd: "/repo" };
    render(<StatusBar />);
    const chip = screen.getByTestId("statusbar-workspace");
    expect(chip.textContent).toBe("main");
    expect(chip.getAttribute("title")).toContain("main checkout");
  });

  it("names the linked worktree and its divergence", () => {
    state.slice.tuiStatus = { cwd: "/repo/.worktrees/feat-x" };
    render(<StatusBar />);
    const chip = screen.getByTestId("statusbar-workspace");
    expect(chip.textContent).toBe("wt: feat/x ↑2");
    expect(chip.getAttribute("title")).toContain("2 ahead, 0 behind main");
  });

  it("renders no chip when the checkout list is unknown", () => {
    ws.worktrees = null;
    state.slice.tuiStatus = { cwd: "/repo/.worktrees/feat-x" };
    render(<StatusBar />);
    expect(screen.queryByTestId("statusbar-workspace")).toBeNull();
  });

  it("renders no chip when the cwd is not a listed checkout", () => {
    state.slice.tuiStatus = { cwd: "/somewhere/else" };
    render(<StatusBar />);
    expect(screen.queryByTestId("statusbar-workspace")).toBeNull();
  });
});
