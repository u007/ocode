import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import { ProjectProvider } from "../stores/projectStore";
import { ApiError } from "../api/client";
import { useChat } from "./useChat";

const mockResolvePermission = vi.fn();
const mockSendMessage = vi.fn();
const mockGetSessionState = vi.fn();
vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  return {
    ApiError: actual.ApiError,
    api: {
      resolvePermission: (...a: unknown[]) => mockResolvePermission(...a),
      sendMessage: (...a: unknown[]) => mockSendMessage(...a),
      getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
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

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <ProjectProvider>
      <ChatProvider>{children}</ChatProvider>
    </ProjectProvider>
  );
}

const ask = { request_id: "call-1", tool: "bash", command: "rm -rf build" };

function setup() {
  return renderHook(
    () => ({ chat: useChat("sess-1"), dispatch: useChatDispatch() }),
    { wrapper: Wrapper },
  );
}

describe("useChat.resolvePermission", () => {
  it("dismisses the dialog when the server no longer holds the ask (404/409)", async () => {
    // The agent was released / the server restarted while the tab still showed
    // the ask from its in-memory slice: the resolve can never succeed, so the
    // dialog must not stay stuck open.
    mockResolvePermission.mockRejectedValueOnce(
      new ApiError("no pending permission found for request_id", 404),
    );
    const { result } = setup();
    act(() => result.current.dispatch({ type: "PERMISSION_REQUEST", sessionId: "sess-1", permission: ask }));
    expect(result.current.chat.pendingPermission?.request_id).toBe("call-1");

    let outcome: Awaited<ReturnType<typeof result.current.chat.resolvePermission>> | undefined;
    await act(async () => {
      outcome = await result.current.chat.resolvePermission("call-1", "allow");
    });
    expect(outcome).toEqual({ ok: false, error: "no pending permission found for request_id" });
    expect(result.current.chat.pendingPermission).toBeNull();
  });

  it("keeps the dialog open on a retryable failure (network / 5xx)", async () => {
    mockResolvePermission.mockRejectedValueOnce(new ApiError("agent error: upstream", 500));
    const { result } = setup();
    act(() => result.current.dispatch({ type: "PERMISSION_REQUEST", sessionId: "sess-1", permission: ask }));

    let outcome: Awaited<ReturnType<typeof result.current.chat.resolvePermission>> | undefined;
    await act(async () => {
      outcome = await result.current.chat.resolvePermission("call-1", "allow");
    });
    expect(outcome).toEqual({ ok: false, error: "agent error: upstream" });
    expect(result.current.chat.pendingPermission?.request_id).toBe("call-1");
  });

  it("dismisses the dialog on success", async () => {
    mockResolvePermission.mockResolvedValueOnce({});
    const { result } = setup();
    act(() => result.current.dispatch({ type: "PERMISSION_REQUEST", sessionId: "sess-1", permission: ask }));
    await act(async () => {
      expect(await result.current.chat.resolvePermission("call-1", "deny")).toEqual({ ok: true });
    });
    expect(mockResolvePermission).toHaveBeenCalledWith("call-1", "sess-1", "deny");
    expect(result.current.chat.pendingPermission).toBeNull();
  });
});

describe("useChat.sendMessage pending-ask recovery", () => {
  // The live `permission` frame can be missed (backgrounded tab) and the
  // sentinel may never reach disk, leaving the user stuck on
  // ErrPermissionPending with no dialog. A refused send must recover the ask
  // from the server's live session state and open the dialog.
  it("hydrates the permission dialog when a send is refused as pending (409)", async () => {
    mockSendMessage.mockRejectedValueOnce(
      new ApiError("a permission decision is pending for this session; resolve it before sending a new message", 409),
    );
    mockGetSessionState.mockResolvedValueOnce({
      bootstrap_stage: "",
      turn_active: false,
      last_seq: 1,
      pending_asks: { permissions: [ask] },
    });
    const { result } = setup();
    await act(async () => {
      await result.current.chat.sendMessage("continue");
    });
    expect(mockGetSessionState).toHaveBeenCalledWith("sess-1");
    expect(result.current.chat.pendingPermission?.request_id).toBe("call-1");
  });

  it("leaves the dialog untouched when the refusal is not a pending ask", async () => {
    mockGetSessionState.mockClear();
    mockSendMessage.mockRejectedValueOnce(new ApiError("agent error: upstream", 500));
    const { result } = setup();
    await act(async () => {
      await result.current.chat.sendMessage("continue");
    });
    expect(mockGetSessionState).not.toHaveBeenCalled();
    expect(result.current.chat.pendingPermission).toBeNull();
  });
});
