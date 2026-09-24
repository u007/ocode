import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import StatusBar from "./StatusBar";
import { STATUS_BAR_COLLAPSED_STORAGE_KEY } from "./statusBarCollapse";

// StatusBar reads two stores and the speech provider; stub all three so the
// test drives only the collapse behavior.
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

const KEY = STATUS_BAR_COLLAPSED_STORAGE_KEY;

describe("StatusBar collapse", () => {
  beforeEach(() => {
    window.localStorage.removeItem(KEY);
    state.spendingUSD = 0;
    state.slice = { isStreaming: false, error: null, live: [], tuiStatus: null, turnActive: false };
  });

  it("renders expanded with both rows and the collapse toggle", () => {
    const { container } = render(<StatusBar />);

    // the "ocode web" label lives in the expanded action row only
    expect(screen.getByText("ocode web")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Collapse status bar" })).toBeInTheDocument();
    // row 2 (session/cwd/context details) is present
    expect(container.querySelectorAll("div > div").length).toBeGreaterThan(1);
  });

  it("keeps the collapse/expand toggle at the right edge in both states", () => {
    const { container } = render(<StatusBar />);

    // Expanded: the collapse chevron is the last control in the right cluster.
    const collapseBtn = screen.getByRole("button", { name: "Collapse status bar" });
    expect(collapseBtn.parentElement?.lastElementChild).toBe(collapseBtn);
    // The brand label now sits to its left, not to its right.
    expect(collapseBtn.previousElementSibling).toHaveTextContent("ocode web");

    fireEvent.click(collapseBtn);

    // Collapsed: the expand chevron is pushed to the far right of the slim row.
    const expandBtn = screen.getByRole("button", { name: "Expand status bar" });
    expect(expandBtn.parentElement?.lastElementChild).toBe(expandBtn);
    expect(expandBtn.className).toContain("ml-auto");
    // Regression: the collapse preference must survive the layout move.
    expect(window.localStorage.getItem(KEY)).toBe("true");
    // Still a single slim inner row.
    const root = container.firstChild as HTMLElement;
    expect(root.querySelectorAll(":scope > div")).toHaveLength(1);
  });

  it("collapses to a slim row that keeps the working indicator and hides the details", () => {
    state.slice = {
      isStreaming: true,
      error: null,
      live: [],
      tuiStatus: { llm_running: true },
      turnActive: false,
    };
    const { container } = render(<StatusBar />);

    fireEvent.click(screen.getByRole("button", { name: "Collapse status bar" }));

    // details gone, expand toggle present, preference persisted
    expect(screen.queryByText("ocode web")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand status bar" })).toBeInTheDocument();
    expect(window.localStorage.getItem(KEY)).toBe("true");

    // the single synchronized working signal survives collapsing
    expect(screen.getByTitle("Agent running status")).toHaveTextContent("⟳ llm");
    // collapsed bar is one slim row (root + a single inner group), slimmer
    // than the expanded py-1.5 two-row container
    const root = container.firstChild as HTMLElement;
    expect(root.className).toContain("py-1");
    expect(root.className).not.toContain("py-1.5");
    expect(root.querySelectorAll(":scope > div")).toHaveLength(1);
  });

  it("keeps the error visible while collapsed", () => {
    state.slice = {
      isStreaming: false,
      error: "provider exploded",
      live: [],
      tuiStatus: null,
      turnActive: false,
    };
    render(<StatusBar />);

    fireEvent.click(screen.getByRole("button", { name: "Collapse status bar" }));

    expect(screen.getByText("provider exploded")).toBeInTheDocument();
  });

  it("starts collapsed when the stored preference says so", () => {
    window.localStorage.setItem(KEY, "true");
    render(<StatusBar />);

    expect(screen.queryByText("ocode web")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand status bar" })).toBeInTheDocument();
  });

  it("re-expands on toggle and persists false", () => {
    window.localStorage.setItem(KEY, "true");
    render(<StatusBar />);

    fireEvent.click(screen.getByRole("button", { name: "Expand status bar" }));

    expect(screen.getByText("ocode web")).toBeInTheDocument();
    expect(window.localStorage.getItem(KEY)).toBe("false");
  });

  it("follows a cross-window storage event", () => {
    render(<StatusBar />);
    expect(screen.getByText("ocode web")).toBeInTheDocument();

    // storage events land outside React's event system — flush inside act
    act(() => {
      window.dispatchEvent(new StorageEvent("storage", { key: KEY, newValue: "true" }));
    });

    expect(screen.queryByText("ocode web")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Expand status bar" })).toBeInTheDocument();
  });
});
