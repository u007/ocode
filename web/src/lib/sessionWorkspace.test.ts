import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { GitWorktree } from "../api/types";
import {
  __resetSessionWorkspaceForTests,
  describeSessionWorkspace,
  useSessionWorkspace,
} from "./sessionWorkspace";

// ── Worktree fixture ────────────────────────────────────────────────────────
// Defaults describe a healthy linked checkout; each test overrides only the
// field it is asserting, so a new default cannot silently weaken a claim.
function wt(over: Partial<GitWorktree> = {}): GitWorktree {
  return {
    path: "/repo/.worktrees/feat-x",
    head: "a1b2c3d4e5f6",
    branch: "feat/x",
    detached: false,
    locked: false,
    prunable: false,
    is_main: false,
    is_current: true,
    exists: true,
    dirty: 0,
    ahead: 0,
    behind: 0,
    contained_in_base: false,
    base_ref: "main",
    base_known: true,
    owner_sessions: [{ session_id: "ses_1" }],
    owner_session_count: 1,
    last_commit: "2026-10-28T12:00:00+08:00",
    ...over,
  };
}

function main(over: Partial<GitWorktree> = {}): GitWorktree {
  return wt({
    path: "/repo",
    branch: "main",
    is_main: true,
    is_current: false,
    ahead: -1,
    behind: -1,
    contained_in_base: null,
    ...over,
  });
}

describe("describeSessionWorkspace", () => {
  it("renders no chip when nothing is known", () => {
    expect(describeSessionWorkspace("/repo", null)).toBeNull();
    expect(describeSessionWorkspace("", [wt()])).toBeNull();
    expect(describeSessionWorkspace("/repo", [])).toBeNull();
  });

  it("renders no chip when the cwd is not one of the listed checkouts", () => {
    // Unknown must never be guessed as "main".
    expect(describeSessionWorkspace("/elsewhere", [main(), wt()])).toBeNull();
  });

  it("labels the main checkout plainly and does not claim divergence", () => {
    const chip = describeSessionWorkspace("/repo", [main(), wt()]);
    expect(chip?.text).toBe("main");
    expect(chip?.tone).toBe("muted");
    expect(chip?.title).toContain("(main checkout)");
    expect(chip?.title).not.toContain("ahead");
    expect(chip?.title).not.toContain("contained");
  });

  it("names the linked worktree, its ahead count and its dirtiness", () => {
    const chip = describeSessionWorkspace(
      "/repo/.worktrees/feat-x",
      [main(), wt({ ahead: 2, dirty: 1 })],
    );
    expect(chip?.text).toBe("wt: feat/x ↑2 · 1 changed");
    expect(chip?.tone).toBe("warn");
    expect(chip?.title).toContain("2 ahead, 0 behind main");
    expect(chip?.title).toContain("not contained in main");
    expect(chip?.title).toContain("1 changed path");
  });

  it("reports containment honestly, naming the base and the ancestor check", () => {
    const chip = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ ahead: 0, behind: 0, contained_in_base: true }),
    ]);
    expect(chip?.text).toBe("wt: feat/x ✓ contained");
    expect(chip?.tone).toBe("ok");
    expect(chip?.title).toContain("contained in main");
    expect(chip?.title).toContain("squash/rebase merges are not detected");
  });

  it("does not claim containment when the server says unknown", () => {
    const chip = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ ahead: -1, behind: -1, contained_in_base: null }),
    ]);
    expect(chip?.text).not.toContain("contained");
    expect(chip?.title).not.toContain("contained");
  });

  it("shows a detached checkout by its short sha", () => {
    const chip = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ branch: "", detached: true, head: "deadbeefcafe" }),
    ]);
    expect(chip?.text).toBe("wt: detached deadbee");
  });

  it("flags a checkout whose directory is gone", () => {
    const chip = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ exists: false, dirty: -1, ahead: -1, behind: -1, contained_in_base: null }),
    ]);
    expect(chip?.text).toContain("(missing)");
    expect(chip?.tone).toBe("danger");
    expect(chip?.title).toContain("checkout directory is missing");
  });

  it("makes no derived claims against a server that does not report them", () => {
    // Version skew: an older server omits every derived field. The chip may
    // name the checkout, but must not invent clean/dirty/contained/ownership.
    const old: GitWorktree = {
      path: "/repo/.worktrees/feat-x",
      head: "a1b2c3d4e5f6",
      branch: "feat/x",
      detached: false,
      locked: false,
      prunable: false,
      is_main: false,
      is_current: true,
    };
    const chip = describeSessionWorkspace("/repo/.worktrees/feat-x", [main(), old]);
    expect(chip?.text).toBe("wt: feat/x");
    expect(chip?.title).not.toContain("clean");
    expect(chip?.title).not.toContain("changed");
    expect(chip?.title).not.toContain("no chat session");
    expect(chip?.title).toContain("base branch unknown");
  });

  it("names the owning chat sessions when the count is known", () => {
    const none = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ owner_sessions: [], owner_session_count: 0 }),
    ]);
    expect(none?.title).toContain("no chat session uses this checkout");

    const many = describeSessionWorkspace("/repo/.worktrees/feat-x", [
      main(),
      wt({ owner_session_count: 3 }),
    ]);
    expect(many?.title).toContain("3 chat sessions");
  });
});

// ── Store ───────────────────────────────────────────────────────────────────

const busFake = vi.hoisted(() => {
  const handlers = new Map<string, Set<(env: unknown) => void>>();
  return {
    on(name: string, cb: (env: unknown) => void) {
      let set = handlers.get(name);
      if (!set) {
        set = new Set();
        handlers.set(name, set);
      }
      set.add(cb);
      return () => {
        set?.delete(cb);
      };
    },
    emit(name: string, env: unknown) {
      for (const cb of handlers.get(name) ?? []) cb(env);
    },
    clear() {
      handlers.clear();
    },
  };
});

const apiFake = vi.hoisted(() => ({
  calls: [] as Array<{ project?: string }>,
  list: [] as unknown[],
  failNext: false,
}));

vi.mock("./eventBus", () => ({ eventBus: { on: busFake.on } }));
vi.mock("../api/client", () => ({
  api: {
    gitWorktrees: (project?: string) => {
      apiFake.calls.push({ project });
      if (apiFake.failNext) {
        apiFake.failNext = false;
        return Promise.reject(new Error("git worktrees unavailable"));
      }
      return Promise.resolve(apiFake.list);
    },
  },
}));

describe("useSessionWorkspace", () => {
  beforeEach(() => {
    apiFake.calls.length = 0;
    apiFake.list = [main(), wt()];
    apiFake.failNext = false;
    busFake.clear();
    __resetSessionWorkspaceForTests();
  });

  afterEach(() => {
    cleanup();
    __resetSessionWorkspaceForTests();
  });

  it("loads the project's checkouts once and reports ready", async () => {
    const { result } = renderHook(() => useSessionWorkspace("/repo"));
    expect(result.current.state).toBe("loading");
    expect(result.current.worktrees).toBeNull();
    await waitFor(() => expect(result.current.state).toBe("ready"));
    expect(result.current.worktrees).toHaveLength(2);
    expect(apiFake.calls).toEqual([{ project: "/repo" }]);
  });

  it("keeps the last known list when a refresh fails", async () => {
    const { result } = renderHook(() => useSessionWorkspace("/repo"));
    await waitFor(() => expect(result.current.state).toBe("ready"));

    apiFake.failNext = true;
    act(() => {
      busFake.emit("git_status", { project: "/repo", data: {} });
    });
    await waitFor(() => expect(result.current.state).toBe("error"));
    // Losing the only signal about where work is happening would be worse
    // than a slightly stale label.
    expect(result.current.worktrees).toHaveLength(2);
  });

  it("refreshes on a git_status event for its own project only", async () => {
    const { result } = renderHook(() => useSessionWorkspace("/repo"));
    await waitFor(() => expect(result.current.state).toBe("ready"));
    expect(apiFake.calls).toHaveLength(1);

    act(() => {
      busFake.emit("git_status", { project: "/other", data: {} });
    });
    expect(apiFake.calls).toHaveLength(1);

    act(() => {
      busFake.emit("git_status", { project: "/repo", data: {} });
    });
    await waitFor(() => expect(apiFake.calls).toHaveLength(2));
  });

  it("never fetches for a remote project and reports unavailable", () => {
    const { result } = renderHook(() => useSessionWorkspace("~/www/app", "me@host"));
    expect(result.current.state).toBe("unavailable");
    expect(result.current.worktrees).toBeNull();
    expect(apiFake.calls).toHaveLength(0);
  });
});
