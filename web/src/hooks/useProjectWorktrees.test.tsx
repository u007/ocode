import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { GitWorktree } from "@/api/types";
import { useProjectWorktrees } from "./useProjectWorktrees";

const mocks = vi.hoisted(() => ({ gitWorktrees: vi.fn() }));

vi.mock("@/api/client", () => ({ api: mocks }));

function worktree(
  path: string,
  overrides: Partial<GitWorktree> = {},
): GitWorktree {
  return {
    path,
    head: "aaaaaaa1111",
    branch: path.split("/").pop() ?? "",
    detached: false,
    locked: false,
    prunable: false,
    is_main: false,
    is_current: false,
    ...overrides,
  };
}

function page(count: number, offset: number): GitWorktree[] {
  return Array.from({ length: count }, (_, i) =>
    worktree(`/proj/wt-${offset + i}`),
  );
}

describe("useProjectWorktrees", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("walks every page with limit 500 until the server returns a short page", async () => {
    mocks.gitWorktrees.mockImplementation(
      async (_project: string, opts?: { limit?: number; offset?: number }) => {
        const offset = opts?.offset ?? 0;
        if (offset === 0) return page(500, 0);
        if (offset === 500) return page(2, 500);
        return [];
      },
    );

    const { result } = renderHook(() =>
      useProjectWorktrees("/proj-pagination"),
    );

    await waitFor(() => expect(result.current.loading).toBe(false));

    expect(result.current.worktrees).toHaveLength(502);
    expect(result.current.error).toBeNull();
    expect(mocks.gitWorktrees).toHaveBeenCalledTimes(2);
    expect(mocks.gitWorktrees).toHaveBeenNthCalledWith(1, "/proj-pagination", {
      limit: 500,
      offset: 0,
    });
    expect(mocks.gitWorktrees).toHaveBeenNthCalledWith(2, "/proj-pagination", {
      limit: 500,
      offset: 500,
    });
  });

  it("surfaces a failed page as an error with no checkouts", async () => {
    mocks.gitWorktrees.mockRejectedValue(new Error("not a git repository"));

    const { result } = renderHook(() => useProjectWorktrees("/proj-failure"));

    await waitFor(() =>
      expect(result.current.error).toBe("not a git repository"),
    );
    expect(result.current.loading).toBe(false);
    expect(result.current.worktrees).toEqual([]);
    expect(mocks.gitWorktrees).toHaveBeenCalledTimes(1);
  });

  it("caches per project and shares one in-flight request between mounts", async () => {
    mocks.gitWorktrees.mockResolvedValue([worktree("/proj-cache/main")]);

    const first = renderHook(() => useProjectWorktrees("/proj-cache"));
    const second = renderHook(() => useProjectWorktrees("/proj-cache"));

    await waitFor(() => expect(first.result.current.loading).toBe(false));
    await waitFor(() => expect(second.result.current.loading).toBe(false));
    expect(first.result.current.worktrees).toHaveLength(1);
    expect(second.result.current.worktrees).toHaveLength(1);
    expect(mocks.gitWorktrees).toHaveBeenCalledTimes(1);

    first.unmount();
    second.unmount();

    const cached = renderHook(() => useProjectWorktrees("/proj-cache"));
    expect(cached.result.current.loading).toBe(false);
    expect(cached.result.current.worktrees).toHaveLength(1);
    expect(mocks.gitWorktrees).toHaveBeenCalledTimes(1);

    act(() => cached.result.current.reload());
    await waitFor(() =>
      expect(cached.result.current.worktrees).toHaveLength(1),
    );
    expect(mocks.gitWorktrees).toHaveBeenCalledTimes(2);
  });

  it("drops a stale response after the project changes", async () => {
    let resolveStale: (value: GitWorktree[]) => void = () => {};
    mocks.gitWorktrees.mockImplementation((project: string) => {
      if (project === "/proj-stale-old") {
        return new Promise<GitWorktree[]>((resolve) => {
          resolveStale = resolve;
        });
      }
      return Promise.resolve([worktree("/proj-stale-new/wt")]);
    });

    const { result, rerender } = renderHook(
      ({ project }: { project: string }) => useProjectWorktrees(project),
      { initialProps: { project: "/proj-stale-old" } },
    );
    rerender({ project: "/proj-stale-new" });

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.worktrees.map((wt) => wt.path)).toEqual([
      "/proj-stale-new/wt",
    ]);

    await act(async () => {
      resolveStale([worktree("/proj-stale-old/wt")]);
    });
    expect(result.current.worktrees.map((wt) => wt.path)).toEqual([
      "/proj-stale-new/wt",
    ]);
  });

  it("loads nothing without a project", async () => {
    const { result } = renderHook(() => useProjectWorktrees(undefined));

    expect(result.current.worktrees).toEqual([]);
    expect(result.current.loading).toBe(false);
    expect(result.current.error).toBeNull();
    expect(mocks.gitWorktrees).not.toHaveBeenCalled();
  });
});
