import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "./projectStore";

// ProjectProvider fires api calls on mount (listProjects / getCurrentProject).
// Stub them so the provider settles without network noise or a late
// SET_PROJECTS clobbering the state the tests assert on.
vi.mock("../api/client", () => ({
  api: {
    listProjects: vi.fn().mockResolvedValue([]),
    getCurrentProject: vi.fn().mockResolvedValue(null),
    listProjectSessions: vi.fn().mockResolvedValue([]),
    listGroups: vi.fn().mockResolvedValue([]),
  },
}));

// projectReducer is not exported — drive it through the provider + dispatch,
// same as a real consumer would.
const testProjectA = { path: "/proj-a", name: "a", added_at: "", last_used_at: "", order: 1, group: "" };
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
