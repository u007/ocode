import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitWorkspace } from "@/api/types";

const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitShow: vi.fn(),
  gitRefAction: vi.fn(),
  gitStashList: vi.fn(),
  gitWorktrees: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: vi.fn(() => () => {}) },
}));

import GitPanel from "./GitPanel";

const VIEW_KEY = "ocode.ui.git-view.v1";

const workspace: GitWorkspace = {
  status: {
    branch: "main",
    staged_files: [],
    changed_files: [],
    conflicts: [],
    has_changes: false,
    is_repo: true,
    ahead: 0,
    behind: 0,
    has_upstream: false,
  },
  staged: [],
  unstaged: [],
};

beforeAll(() => {
  // jsdom ships no matchMedia; useIsMobile reads it on first render.
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

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getGitWorkspace.mockResolvedValue(workspace);
  mocks.gitLog.mockResolvedValue([]);
  mocks.gitShow.mockResolvedValue([]);
  mocks.gitRefAction.mockResolvedValue({ workspace, output: "" });
  mocks.gitStashList.mockResolvedValue([]);
  mocks.gitWorktrees.mockResolvedValue([]);
  window.localStorage.clear();
});

describe("GitPanel Changes / History sub-tabs", () => {
  it("shows the commit box in Changes and swaps it for the history panel in History", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByLabelText("Commit message");
    expect(screen.queryByTestId("git-history")).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: /History/ }));

    expect(await screen.findByTestId("git-history")).toBeTruthy();
    expect(screen.queryByLabelText("Commit message")).toBeNull();
    expect(screen.queryByTestId("git-body")).toBeNull();
    expect(screen.getByRole("tab", { name: /History/ }).getAttribute("aria-selected")).toBe("true");
    expect(window.localStorage.getItem(VIEW_KEY)).toBe("history");
    // Header stays: the branch name is still shown.
    expect(screen.getByText("main")).toBeTruthy();
  });

  it("switches back to Changes from the sub-tab", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByLabelText("Commit message");
    fireEvent.click(screen.getByRole("tab", { name: /History/ }));
    await screen.findByTestId("git-history");

    fireEvent.click(screen.getByRole("tab", { name: /Changes/ }));
    expect(await screen.findByLabelText("Commit message")).toBeTruthy();
    expect(screen.queryByTestId("git-history")).toBeNull();
    expect(window.localStorage.getItem(VIEW_KEY)).toBe("changes");
  });

  it("reopens the saved History view on the next mount", async () => {
    window.localStorage.setItem(VIEW_KEY, "history");
    render(<GitPanel projectPath="/proj" />);

    expect(await screen.findByTestId("git-history")).toBeTruthy();
    expect(screen.queryByLabelText("Commit message")).toBeNull();
    await waitFor(() => expect(mocks.gitLog).toHaveBeenCalled());
  });
});
