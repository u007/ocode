import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import { useSessionHost } from "./useSessionHost";

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      listProjects: vi.fn().mockResolvedValue([]),
      getCurrentProject: vi.fn().mockResolvedValue(null),
      listProjectSessions: vi.fn().mockResolvedValue([]),
      listGroups: vi.fn().mockResolvedValue([]),
      getTabs: vi.fn().mockResolvedValue({ projects: {} }),
      setTabs: vi.fn().mockResolvedValue({ status: "ok" }),
    },
  };
});
vi.mock("../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));

const remoteProject = {
  path: "/srv/app",
  name: "app",
  host: "devbox",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

function Wrapper({ children }: { children: React.ReactNode }) {
  return <ProjectProvider>{children}</ProjectProvider>;
}

describe("useSessionHost", () => {
  it("returns undefined for an unknown session", () => {
    const { result } = renderHook(() => ({ host: useSessionHost(undefined) }), {
      wrapper: Wrapper,
    });
    expect(result.current.host).toBeUndefined();
  });

  it("returns the host for a tab bound to a remote project", () => {
    // Use a shared wrapper so dispatches are visible to the hook
    function SharedWrapper({ children }: { children: React.ReactNode }) {
      return <ProjectProvider>{children}</ProjectProvider>;
    }
    const { result } = renderHook(
      () => ({ host: useSessionHost("sess-1"), project: useProjectState() }),
      { wrapper: SharedWrapper },
    );

    // Seed the project store with a remote project and register a tab under it
    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-1", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" },
      });
    });

    // After act, the hook re-renders with the updated store
    expect(result.current.host).toBe("devbox");
  });

  it("returns undefined when no project is bound", () => {
    const { result } = renderHook(() => ({ host: useSessionHost("nonexistent-tab") }), {
      wrapper: Wrapper,
    });
    expect(result.current.host).toBeUndefined();
  });
});
