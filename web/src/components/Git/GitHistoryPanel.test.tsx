import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitCommit, GitRef, GitStatus, GitWorkspace } from "@/api/types";

const mocks = vi.hoisted(() => ({
  gitLog: vi.fn(),
  gitShow: vi.fn(),
  gitRefAction: vi.fn(),
  copyTextToClipboard: vi.fn(),
}));

vi.mock("@/api/client", () => ({ api: mocks }));
vi.mock("@/lib/eventBus", () => ({
  eventBus: { on: vi.fn(() => () => {}) },
}));
vi.mock("@/lib/clipboard", () => ({ copyTextToClipboard: mocks.copyTextToClipboard }));

import GitHistoryPanel from "./GitHistoryPanel";

const SCOPE_KEY = "ocode.ui.git-history-scope.v1";
const SPLIT_KEY = "ocode.ui.git-history-split.v1";

const status: GitStatus = {
  branch: "main",
  staged_files: [],
  changed_files: [],
  conflicts: [],
  has_changes: false,
  is_repo: true,
  ahead: 0,
  behind: 0,
  has_upstream: false,
};

const cleanWorkspace: GitWorkspace = { status, staged: [], unstaged: [] };
const dirtyWorkspace: GitWorkspace = {
  status: { ...status, has_changes: true },
  staged: [],
  unstaged: [{ path: "src/a.ts", status: "modified", patch: "" }],
};

function commit(hash: string, overrides: Partial<GitCommit> = {}): GitCommit {
  return {
    hash,
    short: hash.slice(0, 7),
    message: `subject ${hash}`,
    author: "Ada",
    email: "ada@example.com",
    date: "2026-10-30T00:00:00Z",
    parents: [],
    refs: [],
    ...overrides,
  };
}

const head = (name: string, current = false): GitRef => ({ name, kind: "head", current });
const remote = (name: string): GitRef => ({ name, kind: "remote", current: false });

// h1 is HEAD on main; x1 is a feature branch; p1 is their shared root.
const fixture: GitCommit[] = [
  commit("h1aaaaaaaa", {
    parents: ["p1aaaaaaaa"],
    refs: [head("main", true), remote("origin/main"), remote("origin/HEAD")],
  }),
  commit("x1aaaaaaaa", {
    parents: ["p1aaaaaaaa"],
    refs: [head("feature"), remote("origin/feature")],
  }),
  commit("p1aaaaaaaa", {
    parents: [],
    refs: [remote("origin/hotfix"), { name: "v1.0", kind: "tag", current: false }],
  }),
];

function renderPanel(props: Partial<Parameters<typeof GitHistoryPanel>[0]> = {}) {
  const onWorkspaceChanged = vi.fn();
  const onShowChanges = vi.fn();
  const utils = render(
    <GitHistoryPanel
      projectPath="/proj"
      active
      workspace={cleanWorkspace}
      onWorkspaceChanged={onWorkspaceChanged}
      onShowChanges={onShowChanges}
      refreshToken={0}
      {...props}
    />,
  );
  return { ...utils, onWorkspaceChanged, onShowChanges };
}

function rowFor(hash: string): HTMLElement {
  const row = document.querySelector<HTMLElement>(`[data-hash="${hash}"]`);
  if (!row) throw new Error(`row ${hash} not rendered`);
  return row;
}

function openMenu(hash: string) {
  fireEvent.contextMenu(rowFor(hash), { clientX: 40, clientY: 50 });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.gitLog.mockResolvedValue(fixture);
  mocks.gitShow.mockResolvedValue([]);
  mocks.gitRefAction.mockResolvedValue({ workspace: cleanWorkspace, output: "" });
  mocks.copyTextToClipboard.mockResolvedValue(true);
  window.localStorage.clear();
});

afterEach(() => {
  window.localStorage.removeItem(SCOPE_KEY);
});

describe("GitHistoryPanel", () => {
  it("loads every branch by default and renders ref chips on each commit", async () => {
    renderPanel();
    await screen.findByText("subject h1aaaaaaaa");

    expect(mocks.gitLog).toHaveBeenCalledWith("/proj", { limit: 200, all: true }, undefined);
    const chips = Array.from(document.querySelectorAll('[data-testid="git-ref-chip"]')).map(
      (el) => `${el.getAttribute("data-kind")}:${el.textContent}`,
    );
    expect(chips).toContain("head:main");
    expect(chips).toContain("remote:origin/main");
    expect(chips).toContain("tag:v1.0");
    // origin/HEAD is a remote pointer, not a branch the user acts on.
    expect(chips).not.toContain("remote:origin/HEAD");
  });

  it("shows the uncommitted row when the workspace is dirty and opens Changes on click", async () => {
    const { onShowChanges } = renderPanel({ workspace: dirtyWorkspace });
    await screen.findByText("subject h1aaaaaaaa");

    const uncommitted = screen.getByTestId("git-history-uncommitted");
    fireEvent.click(uncommitted);
    expect(onShowChanges).toHaveBeenCalledTimes(1);
  });

  it("hides the uncommitted row for a clean workspace", async () => {
    renderPanel();
    await screen.findByText("subject h1aaaaaaaa");
    expect(screen.queryByTestId("git-history-uncommitted")).toBeNull();
  });

  it("opens the commit diff for a clicked row", async () => {
    mocks.gitShow.mockResolvedValue([{ path: "src/changed.ts", status: "modified", patch: "" }]);
    renderPanel();
    await screen.findByText("subject x1aaaaaaaa");

    fireEvent.click(rowFor("x1aaaaaaaa"));
    expect(mocks.gitShow).toHaveBeenCalledWith("x1aaaaaaaa", "/proj", undefined);
    await screen.findByText("src/changed.ts");
  });

  it("offers checkout, merge and reset items for a commit on a feature branch", async () => {
    renderPanel();
    await screen.findByText("subject x1aaaaaaaa");
    openMenu("x1aaaaaaaa");

    expect(screen.getByRole("button", { name: "Checkout feature" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Merge feature into main" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Merge origin/feature into main" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reset main to here (soft)" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reset main to here (mixed)" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reset main to here (hard)" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete branch feature" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Copy SHA" })).toBeTruthy();
    // origin/feature already has a local branch, so no tracking checkout is offered.
    expect(screen.queryByRole("button", { name: /origin\/feature \(track\)/ })).toBeNull();
  });

  it("does not offer the bare HEAD entry of a detached checkout as a branch", async () => {
    const detached = [
      commit("d1aaaaaaaa", {
        parents: ["p1aaaaaaaa"],
        refs: [head("HEAD", true)],
      }),
      ...fixture.slice(2),
    ];
    mocks.gitLog.mockResolvedValue(detached);
    renderPanel({
      workspace: { ...cleanWorkspace, status: { ...status, branch: "HEAD" } },
    });
    await screen.findByText("subject d1aaaaaaaa");
    openMenu("d1aaaaaaaa");

    expect(screen.queryByRole("button", { name: "Checkout HEAD" })).toBeNull();
    expect(screen.queryByRole("button", { name: /into HEAD$/ })).toBeNull();
    expect(screen.getByRole("button", { name: "Reset HEAD to here (soft)" })).toBeTruthy();
  });

  it("offers a tracking checkout for a remote branch with no local copy", async () => {
    renderPanel();
    await screen.findByText("subject p1aaaaaaaa");
    openMenu("p1aaaaaaaa");

    expect(screen.getByRole("button", { name: "Checkout origin/hotfix (track)" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Checkout commit p1aaaaa (detached)" })).toBeTruthy();
  });

  it("posts cherry-pick with the commit hash as the target", async () => {
    renderPanel();
    await screen.findByText("subject x1aaaaaaaa");
    openMenu("x1aaaaaaaa");

    fireEvent.click(screen.getByRole("button", { name: "Cherry-pick commit" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "cherry-pick", target: "x1aaaaaaaa" },
        "/proj",
        undefined,
      ),
    );
  });

  it("asks for confirmation before a hard reset", async () => {
    renderPanel();
    await screen.findByText("subject h1aaaaaaaa");
    openMenu("h1aaaaaaaa");

    fireEvent.click(screen.getByRole("button", { name: "Reset main to here (hard)" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Reset main to h1aaaaa (hard)?")).toBeTruthy();
    expect(mocks.gitRefAction).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole("button", { name: "Reset (hard)" }));
    await waitFor(() =>
      expect(mocks.gitRefAction).toHaveBeenCalledWith(
        { action: "reset-hard", target: "h1aaaaaaaa" },
        "/proj",
        undefined,
      ),
    );
  });

  it("shows git's own error text verbatim when the action is refused", async () => {
    mocks.gitRefAction.mockRejectedValue(
      new Error("error: could not apply x1aaaaa... conflict in src/a.ts"),
    );
    renderPanel();
    await screen.findByText("subject x1aaaaaaaa");
    openMenu("x1aaaaaaaa");

    fireEvent.click(screen.getByRole("button", { name: "Cherry-pick commit" }));
    const alert = await screen.findByTestId("git-history-error");
    expect(alert.textContent).toContain("error: could not apply x1aaaaa... conflict in src/a.ts");
  });

  it("copies the SHA from the menu", async () => {
    renderPanel();
    await screen.findByText("subject h1aaaaaaaa");
    openMenu("h1aaaaaaaa");

    fireEvent.click(screen.getByRole("button", { name: "Copy SHA" }));
    await waitFor(() => expect(mocks.copyTextToClipboard).toHaveBeenCalledWith("h1aaaaaaaa"));
  });

  it("enables every mutating item for a remote project and keeps copy enabled", async () => {
    renderPanel({ projectHost: "ssh-box" });
    await screen.findByText("subject x1aaaaaaaa");
    openMenu("x1aaaaaaaa");

    expect(
      screen.queryByText(
        "Merge, cherry-pick, revert and reset-hard are not available for remote projects yet",
      ),
    ).toBeNull();
    expect((screen.getByRole("button", { name: "Merge feature into main" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Cherry-pick commit" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Revert commit…" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Reset main to here (hard)" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Checkout feature" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Copy SHA" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps loaded pages when a refresh returns an unchanged head", async () => {
    const page1 = Array.from({ length: 200 }, (_, i) => commit(`c${String(i).padStart(4, "0")}`));
    const page2 = [commit("older00000")];
    mocks.gitLog.mockResolvedValueOnce(page1).mockResolvedValueOnce(page2);
    const { rerender } = renderPanel();
    await screen.findByText("subject c0000");

    fireEvent.click(screen.getByTestId("git-history-load-more"));
    await screen.findByText("subject older00000");
    expect(mocks.gitLog).toHaveBeenLastCalledWith(
      "/proj",
      { limit: 200, skip: 200, all: true },
      undefined,
    );

    // GitPanel's poll bumps refreshToken; the head is the same, so the older page must stay.
    mocks.gitLog.mockResolvedValueOnce(page1);
    rerender(
      <GitHistoryPanel
        projectPath="/proj"
        active
        workspace={cleanWorkspace}
        onWorkspaceChanged={vi.fn()}
        onShowChanges={vi.fn()}
        refreshToken={1}
      />,
    );
    await waitFor(() => expect(mocks.gitLog).toHaveBeenCalledTimes(3));
    await waitFor(() => expect(mocks.gitLog.mock.calls[2][1]).toEqual({ limit: 200, all: true }));
    expect(screen.getByText("subject older00000")).toBeTruthy();
    expect(screen.getByTestId("git-history-count").textContent).toContain("201");
  });

  it("does not leave the list stuck loading when a poll lands during the first load", async () => {
    let resolveFirst!: (commits: GitCommit[]) => void;
    mocks.gitLog.mockImplementationOnce(
      () => new Promise<GitCommit[]>((resolve) => { resolveFirst = resolve; }),
    );
    mocks.gitLog.mockResolvedValue(fixture);
    const { rerender } = renderPanel();

    rerender(
      <GitHistoryPanel
        projectPath="/proj"
        active
        workspace={cleanWorkspace}
        onWorkspaceChanged={vi.fn()}
        onShowChanges={vi.fn()}
        refreshToken={1}
      />,
    );
    // The poll is skipped while the first load is pending.
    expect(mocks.gitLog).toHaveBeenCalledTimes(1);

    resolveFirst(fixture);
    await screen.findByText("subject h1aaaaaaaa");
    expect(screen.getByRole("button", { name: "Refresh history" }).hasAttribute("disabled")).toBe(false);
  });

  it("keeps a load-more in place when a poll lands while it is pending", async () => {
    const page1 = Array.from({ length: 200 }, (_, i) => commit(`c${String(i).padStart(4, "0")}`));
    let resolveMore!: (commits: GitCommit[]) => void;
    mocks.gitLog
      .mockResolvedValueOnce(page1)
      .mockImplementationOnce(
        () => new Promise<GitCommit[]>((resolve) => { resolveMore = resolve; }),
      );
    const { rerender } = renderPanel();
    await screen.findByText("subject c0000");

    fireEvent.click(screen.getByTestId("git-history-load-more"));
    rerender(
      <GitHistoryPanel
        projectPath="/proj"
        active
        workspace={cleanWorkspace}
        onWorkspaceChanged={vi.fn()}
        onShowChanges={vi.fn()}
        refreshToken={1}
      />,
    );
    expect(mocks.gitLog).toHaveBeenCalledTimes(2);

    resolveMore([commit("older00000")]);
    await screen.findByText("subject older00000");
    expect(screen.getByTestId("git-history-count").textContent).toContain("201");
  });

  it("shows an empty state when the repository has no commits", async () => {
    mocks.gitLog.mockResolvedValue([]);
    renderPanel();
    await screen.findByText("No commits");
  });

  describe("split persistence", () => {
    async function selectFirstCommit() {
      await screen.findByText("subject h1aaaaaaaa");
      fireEvent.click(rowFor("h1aaaaaaaa"));
      await screen.findByTestId("git-history-detail");
    }

    it("defaults the detail pane to 40% of the split height", async () => {
      mocks.gitShow.mockResolvedValue([{ path: "src/changed.ts", status: "modified", patch: "" }]);
      renderPanel();
      await selectFirstCommit();

      const detail = screen.getByTestId("git-history-detail");
      expect(detail).toHaveStyle({ height: "40%" });
    });

    it("restores a persisted split ratio from localStorage", async () => {
      window.localStorage.setItem(SPLIT_KEY, "0.8");
      mocks.gitShow.mockResolvedValue([{ path: "src/changed.ts", status: "modified", patch: "" }]);
      renderPanel();
      await selectFirstCommit();

      const detail = screen.getByTestId("git-history-detail");
      // ratio 0.8 -> detail takes 1 - 0.8 = 0.2 = 20%.
      expect(detail).toHaveStyle({ height: "20%" });
    });

    it("clamps a persisted ratio below minRatio to the default", async () => {
      window.localStorage.setItem(SPLIT_KEY, "0.05");
      mocks.gitShow.mockResolvedValue([{ path: "src/changed.ts", status: "modified", patch: "" }]);
      renderPanel();
      await selectFirstCommit();

      const detail = screen.getByTestId("git-history-detail");
      expect(detail).toHaveStyle({ height: "40%" });
    });

    it("clamps a persisted ratio above maxRatio to the default", async () => {
      window.localStorage.setItem(SPLIT_KEY, "0.95");
      mocks.gitShow.mockResolvedValue([{ path: "src/changed.ts", status: "modified", patch: "" }]);
      renderPanel();
      await selectFirstCommit();

      const detail = screen.getByTestId("git-history-detail");
      expect(detail).toHaveStyle({ height: "40%" });
    });
  });
});
