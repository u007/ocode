import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GitBranches, GitStatus, GitWorkspace } from "@/api/types";

const mocks = vi.hoisted(() => ({
  gitBranches: vi.fn(),
  gitRefAction: vi.fn(),
  gitPushBranch: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));

import BranchesPanel from "./BranchesPanel";

const status: GitStatus = {
  branch: "main",
  staged_files: [],
  changed_files: [],
  conflicts: [],
  has_changes: false,
  is_repo: true,
  ahead: 0,
  behind: 0,
  has_upstream: true,
};

const workspace: GitWorkspace = { status, staged: [], unstaged: [] };

function row(name: string, refname: string, extra: Record<string, unknown> = {}) {
  return {
    name,
    refname,
    hash: "h1",
    kind: "local" as const,
    current: false,
    ahead: 0,
    behind: 0,
    subject: `subject ${name}`,
    date: "2026-10-30T00:00:00Z",
    ...extra,
  };
}

const fixture: GitBranches = {
  local: [
    row("main", "refs/heads/main", { current: true, upstream: "origin/main", ahead: 2, behind: 1 }),
    row("feature", "refs/heads/feature"),
    row("topic", "refs/heads/topic"),
  ],
  remote: [
    row("origin/main", "refs/remotes/origin/main", { kind: "remote" as const, remote: "origin" }),
    row("origin/hotfix", "refs/remotes/origin/hotfix", { kind: "remote" as const, remote: "origin" }),
  ],
  tags: [row("v1.0", "refs/tags/v1.0", { kind: "tag" as const })],
};

function renderPanel(props: Partial<Parameters<typeof BranchesPanel>[0]> = {}) {
  const onWorkspaceChanged = vi.fn();
  const utils = render(
    <BranchesPanel
      projectPath="/proj"
      active
      refreshToken={0}
      currentBranch="main"
      dirty={false}
      onWorkspaceChanged={onWorkspaceChanged}
      {...props}
    />,
  );
  return { ...utils, onWorkspaceChanged };
}

function branchRow(name: string): HTMLElement {
  const rowEl = document.querySelector<HTMLElement>(
    `[data-testid="git-branch-row"][data-branch-name="${name}"]`,
  );
  if (!rowEl) throw new Error(`branch row ${name} not rendered`);
  return rowEl;
}

async function openMenu(name: string) {
  const actions = branchRow(name).querySelector<HTMLButtonElement>('[data-testid="git-branch-menu"]');
  if (!actions) throw new Error(`menu button for ${name} missing`);
  fireEvent.click(actions);
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.gitBranches.mockResolvedValue(fixture);
  mocks.gitRefAction.mockResolvedValue({ workspace, output: "" });
  mocks.gitPushBranch.mockResolvedValue(status);
});

describe("BranchesPanel", () => {
  it("lists local, remote and tag rows and marks the current branch", async () => {
    renderPanel();
    expect(await screen.findByTestId("git-branches-counts")).toBeTruthy();
    expect(branchRow("main")).toBeTruthy();
    // "origin/main" appears twice: as the remote row and as main's upstream.
    expect(branchRow("origin/main")).toBeTruthy();
    expect(branchRow("v1.0")).toBeTruthy();
    expect(screen.getByTestId("git-branches-counts").textContent).toContain("3 local");
    // The current marker is only on main.
    expect(branchRow("main").querySelector('[aria-label="current branch"]')).toBeTruthy();
    expect(branchRow("feature").querySelector('[aria-label="current branch"]')).toBeNull();
    // Ahead/behind of the current branch against its upstream.
    expect(branchRow("main").textContent).toContain("↑2");
    expect(branchRow("main").textContent).toContain("↓1");
  });

  it("switches to a local branch on double click", async () => {
    const { onWorkspaceChanged } = renderPanel();
    await screen.findByText("feature");
    fireEvent.doubleClick(branchRow("feature"));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "checkout", target: "feature" },
        "/proj",
        undefined,
      ),
    );
    expect(onWorkspaceChanged).toHaveBeenCalledWith(workspace);
    expect(await screen.findByTestId("git-branches-notice")).toBeTruthy();
  });

  it("checks out a remote branch as a new tracking local branch", async () => {
    renderPanel();
    await screen.findByText("origin/hotfix");
    fireEvent.doubleClick(branchRow("origin/hotfix"));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "branch-checkout", target: "origin/hotfix", name: "hotfix" },
        "/proj",
        undefined,
      ),
    );
  });

  it("warns before switching with uncommitted changes and only runs on confirm", async () => {
    renderPanel({ dirty: true });
    await screen.findByText("feature");
    fireEvent.doubleClick(branchRow("feature"));
    expect(await screen.findByText("Switch to feature?")).toBeTruthy();
    expect(mocks.gitRefAction).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Switch anyway" }));
    await waitFor(() => expect(mocks.gitRefAction).toHaveBeenCalledTimes(1));
  });

  it("creates a branch from another branch's row", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("New branch from this…"));
    const input = await screen.findByLabelText("Branch name");
    fireEvent.change(input, { target: { value: "from-feature" } });
    fireEvent.click(screen.getByRole("button", { name: "Create and check out" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "branch-checkout", target: "feature", name: "from-feature" },
        "/proj",
        undefined,
      ),
    );
  });

  it("deletes with confirmation and offers force delete after an unmerged refusal", async () => {
    mocks.gitRefAction.mockRejectedValueOnce(new Error("the branch 'feature' is not fully merged"));
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Delete feature"));
    expect(await screen.findByText("Delete branch feature?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(await screen.findByText(/not fully merged/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Force delete" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenLastCalledWith(
        { action: "branch-force-delete", target: "feature" },
        "/proj",
        undefined,
      ),
    );
  });

  it("merges the selected branch into the current one", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Merge feature into main"));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "merge", target: "feature" },
        "/proj",
        undefined,
      ),
    );
  });

  it("confirms a rebase before running it", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Rebase main onto feature"));
    expect(await screen.findByText("Rebase main onto feature?")).toBeTruthy();
    expect(mocks.gitRefAction).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Rebase" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "rebase", target: "feature" },
        "/proj",
        undefined,
      ),
    );
  });

  it("renames a branch", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Rename…"));
    const input = await screen.findByLabelText("New branch name");
    fireEvent.change(input, { target: { value: "renamed" } });
    fireEvent.click(screen.getByRole("button", { name: "Rename" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "branch-rename", target: "feature", name: "renamed" },
        "/proj",
        undefined,
      ),
    );
  });

  it("deletes a remote branch through the push endpoint", async () => {
    renderPanel();
    await waitFor(() => expect(branchRow("origin/main")).toBeTruthy());
    await openMenu("origin/main");
    fireEvent.click(screen.getByText("Delete origin/main on origin"));
    expect(await screen.findByText("Delete origin/main on origin?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Delete remote branch" }));
    await waitFor(() =>
      expect(mocks.gitPushBranch).toHaveBeenCalledWith(
        { remote: "origin", branch: "main", delete: true },
        "/proj",
        undefined,
      ),
    );
  });

  it("sets the upstream from the remote-branch picker", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Set upstream…"));
    fireEvent.click(await screen.findByRole("button", { name: "Set upstream" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "set-upstream", target: "feature", name: "origin/main" },
        "/proj",
        undefined,
      ),
    );
  });

  it("pushes a branch with -u when it has no upstream", async () => {
    renderPanel();
    await screen.findByText("feature");
    await openMenu("feature");
    fireEvent.click(screen.getByText("Push branch…"));
    expect(await screen.findByRole("heading", { name: /Push feature/ })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Push" }));
    await waitFor(() =>
      expect(mocks.gitPushBranch).toHaveBeenCalledWith(
        { remote: "origin", branch: "feature", set_upstream: true },
        "/proj",
        undefined,
      ),
    );
  });
});
