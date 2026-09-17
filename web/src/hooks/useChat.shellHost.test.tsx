import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import { ChatProvider } from "../stores/chatStore";
import { useChat } from "./useChat";

const mockShellCommand = vi.fn().mockResolvedValue({ output: "ok", exitCode: 0, error: "" });

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      shellCommand: (...a: unknown[]) => mockShellCommand(...a),
      listProjects: vi.fn().mockResolvedValue([]),
      getCurrentProject: vi.fn().mockResolvedValue(null),
      listProjectSessions: vi.fn().mockResolvedValue([]),
      listGroups: vi.fn().mockResolvedValue([]),
      getTabs: vi.fn().mockResolvedValue({ projects: {} }),
      setTabs: vi.fn().mockResolvedValue({ status: "ok" }),
      sendMessage: vi.fn().mockResolvedValue({ status: "ok" }),
      chat: vi.fn().mockResolvedValue({ sessionId: "sess-1" }),
    },
  };
});
vi.mock("../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));

const remoteProject = {
  path: "/srv/app",
  name: "app",
  host: "ci.local",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};
const localProject = {
  path: "/local/app",
  name: "local",
  added_at: "",
  last_used_at: "",
  order: 1,
  group: "",
};

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <ProjectProvider>
      <ChatProvider>{children}</ChatProvider>
    </ProjectProvider>
  );
}

// Renders useChat alongside the project store's dispatch so a test can seed a
// project list and make one active (the same path a real consumer takes).
function renderWithProjects() {
  return renderHook(() => ({ chat: useChat("sess-1"), project: useProjectState() }), {
    wrapper: Wrapper,
  });
}

describe("useChat.executeShell host routing", () => {
  beforeEach(() => {
    mockShellCommand.mockClear();
  });

  // A `!` command in a remote project must run on the host that owns the
  // project. Without the host the server ran it locally, and the local $SHELL
  // produced `fork/exec /bin/zsh: no such file or directory` on the remote.
  it("passes the project host for a remote project", async () => {
    const { result } = renderWithProjects();

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject, localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
    });

    await act(async () => {
      await result.current.chat.executeShell("echo hi");
    });

    expect(mockShellCommand).toHaveBeenCalledWith("echo hi", "/srv/app", "ci.local");
  });

  // A local project keeps the historical call shape: no host, so the server
  // resolves and runs the command locally.
  it("omits the host for a local project", async () => {
    const { result } = renderWithProjects();

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: localProject });
    });

    await act(async () => {
      await result.current.chat.executeShell("echo hi");
    });

    expect(mockShellCommand).toHaveBeenCalledWith("echo hi", "/local/app", undefined);
  });

  // The store allows a local and a remote project to share one path, and
  // sidebar order decides which is found first. Picking the first match would
  // run a `!` command on whichever machine happened to sort first, so an
  // ambiguous path follows the terminal's single-match trust rule and sends
  // no host.
  it("omits the host when the path matches both a local and a remote project", async () => {
    const { result } = renderWithProjects();
    const sharedRemote = { ...remoteProject, path: "/shared/app" };
    const sharedLocal = { ...localProject, path: "/shared/app" };

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [sharedRemote, sharedLocal] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: sharedRemote });
    });

    await act(async () => {
      await result.current.chat.executeShell("echo hi");
    });

    expect(mockShellCommand).toHaveBeenCalledWith("echo hi", "/shared/app", undefined);
  });
});
