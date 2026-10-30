import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitStash, GitStatus, GitWorkspace } from "@/api/types";

const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitHunk: vi.fn(),
  gitDiscard: vi.fn(),
  gitStage: vi.fn(),
  gitUnstage: vi.fn(),
  gitIgnore: vi.fn(),
  gitCommit: vi.fn(),
  gitCommitMessage: vi.fn(),
  gitPush: vi.fn(),
  gitFetch: vi.fn(),
  gitPull: vi.fn(),
  gitStash: vi.fn(),
  gitStashList: vi.fn(),
  gitStashShow: vi.fn(),
  gitStashApply: vi.fn(),
  gitStashDrop: vi.fn(),
  // WorktreesSection loads its list even while collapsed (the header flags
  // checkouts no session uses), so every GitPanel render calls this.
  gitWorktrees: vi.fn(),
  /** git_status bus handlers registered by GitPanel. */
  gitStatusHandlers: [] as Array<(env: unknown) => void>,
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: {
    on: vi.fn((event: string, handler: (env: unknown) => void) => {
      if (event === "git_status") mocks.gitStatusHandlers.push(handler);
      return () => {};
    }),
  },
}));

// jsdom has no PointerEvent (see FileTabContent.test.tsx) and `fireEvent
// .pointerDown` silently drops `clientX` when it falls back to a plain Event —
// the column-resize drag relies on clientX propagation, so polyfill it.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
    }
  }
  // @ts-expect-error assigning a minimal polyfill onto jsdom's window
  window.PointerEvent = PointerEventPolyfill;
}

import GitPanel from "./GitPanel";

/** Simulates the server pushing a git_status event (background refresh). */
async function emitGitStatus(project = "/proj") {
  await act(async () => {
    for (const h of mocks.gitStatusHandlers) h({ project });
    await Promise.resolve();
  });
}

const GIT_PANEL_SECTIONS_KEY = "ocode.ui.git-panel.v1";
const GIT_PANEL_WIDTH_KEY = "ocode.ui.git-panel.width";
const GIT_PANEL_COLLAPSED_KEY = "ocode.ui.git-panel.width.collapsed";

/** Mutable matchMedia result so a test can simulate a narrow viewport. */
let mqMatches = false;

beforeAll(() => {
  // jsdom ships no matchMedia; useIsMobile reads it on first render.
  window.matchMedia = ((media: string) => ({
    get matches() {
      return mqMatches;
    },
    media,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  mqMatches = false;
});

/** Waits for the portal context menu (rendered into document.body) to open. */
async function openContextMenuRow(fileName: string) {
  const row = screen.getByText(fileName).closest(".group")!;
  fireEvent.contextMenu(row);
  return waitFor(() => {
    const el = document.querySelector(".bg-popover");
    if (!el) throw new Error("context menu did not open");
    return el as HTMLElement;
  });
}

const patch = [
  "diff --git a/src/unstaged.ts b/src/unstaged.ts",
  "index 1111111..2222222 100644",
  "--- a/src/unstaged.ts",
  "+++ b/src/unstaged.ts",
  "@@ -1,1 +1,1 @@",
  "-old one",
  "+new one",
  "@@ -10,1 +10,1 @@",
  "-old two",
  "+new two",
].join("\n");

const workspace: GitWorkspace = {
  status: {
    branch: "main",
    staged_files: ["src/staged.ts"],
    changed_files: ["src/unstaged.ts", "src/untracked.txt"],
    conflicts: [],
    has_changes: true,
    is_repo: true,
    ahead: 0,
    behind: 0,
    has_upstream: false,
  },
  staged: [
    {
      path: "src/staged.ts",
      status: "modified",
      patch: "diff --git a/src/staged.ts b/src/staged.ts\n--- a/src/staged.ts\n+++ b/src/staged.ts\n@@ -1,1 +1,1 @@\n-old\n+staged",
    },
  ],
  unstaged: [
    { path: "src/unstaged.ts", status: "modified", patch },
    {
      path: "src/untracked.txt",
      status: "untracked",
      patch: "diff --git a/src/untracked.txt b/src/untracked.txt\nnew file mode 100644\n--- /dev/null\n+++ b/src/untracked.txt\n@@ -0,0 +1 @@\n+untracked",
    },
  ],
};

describe("GitPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getGitWorkspace.mockResolvedValue(workspace);
    mocks.gitLog.mockResolvedValue([]);
    mocks.gitHunk.mockResolvedValue(workspace);
    mocks.gitDiscard.mockResolvedValue(workspace.status);
    mocks.gitStage.mockResolvedValue(workspace.status);
    mocks.gitUnstage.mockResolvedValue(workspace.status);
    mocks.gitIgnore.mockResolvedValue(workspace.status);
    mocks.gitCommit.mockResolvedValue(workspace.status);
    mocks.gitCommitMessage.mockResolvedValue({ message: "", model: "" });
    mocks.gitPush.mockResolvedValue(workspace.status);
    mocks.gitFetch.mockResolvedValue(workspace.status);
    mocks.gitPull.mockResolvedValue(workspace.status);
    mocks.gitStashList.mockResolvedValue([]);
    mocks.gitWorktrees.mockResolvedValue([]);
    mocks.gitStashShow.mockResolvedValue([]);
    mocks.gitStashApply.mockResolvedValue(workspace);
    mocks.gitStashDrop.mockResolvedValue([]);
    mocks.gitStash.mockResolvedValue(workspace.status);
    mocks.gitDiscard.mockResolvedValue(workspace.status);
    mocks.gitStatusHandlers.length = 0;
    window.localStorage.clear();
  });

  it("routes an unstaged hunk to gitHunk with staged=false and its index", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    fireEvent.click(screen.getByText("src/unstaged.ts"));

    const stageButtons = await screen.findAllByRole("button", { name: "Stage hunk" });
    expect(stageButtons).toHaveLength(2);
    fireEvent.click(stageButtons[1]);

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/unstaged.ts", hunk_index: 1, action: "stage", staged: false },
        "/proj",
        undefined,
      ),
    );
  });

  it("routes a staged hunk to gitHunk with staged=true and unstage", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    fireEvent.click(screen.getByText("src/staged.ts"));
    fireEvent.click(await screen.findByRole("button", { name: "Unstage hunk" }));

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/staged.ts", hunk_index: 0, action: "unstage", staged: true },
        "/proj",
        undefined,
      ),
    );
  });

  it("deletes an untracked file through the whole-file discard hunk", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    fireEvent.click(screen.getByTitle("Delete file"));
    // Destructive actions are gated behind a rendered confirmation dialog.
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/untracked.txt", hunk_index: 0, action: "discard", staged: false },
        "/proj",
        undefined,
      ),
    );
  });

  it("asks for confirmation before deleting an untracked file", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    fireEvent.click(screen.getByTitle("Delete file"));

    // The dialog appears and nothing is deleted until it is confirmed.
    expect(await screen.findByText("Delete untracked file?")).toBeTruthy();
    expect(mocks.gitHunk).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText("Delete untracked file?")).toBeNull());
    expect(mocks.gitHunk).not.toHaveBeenCalled();
  });

  it("asks for confirmation before discarding changes to a tracked file", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    fireEvent.click(screen.getByTitle("Discard changes"));

    expect(await screen.findByText("Discard changes?")).toBeTruthy();
    expect(mocks.gitDiscard).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    await waitFor(() =>
      expect(mocks.gitDiscard).toHaveBeenCalledWith(["src/unstaged.ts"], "/proj", undefined),
    );
  });

  it("keeps commit disabled until a non-empty message is supplied", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const commitButton = screen.getByRole("button", { name: "Commit" });
    expect(commitButton).toBeDisabled();
    expect(mocks.gitCommit).not.toHaveBeenCalled();

    fireEvent.change(screen.getByPlaceholderText("Commit message for staged changes…"), {
      target: { value: "save changes" },
    });
    expect(commitButton).toBeEnabled();
    fireEvent.click(commitButton);
    await waitFor(() => expect(mocks.gitCommit).toHaveBeenCalledWith("save changes", [], "/proj", undefined));
  });

  it("generates a commit message from the staged diff and fills the input", async () => {
    mocks.gitCommitMessage.mockResolvedValue({
      message: "feat: generated subject",
      model: "small/model",
    });

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    fireEvent.click(screen.getByRole("button", { name: "Generate commit message with AI" }));

    await waitFor(() => expect(mocks.gitCommitMessage).toHaveBeenCalledTimes(1));
    const [diff, project] = mocks.gitCommitMessage.mock.calls[0] as [string, string];
    expect(project).toBe("/proj");
    expect(diff).toContain("src/staged.ts");
    // Staged patches win over the working tree, mirroring the TUI generator.
    expect(diff).not.toContain("src/unstaged.ts");

    const input = screen.getByPlaceholderText(
      "Commit message for staged changes…",
    ) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("feat: generated subject"));
  });

  it("keeps a generated subject and body on separate lines and commits it whole", async () => {
    const generated =
      "feat: detailed commit messages\n\n- explain what changed\n- explain why";
    mocks.gitCommitMessage.mockResolvedValue({ message: generated, model: "small/model" });

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    fireEvent.click(screen.getByRole("button", { name: "Generate commit message with AI" }));

    const field = screen.getByPlaceholderText("Commit message for staged changes…");
    // A single-line <input> sanitises newlines away, collapsing the body into
    // the subject; the box must be a textarea for the body to survive.
    expect(field.tagName).toBe("TEXTAREA");
    await waitFor(() => expect((field as HTMLTextAreaElement).value).toBe(generated));

    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    await waitFor(() =>
      expect(mocks.gitCommit).toHaveBeenCalledWith(generated, [], "/proj", undefined),
    );
  });

  it("grows the commit box with the message and caps its height", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    const field = screen.getByPlaceholderText(
      "Commit message for staged changes…",
    ) as HTMLTextAreaElement;
    // jsdom has no layout engine: scrollHeight is 0, so the measured height has
    // to be stubbed per case to exercise the clamp.
    const setScrollHeight = (h: number) =>
      Object.defineProperty(field, "scrollHeight", { value: h, configurable: true });

    setScrollHeight(80);
    fireEvent.change(field, { target: { value: "subject\n\n- one\n- two" } });
    await waitFor(() => expect(field.style.height).toBe("80px"));

    // The box is border-box while scrollHeight excludes the border, so the
    // border must be added or the last line is clipped by it.
    Object.defineProperty(field, "offsetHeight", { value: 82, configurable: true });
    Object.defineProperty(field, "clientHeight", { value: 80, configurable: true });
    fireEvent.change(field, { target: { value: "subject\n\n- one\n- two\n- three" } });
    await waitFor(() => expect(field.style.height).toBe("82px"));

    setScrollHeight(900);
    fireEvent.change(field, { target: { value: "subject\n\n- one\n- two\n- three\n- four" } });
    await waitFor(() => expect(field.style.height).toBe("132px"));
  });

  it("wraps the commit box below sm so every button stays on screen on a phone", async () => {
    // jsdom has no layout engine, so this pins the CSS CONTRACT that makes the
    // row responsive rather than geometry. On a ~390px phone viewport the
    // textarea's intrinsic min-width plus the three buttons exceeded the tab
    // body (which is `overflow-hidden`), so the nowrap row pushed
    // "Commit & Push" past the right edge with no way to scroll to it.
    // Verified in a real browser at 320/360/390px: no horizontal overflow, the
    // message field takes the first row and the buttons the second; from `sm`
    // up it is the original single row with the buttons at the right edge.
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");

    const field = screen.getByPlaceholderText("Commit message for staged changes…");
    const box = field.parentElement as HTMLElement;
    const boxTokens = box.className.split(/\s+/);
    const fieldTokens = field.className.split(/\s+/);

    // The row must be allowed to wrap (and must not be pinned to one line).
    expect(boxTokens).toContain("flex-wrap");
    expect(boxTokens).not.toContain("flex-nowrap");

    // Phones: the message field takes a full-width row of its own...
    expect(fieldTokens).toContain("w-full");
    // ...and may shrink instead of forcing the row wider than the pane.
    expect(fieldTokens).toContain("min-w-0");
    // From `sm` up it goes back to sharing the single row.
    expect(fieldTokens).toContain("sm:w-auto");
    expect(fieldTokens).toContain("sm:flex-1");

    // Buttons keep their labels: they must never shrink or wrap their text.
    const commitButton = screen.getByRole("button", { name: "Commit" });
    const pushButton = screen.getByRole("button", { name: "Commit & Push" });
    for (const button of [commitButton, pushButton]) {
      const tokens = button.className.split(/\s+/);
      expect(tokens).toContain("shrink-0");
      expect(tokens).toContain("whitespace-nowrap");
    }
    // The wrapped button row is right-aligned; the desktop row is unchanged.
    const generateButton = screen.getByRole("button", {
      name: "Generate commit message with AI",
    });
    expect(generateButton.className.split(/\s+/)).toContain("ml-auto");
    expect(generateButton.className.split(/\s+/)).toContain("sm:ml-0");
  });

  it("uses the working-tree diff when nothing is staged", async () => {
    mocks.getGitWorkspace.mockResolvedValue({ ...workspace, staged: [] });
    mocks.gitCommitMessage.mockResolvedValue({ message: "fix: from worktree", model: "" });

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    fireEvent.click(screen.getByRole("button", { name: "Generate commit message with AI" }));

    await waitFor(() => expect(mocks.gitCommitMessage).toHaveBeenCalledTimes(1));
    const [diff] = mocks.gitCommitMessage.mock.calls[0] as [string];
    expect(diff).toContain("src/unstaged.ts");
    expect(diff).toContain("src/untracked.txt");
  });

  it("keeps the draft and shows the error when generation fails", async () => {
    mocks.gitCommitMessage.mockRejectedValue(new Error("small/model: rate limited"));

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const input = screen.getByPlaceholderText(
      "Commit message for staged changes…",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "my draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Generate commit message with AI" }));

    expect(await screen.findByText("small/model: rate limited")).toBeTruthy();
    expect(input.value).toBe("my draft");
  });

  it("commits then pushes in one action", async () => {
    const order: string[] = [];
    mocks.gitCommit.mockImplementation(async () => {
      order.push("commit");
      return workspace.status;
    });
    mocks.gitPush.mockImplementation(async () => {
      order.push("push");
      return workspace.status;
    });

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    fireEvent.change(screen.getByPlaceholderText("Commit message for staged changes…"), {
      target: { value: "ship it" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Commit & Push" }));

    await waitFor(() => expect(mocks.gitPush).toHaveBeenCalledWith("/proj", false, undefined));
    expect(mocks.gitCommit).toHaveBeenCalledWith("ship it", [], "/proj", undefined);
    // Commit must complete before the push is attempted.
    expect(order).toEqual(["commit", "push"]);
    expect(screen.getByTestId("git-notice").textContent).toBe("committed and pushed");
  });

  it("does not push when the commit fails", async () => {
    mocks.gitCommit.mockRejectedValueOnce(new Error("no changes added to commit"));

    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    fireEvent.change(screen.getByPlaceholderText("Commit message for staged changes…"), {
      target: { value: "ship it" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Commit & Push" }));

    await waitFor(() => expect(mocks.gitCommit).toHaveBeenCalled());
    expect(mocks.gitPush).not.toHaveBeenCalled();
  });

  it("keeps Commit & Push disabled until a non-empty message is supplied", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const button = screen.getByRole("button", { name: "Commit & Push" });
    expect(button).toBeDisabled();

    fireEvent.change(screen.getByPlaceholderText("Commit message for staged changes…"), {
      target: { value: "ship it" },
    });
    expect(button).toBeEnabled();
  });

  it("stages an unstaged file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const menu = await openContextMenuRow("src/unstaged.ts");

    // The hover row button shares the "Stage file" accessible name; scope to
    // the portal menu.
    fireEvent.click(within(menu).getByRole("button", { name: "Stage file" }));
    await waitFor(() =>
      expect(mocks.gitStage).toHaveBeenCalledWith(["src/unstaged.ts"], "/proj", undefined),
    );
  });

  it("unstages a staged file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    const menu = await openContextMenuRow("src/staged.ts");

    fireEvent.click(within(menu).getByRole("button", { name: "Unstage file" }));
    await waitFor(() =>
      expect(mocks.gitUnstage).toHaveBeenCalledWith(["src/staged.ts"], "/proj", undefined),
    );
  });

  it("adds an untracked file to .gitignore via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    const menu = await openContextMenuRow("src/untracked.txt");

    fireEvent.click(within(menu).getByRole("button", { name: "Add to .gitignore" }));
    await waitFor(() =>
      expect(mocks.gitIgnore).toHaveBeenCalledWith(["src/untracked.txt"], "/proj", undefined),
    );
    // All-untracked selection: the notice carries no "skipped" suffix.
    await waitFor(() =>
      expect(screen.getByTestId("git-notice").textContent).toBe("added src/untracked.txt to .gitignore"),
    );
  });

  it("adds only the untracked members of a mixed selection and reports the skipped tracked ones", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    // Mixed selection: a modified/tracked row + an untracked row.
    fireEvent.click(screen.getByText("src/unstaged.ts"), { metaKey: true });
    fireEvent.click(screen.getByText("src/untracked.txt"), { metaKey: true });
    expect(await screen.findByText("2 selected ✕")).toBeTruthy();

    const menu = await openContextMenuRow("src/unstaged.ts");
    // One untracked member → the singular label.
    fireEvent.click(within(menu).getByRole("button", { name: "Add to .gitignore" }));

    await waitFor(() =>
      expect(mocks.gitIgnore).toHaveBeenCalledWith(["src/untracked.txt"], "/proj", undefined),
    );
    // The tracked member is skipped, and the notice names it.
    await waitFor(() =>
      expect(screen.getByTestId("git-notice").textContent).toBe(
        "added src/untracked.txt to .gitignore (skipped 1 tracked)",
      ),
    );
  });

  it("hides Add to .gitignore for a tracked file row", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const menu = await openContextMenuRow("src/unstaged.ts");

    // A gitignore entry does not untrack an already-tracked file, so the
    // action must not be offered for a modified (non-untracked) row.
    expect(within(menu).queryByRole("button", { name: "Add to .gitignore" })).toBeNull();
  });

  it("hides Add to .gitignore on the staged pane", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    const menu = await openContextMenuRow("src/staged.ts");

    expect(within(menu).queryByRole("button", { name: "Add to .gitignore" })).toBeNull();
  });

  it("deletes an untracked file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    const menu = await openContextMenuRow("src/untracked.txt");

    fireEvent.click(
      within(menu).getByRole("button", { name: "Delete untracked file" }),
    );
    // The context-menu delete also confirms through the rendered dialog.
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/untracked.txt", hunk_index: 0, action: "discard", staged: false },
        "/proj",
        undefined,
      ),
    );
  });

  it("shows the staged diff for a file present in both panes and toggles to working tree", async () => {
    const both: GitWorkspace = {
      status: {
        branch: "main",
        staged_files: ["src/both.ts"],
        changed_files: ["src/both.ts"],
        conflicts: [],
        has_changes: true,
        is_repo: true,
        ahead: 0,
        behind: 0,
        has_upstream: false,
      },
      staged: [
        {
          path: "src/both.ts",
          status: "modified",
          patch: "diff --git a/src/both.ts b/src/both.ts\n--- a/src/both.ts\n+++ b/src/both.ts\n@@ -1,1 +1,1 @@\n-old\n+staged-half",
        },
      ],
      unstaged: [
        {
          path: "src/both.ts",
          status: "modified",
          patch: "diff --git a/src/both.ts b/src/both.ts\n--- a/src/both.ts\n+++ b/src/both.ts\n@@ -1,1 +1,1 @@\n-old\n+working-half",
        },
      ],
    };
    mocks.getGitWorkspace.mockResolvedValue(both);
    render(<GitPanel projectPath="/proj" />);

    // Two rows share the path; the first rendered one is in the Staged section.
    const rows = await screen.findAllByText("src/both.ts");
    expect(rows.length).toBe(2);
    fireEvent.click(rows[0]);

    // Clicking the staged row shows the STAGED diff (Unstage hunk), plus the
    // Working tree | Staged toggle.
    expect(await screen.findByRole("button", { name: "Unstage hunk" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Stage hunk" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Working tree" }));

    // Toggling switches to the working-tree diff of the same file.
    expect(await screen.findByRole("button", { name: "Stage hunk" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Unstage hunk" })).toBeNull();
  });

  it("collapses the staged section and persists the layout across remounts", async () => {
    window.localStorage.clear();
    const { unmount } = render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    expect(screen.getByText("src/staged.ts")).toBeTruthy();

    fireEvent.click(screen.getByTitle("Collapse Staged changes"));
    expect(screen.queryByText("src/staged.ts")).toBeNull();
    expect(screen.getByText("src/unstaged.ts")).toBeTruthy();

    await waitFor(() => {
      const stored = JSON.parse(
        window.localStorage.getItem(GIT_PANEL_SECTIONS_KEY) ?? "{}",
      );
      expect(stored).toMatchObject({ staged: false, unstaged: true, commits: true });
    });

    // A fresh mount restores the collapsed layout from localStorage.
    unmount();
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    expect(screen.queryByText("src/staged.ts")).toBeNull();
  });

  // --- git action error/notice regression tests ---------------------------
  //
  // A failed push used to be wiped by the next background poll: load() began
  // with setError(null) and ran on a 10s interval plus every git_status bus
  // event, so the error "disappeared by itself" within seconds. These tests
  // pin the fix (background refreshes never clear a user-visible error) and
  // the new 5-second success notice.

  it("keeps a failed push error visible across background refreshes", async () => {
    mocks.gitPush.mockRejectedValueOnce(new Error("remote rejected (fetch first)"));
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    fireEvent.click(screen.getByLabelText("Push to remote"));
    expect(await screen.findByText("remote rejected (fetch first)")).toBeTruthy();

    // The server pushes a git_status event (the real background-refresh path
    // used by the 10s poll and other clients mutating the repo). It must NOT
    // clear the error — this is the "disappears by itself" regression.
    await emitGitStatus();
    expect(screen.getByText("remote rejected (fetch first)")).toBeTruthy();
  });

  // stageNetwork configures the before-status (read from workspace.status by
  // runMutation) and the after-status (returned by getGitWorkspace on load),
  // so a pull/push/fetch can report a real ahead/behind delta.
  const stageNetwork = (
    before: Partial<GitStatus>,
    after: GitStatus,
  ) => {
    workspace.status = { ...workspace.status, ...before, has_upstream: true };
    mocks.getGitWorkspace.mockResolvedValue({ ...workspace, status: after });
  };

  afterEach(() => {
    // Restore the shared workspace status so other tests see the defaults.
    workspace.status = {
      ...workspace.status,
      ahead: 0,
      behind: 0,
      has_upstream: false,
    };
  });

  it("shows a transient success notice after a push and clears it after 5s", async () => {
    vi.useFakeTimers();
    try {
      render(<GitPanel projectPath="/proj" />);
      // Let the initial load resolve before staging the before/after status,
      // otherwise the mount load clobbers the `before` state runMutation reads.
      await act(async () => {
        await Promise.resolve();
      });
      // ahead = 2 before the push, 0 after → 2 commits pushed.
      stageNetwork(
        { ahead: 2 },
        { ...workspace.status, ahead: 0, has_upstream: true },
      );

      const pushBtn = screen.getByLabelText("Push to remote");
      await act(async () => {
        fireEvent.click(pushBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("pushed 2 commits");

      await act(async () => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.queryByTestId("git-notice")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  // REGRESSION: the server's `has_upstream` does NOT mean "the branch has an
  // upstream" — it means "the branch currently DIVERGES from its upstream"
  // (internal/server/handler_git.go skips the "+0"/"-0" entries of
  // `git status --porcelain=v2 --branch` and only falls back to
  // `rev-parse @{upstream}` when that line is absent). So HasUpstream is FALSE
  // whenever the branch is in sync — which is exactly the state right after a
  // successful push or pull, i.e. precisely when the `after` snapshot is taken.
  //
  // Gating the count on `has_upstream` therefore reported "nothing to push"
  // after a successful 2-commit push, and "nothing to pull" after a successful
  // pull. These two tests pin the REAL after-state (has_upstream: false).
  it("counts pushed commits even though the server reports has_upstream false after the push", async () => {
    vi.useFakeTimers();
    try {
      render(<GitPanel projectPath="/proj" />);
      await act(async () => {
        await Promise.resolve();
      });
      // ahead = 2 before the push, 0 after → 2 commits pushed. The after-state
      // has has_upstream:false because the branch is now in sync — exactly what
      // the server really returns.
      stageNetwork(
        { ahead: 2, has_upstream: true },
        { ...workspace.status, ahead: 0, behind: 0, has_upstream: false },
      );

      const pushBtn = screen.getByLabelText("Push to remote");
      await act(async () => {
        fireEvent.click(pushBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("pushed 2 commits");
    } finally {
      vi.useRealTimers();
    }
  });

  it("counts pulled commits even though the server reports has_upstream false after the pull", async () => {
    vi.useFakeTimers();
    try {
      render(<GitPanel projectPath="/proj" />);
      await act(async () => {
        await Promise.resolve();
      });
      // behind = 5 before the pull, 0 after → 5 commits pulled.
      stageNetwork(
        { behind: 5, has_upstream: true },
        { ...workspace.status, ahead: 0, behind: 0, has_upstream: false },
      );

      const pullBtn = screen.getByLabelText("Pull from remote");
      await act(async () => {
        fireEvent.click(pullBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("pulled 5 commits");
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports 'nothing to push' when nothing is ahead of the upstream", async () => {
    vi.useFakeTimers();
    try {
      stageNetwork(
        { ahead: 0 },
        { ...workspace.status, ahead: 0, has_upstream: true },
      );
      render(<GitPanel projectPath="/proj" />);
      await act(async () => {
        await Promise.resolve();
      });

      const pushBtn = screen.getByLabelText("Push to remote");
      await act(async () => {
        fireEvent.click(pushBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("nothing to push");
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports 'pulled N commits' and 'nothing to pull' for pull", async () => {
    vi.useFakeTimers();
    try {
      render(<GitPanel projectPath="/proj" />);
      // Let the initial load resolve before staging the before/after status,
      // otherwise the mount load clobbers the `before` state runMutation reads.
      await act(async () => {
        await Promise.resolve();
      });
      // behind = 2 before, 0 after → 2 commits pulled.
      stageNetwork(
        { behind: 2 },
        { ...workspace.status, behind: 0, has_upstream: true },
      );

      const pullBtn = screen.getByLabelText("Pull from remote");
      await act(async () => {
        fireEvent.click(pullBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("pulled 2 commits");
    } finally {
      vi.useRealTimers();
    }
  });

  it("reports 'fetched N commits' and 'nothing fetched' for fetch", async () => {
    vi.useFakeTimers();
    try {
      render(<GitPanel projectPath="/proj" />);
      // Let the initial load resolve before staging the before/after status,
      // otherwise the mount load clobbers the `before` state runMutation reads.
      await act(async () => {
        await Promise.resolve();
      });
      // behind = 0 before, 4 after → 4 commits fetched.
      stageNetwork(
        { behind: 0 },
        { ...workspace.status, behind: 4, has_upstream: true },
      );

      const fetchBtn = screen.getByLabelText("Fetch all remotes");
      await act(async () => {
        fireEvent.click(fetchBtn);
        await Promise.resolve();
      });

      expect(screen.getByTestId("git-notice").textContent).toBe("fetched 4 commits");
    } finally {
      vi.useRealTimers();
    }
  });

  // --- resizable file-list column -----------------------------------------
  //
  // The left column (files + commits) is drag-resizable and the width is
  // persisted, matching the app sidebar/file-tree. `useResizableSidebar`
  // resolves the width from localStorage and writes it back on every change.

  it("resizes the file-list column by dragging its divider and persists it", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    const handle = screen.getByRole("separator", { name: /resize file list and diff/i });
    const column = handle.previousElementSibling as HTMLElement;
    expect(column.style.width).toBe("288px");

    fireEvent.pointerDown(handle, { clientX: 288, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 388, pointerId: 1 });
    expect(column.style.width).toBe("388px");

    // The width is persisted immediately (not only on pointerup).
    await waitFor(() => expect(window.localStorage.getItem(GIT_PANEL_WIDTH_KEY)).toBe("388"));

    // Releasing the pointer stops tracking.
    fireEvent.pointerUp(window, { pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 120, pointerId: 1 });
    expect(column.style.width).toBe("388px");
  });

  it("restores a persisted column width on mount and resets on double-click", async () => {
    window.localStorage.setItem(GIT_PANEL_WIDTH_KEY, "400");
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    const handle = screen.getByRole("separator", { name: /resize file list and diff/i });
    const column = handle.previousElementSibling as HTMLElement;
    expect(column.style.width).toBe("400px");

    fireEvent.doubleClick(handle);
    expect(column.style.width).toBe("288px");
  });

  it("collapses the file-list pane from the header toggle and persists it", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    const pane = screen.getByTestId("git-file-pane");
    expect(pane.style.width).toBe("288px");
    expect(
      screen.getByRole("separator", { name: /resize file list and diff/i }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Hide file list" }));

    // Collapsed: zero width, and the resize handle is gone.
    expect(pane.style.width).toBe("0px");
    expect(
      screen.queryByRole("separator", { name: /resize file list and diff/i }),
    ).toBeNull();
    await waitFor(() =>
      expect(window.localStorage.getItem(GIT_PANEL_COLLAPSED_KEY)).toBe("1"),
    );

    // The toggle flips to "Show" and restores the pane.
    fireEvent.click(screen.getByRole("button", { name: "Show file list" }));
    expect(pane.style.width).toBe("288px");
  });

  it("restores a collapsed file-list pane on mount", async () => {
    window.localStorage.setItem(GIT_PANEL_COLLAPSED_KEY, "1");
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    expect(screen.getByTestId("git-file-pane").style.width).toBe("0px");
  });

  it("overlays the file list on narrow viewports instead of splitting", async () => {
    mqMatches = true;
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    // A column resize handle is meaningless for an overlay drawer.
    expect(
      screen.queryByRole("separator", { name: /resize file list and diff/i }),
    ).toBeNull();

    const pane = screen.getByTestId("git-file-pane");
    // Out of flow and parked off-screen: closed by default, so the diff owns
    // the full width instead of both panes sharing a phone screen.
    expect(pane.className).toContain("absolute");
    expect(pane.className).toContain("-translate-x-full");
    expect(pane.style.width).toBe("");
    expect(pane.style.height).toBe("");
    expect(screen.queryByTestId("git-file-backdrop")).toBeNull();
    expect(
      screen.getByRole("button", { name: "Show file list" }).getAttribute("aria-expanded"),
    ).toBe("false");

    // The header toggle slides it over the diff, behind a dismissable backdrop.
    fireEvent.click(screen.getByRole("button", { name: "Show file list" }));
    expect(pane.className).not.toContain("-translate-x-full");
    expect(screen.getByTestId("git-file-backdrop")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Hide file list" }).getAttribute("aria-expanded"),
    ).toBe("true");

    // Choosing a file dismisses the drawer so the diff it selected is visible.
    fireEvent.click(screen.getByText("src/unstaged.ts"));
    await waitFor(() => expect(pane.className).toContain("-translate-x-full"));
    expect(screen.queryByTestId("git-file-backdrop")).toBeNull();
  });
});

describe("GitPanel stash", () => {
  const stashEntries: GitStash[] = [
    {
      index: 0,
      ref: "stash@{0}",
      hash: "abc123",
      short: "abc123",
      message: "WIP on main: abc123 base subject",
      author: "Test",
      date: new Date().toISOString(),
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getGitWorkspace.mockResolvedValue(workspace);
    mocks.gitLog.mockResolvedValue([]);
    mocks.gitStashList.mockResolvedValue(stashEntries);
    mocks.gitStashShow.mockResolvedValue([
      { path: "stash-a.ts", status: "modified", patch: "" },
      { path: "stash-b.ts", status: "modified", patch: "" },
    ]);
    mocks.gitStashApply.mockResolvedValue(workspace);
    mocks.gitStashDrop.mockResolvedValue([]);
    mocks.gitStash.mockResolvedValue(workspace.status);
    mocks.gitDiscard.mockResolvedValue(workspace.status);
    mocks.gitStatusHandlers.length = 0;
    window.localStorage.clear();
  });

  it("lists stashes and loads the selected stash's files", async () => {
    render(<GitPanel projectPath="/proj" />);
    const entry = await screen.findByTitle("stash@{0}: WIP on main: abc123 base subject");
    fireEvent.click(entry);

    await waitFor(() => expect(mocks.gitStashShow).toHaveBeenCalledWith(0, "/proj", undefined));
    expect(await screen.findByText("stash-a.ts")).toBeTruthy();
    expect(screen.getByText("stash-b.ts")).toBeTruthy();
  });

  it("restores only the checked files from a stash (no overwrite confirmation)", async () => {
    render(<GitPanel projectPath="/proj" />);
    fireEvent.click(await screen.findByTitle("stash@{0}: WIP on main: abc123 base subject"));
    const checkbox = await screen.findByRole("checkbox", { name: "Select stash-a.ts" });
    fireEvent.click(checkbox);
    fireEvent.click(screen.getByRole("button", { name: /Restore selected/ }));

    await waitFor(() =>
      expect(mocks.gitStashApply).toHaveBeenCalledWith(0, ["stash-a.ts"], "/proj", undefined),
    );
  });

  it("confirms before restoring a file that has local changes", async () => {
    mocks.gitStashShow.mockResolvedValue([
      { path: "src/unstaged.ts", status: "modified", patch: "" },
    ]);
    render(<GitPanel projectPath="/proj" />);
    fireEvent.click(await screen.findByTitle("stash@{0}: WIP on main: abc123 base subject"));
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Select src/unstaged.ts" }),
    );
    fireEvent.click(screen.getByRole("button", { name: /Restore selected/ }));

    // The overwrite warning must appear and nothing is applied yet.
    expect(await screen.findByText("Overwrite local changes?")).toBeTruthy();
    expect(mocks.gitStashApply).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Overwrite and restore" }));
    await waitFor(() =>
      expect(mocks.gitStashApply).toHaveBeenCalledWith(0, ["src/unstaged.ts"], "/proj", undefined),
    );
  });

  it("asks for confirmation before deleting a stash", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByTitle("stash@{0}: WIP on main: abc123 base subject");

    fireEvent.click(screen.getByRole("button", { name: "Delete stash@{0}" }));
    const confirm = await screen.findByRole("button", { name: "Delete stash" });
    expect(mocks.gitStashDrop).not.toHaveBeenCalled();

    fireEvent.click(confirm);
    await waitFor(() => expect(mocks.gitStashDrop).toHaveBeenCalledWith(0, "/proj", undefined));
  });

  it("stashes all changes with the dialog's message and untracked flag", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    fireEvent.click(screen.getByRole("button", { name: "Stash all" }));
    fireEvent.change(screen.getByPlaceholderText("Stash message (optional)"), {
      target: { value: "wip: my stash" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Stash" }));

    await waitFor(() =>
      expect(mocks.gitStash).toHaveBeenCalledWith("wip: my stash", [], "/proj", undefined, true),
    );
  });
});

describe("GitPanel per-file stash & multi-select", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getGitWorkspace.mockResolvedValue(workspace);
    mocks.gitLog.mockResolvedValue([]);
    mocks.gitStashList.mockResolvedValue([]);
    mocks.gitWorktrees.mockResolvedValue([]);
    mocks.gitStashShow.mockResolvedValue([]);
    mocks.gitStashApply.mockResolvedValue(workspace);
    mocks.gitStashDrop.mockResolvedValue([]);
    mocks.gitStash.mockResolvedValue(workspace.status);
    mocks.gitDiscard.mockResolvedValue(workspace.status);
    mocks.gitStatusHandlers.length = 0;
    window.localStorage.clear();
  });

  it("offers 'Stash file' on an unstaged row and stashes only that path", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    const menu = await openContextMenuRow("src/unstaged.ts");
    fireEvent.click(within(menu).getByRole("button", { name: "Stash file" }));

    fireEvent.change(await screen.findByPlaceholderText("Stash message (optional)"), {
      target: { value: "park this one" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Stash" }));

    await waitFor(() =>
      expect(mocks.gitStash).toHaveBeenCalledWith(
        "park this one",
        ["src/unstaged.ts"],
        "/proj",
        undefined,
        true,
      ),
    );
  });

  it("offers 'Stash file' on a staged row too", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");

    const menu = await openContextMenuRow("src/staged.ts");
    expect(within(menu).getByRole("button", { name: "Stash file" })).toBeTruthy();
  });

  it("cmd-click multi-selects rows and stashes the whole selection", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    // Modifier clicks build the selection without changing the diff pane.
    fireEvent.click(screen.getByText("src/unstaged.ts"), { metaKey: true });
    fireEvent.click(screen.getByText("src/untracked.txt"), { metaKey: true });
    expect(await screen.findByText("2 selected ✕")).toBeTruthy();

    const menu = await openContextMenuRow("src/unstaged.ts");
    fireEvent.click(within(menu).getByRole("button", { name: "Stash 2 files" }));
    fireEvent.click(await screen.findByRole("button", { name: "Stash" }));

    await waitFor(() =>
      expect(mocks.gitStash).toHaveBeenCalledWith(
        "",
        ["src/unstaged.ts", "src/untracked.txt"],
        "/proj",
        undefined,
        true,
      ),
    );
  });

  it("discards the whole multi-selection, deleting untracked members", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    fireEvent.click(screen.getByText("src/unstaged.ts"), { metaKey: true });
    fireEvent.click(screen.getByText("src/untracked.txt"), { metaKey: true });
    expect(await screen.findByText("2 selected ✕")).toBeTruthy();

    const menu = await openContextMenuRow("src/unstaged.ts");
    fireEvent.click(within(menu).getByRole("button", { name: "Discard 2 files" }));
    // A mixed tracked/untracked selection is a "Discard", not a "Delete".
    fireEvent.click(await screen.findByRole("button", { name: "Discard" }));

    // Tracked members revert in one bulk call; the untracked one is removed
    // with the whole-file discard hunk (git cannot restore an untracked file).
    await waitFor(() =>
      expect(mocks.gitDiscard).toHaveBeenCalledWith(["src/unstaged.ts"], "/proj", undefined),
    );
    expect(mocks.gitHunk).toHaveBeenCalledWith(
      { path: "src/untracked.txt", hunk_index: 0, action: "discard", staged: false },
      "/proj",
      undefined,
    );
  });

  it("shift-click selects a range, and shift-clicking it again deselects it", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");

    fireEvent.click(screen.getByText("src/unstaged.ts"));
    expect(await screen.findByText("1 selected ✕")).toBeTruthy();

    fireEvent.click(screen.getByText("src/untracked.txt"), { shiftKey: true });
    expect(await screen.findByText("2 selected ✕")).toBeTruthy();

    fireEvent.click(screen.getByText("src/untracked.txt"), { shiftKey: true });
    await waitFor(() => expect(screen.queryByText(/\d+ selected/)).toBeNull());
  });
});
