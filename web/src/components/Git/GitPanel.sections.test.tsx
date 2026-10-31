import { fireEvent, render, screen } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitStash, GitWorktree, GitWorkspace } from "@/api/types";

// jsdom has no layout engine, so these tests pin the CSS CONTRACT that makes
// an expanded list use the height it needs instead of a rigid flex share.
// Geometry was verified in a real browser at a phone viewport (see
// docs/concepts/web-git-panel-sections.md).
const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitStashList: vi.fn(),
  gitStashShow: vi.fn(),
  gitWorktrees: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: vi.fn(() => () => {}) },
}));

import GitPanel from "./GitPanel";

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

const stashes: GitStash[] = [
  {
    index: 0,
    ref: "stash@{0}",
    hash: "1111111",
    short: "1111111",
    message: "WIP on main: autostash",
    author: "dev",
    date: "2026-10-30T00:00:00Z",
  },
  {
    index: 1,
    ref: "stash@{1}",
    hash: "2222222",
    short: "2222222",
    message: "WIP on main: second",
    author: "dev",
    date: "2026-10-29T00:00:00Z",
  },
];

const worktrees: GitWorktree[] = [
  {
    path: "/proj",
    head: "aaaaaaaa",
    branch: "main",
    detached: false,
    locked: false,
    prunable: false,
    is_main: true,
    is_current: true,
  },
  {
    path: "/proj/.worktrees/feature",
    head: "bbbbbbbb",
    branch: "feature",
    detached: false,
    locked: false,
    prunable: false,
    is_main: false,
    is_current: false,
  },
];

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
  mocks.gitStashList.mockResolvedValue(stashes);
  mocks.gitStashShow.mockResolvedValue([]);
  mocks.gitWorktrees.mockResolvedValue(worktrees);
  window.localStorage.clear();
});

/** Class tokens of an element. */
function tokens(el: Element | null): string[] {
  if (!el) throw new Error("element not found");
  return el.className.split(/\s+/);
}

/**
 * Section root element for a section whose header toggle carries `title`.
 * Commits renders its header AS the toggle button, so its root is one level
 * up; worktrees/stashes wrap the toggle in a header row, so theirs is two.
 */
function sectionRoot(toggleTitle: string, depth: 1 | 2): HTMLElement {
  let el: HTMLElement | null = screen.getByTitle(toggleTitle);
  for (let i = 0; i < depth; i += 1) el = el?.parentElement ?? null;
  if (!el) throw new Error("section root not found");
  return el;
}

describe("GitPanel section heights", () => {
  it("sizes an expanded list by its content instead of an equal flex share", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("stash@{0}");

    const commits = tokens(sectionRoot("Collapse commits", 1));
    // Content-sized: no `flex-1` (which split the pane into equal thirds and
    // clipped a two-item list to one row), capped so it cannot crowd out the
    // other sections, and the cap is the tight one because the log is long.
    expect(commits).toContain("flex-none");
    expect(commits).not.toContain("flex-1");
    expect(commits).toContain("max-h-[30%]");
    expect(commits).toContain("md:max-h-[55%]");

    const stashes = tokens(sectionRoot("Collapse stashes", 2));
    expect(stashes).toContain("flex-none");
    expect(stashes).not.toContain("flex-1");
    // Short lists get the roomier cap than the commit log.
    expect(stashes).toContain("max-h-[45%]");
    expect(stashes).not.toContain("max-h-[30%]");
  });

  it("leaves a collapsed section at its header height", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("stash@{0}");

    // Worktrees are collapsed by default: header only, never a flex item that
    // could claim a share of the pane.
    const collapsed = tokens(sectionRoot("Expand worktrees", 2));
    expect(collapsed).toContain("shrink-0");
    expect(collapsed).not.toContain("flex-none");
    expect(collapsed.some((t) => t.startsWith("max-h-"))).toBe(false);
  });

  it("gives the worktree list the same content sizing once expanded", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("stash@{0}");

    fireEvent.click(screen.getByTitle("Expand worktrees"));

    const expanded = tokens(sectionRoot("Collapse worktrees", 2));
    expect(expanded).toContain("flex-none");
    expect(expanded).toContain("max-h-[45%]");
    expect(expanded).not.toContain("flex-1");
    // The list itself is content-sized too: no `flex-1`, so it contributes its
    // natural height to the section instead of flexing to a share of it.
    const list = sectionRoot("Collapse worktrees", 2).querySelector(".overflow-y-auto");
    expect(list).not.toBeNull();
    expect(tokens(list)).not.toContain("flex-1");
    expect(tokens(list)).toContain("overscroll-contain");
    // Both checkouts are rendered (the list is not clipped to one row).
    expect(await screen.findByText("feature")).toBeTruthy();
  });

  it("scrolls the section column, not each list into a sliver", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("stash@{0}");

    // The pane is the single scroll container for the stacked sections, so a
    // list longer than its cap scrolls inside it while the rest stay reachable.
    expect(tokens(screen.getByTestId("git-file-pane"))).toContain("overflow-y-auto");

    for (const [title, depth] of [
      ["Collapse commits", 1],
      ["Collapse stashes", 2],
    ] as const) {
      const list = sectionRoot(title, depth).querySelector(".overflow-y-auto");
      expect(list).not.toBeNull();
      expect(tokens(list)).toContain("overscroll-contain");
    }
  });
});
