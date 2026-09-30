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
    // Seed one committed message first so `hasConversation` is already true:
    // the FIRST live part on an empty session legitimately flips it (that
    // one-time render is pinned in useChat.test.tsx). Here we pin the steady
    // state — mid-turn deltas must cost nothing.
    act(() => {
      hook.result.current.dispatch({
        type: "SET_MESSAGES",
        sessionId: "sess-1",
        messages: [{ role: "user", content: "hello" }],
      });
    });
    await settle(counts);
    const before = counts.renders;

    act(() => {
      // A burst of the dispatches a streaming turn produces. None of these
      // touch pendingPermission / pendingQuestion / wasInterrupted /
      // isStreaming (turnActive stays false) and hasConversation is already
      // true, so the consumer must not render.
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

  it("does not re-render for the recent-inputs fields on unrelated dispatches", async () => {
    // `recentInputs` is the one returned field whose selector allocates a FRESH
    // array on every call (recentUserInputs builds it), so it is the one that
    // genuinely needs a shallow comparator: without it, every streamed token
    // would hand the composer a new array identity and re-render it. The store
    // field it reads, `transcriptScrolledUp`, is a boolean that only flips when
    // the reader crosses the tail threshold.
    const { counts, hook } = setup("sess-recent");
    act(() => {
      hook.result.current.dispatch({
        type: "SET_MESSAGES",
        sessionId: "sess-recent",
        messages: [
          { role: "user", content: "first ask" },
          { role: "assistant", content: "ok" },
          { role: "user", content: "second ask" },
        ],
      });
    });
    await settle(counts);
    const before = counts.renders;

    act(() => {
      hook.result.current.dispatch({
        type: "LIVE_DELTA",
        sessionId: "sess-recent",
        kind: "text",
        delta: "working…",
      });
      hook.result.current.dispatch({
        type: "SET_TOTAL",
        sessionId: "sess-recent",
        total: 3,
      });
    });
    expect(counts.renders).toBe(before);
  });

  it("re-renders when the recent inputs themselves change, and hides injections", async () => {
    const { counts, hook } = setup("sess-recent-2");
    act(() => {
      hook.result.current.dispatch({
        type: "SET_MESSAGES",
        sessionId: "sess-recent-2",
        messages: [{ role: "user", content: "first ask" }],
      });
    });
    await settle(counts);
    expect(hook.result.current.chat.recentInputs).toEqual(["first ask"]);

    // A NEW input must re-render and be reflected.
    const before = counts.renders;
    act(() => {
      hook.result.current.dispatch({
        type: "SET_MESSAGES",
        sessionId: "sess-recent-2",
        messages: [
          { role: "user", content: "first ask" },
          { role: "assistant", content: "ok" },
          { role: "user", content: "second ask" },
        ],
      });
    });
    expect(counts.renders).toBeGreaterThan(before);
    expect(hook.result.current.chat.recentInputs).toEqual(["first ask", "second ask"]);

    // A system-injected user-role message must NOT enter the strip, and must not
    // cost a render (the visible texts are unchanged).
    const afterInputs = counts.renders;
    act(() => {
      hook.result.current.dispatch({
        type: "SET_MESSAGES",
        sessionId: "sess-recent-2",
        messages: [
          { role: "user", content: "first ask" },
          { role: "assistant", content: "ok" },
          { role: "user", content: "second ask" },
          { role: "user", content: "[advisor plan checkpoint] An advisor reviewed the changes:" },
        ],
      });
    });
    expect(hook.result.current.chat.recentInputs).toEqual(["first ask", "second ask"]);
    expect(counts.renders).toBe(afterInputs);
  });

  it("re-renders when transcriptScrolledUp flips", async () => {
    const { counts, hook } = setup("sess-scrolled-flag");
    await settle(counts);
    const before = counts.renders;
    act(() => {
      hook.result.current.dispatch({
        type: "SET_TRANSCRIPT_SCROLLED_UP",
        sessionId: "sess-scrolled-flag",
        scrolledUp: true,
      });
    });
    expect(hook.result.current.chat.transcriptScrolledUp).toBe(true);
    expect(counts.renders).toBeGreaterThan(before);
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
    expect(chatMock).toHaveBeenCalledWith("hi", undefined, "claude-x", "new-123", "/tmp/proj", undefined, undefined);
  });
});
