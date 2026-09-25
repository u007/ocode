import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitOperation, GitStatus, GitWorkspace } from "@/api/types";

// The union the server's operation kinds must stay within. Derived from the
// API type rather than re-spelled, so adding a kind to the server is a
// compile error here instead of a silent drift.
type OperationKind = GitOperation["kind"];

// Phase 06 UI contract: the conflicts section and the operation banner.
//
// The highest-consequence detail in this feature is the ours/theirs inversion
// during a rebase. Git's "ours" is the UPSTREAM branch during a rebase and
// "theirs" is the user's own commit — the opposite of the intuitive reading.
// A user who reads "Use ours" as "keep my work" during a rebase would discard
// their commit while believing they had kept it. So the tests below assert the
// WORDING, not merely the presence of two buttons.

const mocks = vi.hoisted(() => ({
  getGitWorkspace: vi.fn(),
  gitLog: vi.fn(),
  gitHunk: vi.fn(),
  gitDiscard: vi.fn(),
  gitStage: vi.fn(),
  gitUnstage: vi.fn(),
  gitCommit: vi.fn(),
  gitPush: vi.fn(),
  gitFetch: vi.fn(),
  gitPull: vi.fn(),
  gitForcePush: vi.fn(),
  gitStash: vi.fn(),
  gitStashList: vi.fn(),
  gitStashShow: vi.fn(),
  gitStashApply: vi.fn(),
  gitStashDrop: vi.fn(),
  gitResetRemote: vi.fn(),
  gitResolveConflict: vi.fn(),
  gitOperation: vi.fn(),
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

import GitPanel from "./GitPanel";

beforeAll(() => {
  window.matchMedia = ((media: string) => ({
    get matches() {
      return false;
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

function ws(status: Partial<GitStatus>): GitWorkspace {
  return {
    status: {
      branch: "main",
      staged_files: [],
      changed_files: [],
      conflicts: [],
      has_changes: true,
      is_repo: true,
      ahead: 0,
      behind: 0,
      ...status,
    },
    staged: [],
    unstaged: [],
  } as GitWorkspace;
}

const conflicted = ws({
  conflicts: [
    { path: "src/a.ts", code: "UU", ours: true, theirs: true },
    { path: "src/deleted.ts", code: "UD", ours: true, theirs: false },
  ],
  has_changes: true,
});

beforeEach(() => {
  vi.clearAllMocks();
  mocks.gitStatusHandlers.length = 0;
  localStorage.clear();
  mocks.getGitWorkspace.mockResolvedValue(conflicted);
  mocks.gitLog.mockResolvedValue([]);
  mocks.gitStashList.mockResolvedValue([]);
  mocks.gitResolveConflict.mockResolvedValue(conflicted);
  mocks.gitOperation.mockResolvedValue({ workspace: ws({}) });
});

async function renderPanel() {
  render(<GitPanel projectPath="/proj" />);
  await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());
}

describe("conflicts section", () => {
  it("lists each conflicted file with its status code and three actions", async () => {
    await renderPanel();
    expect(await screen.findByText("src/a.ts")).toBeInTheDocument();
    expect(screen.getByText("src/deleted.ts")).toBeInTheDocument();
    // Both files must be offered a resolution; one action set per file.
    expect(screen.getAllByRole("button", { name: /use ours/i }).length).toBe(2);
    expect(screen.getAllByRole("button", { name: /use theirs/i }).length).toBe(2);
    expect(screen.getAllByRole("button", { name: /mark resolved/i }).length).toBe(2);
  });

  it("is absent when the repository has no conflicts", async () => {
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [] }));
    await renderPanel();
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /use ours/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/conflicts/i)).not.toBeInTheDocument();
  });

  it("calls the resolve endpoint with the path and resolution", async () => {
    await renderPanel();
    const row = (await screen.findByText("src/a.ts")).closest("div")!;
    fireEvent.click(within(row).getByRole("button", { name: /use ours/i }));
    await waitFor(() => {
      expect(mocks.gitResolveConflict).toHaveBeenCalledWith(
        { path: "src/a.ts", resolution: "ours" },
        "/proj",
        undefined,
      );
    });
  });

  it("marks a file resolved", async () => {
    await renderPanel();
    const row = (await screen.findByText("src/a.ts")).closest("div")!;
    fireEvent.click(within(row).getByRole("button", { name: /mark resolved/i }));
    await waitFor(() => {
      expect(mocks.gitResolveConflict).toHaveBeenCalledWith(
        { path: "src/a.ts", resolution: "mark" },
        "/proj",
        undefined,
      );
    });
  });

  // The dangerous inversion. During a rebase these labels MUST change.
  it("renames the side buttons during a rebase to say which side is which", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: conflicted.status.conflicts, operation: { kind: "rebase", label: "Rebasing feature (2/5)", step: 2, total: 5 } }),
    );
    await renderPanel();
    await screen.findByText("src/a.ts");

    // The rebase wording, not "ours"/"theirs".
    expect(screen.getAllByRole("button", { name: /keep upstream/i }).length).toBe(2);
    expect(screen.getAllByRole("button", { name: /keep my commit/i }).length).toBe(2);
    // The misleading wording must be GONE while a rebase is in progress.
    expect(screen.queryByRole("button", { name: /^use ours$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^use theirs$/i })).not.toBeInTheDocument();
  });

  it("uses the plain ours/theirs wording outside a rebase", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: conflicted.status.conflicts, operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await renderPanel();
    await screen.findByText("src/a.ts");
    expect(screen.getAllByRole("button", { name: /use ours/i }).length).toBe(2);
    expect(screen.queryByRole("button", { name: /keep upstream/i })).not.toBeInTheDocument();
  });
});

describe("operation banner", () => {
  it("shows the label with step progress", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "rebase", label: "Rebasing feature (2/5)", step: 2, total: 5 } }),
    );
    await renderPanel();
    expect(await screen.findByText(/Rebasing feature \(2\/5\)/)).toBeInTheDocument();
  });

  it("offers Continue and Abort for a merge, and disables Continue while conflicts remain", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: conflicted.status.conflicts, operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await renderPanel();
    const cont = await screen.findByRole("button", { name: /continue/i });
    expect(cont).toBeDisabled();
    // Abort is never disabled.
    expect(screen.getByRole("button", { name: /^abort$/i })).toBeEnabled();
  });

  it("enables Continue once the conflicts are gone", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await renderPanel();
    expect(await screen.findByRole("button", { name: /continue/i })).toBeEnabled();
  });

  it("shows Good, Bad, Skip and Reset for a bisect, and no Continue", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "bisect", label: "Bisecting", step: 0, total: 0 } }),
    );
    await renderPanel();
    expect(await screen.findByRole("button", { name: /^good$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^bad$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^skip$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^reset$/i })).toBeInTheDocument();
    // A bisect has no "continue"; showing one would be a lie.
    expect(screen.queryByRole("button", { name: /^continue$/i })).not.toBeInTheDocument();
  });

  it("shows no banner at all when idle", async () => {
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [] }));
    await renderPanel();
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());
    expect(screen.queryByRole("button", { name: /^continue$/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^abort$/i })).not.toBeInTheDocument();
  });

  it("sends the detected kind with the action, never a client-chosen one", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "am", label: "Applying patch series", step: 0, total: 0 } }),
    );
    await renderPanel();
    // Abort is destructive, so it opens the confirmation dialog first; the
    // call must not fire until the user confirms.
    fireEvent.click(await screen.findByRole("button", { name: /^abort$/i }));
    expect(mocks.gitOperation).not.toHaveBeenCalled();

    fireEvent.click(await screen.findByRole("button", { name: /^abort$/i, hidden: false }));
    await waitFor(() => {
      expect(mocks.gitOperation).toHaveBeenCalledWith(
        { action: "abort", kind: "am" },
        "/proj",
        undefined,
      );
    });
  });

  it("does not fire a destructive operation when the dialog is cancelled", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: /^abort$/i }));
    fireEvent.click(await screen.findByRole("button", { name: /^cancel$/i }));
    expect(mocks.gitOperation).not.toHaveBeenCalled();
  });

  it("fires a non-destructive operation immediately, with no confirmation", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "rebase", label: "Rebasing", step: 0, total: 0 } }),
    );
    await renderPanel();
    fireEvent.click(await screen.findByRole("button", { name: /^skip$/i }));
    await waitFor(() => {
      expect(mocks.gitOperation).toHaveBeenCalledWith(
        { action: "skip", kind: "rebase" },
        "/proj",
        undefined,
      );
    });
  });
});

describe("mid-operation gating and the stale-error transition", () => {
  it("disables commit, pull, push and stash while an operation is in progress", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], staged_files: ["a.ts"], operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await renderPanel();
    await screen.findByTestId("git-operation-banner");
    for (const label of [/^commit$/i, /commit & push/i, /pull from remote/i, /push to remote/i, /fetch all remotes/i, /reset to remote/i]) {
      expect(screen.getByRole("button", { name: label })).toBeDisabled();
    }
  });

  it("leaves those controls enabled when idle", async () => {
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [], staged_files: ["a.ts"] }));
    await renderPanel();
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());
    // Commit additionally requires a message, so give it one; this test is
    // about the operation gate, not the message precondition.
    fireEvent.change(screen.getByPlaceholderText(/commit message/i), { target: { value: "msg" } });
    expect(screen.getByRole("button", { name: /^commit$/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /pull from remote/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /push to remote/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /reset to remote/i })).toBeEnabled();
  });

  it("clears a stale pull error on the idle-to-operation transition", async () => {
    mocks.gitPull.mockRejectedValue(new Error("git pull failed: CONFLICT (content): Merge conflict in a.ts"));
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [] }));
    render(<GitPanel projectPath="/proj" />);
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: /pull from remote/i }));
    expect(await screen.findByText(/git pull failed: CONFLICT/)).toBeInTheDocument();

    // The pull actually stopped on a conflict: the next poll reports the
    // operation, and the stale error must go with it.
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [{ path: "a.ts", code: "UU", ours: true, theirs: true }], operation: { kind: "merge", label: "Merging", step: 0, total: 0 } }),
    );
    await act(async () => {
      for (const h of mocks.gitStatusHandlers) h({ project: "/proj" });
      await Promise.resolve();
    });
    await waitFor(() => expect(screen.queryByText(/git pull failed: CONFLICT/)).not.toBeInTheDocument());
    expect(await screen.findByTestId("git-operation-banner")).toBeInTheDocument();
  });

  it("keeps an unrelated sticky error across a background refresh", async () => {
    mocks.gitPush.mockRejectedValue(new Error("git push failed: rejected"));
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [] }));
    render(<GitPanel projectPath="/proj" />);
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());

    fireEvent.click(screen.getByRole("button", { name: /push to remote/i }));
    expect(await screen.findByText(/git push failed: rejected/)).toBeInTheDocument();

    // A background refresh with NO operation must not clear it — this is the
    // regression the sticky-error contract exists to prevent.
    await act(async () => {
      for (const h of mocks.gitStatusHandlers) h({ project: "/proj" });
      await Promise.resolve();
    });
    expect(screen.getByText(/git push failed: rejected/)).toBeInTheDocument();
  });
});

describe("older-server tolerance", () => {
  // A server predating this feature omits `conflicts` (and `operation`) from
  // the JSON. Both are read during render, so an undefined there would crash
  // the whole panel — taking the entire Git tab down against a mixed-version
  // deployment. This pins the normalization.
  it("renders without crashing when the server omits conflicts and operation", async () => {
    const legacy = {
      ...ws({}),
      status: {
        branch: "main",
        staged_files: [],
        changed_files: [],
        has_changes: false,
        is_repo: true,
        ahead: 0,
        behind: 0,
      },
    } as unknown as GitWorkspace;
    mocks.getGitWorkspace.mockResolvedValue(legacy);
    render(<GitPanel projectPath="/proj" />);
    await waitFor(() => expect(mocks.getGitWorkspace).toHaveBeenCalled());
    expect(screen.queryByTestId("git-conflicts")).not.toBeInTheDocument();
    expect(screen.queryByTestId("git-operation-banner")).not.toBeInTheDocument();
  });
});

// Phase 07 badge parity. The Git tab's own header summary is the THIRD surface
// that reports "how much work is in this repository" (the other two are the
// session tab badge in TopTabs and the project sidebar in projectGitCounts).
// Phase 02 removed conflicted paths from staged_files/changed_files, so a halted
// merge made this summary read "0 staged · 0 unstaged" directly above a
// "Conflicts 3" section — plausible-looking, and silently wrong. The term is
// shown only when conflicts exist, mirroring the TopTabs badge title.
describe("header summary parity", () => {
  it("includes the conflicted count", async () => {
    await renderPanel();
    // The count is its own styled span, so assert on the summary's combined
    // textContent rather than on a single text node.
    await waitFor(() =>
      expect(screen.getByTestId("git-summary-counts").textContent).toBe(
        "2 conflicted · 0 staged · 0 unstaged",
      ),
    );
  });
  it("omits the conflicted term entirely when there are none", async () => {
    mocks.getGitWorkspace.mockResolvedValue(ws({ conflicts: [] }));
    await renderPanel();
    expect(await screen.findByText("0 staged · 0 unstaged")).toBeInTheDocument();
    expect(screen.queryByText(/conflicted ·/)).not.toBeInTheDocument();
  });
});

// Phase 07 review fixes. Two defects were found by comparing the client's
// hand-copied OPERATION_ACTIONS table against the server's gitOperationCommand:
//
//  1. The client sent action "reset" for a bisect's Reset button, but the
//     server's table spells that action "abort" (it maps to `git bisect
//     reset`). Every click returned 400.
//  2. The client treated "am" like a rebase when labelling the conflict-side
//     buttons. Verified in a scratch repo with `git am -3`: stage 2 ("ours")
//     is the current HEAD and stage 3 ("theirs") is the incoming patch —
//     cherry-pick semantics, NOT rebase semantics. Labelling am's sides
//     "Keep upstream / Keep my commit" pointed the user at the wrong side.
describe("operation action vocabulary", () => {
  it("posts action=abort for a bisect Reset, which the server maps to `git bisect reset`", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({ conflicts: [], operation: { kind: "bisect", label: "Bisecting", step: 0, total: 0 } }),
    );
    await renderPanel();
    // Reset is destructive, so it confirms first; the dialog's confirm button
    // carries the same label.
    fireEvent.click(await screen.findByRole("button", { name: /^reset$/i }));
    expect(mocks.gitOperation).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: /^reset$/i }));
    await waitFor(() => {
      expect(mocks.gitOperation).toHaveBeenCalledWith(
        { action: "abort", kind: "bisect" },
        "/proj",
        undefined,
      );
    });
    // The wire action is the server's verb. A regression to "reset" is the
    // exact defect this test exists to catch.
    const calls = mocks.gitOperation.mock.calls;
    expect(calls[calls.length - 1]?.[0]).toEqual({
      action: "abort",
      kind: "bisect",
    });
  });
  it("labels am's conflict sides with git's own ours/theirs, not the rebase wording", async () => {
    mocks.getGitWorkspace.mockResolvedValue(
      ws({
        conflicts: conflicted.status.conflicts,
        operation: { kind: "am", label: "Applying patch series", step: 0, total: 0 },
      }),
    );
    await renderPanel();
    // One button per conflicted file, so query all of them.
    await waitFor(() =>
      expect(screen.getAllByRole("button", { name: /use ours/i })).toHaveLength(
        conflicted.status.conflicts.length,
      ),
    );
    expect(
      screen.getAllByRole("button", { name: /use theirs/i }),
    ).toHaveLength(conflicted.status.conflicts.length);
    // The rebase-only wording must not appear during an am.
    expect(screen.queryByRole("button", { name: /keep upstream/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /keep my commit/i })).not.toBeInTheDocument();
  });

  it("keeps the rebase wording, which IS inverted, for rebase and rebase-interactive", async () => {
    for (const kind of ["rebase", "rebase-interactive"] as const) {
      mocks.getGitWorkspace.mockResolvedValue(
        ws({
          conflicts: conflicted.status.conflicts,
          operation: { kind, label: "Rebasing", step: 0, total: 0 },
        }),
      );
      const view = render(<GitPanel projectPath="/proj" />);
      // One button per conflicted file, so query all of them.
      await waitFor(() =>
        expect(screen.getAllByRole("button", { name: /keep upstream/i })).toHaveLength(
          conflicted.status.conflicts.length,
        ),
      );
      expect(
        screen.getAllByRole("button", { name: /keep my commit/i }),
      ).toHaveLength(conflicted.status.conflicts.length);
      view.unmount();
    }
  });
});

// The client's OPERATION_ACTIONS is a hand copy of the server's
// gitOperationCommand table (internal/server/handler_git_conflicts.go, the
// `table` map). The bisect/reset drift above is the proof that the copy rots
// silently: the panel rendered, the button existed, and the server answered
// 400. This pins the wire action for every button of every kind, so the next
// edit to either table has to move both.
const SERVER_ACTION_TABLE: Record<OperationKind, { label: string; action: string }[]> = {
  merge: [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
  ],
  rebase: [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
    { label: "Skip", action: "skip" },
  ],
  "rebase-interactive": [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
    { label: "Skip", action: "skip" },
  ],
  am: [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
    { label: "Skip", action: "skip" },
  ],
  "cherry-pick": [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
    { label: "Skip", action: "skip" },
  ],
  revert: [
    { label: "Continue", action: "continue" },
    { label: "Abort", action: "abort" },
    { label: "Skip", action: "skip" },
  ],
  bisect: [
    { label: "Good", action: "good" },
    { label: "Bad", action: "bad" },
    { label: "Skip", action: "skip" },
    // git calls it `git bisect reset`; the server's action verb is "abort".
    { label: "Reset", action: "abort" },
  ],
};

describe("client table matches the server action table", () => {
  // Object.entries widens keys to string; re-narrow them so `kind` is the
  // union the API accepts (tsconfig targets ES2020, so no Iterator Helpers).
  const kinds = Object.entries(SERVER_ACTION_TABLE) as [
    OperationKind,
    { label: string; action: string }[],
  ][];
  for (const [kind, buttons] of kinds) {
    it(`posts the server's actions for ${kind}`, async () => {
      mocks.getGitWorkspace.mockResolvedValue(
        ws({ conflicts: [], operation: { kind, label: "Working", step: 0, total: 0 } }),
      );
      await renderPanel();
      for (const { label, action } of buttons) {
        const re = new RegExp(`^${label}$`, "i");
        fireEvent.click(await screen.findByRole("button", { name: re }));
        // A destructive action opens a confirm dialog carrying the same label.
        const confirm = screen.queryByRole("button", { name: re });
        if (confirm) fireEvent.click(confirm);
        await waitFor(() => {
          expect(mocks.gitOperation).toHaveBeenCalledWith(
            { action, kind },
            "/proj",
            undefined,
          );
        });
      }
    });
  }
});
