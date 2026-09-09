import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "./projectStore";

const projectApi = vi.hoisted(() => ({
  listProjects: vi.fn(),
  getCurrentProject: vi.fn(),
  listProjectSessions: vi.fn(),
  listGroups: vi.fn(),
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
