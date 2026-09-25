import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import { useChat } from "./useChat";

const mocks = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  getSessionState: vi.fn(),
  loadPendingRewind: vi.fn(),
  clearPendingRewind: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      sendMessage: (...a: unknown[]) => mocks.sendMessage(...a),
      getSessionState: (...a: unknown[]) => mocks.getSessionState(...a),
      listProjects: vi.fn().mockResolvedValue([]),
      getCurrentProject: vi.fn().mockResolvedValue(null),
      listProjectSessions: vi.fn().mockResolvedValue([]),
      listGroups: vi.fn().mockResolvedValue([]),
      getTabs: vi.fn().mockResolvedValue({ projects: {} }),
      setTabs: vi.fn().mockResolvedValue({ status: "ok" }),
    },
  };
});

vi.mock("../lib/pendingRewindStore", () => ({
  loadPendingRewind: (...a: unknown[]) => mocks.loadPendingRewind(...a),
  clearPendingRewind: (...a: unknown[]) => mocks.clearPendingRewind(...a),
}));

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
  return (
    <ProjectProvider>
      <ChatProvider>{children}</ChatProvider>
    </ProjectProvider>
  );
}

function setup() {
  const rendered = renderHook(
    () => ({ chat: useChat("sess-remote"), project: useProjectState(), dispatch: useChatDispatch() }),
    { wrapper: Wrapper },
  );
  act(() => {
    rendered.result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject] });
    rendered.result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
    rendered.result.current.project.dispatch({
      type: "ADD_TAB",
      tab: { id: "sess-remote", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" as const },
    });
  });
  return rendered;
}

const armedRecord = {
  version: 1 as const,
  host: "devbox",
  sessionId: "sess-remote",
  token: "tok",
  draft: "edited",
  previousDraft: "",
  targetPreview: "target",
  expiresAt: new Date(Date.now() + 60_000).toISOString(),
};

describe("useChat durable rewind send", () => {
  beforeEach(() => {
    mocks.sendMessage.mockReset().mockResolvedValue({ sessionId: "sess-remote" });
    mocks.getSessionState.mockReset().mockResolvedValue({});
    mocks.loadPendingRewind.mockReset();
    mocks.clearPendingRewind.mockReset().mockReturnValue(true);
  });

  it("passes the armed token and host, then clears the record after an accepted send", async () => {
    mocks.loadPendingRewind.mockReturnValue(armedRecord);
    const { result } = setup();

    let accepted = false;
    await act(async () => {
      accepted = await result.current.chat.sendMessage("edited");
    });

    expect(accepted).toBe(true);
    expect(mocks.sendMessage).toHaveBeenCalledWith("sess-remote", "edited", "devbox", "tok");
    expect(mocks.clearPendingRewind).toHaveBeenCalledWith("devbox", "sess-remote");
  });

  it("does not send or clear a token when no armed record exists", async () => {
    const { result } = setup();

    await act(async () => {
      await result.current.chat.sendMessage("plain");
    });

    expect(mocks.sendMessage).toHaveBeenCalledWith("sess-remote", "plain", "devbox", undefined);
    expect(mocks.clearPendingRewind).not.toHaveBeenCalled();
  });

  it("clears a stale token and preserves the draft on a 409", async () => {
    const { ApiError } = await import("../api/client");
    mocks.loadPendingRewind.mockReturnValue(armedRecord);
    mocks.sendMessage.mockRejectedValue(new ApiError("pending rewind is stale", 409));
    const { result } = setup();

    let accepted = true;
    await act(async () => {
      accepted = await result.current.chat.sendMessage("edited");
    });

    expect(accepted).toBe(false);
    expect(mocks.clearPendingRewind).toHaveBeenCalledWith("devbox", "sess-remote");
  });

  it("keeps the token retryable on a transport error and probes committed state", async () => {
    mocks.loadPendingRewind.mockReturnValue(armedRecord);
    mocks.sendMessage.mockRejectedValue(new Error("network down"));
    const { result } = setup();

    let accepted = true;
    await act(async () => {
      accepted = await result.current.chat.sendMessage("edited");
    });

    expect(accepted).toBe(false);
    expect(mocks.clearPendingRewind).not.toHaveBeenCalled();
  });
});
