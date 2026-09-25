import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "./projectStore";
import { resolveSessionHost } from "../hooks/useSessionHost";

const projectApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
  renameProject: vi.fn(),
  reorderProjects: vi.fn(),
  setProjectGroup: vi.fn(),
  removeProject: vi.fn(),
  deleteGroup: vi.fn(),
  removeRemoteProject: vi.fn(),
  getTabs: vi.fn(),
  setTabs: vi.fn(),
}));

// ProjectProvider fires api calls on mount (listProjects / getCurrentProject).
// Stub them so the provider settles without network noise or a late
// SET_PROJECTS clobbering the state the tests assert on.
vi.mock("../api/client", () => ({
  api: {
    listProjects: projectApi.listProjects,
    getCurrentProject: projectApi.getCurrentProject,
    listProjectSessions: projectApi.listProjectSessions,
    listGroups: projectApi.listGroups,
    renameProject: projectApi.renameProject,
    reorderProjects: projectApi.reorderProjects,
    setProjectGroup: projectApi.setProjectGroup,
    removeProject: projectApi.removeProject,
    deleteGroup: projectApi.deleteGroup,
    removeRemoteProject: projectApi.removeRemoteProject,
    getTabs: projectApi.getTabs,
    setTabs: projectApi.setTabs,
  },
}));

// The provider subscribes to the server event bus for cross-window tab sync;
// capture handlers so tests can fire `tabs_changed` without a real stream.
const busHandlers = vi.hoisted(() => new Map<string, (env: unknown) => void>());
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: (env: unknown) => void) => {
      busHandlers.set(event, handler);
      return () => busHandlers.delete(event);
    },
    onReconnect: () => () => {},
  },
}));

// projectReducer is not exported — drive it through the provider + dispatch,
// same as a real consumer would.
const testProjectA = { path: "/proj-a", name: "a", added_at: "", last_used_at: "", order: 1, group: "" };
const testRemoteProject = {
  path: "/remote",
  name: "remote",
  host: "dev@example.com",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

beforeEach(() => {
  projectApi.listProjects.mockReset().mockResolvedValue([]);
  projectApi.getCurrentProject.mockReset().mockResolvedValue(null);
  projectApi.listProjectSessions.mockReset().mockResolvedValue([]);
  projectApi.listGroups.mockReset().mockResolvedValue([]);
  projectApi.renameProject.mockReset().mockResolvedValue({ status: "ok" });
  projectApi.reorderProjects.mockReset().mockResolvedValue({ status: "ok" });
  projectApi.setProjectGroup.mockReset().mockResolvedValue({ status: "ok" });
  projectApi.removeRemoteProject.mockReset().mockResolvedValue({ status: "ok" });
  projectApi.getTabs.mockReset().mockResolvedValue({ projects: {} });
  projectApi.setTabs.mockReset().mockResolvedValue({ status: "ok" });
  busHandlers.clear();
  window.localStorage.clear();
});

function setup() {
  return renderHook(() => useProjectState(), {
    wrapper: ({ children }) => <ProjectProvider>{children}</ProjectProvider>,
  });
}

describe("projectStore tab actions across projects", () => {
  it("UPDATE_TAB_TITLE updates a tab that belongs to a non-active project", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: testProjectA,
      });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-b", projectPath: "/proj-b", title: "old", activeSubTab: "chat" },
      });
      // Switch active project away from proj-b before renaming its tab.
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: testProjectA,
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-b",
        title: "renamed",
      });
    });
    // Flush the provider's mount-time api promises (listProjects etc.) so
    // their late dispatches land inside act, not after the test body.
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-b"][0].title).toBe(
      "renamed",
    );
  });

  it("UPDATE_TAB_ID rekeys a tab that belongs to a non-active project", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: testProjectA,
      });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "new-1", projectPath: "/proj-b", title: "New session", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_ID",
        oldId: "new-1",
        newId: "sess-real",
        newTitle: "Real title",
      });
    });
    await act(async () => {});
    const tabs = result.current.state.tabsByProject["/proj-b"];
    expect(tabs.find((t) => t.id === "sess-real")?.title).toBe("Real title");
    expect(tabs.find((t) => t.id === "new-1")).toBeUndefined();
  });

  it("an auto title (manual: false/omitted) never overwrites a manually renamed tab", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-a", projectPath: "/proj-a", title: "old", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-a",
        title: "My Custom Name",
        manual: true,
      });
      // Simulate a later background status broadcast carrying a generated title.
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-a",
        title: "LLM generated title",
      });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-a"][0].title).toBe(
      "My Custom Name",
    );
  });

  it("a second explicit rename (manual: true) can still override a prior manual rename", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-a", projectPath: "/proj-a", title: "old", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-a",
        title: "First rename",
        manual: true,
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "sess-a",
        title: "Second rename",
        manual: true,
      });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-a"][0].title).toBe(
      "Second rename",
    );
  });

  it("re-applying an unchanged auto title leaves the tab object untouched (no render/persist churn)", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-a", projectPath: "/proj-a", title: "Generated", activeSubTab: "chat" },
      });
    });
    await act(async () => {});
    const before = result.current.state.tabsByProject["/proj-a"][0];
    act(() => {
      // Reconcile / cross-process revalidation re-sends the same title.
      result.current.dispatch({ type: "UPDATE_TAB_TITLE", id: "sess-a", title: "Generated" });
    });
    const after = result.current.state.tabsByProject["/proj-a"][0];
    expect(after).toBe(before);
    expect(after.title).toBe("Generated");
  });


  it("UPDATE_TAB_ID preserves a manually-renamed temp tab's title instead of the rekey's newTitle", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "new-1", projectPath: "/proj-a", title: "New session", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "UPDATE_TAB_TITLE",
        id: "new-1",
        title: "Renamed before first message",
        manual: true,
      });
      result.current.dispatch({
        type: "UPDATE_TAB_ID",
        oldId: "new-1",
        newId: "sess-real",
        newTitle: "New session",
      });
    });
    await act(async () => {});
    const tabs = result.current.state.tabsByProject["/proj-a"];
    expect(tabs.find((t) => t.id === "sess-real")?.title).toBe(
      "Renamed before first message",
    );
  });
});

describe("project metadata readiness", () => {
  it("does not become ready until the initial project request succeeds", async () => {
    let resolve!: (projects: typeof testRemoteProject[]) => void;
    projectApi.listProjects.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    const { result } = setup();
    expect(result.current.state.projectsStatus).toBe("loading");
    expect(result.current.state.projects).toEqual([]);

    await act(async () => resolve([testRemoteProject]));

    await waitFor(() => expect(result.current.state.projectsStatus).toBe("ready"));
    expect(result.current.state.projects).toEqual([testRemoteProject]);
  });

  it("keeps terminals withheld after an initial failure and allows a safe retry", async () => {
    projectApi.listProjects.mockRejectedValueOnce(new Error("initial failure"));
    projectApi.listProjects.mockResolvedValueOnce([testRemoteProject]);

    const { result } = setup();
    await waitFor(() => expect(result.current.state.projectsStatus).toBe("error"));
    expect(result.current.state.projects).toEqual([]);

    await act(async () => result.current.refreshProjects());

    expect(result.current.state.projectsStatus).toBe("ready");
    expect(result.current.state.projects[0]).toMatchObject({ path: "/remote", host: "dev@example.com" });
  });

  it("retains the trusted snapshot when a later refresh fails", async () => {
    projectApi.listProjects.mockResolvedValueOnce([testRemoteProject]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projectsStatus).toBe("ready"));

    projectApi.listProjects.mockRejectedValueOnce(new Error("refresh failure"));
    await act(async () => result.current.refreshProjects());

    expect(result.current.state.projectsStatus).toBe("ready");
    expect(result.current.state.projects).toEqual([testRemoteProject]);
    expect(result.current.state.loading).toBe(false);
  });

  it("does not let an older refresh overwrite the newest host snapshot", async () => {
    projectApi.listProjects.mockResolvedValueOnce([testRemoteProject]);
    const { result } = setup();
    await waitFor(() => expect(result.current.state.projectsStatus).toBe("ready"));

    let resolveOlder!: (projects: typeof testRemoteProject[]) => void;
    let resolveNewer!: (projects: typeof testRemoteProject[]) => void;
    projectApi.listProjects
      .mockReturnValueOnce(new Promise((r) => { resolveOlder = r; }))
      .mockReturnValueOnce(new Promise((r) => { resolveNewer = r; }));
    let older!: Promise<void>;
    let newer!: Promise<void>;
    await act(async () => {
      older = result.current.refreshProjects();
      newer = result.current.refreshProjects();
      resolveNewer([{ ...testRemoteProject, host: "new.example.com" }]);
      await newer;
      resolveOlder([{ ...testRemoteProject, host: "old.example.com" }]);
      await older;
    });

    expect(result.current.state.projects[0].host).toBe("new.example.com");
  });
});

describe("projectStore remote mutations stay host-scoped", () => {
  it("renameProject forwards host for an SSH entry", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.renameProject("/home/user/app", "renamed", "devbox");
    });
    expect(projectApi.renameProject).toHaveBeenCalledWith("/home/user/app", "renamed", "devbox");
  });

  it("renameProject forwards host for a WSL entry", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.renameProject(`C:\\Users\\james\\app`, "wsl-app", "wsl:Ubuntu");
    });
    expect(projectApi.renameProject).toHaveBeenCalledWith(
      `C:\\Users\\james\\app`,
      "wsl-app",
      "wsl:Ubuntu",
    );
  });

  it("setProjectGroup forwards host for a remote entry", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.setProjectGroup("/home/user/app", "g", "devbox");
    });
    expect(projectApi.setProjectGroup).toHaveBeenCalledWith("/home/user/app", "g", "devbox");
  });

  it("reorderProjects forwards scoped refs with verbatim remote paths", async () => {
    const { result } = setup();
    const refs = [
      { path: "/home/user/app", host: "wsl:Ubuntu" },
      { path: "/local" },
      { path: "/home/user/app", host: "devbox" },
    ];
    await act(async () => {
      await result.current.reorderProjects(refs);
    });
    expect(projectApi.reorderProjects).toHaveBeenCalledWith(refs);
  });

  it("renameProject propagates errors so the inline editor can display them", async () => {
    projectApi.renameProject.mockRejectedValueOnce(new Error("server rejected rename"));
    const { result } = setup();
    await act(async () => {
      await expect(
        result.current.renameProject("/home/user/app", "renamed", "devbox"),
      ).rejects.toThrow("server rejected rename");
    });
    expect(projectApi.renameProject).toHaveBeenCalledWith("/home/user/app", "renamed", "devbox");
  });
});

// A failed removal used to be logged and swallowed, so the sidebar's confirm
// dialog closed as if the project were gone. Both destructive list mutations
// must reject like renameProject does, and must still log the attempt.
describe("projectStore destructive mutations propagate failures", () => {
  it("removeProject propagates a local failure", async () => {
    projectApi.removeProject.mockRejectedValueOnce(new Error("remove project: 404"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(result.current.removeProject("/proj-a")).rejects.toThrow("404");
    });
    // Logged at the boundary, not only rendered.
    expect(errSpy).toHaveBeenCalledWith("Failed to remove project:", expect.any(Error));
    errSpy.mockRestore();
  });

  it("removeProject propagates a remote failure without touching the local call", async () => {
    // Mocks are not auto-cleared between tests in this file.
    projectApi.removeProject.mockClear();
    projectApi.removeRemoteProject.mockRejectedValueOnce(new Error("remote connect failed"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(
        result.current.removeProject("/remote", "dev@example.com"),
      ).rejects.toThrow("remote connect failed");
    });
    expect(projectApi.removeRemoteProject).toHaveBeenCalledWith("/remote", "dev@example.com");
    expect(projectApi.removeProject).not.toHaveBeenCalled();
    errSpy.mockRestore();
  });

  it("deleteGroup propagates a failure", async () => {
    projectApi.deleteGroup.mockRejectedValueOnce(new Error("delete group: 404"));
    const errSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    const { result } = setup();
    await act(async () => {
      await expect(result.current.deleteGroup("g")).rejects.toThrow("404");
    });
    expect(errSpy).toHaveBeenCalledWith("Failed to delete group:", expect.any(Error));
    errSpy.mockRestore();
  });
});

describe("projectStore session sub-tabs", () => {
  it("ADD_TAB defaults a new tab's activeSubTab to chat", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-1", projectPath: "/proj-a", title: "s1", activeSubTab: "chat" },
      });
    });
    await act(async () => {});
    expect(result.current.tabs.find((t) => t.id === "sess-1")?.activeSubTab).toBe("chat");
  });

  it("SET_TAB_SUB_TAB updates a tab that belongs to a non-active project", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-b", projectPath: "/proj-b", title: "b", activeSubTab: "chat" },
      });
      result.current.dispatch({
        type: "SET_ACTIVE_PROJECT",
        project: testProjectA,
      });
      result.current.dispatch({ type: "SET_TAB_SUB_TAB", id: "sess-b", subTab: "agents" });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-b"][0].activeSubTab).toBe("agents");
  });

  it("SET_TAB_SUB_TAB is a no-op for an unknown tab id", async () => {
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({ type: "SET_TAB_SUB_TAB", id: "does-not-exist", subTab: "agents" });
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-a"]).toBeUndefined();
  });
});

describe("no auto-ensured New session tab", () => {
  it("selectProject does not create a New session tab", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.selectProject(testProjectA);
    });
    await act(async () => {});
    expect(result.current.state.tabsByProject["/proj-a"]).toBeUndefined();
    expect(result.current.activeTabId).toBeNull();
  });

  it("openNewSessionTab still creates a New tab on explicit user action", async () => {
    const { result } = setup();
    await act(async () => {
      await result.current.selectProject(testProjectA);
    });
    await act(async () => {
      result.current.openNewSessionTab(false);
    });
    expect(result.current.state.tabsByProject["/proj-a"]).toHaveLength(1);
    expect(result.current.state.tabsByProject["/proj-a"][0].id).toMatch(/^new-/);
    expect(result.current.activeTabId).toBe(
      result.current.state.tabsByProject["/proj-a"][0].id,
    );
  });
});

describe("projectStore server-side tab persistence", () => {
  it("restores open tabs from the server on mount, mapping sub_tab", async () => {
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "s1", title: "One", sub_tab: "changes" }, { id: "s2", title: "Two" }], active: "s1" } },
    });
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    expect(result.current.state.tabsByProject["/proj-a"].map((t) => t.id)).toEqual(["s1", "s2"]);
    expect(result.current.state.tabsByProject["/proj-a"][0].activeSubTab).toBe("changes");
    expect(result.current.state.activeTabByProject["/proj-a"]).toBe("s1");
  });

  it("persists tab changes to the server as a full projects map", async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-1", projectPath: "/proj-a", title: "One", activeSubTab: "logs" },
      });
    });
    await waitFor(() => expect(projectApi.setTabs).toHaveBeenCalled());
    const last = projectApi.setTabs.mock.calls[projectApi.setTabs.mock.calls.length - 1]?.[0];
    expect(last).toEqual({ "/proj-a": { tabs: [{ id: "sess-1", title: "One", sub_tab: "logs" }], active: "sess-1" } });
  });

  it("never writes to the server before the restore settled", async () => {
    let resolveGet: (v: { projects: Record<string, never> }) => void = () => {};
    projectApi.getTabs.mockReturnValue(new Promise((r) => { resolveGet = r; }));
    const { result } = setup();
    await act(async () => {
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-1", projectPath: "/proj-a", title: "One", activeSubTab: "chat" },
      });
      await new Promise((r) => setTimeout(r, 500));
    });
    expect(projectApi.setTabs).not.toHaveBeenCalled();
    await act(async () => { resolveGet({ projects: {} }); });
    // The locally-added tab survives the restore and is then written through.
    await waitFor(() => expect(projectApi.setTabs).toHaveBeenCalled());
    expect(result.current.state.tabsByProject["/proj-a"][0].id).toBe("sess-1");
  });

  it("migrates legacy localStorage tabs once when the server has none", async () => {
    window.localStorage.setItem(
      "ocode.ui.tabs.v1",
      JSON.stringify({ version: 1, projects: { "/proj-a": { tabs: [{ id: "old", title: "Old", subTab: "agents" }], active: "old" } } }),
    );
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    expect(result.current.state.tabsByProject["/proj-a"][0]).toMatchObject({ id: "old", activeSubTab: "agents" });
    expect(window.localStorage.getItem("ocode.ui.tabs.v1")).toBeNull();
    await waitFor(() => expect(projectApi.setTabs).toHaveBeenCalled());
  });

  it("refetches and merges on a tabs_changed bus event, keeping local new-* tabs", async () => {
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({
        type: "ADD_TAB",
        tab: { id: "new-1", projectPath: "/proj-a", title: "New", activeSubTab: "chat" },
      });
    });
    await waitFor(() => expect(projectApi.setTabs).toHaveBeenCalled());
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "remote-1", title: "Opened elsewhere" }], active: "remote-1" } },
    });
    await act(async () => {
      busHandlers.get("tabs_changed")?.({ event: "tabs_changed", seq: 1, data: null });
    });
    await waitFor(() =>
      expect(result.current.state.tabsByProject["/proj-a"].map((t) => t.id)).toEqual(["remote-1", "new-1"]),
    );
    expect(result.current.state.activeTabByProject["/proj-a"]).toBe("new-1");
  });

  it("sends an explicit empty entry when a known project's last tab closes (server deletes it)", async () => {
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "s1", title: "One" }], active: "s1" } },
    });
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({ type: "REMOVE_TAB", id: "s1" });
    });
    // The bulk PUT merges, so the closed project must be sent as an explicit
    // empty entry — omitting it would leave the old tabs on the server.
    await waitFor(() => {
      const calls = projectApi.setTabs.mock.calls;
      expect(calls[calls.length - 1]?.[0]).toEqual({ "/proj-a": { tabs: [], active: "" } });
    });
  });

  it("rekeys tabs onto the new path and explicitly empties the old path", async () => {
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "s1", title: "One" }], active: "s1" } },
    });
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({ type: "REKEY_TABS", oldPath: "/proj-a", newPath: "/proj-b" });
    });
    // The old path is gone locally but must be sent as an explicit deletion.
    // (A later write, after the deletion is acknowledged, omits it again.)
    await waitFor(() => {
      expect(projectApi.setTabs.mock.calls.some((c) => {
        const payload = c[0] as Record<string, unknown>;
        return (
          JSON.stringify(payload?.["/proj-a"]) === JSON.stringify({ tabs: [], active: "" }) &&
          JSON.stringify(payload?.["/proj-b"]) ===
            JSON.stringify({ tabs: [{ id: "s1", title: "One", sub_tab: "chat" }], active: "s1" })
        );
      })).toBe(true);
    });
    expect(result.current.state.tabsByProject["/proj-a"]).toBeUndefined();
  });

  it("clears a pending deletion once the write carrying it succeeds", async () => {
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "s1", title: "One" }], active: "s1" } },
    });
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({ type: "REMOVE_TAB", id: "s1" });
    });
    expect(result.current.state.pendingTabDeletes).toContain("/proj-a");
    await waitFor(() => expect(result.current.state.pendingTabDeletes).toEqual([]));
  });

  it("does not resurrect a closed project when a refetch returns it before the delete is acknowledged", async () => {
    projectApi.getTabs.mockResolvedValue({
      projects: { "/proj-a": { tabs: [{ id: "s1", title: "One" }], active: "s1" } },
    });
    // The delete write fails, so the server still holds the old tabs and the
    // deletion stays pending — exactly the race the pending set guards.
    projectApi.setTabs.mockRejectedValue(new Error("offline"));
    const { result } = setup();
    await waitFor(() => expect(result.current.state.tabsRestored).toBe(true));
    await act(async () => {
      result.current.dispatch({ type: "SET_ACTIVE_PROJECT", project: testProjectA });
      result.current.dispatch({ type: "REMOVE_TAB", id: "s1" });
    });
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(result.current.state.pendingTabDeletes).toContain("/proj-a");
    // Refetch (as a tabs_changed event would) still returns the old tabs.
    await act(async () => {
      busHandlers.get("tabs_changed")?.({ event: "tabs_changed", seq: 1, data: null });
      await new Promise((r) => setTimeout(r, 20));
    });
    expect(result.current.state.tabsByProject["/proj-a"] ?? []).toEqual([]);
  });
});

describe("project session-list cache (snappy project switching)", () => {
  const mkSession = (id: string, title: string) => ({
    id,
    title,
    created_at: "",
    updated_at: "",
  });

  it("a cache hit paints the list immediately and revalidates without a spinner", async () => {
    projectApi.listProjectSessions.mockResolvedValueOnce([mkSession("s1", "One")]);
    const { result } = setup();
    await act(async () => {});

    // First visit: a real fetch (cache miss).
    await act(async () => {
      await result.current.selectProject(testProjectA);
    });
    expect(result.current.state.projectSessions.map((s) => s.id)).toEqual(["s1"]);

    // Park the revalidation so we can prove the cached list is already visible
    // before the network reply lands.
    let resolveRevalidation!: (v: unknown) => void;
    projectApi.listProjectSessions.mockImplementationOnce(
      () => new Promise((res) => { resolveRevalidation = res as (v: unknown) => void; }),
    );

    await act(async () => {
      await result.current.selectProject(testProjectA);
    });

    // Painted from cache: list present, no loading state, revalidation in flight.
    expect(result.current.state.projectSessions.map((s) => s.id)).toEqual(["s1"]);
    expect(result.current.state.sessionsLoading).toBe(false);
    expect(projectApi.listProjectSessions).toHaveBeenCalledTimes(2);

    // The background reply refreshes the list in place.
    await act(async () => {
      resolveRevalidation([mkSession("s1", "One"), mkSession("s2", "Two")]);
    });
    expect(result.current.state.projectSessions.map((s) => s.id)).toEqual(["s1", "s2"]);
  });

  it("prefetchProjectSessions warms the cache so the click has no fetch on its path", async () => {
    projectApi.listProjectSessions.mockResolvedValueOnce([mkSession("w1", "Warm")]);
    const { result } = setup();
    await act(async () => {});

    // Hover-warm.
    await act(async () => {
      result.current.prefetchProjectSessions(testProjectA);
    });
    await act(async () => {});
    expect(projectApi.listProjectSessions).toHaveBeenCalledWith("/proj-a", undefined);
    expect(result.current.state.sessionsByProject["/proj-a"].sessions.map((s) => s.id)).toEqual(["w1"]);

    // Click: paints from the warm cache without entering the loading state.
    let resolveRevalidation!: (v: unknown) => void;
    projectApi.listProjectSessions.mockImplementationOnce(
      () => new Promise((res) => { resolveRevalidation = res as (v: unknown) => void; }),
    );
    await act(async () => {
      await result.current.selectProject(testProjectA);
    });
    expect(result.current.state.sessionsLoading).toBe(false);
    expect(result.current.state.projectSessions.map((s) => s.id)).toEqual(["w1"]);
    await act(async () => {
      resolveRevalidation([]);
    });
  });

  it("keeps each host:path pair's session list in its own cache entry", async () => {
    projectApi.listProjectSessions.mockImplementation(async (_path: string, host?: string) => [
      mkSession(host ? "remote-1" : "local-1", host ? "Remote" : "Local"),
    ]);
    const { result } = setup();
    await act(async () => {});

    await act(async () => {
      await result.current.selectProject(testProjectA);
    });
    await act(async () => {
      await result.current.selectProject(testRemoteProject);
    });

    // Remote listings never come from this server's local session directory;
    // the handler returns [] for a remote host, so the cache must be keyed by
    // (host, path) rather than path alone.
    expect(projectApi.listProjectSessions).toHaveBeenCalledWith("/proj-a", undefined);
    expect(projectApi.listProjectSessions).toHaveBeenCalledWith("/remote", "dev@example.com");
    // Keys are `host::path`, so the remote entry can never shadow the local
    // `/proj-a` entry (or a same-named local path).
    expect(
      result.current.state.sessionsByProject["dev@example.com::/remote"].sessions.map((s) => s.id),
    ).toEqual(["remote-1"]);
    expect(result.current.state.sessionsByProject["/proj-a"].sessions.map((s) => s.id)).toEqual(["local-1"]);
  });

  it("binds a resumed remote session's tab under its own project so the host resolves", async () => {
    projectApi.listProjectSessions.mockResolvedValueOnce([mkSession("remote-s1", "Remote one")]);
    const { result } = setup();
    await act(async () => {});

    await act(async () => {
      result.current.dispatch({ type: "SET_PROJECTS", projects: [testRemoteProject] });
      await result.current.selectProject(testRemoteProject);
    });

    // Resuming the session from the list (SessionDialog's open action).
    await act(async () => {
      result.current.openSessionTab("remote-s1", "Remote one");
    });
    await act(async () => {});

    // The tab is owned by the remote project, so findProjectPathForTab +
    // getTrustedTerminalProject resolve "dev@example.com" — a tab bound to the
    // local path would silently route the resumed session to the local server.
    expect(result.current.state.tabsByProject["/remote"].map((t) => t.id)).toContain("remote-s1");
    expect(resolveSessionHost(result.current.state, "remote-s1")).toBe("dev@example.com");
  });

  it("binds a session opened from a non-active project's list to that project", async () => {
    const { result } = setup();
    await act(async () => {});

    // Active project is the local one; the user is browsing the remote
    // project's chat list in the sidebar (RemoteProjectStatus).
    await act(async () => {
      result.current.dispatch({ type: "SET_PROJECTS", projects: [testProjectA, testRemoteProject] });
      await result.current.selectProject(testProjectA);
    });

    await act(async () => {
      result.current.openSessionTab("remote-s2", "Remote two", testRemoteProject.path);
    });

    // Threaded explicitly: with only the active project's path the tab would be
    // filed under /proj-a and its session routed through the local server.
    expect(result.current.state.tabsByProject["/remote"].map((t) => t.id)).toContain("remote-s2");
    expect(resolveSessionHost(result.current.state, "remote-s2")).toBe("dev@example.com");
    expect(result.current.state.tabsByProject["/proj-a"] ?? []).toHaveLength(0);
  });
});
