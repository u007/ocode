import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import { useChat } from "./useChat";

const mockSendMessage = vi.fn().mockResolvedValue({ status: "ok" });
const mockChat = vi.fn().mockResolvedValue({ sessionId: "new-sess" });
const mockCancelSession = vi.fn().mockResolvedValue({ cancelled: true });
const mockGetSessionState = vi.fn().mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 0 });
const mockRetrySession = vi.fn().mockResolvedValue({ sessionId: "sess-remote" });
const mockResolvePermission = vi.fn().mockResolvedValue({ ok: true });
const mockAnswerQuestion = vi.fn().mockResolvedValue({ status: "ok" });
const mockCancelQuestion = vi.fn().mockResolvedValue({ status: "ok" });

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      sendMessage: (...a: unknown[]) => mockSendMessage(...a),
      chat: (...a: unknown[]) => mockChat(...a),
      cancelSession: (...a: unknown[]) => mockCancelSession(...a),
      retrySession: (...a: unknown[]) => mockRetrySession(...a),
      getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
      resolvePermission: (...a: unknown[]) => mockResolvePermission(...a),
      answerQuestion: (...a: unknown[]) => mockAnswerQuestion(...a),
      cancelQuestion: (...a: unknown[]) => mockCancelQuestion(...a),
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

/** Render useChat with a specific sessionId alongside the project store's dispatch. */
function renderWithSession(sessionId: string) {
  return renderHook(
    () => ({ chat: useChat(sessionId), project: useProjectState(), dispatch: useChatDispatch() }),
    { wrapper: Wrapper },
  );
}

describe("useChat remote host routing", () => {
  beforeEach(() => {
    mockSendMessage.mockClear();
    mockChat.mockClear();
    mockCancelSession.mockClear();
    mockGetSessionState.mockClear();
    mockResolvePermission.mockClear();
    mockAnswerQuestion.mockClear();
    mockCancelQuestion.mockClear();
    mockRetrySession.mockClear();
  });

  // A tab bound to a remote project sends the host with chat and sendMessage.
  it("passes host for a remote project tab on chat and sendMessage", async () => {
    const { result } = renderWithSession("sess-remote");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject, localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
      // Register the tab under the remote project's path
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-remote", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" as const },
      });
    });

    await act(async () => {
      await result.current.chat.sendMessage("hello");
    });

    expect(mockSendMessage).toHaveBeenCalledWith("sess-remote", "hello", "devbox", undefined);
  });

  // A brand-new draft tab (no registered tab) sends chat with the active
  // project's host.
  it("passes host for a new draft tab under a remote active project", async () => {
    const { result } = renderWithSession("new-draft-1");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
    });

    await act(async () => {
      await result.current.chat.sendMessage("hello");
    });

    // Draft tab → api.chat() with host
    expect(mockChat).toHaveBeenCalledWith(
      "hello",
      undefined,
      undefined,
      "new-draft-1",
      "/srv/app",
      "devbox",
      undefined,
    );
  });

  // A new-* draft tab that IS registered under a remote project path resolves
  // the host from that path, even when the active project is local.
  it("passes host for a registered draft tab under remote path with local active project", async () => {
    const { result } = renderWithSession("new-draft-reg");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject, localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: localProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "new-draft-reg", projectPath: "/srv/app", title: "Draft", activeSubTab: "chat" },
      });
    });

    await act(async () => {
      await result.current.chat.sendMessage("hello");
    });

    // Registered under /srv/app → host = devbox, even though active project is local
    expect(mockChat).toHaveBeenCalledWith(
      "hello",
      undefined,
      undefined,
      "new-draft-reg",
      "/srv/app",
      "devbox",
      undefined,
    );
  });

  // After the active project switches to a local project, the SAME remote tab
  // still sends devbox because its path resolves through findProjectPathForTab.
  it("remote tab keeps its host after active project switches to local", async () => {
    const { result } = renderWithSession("sess-remote");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject, localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-remote", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" as const },
      });
    });

    // Switch active project to local
    act(() => {
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: localProject });
    });

    await act(async () => {
      await result.current.chat.sendMessage("still remote");
    });

    // Tab is still registered under /srv/app → host = devbox
    expect(mockSendMessage).toHaveBeenCalledWith("sess-remote", "still remote", "devbox", undefined);
  });

  // A tab bound to a local project sends no host.
  it("omits host for a local project tab", async () => {
    const { result } = renderWithSession("sess-local");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: localProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-local", projectPath: "/local/app", title: "Local", activeSubTab: "chat" as const },
      });
    });

    await act(async () => {
      await result.current.chat.sendMessage("hello");
    });

    expect(mockSendMessage).toHaveBeenCalledWith("sess-local", "hello", undefined, undefined);
  });

  // An ambiguous path (saved both local and remote) sends no host.
  it("omits host when the path is ambiguous", async () => {
    const sharedRemote = { ...remoteProject, path: "/shared/app" };
    const sharedLocal = { ...localProject, path: "/shared/app" };
    const { result } = renderWithSession("sess-shared");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [sharedRemote, sharedLocal] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: sharedRemote });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-shared", projectPath: "/shared/app", title: "Shared", activeSubTab: "chat" as const },
      });
    });

    await act(async () => {
      await result.current.chat.sendMessage("hello");
    });

    expect(mockSendMessage).toHaveBeenCalledWith("sess-shared", "hello", undefined, undefined);
  });

  // cancelSession also receives the host.
  it("passes host to cancelSession", async () => {
    const { result } = renderWithSession("sess-remote");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-remote", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" as const },
      });
    });

    act(() => {
      result.current.chat.stop();
    });

    expect(mockCancelSession).toHaveBeenCalledWith("sess-remote", "devbox");
  });

  // Retry (composer after Stop/LLM error) must reach the session's own server
  // for a remote project — a local retry would re-run the wrong transcript.
  it("passes host to retrySession for a remote project tab", async () => {
    const { result } = renderWithSession("sess-remote");

    act(() => {
      result.current.project.dispatch({ type: "SET_PROJECTS", projects: [remoteProject, localProject] });
      result.current.project.dispatch({ type: "SET_ACTIVE_PROJECT", project: remoteProject });
      result.current.project.dispatch({
        type: "ADD_TAB",
        tab: { id: "sess-remote", projectPath: "/srv/app", title: "Remote", activeSubTab: "chat" as const },
      });
    });

    await act(async () => {
      await result.current.chat.retryLastTurn();
    });

    expect(mockRetrySession).toHaveBeenCalledWith("sess-remote", "devbox");
  });
});

describe("useChat question visibility", () => {
  beforeEach(() => {
    mockCancelQuestion.mockClear();
  });

  it("hides a question locally without calling the server cancellation endpoint", () => {
    const { result } = renderWithSession("sess-hide");
    act(() => {
      result.current.dispatch({
        type: "QUESTION_REQUEST",
        sessionId: "sess-hide",
        question: { request_id: "q-1", questions: [] },
      });
    });

    act(() => result.current.chat.hideQuestion("q-1"));

    expect(result.current.chat.pendingQuestion?.request_id).toBe("q-1");
    expect(result.current.chat.hiddenQuestionRequestId).toBe("q-1");
    expect(mockCancelQuestion).not.toHaveBeenCalled();
  });

  it("retains the explicit final cancellation for the footer action", async () => {
    const { result } = renderWithSession("sess-dont-answer");
    act(() => {
      result.current.dispatch({
        type: "QUESTION_REQUEST",
        sessionId: "sess-dont-answer",
        question: { request_id: "q-1", questions: [] },
      });
    });

    await act(async () => {
      await result.current.chat.cancelQuestion("q-1");
    });

    expect(mockCancelQuestion).toHaveBeenCalledWith("q-1", "sess-dont-answer", undefined);
    expect(result.current.chat.pendingQuestion).toBeNull();
    expect(result.current.chat.hiddenQuestionRequestId).toBeNull();
  });
});

describe("useChat.submitQuestionAnswers optimistic echo", () => {
  beforeEach(() => {
    mockAnswerQuestion.mockClear();
    mockGetSessionState.mockClear();
    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 0 });
  });

  // The answer endpoint acknowledges with 202 and runs the continuation in the
  // background, so the dialog must dismiss WITHOUT waiting for the POST — the
  // submit used to look hung because the local echo was gated on the response.
  it("dismisses and echoes before the answer POST settles", async () => {
    let resolvePost: (v: unknown) => void = () => {};
    mockAnswerQuestion.mockImplementationOnce(() => new Promise((r) => (resolvePost = r)));
    const { result } = renderWithSession("sess-q");

    act(() => {
      result.current.dispatch({
        type: "QUESTION_REQUEST",
        sessionId: "sess-q",
        question: { request_id: "q-1", questions: [] },
      });
    });
    expect(result.current.chat.pendingQuestion?.request_id).toBe("q-1");

    let pending: Promise<boolean> | undefined;
    await act(async () => {
      pending = result.current.chat.submitQuestionAnswers("q-1", []);
      // The POST is still unresolved here; the dialog must already be gone.
      await Promise.resolve();
    });
    expect(result.current.chat.pendingQuestion).toBeNull();

    await act(async () => {
      resolvePost({ status: "ok" });
      expect(await pending).toBe(true);
    });
  });

  // A retryable failure re-hydrates the live ask so the user is not left
  // without a dialog (the continuation never ran).
  it("re-hydrates the question on a retryable failure", async () => {
    mockAnswerQuestion.mockRejectedValueOnce(new Error("agent error: upstream"));
    mockGetSessionState.mockResolvedValueOnce({
      bootstrap_stage: "ready",
      turn_active: false,
      last_seq: 0,
      pending_asks: { questions: [{ request_id: "q-1", questions: [] }] },
    });
    const { result } = renderWithSession("sess-q2");

    act(() => {
      result.current.dispatch({
        type: "QUESTION_REQUEST",
        sessionId: "sess-q2",
        question: { request_id: "q-1", questions: [] },
      });
    });

    await act(async () => {
      expect(await result.current.chat.submitQuestionAnswers("q-1", [])).toBe(false);
    });
    expect(result.current.chat.pendingQuestion?.request_id).toBe("q-1");
  });
});
