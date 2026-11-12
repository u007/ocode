import "fake-indexeddb/auto";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "./projectStore";
import { syncProjectToCache } from "../lib/offlineSync";
import { OfflineDB } from "../lib/offlineDb";

const projectApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  renameProject: vi.fn(),
  reorderProjects: vi.fn(),
  setProjectGroup: vi.fn(),
  setProjectProfile: vi.fn(),
  removeProject: vi.fn(),
  deleteGroup: vi.fn(),
  removeRemoteProject: vi.fn(),
  updateRemoteProject: vi.fn(),
  duplicateProjectAsRemote: vi.fn(),
  getTabs: vi.fn(),
  setTabs: vi.fn(),
}));

vi.mock("../api/client", () => ({
  api: {
    listProjects: projectApi.listProjects,
    getCurrentProject: projectApi.getCurrentProject,
    listProjectSessions: projectApi.listProjectSessions,
    listGroups: projectApi.listGroups,
    renameProject: projectApi.renameProject,
    reorderProjects: projectApi.reorderProjects,
    setProjectGroup: projectApi.setProjectGroup,
    setProjectProfile: projectApi.setProjectProfile,
    removeProject: projectApi.removeProject,
    deleteGroup: projectApi.deleteGroup,
    removeRemoteProject: projectApi.removeRemoteProject,
    updateRemoteProject: projectApi.updateRemoteProject,
    duplicateProjectAsRemote: projectApi.duplicateProjectAsRemote,
    getTabs: projectApi.getTabs,
    setTabs: projectApi.setTabs,
  },
}));

const busHandlers = vi.hoisted(() => new Map<string, (env: unknown) => void>());
const reconnectHandlers = vi.hoisted(() => new Set<() => void>());
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      busHandlers.set(event, handler);
      return () => busHandlers.delete(event);
    },
    onReconnect: (handler: () => void) => {
      reconnectHandlers.add(handler);
      return () => {
        reconnectHandlers.delete(handler);
      };
    },
  },
}));

beforeEach(async () => {
  // clear IndexedDB
  const { OfflineDB } = await import("../lib/offlineDb");
  const db = new OfflineDB();
  await db.clearAll();
  projectApi.listProjects.mockReset().mockRejectedValue(new Error("offline"));
  projectApi.getCurrentProject.mockReset().mockResolvedValue(null);
  projectApi.listProjectSessions.mockReset().mockResolvedValue([]);
  projectApi.listGroups.mockReset().mockResolvedValue([]);
  projectApi.getTabs.mockReset().mockResolvedValue({ projects: {} });
  busHandlers.clear();
  reconnectHandlers.clear();
  window.localStorage.clear();
});

/** Lets pending IndexedDB work and promise chains finish. Uses the real
 *  setImmediate, so it still works while setTimeout is faked. */
async function settle() {
  for (let i = 0; i < 10; i++) {
    await act(async () => {
      await new Promise<void>((resolve) => setImmediate(resolve));
    });
  }
}

function setup() {
  return renderHook(() => useProjectState(), {
    wrapper: ({ children }) => <ProjectProvider>{children}</ProjectProvider>,
  });
}

describe("projectStore offline hydration", () => {
  it("hydrates cached projects when initial load fails", async () => {
    await syncProjectToCache({ path: "/cached", name: "cached" }, 0);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(1));
    expect(result.current.state.projects[0].path).toBe("/cached");
    expect(result.current.state.projectsStatus).toBe("error");
  });

  it("stays empty when cache is empty", async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projectsStatus).toBe("error"));
    expect(result.current.state.projects).toEqual([]);
    expect(result.current.state.offline).toBe(true);
  });

  it("clears offline flag on successful reload", async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.state.offline).toBe(true));
    projectApi.listProjects.mockReset().mockResolvedValue([{ path: "/p", name: "p" }]);
    await act(async () => {
      await result.current.refreshProjects();
    });
    await waitFor(() => expect(result.current.state.offline).toBe(false));
    expect(result.current.state.projects[0].path).toBe("/p");
  });

  it("does not hydrate cached projects on a 401 (the server answered)", async () => {
    await syncProjectToCache({ path: "/cached", name: "cached" }, 0);
    projectApi.listProjects
      .mockReset()
      .mockRejectedValue(Object.assign(new Error("Unauthorized"), { status: 401 }));
    const { result } = setup();
    // The failure is applied synchronously. A hydrate would arrive later, so
    // waiting for "error" would also wait out a buggy hydrate.
    await waitFor(() => expect(result.current.state.projectsStatus).toBe("error"));
    await settle();
    expect(result.current.state.projects).toEqual([]);
    expect(result.current.state.offline).toBe(false);
  });

  it("drops the cached list when a later refresh is refused with 401", async () => {
    await syncProjectToCache({ path: "/cached", name: "cached" }, 0);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(1));
    expect(result.current.state.offline).toBe(true);

    projectApi.listProjects
      .mockReset()
      .mockRejectedValue(Object.assign(new Error("Unauthorized"), { status: 401 }));
    await act(async () => {
      await result.current.refreshProjects();
    });

    await waitFor(() => expect(result.current.state.offline).toBe(false));
    expect(result.current.state.projects).toEqual([]);
  });

  it("keeps a local and a remote project that share a path as separate cache entries", async () => {
    await syncProjectToCache({ path: "/same", name: "local" }, 0);
    await syncProjectToCache({ path: "/same", host: "dev@box", name: "remote" }, 1);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));
    expect(result.current.state.projects.map((p) => p.name)).toEqual(["local", "remote"]);
  });

  it("prunes cache records the server no longer lists after a successful load", async () => {
    await syncProjectToCache({ path: "/gone", name: "gone" }, 0);
    projectApi.listProjects.mockReset().mockResolvedValue([{ path: "/kept", name: "kept" }]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects[0]?.path).toBe("/kept"));
    await waitFor(async () => {
      expect(await new OfflineDB().getProject("/gone")).toBeUndefined();
    });
  });

  it("does not prune the cache when the project list request fails", async () => {
    await syncProjectToCache({ path: "/cached", name: "cached" }, 0);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(1));
    expect(await new OfflineDB().getProject("/cached")).toBeDefined();
  });

  it("recovers from offline on a timer, with no focus or online event", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);

      projectApi.listProjects.mockReset().mockResolvedValue([{ path: "/p", name: "p" }]);
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      await settle();

      expect(result.current.state.offline).toBe(false);
      expect(result.current.state.projects[0].path).toBe("/p");

      // Once recovered, the retry timer stops.
      const callsAfterRecovery = projectApi.listProjects.mock.calls.length;
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(callsAfterRecovery);
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not start the next retry while a hung attempt is inside its timeout", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);

      // The retry hangs and never settles. mockReset clears the call history,
      // so the counts below are attempts made since this point.
      projectApi.listProjects.mockReset().mockReturnValue(new Promise(() => {}));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);

      // Still inside the 30 s bound: no second attempt yet.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(29_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);
      expect(result.current.state.offline).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("schedules another attempt after a hung one, and a later success clears offline", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);

      // Attempt 2 never settles; attempt 3 succeeds.
      projectApi.listProjects
        .mockReset()
        .mockReturnValueOnce(new Promise(() => {}))
        .mockResolvedValue([{ path: "/p", name: "p" }]);

      // Counts are attempts since the mockReset above.
      // t = 10 s: attempt 2 starts and hangs.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);

      // t = 40 s: the 30 s bound gives up on attempt 2; t = 50 s: attempt 3.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(40_000);
      });
      await settle();
      expect(projectApi.listProjects).toHaveBeenCalledTimes(2);
      expect(result.current.state.offline).toBe(false);
      expect(result.current.state.projects[0].path).toBe("/p");

      // Once recovered, no further attempts.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("stops the retry chain on unmount while an attempt is hung", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result, unmount } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);

      projectApi.listProjects.mockReset().mockReturnValue(new Promise(() => {}));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);

      unmount();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(120_000);
      });

      expect(projectApi.listProjects).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });

  it("stops the retry timer on unmount", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result, unmount } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);
      projectApi.listProjects.mockReset();

      unmount();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(60_000);
      });

      expect(projectApi.listProjects).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("refreshes projects when the event bus reconnects", async () => {
    const { result } = setup();
    await settle();
    expect(result.current.state.offline).toBe(true);

    projectApi.listProjects.mockReset().mockResolvedValue([{ path: "/p", name: "p" }]);
    await act(async () => {
      for (const handler of reconnectHandlers) handler();
    });
    await settle();

    expect(result.current.state.offline).toBe(false);
    expect(result.current.state.projects[0].path).toBe("/p");
  });
});

describe("projectStore cache on project removal", () => {
  it("deletes the removed project's cache record after the server confirms, before the follow-up list", async () => {
    await syncProjectToCache({ path: "/gone", name: "gone" }, 0);
    await syncProjectToCache({ path: "/kept", name: "kept" }, 1);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));

    // The follow-up list fails as offline, and records what the cache held
    // at the moment it was asked.
    let cacheSeenByFollowUp: unknown = "list not called";
    projectApi.removeProject.mockReset().mockResolvedValue({ status: "ok" });
    projectApi.listProjects.mockReset().mockImplementation(async () => {
      cacheSeenByFollowUp = await new OfflineDB().getProject("/gone");
      throw new Error("offline");
    });

    await act(async () => {
      await result.current.removeProject("/gone");
    });

    expect(projectApi.removeProject).toHaveBeenCalledWith("/gone");
    expect(cacheSeenByFollowUp).toBeUndefined();
    expect(await new OfflineDB().getProject("/gone")).toBeUndefined();
    expect(await new OfflineDB().getProject("/kept")).toBeDefined();
    // The removed project does not come back from the cache.
    expect(result.current.state.projects.map((p) => p.path)).toEqual(["/kept"]);
  });

  it("deletes only the host-qualified record when a remote project is removed", async () => {
    await syncProjectToCache({ path: "/same", name: "local" }, 0);
    await syncProjectToCache({ path: "/same", host: "dev@box", name: "remote" }, 1);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));

    projectApi.removeRemoteProject.mockReset().mockResolvedValue({ status: "ok" });
    await act(async () => {
      await result.current.removeProject("/same", "dev@box");
    });

    expect(projectApi.removeRemoteProject).toHaveBeenCalledWith("/same", "dev@box");
    expect(await new OfflineDB().getProject("dev@box::/same")).toBeUndefined();
    expect(await new OfflineDB().getProject("/same")).toBeDefined();
  });

  it("leaves the cache record when the removal fails", async () => {
    await syncProjectToCache({ path: "/gone", name: "gone" }, 0);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(1));

    projectApi.removeProject
      .mockReset()
      .mockRejectedValue(Object.assign(new Error("Server error"), { status: 500 }));
    await act(async () => {
      await expect(result.current.removeProject("/gone")).rejects.toThrow("Server error");
    });

    expect(await new OfflineDB().getProject("/gone")).toBeDefined();
  });

  it("deletes the old host-qualified record after a successful remote edit moves the project", async () => {
    await syncProjectToCache({ path: "/old", host: "dev@box", name: "old" }, 0);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(1));

    projectApi.updateRemoteProject
      .mockReset()
      .mockResolvedValue({ path: "/new", host: "dev@box", name: "old" });
    await act(async () => {
      await result.current.updateRemoteProject({
        old_host: "dev@box",
        old_path: "/old",
        path: "/new",
        kind: "ssh",
      } as any);
    });

    expect(await new OfflineDB().getProject("dev@box::/old")).toBeUndefined();
  });
});

/** A list request that never answers on its own, but rejects once its signal
 *  aborts, the way fetch does. */
function hangUntilAborted() {
  return (signal?: AbortSignal) =>
    new Promise((_resolve, reject) => {
      signal?.addEventListener("abort", () =>
        reject(Object.assign(new Error("The operation was aborted."), { name: "AbortError" })),
      );
    });
}

describe("projectStore offline retry abort", () => {
  it("aborts the attempt's request when the 30 s bound gives up on it", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { result } = setup();
      await settle();
      expect(result.current.state.offline).toBe(true);

      projectApi.listProjects.mockReset().mockImplementation(hangUntilAborted());
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      const signal = projectApi.listProjects.mock.calls[0][0] as AbortSignal;
      expect(signal).toBeInstanceOf(AbortSignal);
      expect(signal.aborted).toBe(false);

      // t = 40 s: the bound gives up on the hung attempt.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000);
      });
      expect(signal.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("aborts an in-flight attempt on unmount", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { unmount } = setup();
      await settle();

      projectApi.listProjects.mockReset().mockImplementation(hangUntilAborted());
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      const signal = projectApi.listProjects.mock.calls[0][0] as AbortSignal;
      expect(signal.aborted).toBe(false);

      unmount();
      expect(signal.aborted).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("an aborted attempt changes no state and logs nothing", async () => {
    await syncProjectToCache({ path: "/cached", name: "cached" }, 0);
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const { result } = setup();
      await settle();
      expect(result.current.state.projects).toHaveLength(1);
      expect(result.current.state.offline).toBe(true);
      const projectsBefore = result.current.state.projects;
      errorSpy.mockClear();

      projectApi.listProjects.mockReset().mockImplementation(hangUntilAborted());
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(30_000);
      });
      await settle();

      const signal = projectApi.listProjects.mock.calls[0][0] as AbortSignal;
      expect(signal.aborted).toBe(true);
      // The same array instance: no SET_PROJECTS, no SET_CACHED_PROJECTS re-hydrate.
      expect(result.current.state.projects).toBe(projectsBefore);
      expect(result.current.state.projectsStatus).toBe("error");
      expect(result.current.state.offline).toBe(true);
      expect(errorSpy).not.toHaveBeenCalled();
    } finally {
      errorSpy.mockRestore();
      vi.useRealTimers();
    }
  });
});

describe("projectStore in-memory removal", () => {
  it("drops the removed project when the follow-up list fails while the store is ready", async () => {
    projectApi.listProjects
      .mockReset()
      .mockResolvedValue([
        { path: "/gone", name: "gone" },
        { path: "/kept", name: "kept" },
      ]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));
    expect(result.current.state.projectsStatus).toBe("ready");

    projectApi.removeProject.mockReset().mockResolvedValue({ status: "ok" });
    projectApi.listProjects.mockReset().mockRejectedValue(new Error("offline"));
    await act(async () => {
      await result.current.removeProject("/gone");
    });

    expect(result.current.state.projects.map((p) => p.path)).toEqual(["/kept"]);
    expect(result.current.state.projectsStatus).toBe("ready");
  });

  it("removing a remote project keeps the local project that shares its path", async () => {
    projectApi.listProjects
      .mockReset()
      .mockResolvedValue([
        { path: "/same", name: "local" },
        { path: "/same", host: "dev@box", name: "remote" },
      ]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));

    projectApi.removeRemoteProject.mockReset().mockResolvedValue({ status: "ok" });
    projectApi.listProjects.mockReset().mockRejectedValue(new Error("offline"));
    await act(async () => {
      await result.current.removeProject("/same", "dev@box");
    });

    expect(result.current.state.projects.map((p) => p.name)).toEqual(["local"]);
  });

  it("removing a local project keeps the remote project that shares its path", async () => {
    projectApi.listProjects
      .mockReset()
      .mockResolvedValue([
        { path: "/same", name: "local" },
        { path: "/same", host: "dev@box", name: "remote" },
      ]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));

    projectApi.removeProject.mockReset().mockResolvedValue({ status: "ok" });
    projectApi.listProjects.mockReset().mockRejectedValue(new Error("offline"));
    await act(async () => {
      await result.current.removeProject("/same");
    });

    expect(result.current.state.projects.map((p) => p.name)).toEqual(["remote"]);
  });

  it("a failed removal leaves the list as it was", async () => {
    projectApi.listProjects
      .mockReset()
      .mockResolvedValue([
        { path: "/gone", name: "gone" },
        { path: "/kept", name: "kept" },
      ]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projects).toHaveLength(2));
    const projectsBefore = result.current.state.projects;
    const listCallsBefore = projectApi.listProjects.mock.calls.length;

    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      projectApi.removeProject
        .mockReset()
        .mockRejectedValue(Object.assign(new Error("Server error"), { status: 500 }));
      await act(async () => {
        await expect(result.current.removeProject("/gone")).rejects.toThrow("Server error");
      });
    } finally {
      errorSpy.mockRestore();
    }

    expect(result.current.state.projects).toBe(projectsBefore);
    // No follow-up list request after a failed removal.
    expect(projectApi.listProjects.mock.calls.length).toBe(listCallsBefore);
  });
});
