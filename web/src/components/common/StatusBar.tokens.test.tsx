import { render, screen } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import StatusBar from "./StatusBar";
import { STATUS_BAR_COLLAPSED_STORAGE_KEY } from "./statusBarCollapse";

// jsdom ships no matchMedia and StatusBar now reads the mobile breakpoint via
// useIsMobile; default to the desktop answer (matches: false).
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
  // resolveSessionHost (useSessionHost.ts) imports this directly, so the
  // real implementation runs against the stub state in these tests.
  findTabForSession: () => undefined,
  // StatusBar now resolves the session's host for the workspace chip; an
  // unknown path must resolve to "no host", not throw.
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

describe("StatusBar context segment", () => {
  beforeEach(() => {
    state.spendingUSD = 0;
    state.slice = { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false };
    window.localStorage.clear();
  });

  it("shows a compact colored ctx percentage in the collapsed row", () => {
    window.localStorage.setItem(STATUS_BAR_COLLAPSED_STORAGE_KEY, "true");
    state.slice.tuiStatus = { context_current_tokens: 12000, context_max_tokens: 200000 };
    render(<StatusBar />);
    const el = screen.getByText("ctx 6%");
    expect(el.className).toContain("text-emerald-500");
    expect(screen.getByTitle("Context: 12000 / 200000 tokens (6% of window)")).toBeTruthy();
  });

  it("colors the collapsed row's compact ctx percentage red from 85%", () => {
    window.localStorage.setItem(STATUS_BAR_COLLAPSED_STORAGE_KEY, "true");
    state.slice.tuiStatus = { context_current_tokens: 85000, context_max_tokens: 100000 };
    render(<StatusBar />);
    const el = screen.getByText("ctx 85%");
    expect(el.className).toContain("text-red-500");
  });

  it("omits the compact ctx percentage in the collapsed row when usage is unknown", () => {
    window.localStorage.setItem(STATUS_BAR_COLLAPSED_STORAGE_KEY, "true");
    state.slice.tuiStatus = { context_current_tokens: 0, context_max_tokens: 200000 };
    render(<StatusBar />);
    expect(screen.queryByText(/ctx \d+%/)).toBeNull();
  });

  it("shows the context percentage colored green under 65%", () => {
    state.slice.tuiStatus = { context_current_tokens: 12000, context_max_tokens: 200000 };
    render(<StatusBar />);
    const el = screen.getByText(/ctx: 12k\/200k \(6%\)/);
    expect(el.className).toContain("text-emerald-500");
    expect(screen.getByTitle("Context: 12000 / 200000 tokens (6% of window)")).toBeTruthy();
  });

  it("colors the context percentage yellow from 65%", () => {
    state.slice.tuiStatus = { context_current_tokens: 65000, context_max_tokens: 100000 };
    render(<StatusBar />);
    const el = screen.getByText(/ctx: 65k\/100k \(65%\)/);
    expect(el.className).toContain("text-yellow-500");
  });

  it("colors the context percentage red from 85%", () => {
    state.slice.tuiStatus = { context_current_tokens: 85000, context_max_tokens: 100000 };
    render(<StatusBar />);
    const el = screen.getByText(/ctx: 85k\/100k \(85%\)/);
    expect(el.className).toContain("text-red-500");
  });

  it("clamps the percentage to 100% when usage exceeds the window", () => {
    state.slice.tuiStatus = { context_current_tokens: 250000, context_max_tokens: 200000 };
    render(<StatusBar />);
    const el = screen.getByText(/ctx: 250k\/200k \(100%\)/);
    expect(el.className).toContain("text-red-500");
    expect(el.textContent).not.toContain("125%");
  });

  it("omits the percentage and stays muted when current usage is unknown", () => {
    state.slice.tuiStatus = { context_current_tokens: 0, context_max_tokens: 200000 };
    render(<StatusBar />);
    const el = screen.getByText("ctx: ?/200k");
    expect(el.className).toContain("text-muted-foreground");
    expect(el.textContent).not.toContain("%");
  });

  it("never prints \"null%\" in the tooltip when the window size is unknown", () => {
    state.slice.tuiStatus = { context_current_tokens: 12000, context_max_tokens: 0 };
    const { container } = render(<StatusBar />);
    const titles = Array.from(container.querySelectorAll("[title]")).map((el) => el.getAttribute("title") ?? "");
    const ctxTitle = titles.find((t) => t.startsWith("Context:"));
    expect(ctxTitle).toBe("Context: 12000 / unknown tokens");
    expect(titles.join(" ")).not.toContain("null");
  });
});
