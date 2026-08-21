import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { ChatProvider, useChatDispatch } from "../stores/chatStore";
import { RECONCILE_PAGE_SIZE } from "../lib/sessionEvents";
import { useTurnWatchdogAll, applyReconcileState, STALL_THRESHOLD_MS } from "./useTurnWatchdog";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
  },
}));

function Wrapper({ children }: { children: ReactNode }) {
  return <ChatProvider>{children}</ChatProvider>;
}

describe("applyReconcileState", () => {
  it("clears turn state, streaming and stall when the server reports turn_active=false", () => {
    const actions: unknown[] = [];
    applyReconcileState(
      (a) => actions.push(a),
      "s1",
      { bootstrap_stage: "ready", turn_active: false, last_seq: 9 },
    );
    expect(actions).toEqual([
      { type: "SET_TURN_STATE", sessionId: "s1", turnActive: false },
      { type: "SET_STREAMING", sessionId: "s1", isStreaming: false },
      { type: "SET_TURN_STALLED", sessionId: "s1", stalled: false },
    ]);
  });

  it("records the bootstrap stage when the turn is still active", () => {
    const actions: unknown[] = [];
    applyReconcileState((a) => actions.push(a), "s1", {
      bootstrap_stage: "mcp",
      turn_active: true,
      last_seq: 3,
    });
    expect(actions).toEqual([
      { type: "SET_BOOTSTRAP_STAGE", sessionId: "s1", stage: "mcp" },
    ]);
  });
});

describe("useTurnWatchdogAll", () => {
  beforeEach(() => {
    // Fake Date too, so the watchdog's Date.now() comparisons advance with the
    // timers (lastHeartbeatAt stamping relies on it).
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date"] });
    mockGetSessionState.mockReset();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("does nothing for placeholder new-* tabs", () => {
    renderHook(() => useTurnWatchdogAll(new Set(["new-123"])), { wrapper: Wrapper });
    vi.advanceTimersByTime(STALL_THRESHOLD_MS + 10_000);
    expect(mockGetSessionState).not.toHaveBeenCalled();
  });

  it("does nothing while every session is idle (no active turn)", () => {
    renderHook(() => useTurnWatchdogAll(new Set(["s1", "s2"])), { wrapper: Wrapper });
    vi.advanceTimersByTime(STALL_THRESHOLD_MS + 10_000);
    expect(mockGetSessionState).not.toHaveBeenCalled();
  });

  it("marks a stalled turn and reconciles once the heartbeat stops, for a backgrounded session too", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    // Watchdog + turn activator live in the same component so the dispatch
    // lands in the same store the watchdog reads. "s2" stands in for a tab
    // that is open but not the active one.
    renderHook(
      ({ ids }: { ids: string[] }) => {
        useTurnWatchdogAll(new Set(ids));
        const dispatch = useChatDispatch();
        useEffect(() => {
          ids.forEach((id) => dispatch({ type: "SET_TURN_STATE", sessionId: id, turnActive: true }));
        }, [dispatch, ids]);
        return null;
      },
      { initialProps: { ids: ["s1", "s2"] }, wrapper: Wrapper },
    );

    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "ready", turn_active: false, last_seq: 9 });
    mockGetSession.mockResolvedValue({
      messages: [
        { role: "user", content: "hi" },
        { role: "assistant", content: "done" },
      ],
      total: 2,
    });
    // Advance past the stall threshold plus one watchdog tick.
    vi.advanceTimersByTime(STALL_THRESHOLD_MS + 5_000);
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve(); // settle the reconcile promise chain (state fetch + transcript refetch)

    expect(warn).toHaveBeenCalledWith(expect.stringContaining("s1"));
    expect(warn).toHaveBeenCalledWith(expect.stringContaining("s2"));
    expect(mockGetSessionState).toHaveBeenCalledWith("s1");
    expect(mockGetSessionState).toHaveBeenCalledWith("s2");
    // The turn finished server-side while the client thought it active, so
    // the committed transcript is refetched (recovery = refetch, not replay).
    expect(mockGetSession).toHaveBeenCalledWith("s1", { limit: RECONCILE_PAGE_SIZE });
    expect(mockGetSession).toHaveBeenCalledWith("s2", { limit: RECONCILE_PAGE_SIZE });
    warn.mockRestore();
  });

  it("does not re-mark a stall that was already reported", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    renderHook(
      ({ id }: { id: string }) => {
        useTurnWatchdogAll(new Set([id]));
        const dispatch = useChatDispatch();
        useEffect(() => {
          dispatch({ type: "SET_TURN_STATE", sessionId: id, turnActive: true });
        }, [dispatch, id]);
        return null;
      },
      { initialProps: { id: "s1" }, wrapper: Wrapper },
    );

    mockGetSessionState.mockResolvedValue({ bootstrap_stage: "", turn_active: true, last_seq: 9 });
    await act(async () => {
      vi.advanceTimersByTime(STALL_THRESHOLD_MS + 5_000);
    });
    // Second tick: still stalled, but the warn fires once (turnStalled gate —
    // the SET_TURN_STALLED dispatch from the first stall must have flushed).
    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });
    expect(warn.mock.calls.filter((c) => String(c[0]).includes("stalled")).length).toBe(1);
    // Turn still active server-side — no transcript refetch mid-turn.
    expect(mockGetSession).not.toHaveBeenCalled();
    warn.mockRestore();
  });
});
