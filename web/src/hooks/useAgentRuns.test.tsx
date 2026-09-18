import { beforeEach, describe, expect, it, vi } from "vitest";
import { useEffect, useState } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import type { Tab } from "../stores/projectStore";
import type { Project } from "../api/types";
import { useAgentRuns } from "./useAgentRuns";

const hoisted = vi.hoisted(() => ({
  listAgentRuns: vi.fn(async () => []),
  listProjects: vi.fn(async (): Promise<unknown[]> => []),
}));

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      listAgentRuns: hoisted.listAgentRuns,
      listProjects: hoisted.listProjects,
      getCurrentProject: vi.fn(async () => null),
      listProjectSessions: vi.fn(async () => []),
      listGroups: vi.fn(async () => []),
      getTabs: vi.fn(async () => ({ projects: {} })),
      setTabs: vi.fn(async () => ({ status: "ok" })),
    },
  };
});
vi.mock("../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {}, start: () => {}, stop: () => {} },
}));

const remoteProject: Project = {
  path: "/remote",
  name: "remote",
  host: "devbox",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};
const localProject: Project = {
  path: "/local",
  name: "local",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

interface Seed {
  activeProject?: Project;
  tabs?: Tab[];
}

/**
 * Bind the seed tabs once the provider's project snapshot has landed, then
 * render the hook. Gating on `ready` is load-bearing: a hook mounted before its
 * tab is bound sees an unknown session id and would seed the LOCAL server — the
 * exact silent fallback these tests must forbid.
 */
function SeededProvider({ seed, children }: { seed: Seed; children: React.ReactNode }) {
  const { state, dispatch } = useProjectState();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    if (state.projects.length === 0) return;
    if (seed.activeProject) dispatch({ type: "SET_ACTIVE_PROJECT", project: seed.activeProject });
    for (const tab of seed.tabs ?? []) dispatch({ type: "ADD_TAB", tab });
    setReady(true);
  }, [state.projects.length, seed, dispatch]);
  return ready ? <>{children}</> : null;
}

function renderSeeded(sessionId: string, seed: Seed) {
  const Wrapper = ({ children }: { children: React.ReactNode }) => (
    <ProjectProvider>
      <SeededProvider seed={seed}>{children}</SeededProvider>
    </ProjectProvider>
  );
  return renderHook(() => useAgentRuns(sessionId), { wrapper: Wrapper });
}

describe("useAgentRuns remote host routing", () => {
  beforeEach(() => {
    hoisted.listAgentRuns.mockClear();
    hoisted.listProjects.mockReset();
    hoisted.listProjects.mockResolvedValue([]);
  });

  it("seeds a remote session's run tree from the host-prefixed runs endpoint", async () => {
    hoisted.listProjects.mockResolvedValue([remoteProject, localProject]);
    const { result } = renderSeeded("sess-remote", {
      activeProject: remoteProject,
      tabs: [{ id: "sess-remote", projectPath: "/remote", title: "Remote", activeSubTab: "chat" }],
    });

    // The hook resolves the session's project host and passes it to
    // listAgentRuns, which prefixes `/api/remote/<host>/api/agents/runs`.
    await waitFor(() => expect(hoisted.listAgentRuns).toHaveBeenCalledWith("sess-remote", "devbox"));
    // ...and never seeded the local server for the remote session.
    expect(hoisted.listAgentRuns).not.toHaveBeenCalledWith("sess-remote", undefined);
    await waitFor(() => expect(result.current.loaded).toBe(true));
  });

  it("seeds a local session without a host (byte-identical local URL)", async () => {
    hoisted.listProjects.mockResolvedValue([remoteProject, localProject]);
    renderSeeded("sess-local", {
      activeProject: localProject,
      tabs: [{ id: "sess-local", projectPath: "/local", title: "Local", activeSubTab: "chat" }],
    });

    await waitFor(() => expect(hoisted.listAgentRuns).toHaveBeenCalledWith("sess-local", undefined));
  });

  it("falls back to a local seed when the tab's project is absent from a ready snapshot", async () => {
    // `/ghost` is not among the saved projects. The snapshot has loaded (status
    // "ready"), so the project genuinely does not exist for this server:
    // deferring forever would leave the agents rail empty, so seed the local
    // server — the pre-change behavior. (While the snapshot is still loading the
    // same route defers; the ambiguous-path test below covers the deferral.)
    hoisted.listProjects.mockResolvedValue([remoteProject]);
    renderSeeded("sess-ghost", {
      tabs: [{ id: "sess-ghost", projectPath: "/ghost", title: "Ghost", activeSubTab: "chat" }],
    });

    await waitFor(() => expect(hoisted.listAgentRuns).toHaveBeenCalledWith("sess-ghost", undefined));
  });

  it("skips the seed for a path saved as both local and remote (ambiguous)", async () => {
    hoisted.listProjects.mockResolvedValue([
      { ...remoteProject, path: "/dup" },
      { ...localProject, path: "/dup" },
    ]);
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    try {
      renderSeeded("sess-dup", {
        tabs: [{ id: "sess-dup", projectPath: "/dup", title: "Dup", activeSubTab: "chat" }],
      });

      await waitFor(() =>
        expect(warn).toHaveBeenCalledWith(
          "useAgentRuns: deferring seed for unresolved project",
          expect.objectContaining({ sessionId: "sess-dup", projectPath: "/dup" }),
        ),
      );
      expect(hoisted.listAgentRuns).not.toHaveBeenCalled();
    } finally {
      warn.mockRestore();
    }
  });

  it("does not seed a draft tab (no real session yet)", () => {
    renderHook(() => useAgentRuns("new-1758000000000"), { wrapper: ProjectProvider });
    expect(hoisted.listAgentRuns).not.toHaveBeenCalled();
  });
});
