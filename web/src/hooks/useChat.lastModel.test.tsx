import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ProjectProvider } from "../stores/projectStore";
import { ChatProvider, getSessionSlice, useChatDispatch, useChatState } from "../stores/chatStore";
import { useChat } from "./useChat";

const mocks = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  retrySession: vi.fn(),
  resolvePermission: vi.fn(),
  answerQuestion: vi.fn(),
  loadPendingRewind: vi.fn(),
  clearPendingRewind: vi.fn(),
}));

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      sendMessage: (...args: unknown[]) => mocks.sendMessage(...args),
      retrySession: (...args: unknown[]) => mocks.retrySession(...args),
      resolvePermission: (...args: unknown[]) => mocks.resolvePermission(...args),
      answerQuestion: (...args: unknown[]) => mocks.answerQuestion(...args),
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
  loadPendingRewind: (...args: unknown[]) => mocks.loadPendingRewind(...args),
  clearPendingRewind: (...args: unknown[]) => mocks.clearPendingRewind(...args),
}));

vi.mock("../lib/eventBus", () => ({
  eventBus: { on: () => () => {}, onReconnect: () => () => {} },
}));

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <ProjectProvider>
      <ChatProvider>{children}</ChatProvider>
    </ProjectProvider>
  );
}

function setup() {
  return renderHook(
    () => ({
      chat: useChat("s1"),
      state: useChatState(),
      dispatch: useChatDispatch(),
    }),
    { wrapper: Wrapper },
  );
}

describe("useChat last dispatched model", () => {
  beforeEach(() => {
    mocks.sendMessage.mockReset();
    mocks.retrySession.mockReset();
    mocks.resolvePermission.mockReset();
    mocks.answerQuestion.mockReset();
    mocks.loadPendingRewind.mockReset().mockReturnValue(undefined);
    mocks.clearPendingRewind.mockReset().mockReturnValue(true);
  });

  it("records the backend model after a successful send", async () => {
    mocks.sendMessage.mockResolvedValueOnce({ sessionId: "s1", model: "openai/gpt-4o" });
    mocks.loadPendingRewind.mockReturnValue(undefined);
    const { result } = setup();

    let accepted = false;
    await act(async () => {
      accepted = await result.current.chat.sendMessage("hello");
    });

    expect(accepted).toBe(true);
    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("openai/gpt-4o");
  });

  it("retains the previous model when the send is rejected", async () => {
    mocks.sendMessage.mockRejectedValueOnce(new Error("network down"));
    const { result } = setup();
    act(() => {
      result.current.dispatch({ type: "SET_LAST_DISPATCHED_MODEL", sessionId: "s1", model: "openai/gpt-4o" });
    });

    let accepted = true;
    await act(async () => {
      accepted = await result.current.chat.sendMessage("hello");
    });

    expect(accepted).toBe(false);
    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("openai/gpt-4o");
  });

  it("retains the previous model when an accepted response has no model", async () => {
    mocks.sendMessage.mockResolvedValueOnce({ sessionId: "s1", model: "  " });
    const { result } = setup();
    act(() => {
      result.current.dispatch({ type: "SET_LAST_DISPATCHED_MODEL", sessionId: "s1", model: "openai/gpt-4o" });
    });

    await act(async () => {
      expect(await result.current.chat.sendMessage("hello")).toBe(true);
    });

    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("openai/gpt-4o");
  });

  it("records the model returned by a retry dispatch", async () => {
    mocks.retrySession.mockResolvedValueOnce({ sessionId: "s1", model: "retry/model" });
    const { result } = setup();

    await act(async () => {
      expect(await result.current.chat.retryLastTurn()).toBe(true);
    });

    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("retry/model");
  });

  it("records the model returned by a permission continuation", async () => {
    mocks.resolvePermission.mockResolvedValueOnce({ sessionId: "s1", model: "permission/model" });
    const { result } = setup();

    await act(async () => {
      expect(await result.current.chat.resolvePermission("call-1", "allow")).toEqual({ ok: true });
    });

    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("permission/model");
  });

  it("records the model returned by a question continuation", async () => {
    mocks.answerQuestion.mockResolvedValueOnce({ sessionId: "s1", model: "question/model" });
    const { result } = setup();

    await act(async () => {
      expect(await result.current.chat.submitQuestionAnswers("call-1", [])).toBe(true);
    });

    expect(getSessionSlice(result.current.state, "s1").lastDispatchedModel).toBe("question/model");
  });
});
