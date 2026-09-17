import { describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import { ProjectProvider, useProjectState } from "../stores/projectStore";
import { useChat } from "./useChat";

// Perf regression coverage for useChat's subscription shape.
//
// `useChat` used to subscribe to the whole per-session slice
// (`useChatSelector((s) => getSessionSlice(s, sessionId))`). Because
// `updateSession` replaces the slice object on every dispatch, that re-rendered
// every consumer — HomeApp plus one ChatInput per open tab — on each streamed
// token (LIVE_DELTA flushes every 90ms, see LIVE_DELTA_FLUSH_MS). The hook now
// subscribes field-by-field, so a dispatch that does not change a returned
// field must not re-render the consumer.
//
// These tests pin that contract: streaming-heavy dispatches cost nothing,
// while the returned fields still drive renders when they actually change.

vi.mock("../api/client", async () => {
  const actual = await vi.importActual<typeof import("../api/client")>("../api/client");
  const chat = vi.fn().mockResolvedValue({ sessionId: "real-1" });
  return {
    ApiError: actual.ApiError,
    api: {
      chat,
      sendMessage: vi.fn().mockResolvedValue({ status: "ok" }),
      listProjects: vi.fn().mockResolvedValue([{ path: "/tmp/proj", name: "proj" }]),
      getCurrentProject: vi
        .fn()
        .mockResolvedValue({ project: { path: "/tmp/proj", name: "proj" } }),
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

import { api } from "../api/client";

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <ProjectProvider>
      <ChatProvider>{children}</ChatProvider>
    </ProjectProvider>
  );
}

/** Renders useChat and counts how many times the consuming component renders. */
function setup(sessionId: string) {
  const counts = { renders: 0 };
  const hook = renderHook(
    () => {
      counts.renders += 1;
      return { chat: useChat(sessionId), dispatch: useChatDispatch() };
    },
    { wrapper: Wrapper },
  );
  return { counts, hook };
}

/** Let the ProjectProvider's mount/auto-select fetches settle — it kicks off
 *  several chained async dispatches (refreshProjects → listProjects →
 *  selectProject → listProjectSessions) whose re-renders must land before the
 *  baseline count is captured. Wait until the count stops changing across a
 *  macrotask boundary. */
async function settle(counts: { renders: number }) {
  let previous = -1;
  for (let i = 0; i < 25 && previous !== counts.renders; i++) {
    previous = counts.renders;
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }
  return counts.renders;
}

describe("useChat render cost", () => {
  it("does not re-render on streaming deltas that leave the returned fields unchanged", async () => {
    const { counts, hook } = setup("sess-1");
    await settle(counts);
    const before = counts.renders;

    act(() => {
      // A burst of the dispatches a streaming turn produces. None of these
      // touch pendingPermission / pendingQuestion / wasInterrupted /
      // isStreaming (turnActive stays false), so the consumer must not render.
      hook.result.current.dispatch({
        type: "LIVE_DELTA",
        sessionId: "sess-1",
        kind: "thinking",
        delta: "thinking…",
      });
      hook.result.current.dispatch({
        type: "LIVE_DELTA",
        sessionId: "sess-1",
        kind: "text",
        delta: "Hello",
      });
      hook.result.current.dispatch({
        type: "LIVE_TOOL_START",
        sessionId: "sess-1",
        tool: "bash",
        callId: "c1",
      });
      hook.result.current.dispatch({
        type: "LIVE_TOOL_OUTPUT",
        sessionId: "sess-1",
        callId: "c1",
        chunk: "output",
      });
      hook.result.current.dispatch({
        type: "SET_TOTAL",
        sessionId: "sess-1",
        total: 42,
      });
    });

    expect(counts.renders).toBe(before);
  });

  it("still re-renders when a returned field changes", async () => {
    const { counts, hook } = setup("sess-2");
    await settle(counts);

    const before = counts.renders;
    act(() => {
      hook.result.current.dispatch({ type: "SET_STREAMING", sessionId: "sess-2", isStreaming: true });
    });
    expect(counts.renders).toBeGreaterThan(before);

    const afterStreaming = counts.renders;
    act(() => {
      hook.result.current.dispatch({ type: "SET_TURN_STATE", sessionId: "sess-2", turnActive: true });
    });
    expect(hook.result.current.chat.isStreaming).toBe(true);
    // turnActive flips the same derived boolean; it was already true so the
    // selector value is unchanged and no extra render is required.
    expect(counts.renders).toBe(afterStreaming);

    const afterTurn = counts.renders;
    act(() => {
      hook.result.current.dispatch({
        type: "SET_WAS_INTERRUPTED",
        sessionId: "sess-2",
        wasInterrupted: true,
      });
    });
    expect(hook.result.current.chat.wasInterrupted).toBe(true);
    expect(counts.renders).toBeGreaterThan(afterTurn);
  });

  it("reads a draft tab's model imperatively at send time", async () => {
    // Exposes project state so the test can wait for the auto-selected project;
    // without a project the draft send short-circuits before reaching api.chat.
    const hook = renderHook(
      () => ({ chat: useChat("new-123"), dispatch: useChatDispatch(), project: useProjectState() }),
      { wrapper: Wrapper },
    );
    await waitFor(() =>
      expect(hook.result.current.project.state.activeProject?.path).toBe("/tmp/proj"),
    );

    act(() => {
      // The sidebar Model picker stores the draft tab's model on its slice.
      const dispatch = hook.result.current.dispatch;
      dispatch({ type: "SET_SESSION_MODEL", sessionId: "new-123", model: "claude-x" });
    });

    await act(async () => {
      await hook.result.current.chat.sendMessage("hi");
    });

    const chatMock = api.chat as unknown as ReturnType<typeof vi.fn>;
    expect(chatMock).toHaveBeenCalledWith("hi", undefined, "claude-x", "new-123", "/tmp/proj", undefined);
  });
});
