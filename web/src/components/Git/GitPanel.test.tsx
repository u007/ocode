import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GitWorkspace } from "@/api/types";

const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitHunk: vi.fn(),
  gitStage: vi.fn(),
  gitUnstage: vi.fn(),
  gitCommit: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: vi.fn(() => () => {}) },
}));

import GitPanel from "./GitPanel";

const GIT_PANEL_SECTIONS_KEY = "ocode.ui.git-panel.v1";

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
    has_changes: true,
    is_repo: true,
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
    mocks.gitStage.mockResolvedValue(workspace.status);
    mocks.gitUnstage.mockResolvedValue(workspace.status);
    mocks.gitCommit.mockResolvedValue(workspace.status);
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
      ),
    );
  });

  it("deletes an untracked file through the whole-file discard hunk", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    fireEvent.click(screen.getByTitle("Delete file"));

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/untracked.txt", hunk_index: 0, action: "discard", staged: false },
        "/proj",
      ),
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
    await waitFor(() => expect(mocks.gitCommit).toHaveBeenCalledWith("save changes", [], "/proj"));
  });

  it("stages an unstaged file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/unstaged.ts");
    const menu = await openContextMenuRow("src/unstaged.ts");

    // The hover row button shares the "Stage file" accessible name; scope to
    // the portal menu.
    fireEvent.click(within(menu).getByRole("button", { name: "Stage file" }));
    await waitFor(() =>
      expect(mocks.gitStage).toHaveBeenCalledWith(["src/unstaged.ts"], "/proj"),
    );
  });

  it("unstages a staged file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/staged.ts");
    const menu = await openContextMenuRow("src/staged.ts");

    fireEvent.click(within(menu).getByRole("button", { name: "Unstage file" }));
    await waitFor(() =>
      expect(mocks.gitUnstage).toHaveBeenCalledWith(["src/staged.ts"], "/proj"),
    );
  });

  it("deletes an untracked file via the right-click context menu", async () => {
    render(<GitPanel projectPath="/proj" />);
    await screen.findByText("src/untracked.txt");
    const menu = await openContextMenuRow("src/untracked.txt");

    fireEvent.click(
      within(menu).getByRole("button", { name: "Delete untracked file" }),
    );

    await waitFor(() =>
      expect(mocks.gitHunk).toHaveBeenCalledWith(
        { path: "src/untracked.txt", hunk_index: 0, action: "discard", staged: false },
        "/proj",
      ),
    );
  });

  it("shows the staged diff for a file present in both panes and toggles to working tree", async () => {
    const both: GitWorkspace = {
      status: {
        branch: "main",
        staged_files: ["src/both.ts"],
        changed_files: ["src/both.ts"],
        has_changes: true,
        is_repo: true,
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
});
