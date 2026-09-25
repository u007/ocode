import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { GitStatus } from "../api/types";
import {
  GIT_COUNTS_POLL_MS,
  NO_GIT_COUNTS,
  __resetProjectGitCountsForTests,
  useProjectGitCounts,
} from "./projectGitCounts";

// ── Mocks ───────────────────────────────────────────────────────────────────
// api.getGitStatus is the only client call the store makes; eventBus is
// stubbed so no real SSE stream starts from a test.

const apiFake = vi.hoisted(() => ({
  calls: [] as Array<{ project?: string; host?: string }>,
  status: null as GitStatus | null,
  failNext: false,
}));

vi.mock("../api/client", () => ({
  api: {
    getGitStatus: (project?: string, host?: string) => {
      apiFake.calls.push({ project, host });
      if (apiFake.failNext) {
        apiFake.failNext = false;
        return Promise.reject(new Error("git status unavailable"));
      }
      return Promise.resolve(
        apiFake.status ?? {
          branch: "main",
          staged_files: [],
          changed_files: [],
          has_changes: false,
          is_repo: true,
          ahead: 0,
          behind: 0,
          has_upstream: false,
        },
      );
    },
  },
}));

const busFake = vi.hoisted(() => ({
  handlers: new Map<string, Set<(env: unknown) => void>>(),
}));

vi.mock("./eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      let set = busFake.handlers.get(event);
      if (!set) {
        set = new Set();
        busFake.handlers.set(event, set);
      }
      set.add(handler);
      return () => busFake.handlers.get(event)?.delete(handler);
    },
  },
}));

function statusOf(over: Partial<GitStatus> = {}): GitStatus {
  return {
    branch: "main",
    staged_files: [],
    changed_files: [],
    conflicts: [],
    has_changes: true,
    is_repo: true,
    ahead: 0,
    behind: 0,
    has_upstream: false,
    ...over,
  };
}

function emitGitStatus(project: string, data: GitStatus = statusOf()): void {
  for (const handler of busFake.handlers.get("git_status") ?? []) {
    handler({ event: "git_status", project, seq: 1, data });
  }
}

/** Flush pending microtasks (and any setState they triggered). */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

let errorSpy: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  apiFake.calls.length = 0;
  apiFake.status = null;
  apiFake.failNext = false;
  busFake.handlers.clear();
  __resetProjectGitCountsForTests();
  errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
});

afterEach(() => {
  cleanup();
  __resetProjectGitCountsForTests();
  vi.useRealTimers();
  errorSpy.mockRestore();
});

// ── Tests ───────────────────────────────────────────────────────────────────

describe("useProjectGitCounts", () => {
  it("fetches on first subscription and reports staged, unstaged, and their total", async () => {
    apiFake.status = statusOf({ staged_files: ["a.ts"], changed_files: ["b.ts", "c.ts"] });
    const { result } = renderHook(() => useProjectGitCounts("/proj"));

    await waitFor(() => expect(result.current.total).toBe(3));
    expect(result.current.staged).toBe(1);
    expect(result.current.unstaged).toBe(2);
    expect(result.current.isRepo).toBe(true);
    expect(apiFake.calls).toEqual([{ project: "/proj", host: undefined }]);
  });

  it("shares one request across every subscriber of the same project (row + rail)", async () => {
    apiFake.status = statusOf({ changed_files: ["only.ts"] });
    const row = renderHook(() => useProjectGitCounts("/proj"));
    const rail = renderHook(() => useProjectGitCounts("/proj"));

    await waitFor(() => expect(rail.result.current.total).toBe(1));
    expect(row.result.current.total).toBe(1);
    expect(apiFake.calls.length).toBe(1);
  });

  it("counts conflicted files in the total, and reports them separately", async () => {
    // A conflicted path is deliberately absent from staged_files and
    // changed_files (git listed it three times across those two lists), so the
    // badge must add the conflicts list or it silently undercounts exactly
    // when the user most needs to know something needs resolving.
    apiFake.status = statusOf({
      staged_files: ["a.ts"],
      changed_files: ["b.ts"],
      conflicts: [
        { path: "c.ts", code: "UU", ours: true, theirs: true },
        { path: "d.ts", code: "UD", ours: true, theirs: false },
      ],
    });
    const { result } = renderHook(() => useProjectGitCounts("/proj"));

    await waitFor(() => expect(result.current.total).toBe(4));
    expect(result.current.staged).toBe(1);
    expect(result.current.unstaged).toBe(1);
    expect(result.current.conflicted).toBe(2);
  });

  it("treats a status from a server without the conflicts field as zero, not a crash", async () => {
    // An older server omits the field entirely. The badge must keep working.
    const legacy = statusOf({ changed_files: ["b.ts"] });
    delete (legacy as { conflicts?: unknown }).conflicts;
    apiFake.status = legacy;

    const { result } = renderHook(() => useProjectGitCounts("/proj"));
    await waitFor(() => expect(result.current.total).toBe(1));
    expect(result.current.conflicted).toBe(0);
  });

  it("keeps entries separate per host, so one path on two machines never shares counts", async () => {
    apiFake.status = statusOf({ changed_files: ["local.ts"] });
    const local = renderHook(() => useProjectGitCounts("/same/path"));
    apiFake.status = statusOf({ staged_files: ["r1.ts", "r2.ts"] });
    const remote = renderHook(() => useProjectGitCounts("/same/path", "devbox"));

    await waitFor(() => expect(remote.result.current.staged).toBe(2));
    await waitFor(() => expect(local.result.current.unstaged).toBe(1));
    expect(local.result.current.staged).toBe(0);
    expect(apiFake.calls).toEqual([
      { project: "/same/path", host: undefined },
      { project: "/same/path", host: "devbox" },
    ]);
  });

  it("re-reads through the entry's own host when the bus pushes git_status for the project", async () => {
    apiFake.status = statusOf({ changed_files: ["one.ts"] });
    const { result } = renderHook(() => useProjectGitCounts("/proj", "devbox"));
    await waitFor(() => expect(result.current.total).toBe(1));

    apiFake.status = statusOf({ changed_files: ["one.ts", "two.ts"] });
    act(() => emitGitStatus("/proj"));

    await waitFor(() => expect(result.current.total).toBe(2));
    // The envelope has no host and its payload is never applied directly —
    // the refresh must go out with the entry's own host.
    expect(apiFake.calls[apiFake.calls.length - 1]).toEqual({ project: "/proj", host: "devbox" });
  });

  it("ignores a git_status event for a different project", async () => {
    apiFake.status = statusOf({ changed_files: ["one.ts"] });
    const { result } = renderHook(() => useProjectGitCounts("/proj"));
    await waitFor(() => expect(result.current.total).toBe(1));

    act(() => emitGitStatus("/other"));
    await flush();
    expect(apiFake.calls.length).toBe(1);
  });

  it("never fetches while disabled, and reports no changes until it is enabled", async () => {
    apiFake.status = statusOf({ changed_files: ["remote.ts"] });
    const { result, rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) => useProjectGitCounts("/proj", "devbox", enabled),
      { initialProps: { enabled: false } },
    );

    await flush();
    expect(apiFake.calls.length).toBe(0);
    expect(result.current).toBe(NO_GIT_COUNTS);

    rerender({ enabled: true });
    await waitFor(() => expect(result.current.total).toBe(1));

    // Host disconnects: the badge must clear rather than show stale counts.
    rerender({ enabled: false });
    await flush();
    expect(result.current).toBe(NO_GIT_COUNTS);
    expect(apiFake.calls.length).toBe(1);
  });

  it("keeps the last known counts when a refresh fails", async () => {
    apiFake.status = statusOf({ changed_files: ["one.ts", "two.ts"] });
    const { result } = renderHook(() => useProjectGitCounts("/proj"));
    await waitFor(() => expect(result.current.total).toBe(2));

    apiFake.failNext = true;
    act(() => emitGitStatus("/proj"));
    await waitFor(() => expect(apiFake.calls.length).toBe(2));
    await flush();

    expect(result.current.total).toBe(2);
    expect(errorSpy).toHaveBeenCalled();
  });

  it("polls on the interval while mounted and stops after the last subscriber unmounts", async () => {
    vi.useFakeTimers();
    const { unmount } = renderHook(() => useProjectGitCounts("/proj"));
    await flush();
    expect(apiFake.calls.length).toBe(1);

    await act(async () => {
      vi.advanceTimersByTime(GIT_COUNTS_POLL_MS);
    });
    await flush();
    expect(apiFake.calls.length).toBe(2);

    unmount();
    await act(async () => {
      vi.advanceTimersByTime(GIT_COUNTS_POLL_MS * 5);
    });
    await flush();
    expect(apiFake.calls.length).toBe(2);
    // The bus subscription is released with the last row too.
    expect(busFake.handlers.get("git_status")?.size ?? 0).toBe(0);
  });

  it("re-probes a directory that is not a repo only every 5 minutes", async () => {
    vi.useFakeTimers();
    apiFake.status = statusOf({ is_repo: false, has_changes: false });
    renderHook(() => useProjectGitCounts("/not-a-repo"));
    await flush();
    expect(apiFake.calls.length).toBe(1);

    // Four poll ticks inside the backoff window: no request.
    await act(async () => {
      vi.advanceTimersByTime(GIT_COUNTS_POLL_MS * 4);
    });
    await flush();
    expect(apiFake.calls.length).toBe(1);

    // The fifth tick reaches the backoff horizon: probe again.
    await act(async () => {
      vi.advanceTimersByTime(GIT_COUNTS_POLL_MS);
    });
    await flush();
    expect(apiFake.calls.length).toBe(2);
  });

  it("reports a clean repo as zero changes (the badge never renders for it)", async () => {
    apiFake.status = statusOf({ has_changes: false });
    const { result } = renderHook(() => useProjectGitCounts("/clean"));
    await waitFor(() => expect(result.current.isRepo).toBe(true));
    expect(result.current.total).toBe(0);
    expect(result.current.staged).toBe(0);
    expect(result.current.unstaged).toBe(0);
  });
});
