import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { usePulseTail } from "./usePulseTail";
import type { BusEnvelope } from "../../lib/eventBus";

const mockGetSessionState = vi.fn();
const mockGetSession = vi.fn();
vi.mock("../../api/client", () => ({
  api: {
    getSessionState: (...a: unknown[]) => mockGetSessionState(...a),
    getSession: (...a: unknown[]) => mockGetSession(...a),
  },
}));

type Handler = (env: BusEnvelope) => void;
const busHandlers = new Map<string, Set<Handler>>();
/** Every unsubscribe the hook obtained and has since been called back. */
let unsubscribeCount = 0;
vi.mock("../../lib/eventBus", () => ({
  eventBus: {
    on: (event: string, handler: Handler) => {
      let set = busHandlers.get(event);
      if (!set) {
        set = new Set();
        busHandlers.set(event, set);
      }
      set.add(handler);
      return () => {
        set.delete(handler);
        unsubscribeCount += 1;
      };
    },
    onReconnect: () => () => {},
  },
}));

/** Fire one SSE envelope at every live "text" subscriber. */
function emitText(sessionId: string, delta: string) {
  act(() => {
    busHandlers.get("text")?.forEach((h) =>
      h({ event: "text", session_id: sessionId, seq: 1, data: { delta } } as BusEnvelope),
    );
  });
}

function textHandlerCount(): number {
  return busHandlers.get("text")?.size ?? 0;
}

function loggedErrors(): string {
  return vi
    .mocked(console.error)
    .mock.calls.map((call) => call.map((a) => String(a)).join(" "))
    .join("\n");
}

/** Resolve-on-demand promise so a test can control WHEN a response lands. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("usePulseTail", () => {
  beforeEach(() => {
    mockGetSessionState.mockReset();
    mockGetSession.mockReset();
    busHandlers.clear();
    unsubscribeCount = 0;
    vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => vi.restoreAllMocks());

  describe("disabled", () => {
    it("subscribes to nothing, fetches nothing, and stays empty", async () => {
      const { result } = renderHook(() => usePulseTail("s1", false, "running"));

      expect(result.current.lines).toEqual([]);
      expect(textHandlerCount()).toBe(0);
      await act(async () => {});
      expect(mockGetSessionState).not.toHaveBeenCalled();
      expect(mockGetSession).not.toHaveBeenCalled();
      expect(result.current.lines).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe("running", () => {
    it("seeds from the buffered text frames, ignoring non-text frames", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [
          { event: "text", data: { delta: "alpha\nbeta" }, seq: 1 },
          { event: "tool_start", data: { tool: "bash" }, seq: 2 },
          { event: "text", data: { delta: "\ngamma" }, seq: 3 },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      await waitFor(() => expect(result.current.lines).toEqual(["alpha", "beta", "gamma"]));
      expect(result.current.error).toBeNull();
    });

    it("appends only this session's deltas, ignoring other sessions", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [{ event: "text", data: { delta: "alpha" }, seq: 1 }],
      });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await waitFor(() => expect(result.current.lines).toEqual(["alpha"]));

      emitText("other-session", "WRONG");

      expect(result.current.lines).toEqual(["alpha"]);

      emitText("s1", "\nbeta");

      expect(result.current.lines).toEqual(["alpha", "beta"]);
    });

    it("keeps only the last 6 lines, newest last", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [{ event: "text", data: { delta: "1\n2\n3\n4\n5\n6\n7\n8" }, seq: 1 }],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      await waitFor(() => expect(result.current.lines).toHaveLength(6));
      expect(result.current.lines).toEqual(["3", "4", "5", "6", "7", "8"]);
    });

    it("reports a seed-fetch failure naming the session instead of an empty tail", async () => {
      mockGetSessionState.mockRejectedValue(new Error("state boom"));

      const { result } = renderHook(() => usePulseTail("ses_abc", true, "running"));

      await waitFor(() => expect(result.current.error).toContain("state boom"));
      expect(result.current.error).toContain("ses_abc");
      expect(loggedErrors()).toContain("ses_abc");
      expect(loggedErrors()).toContain("state boom");
    });
  });

  describe.each(["idle", "error"] as const)("%s", (status) => {
    it("reads the last assistant message and never subscribes to live events", async () => {
      mockGetSession.mockResolvedValue({
        messages: [
          { role: "user", content: "the question" },
          { role: "assistant", content: "old\nreply" },
          { role: "tool", content: "tool output" },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, status));

      await waitFor(() => expect(result.current.lines).toEqual(["old", "reply"]));
      expect(mockGetSession).toHaveBeenCalledWith("s1", { limit: 200 });
      expect(mockGetSessionState).not.toHaveBeenCalled();
      expect(textHandlerCount()).toBe(0);
    });

    it("caps a long last message at the last 6 lines", async () => {
      mockGetSession.mockResolvedValue({
        messages: [{ role: "assistant", content: "1\n2\n3\n4\n5\n6\n7" }],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, status));

      await waitFor(() => expect(result.current.lines).toHaveLength(6));
      expect(result.current.lines).toEqual(["2", "3", "4", "5", "6", "7"]);
    });

    it("reports a transcript-fetch failure naming the session", async () => {
      mockGetSession.mockRejectedValue(new Error("transcript boom"));

      const { result } = renderHook(() => usePulseTail("ses_xyz", true, status));

      await waitFor(() => expect(result.current.error).toContain("transcript boom"));
      expect(result.current.error).toContain("ses_xyz");
      expect(loggedErrors()).toContain("ses_xyz");
    });
  });

  it("is empty (not an error) when the transcript has no assistant message", async () => {
    mockGetSession.mockResolvedValue({ messages: [{ role: "user", content: "hi" }] });

    const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

    await waitFor(() => expect(mockGetSession).toHaveBeenCalled());
    expect(result.current.lines).toEqual([]);
    expect(result.current.error).toBeNull();
  });

  describe("staleness", () => {
    it("ignores a seed response that lands after the session id changed", async () => {
      const first = deferred<unknown>();
      mockGetSessionState.mockImplementation((id: string) =>
        id === "s1"
          ? first.promise
          : Promise.resolve({ live_frames: [{ event: "text", data: { delta: "new-session" }, seq: 1 }] }),
      );

      const { result, rerender } = renderHook(
        ({ id }: { id: string }) => usePulseTail(id, true, "running"),
        { initialProps: { id: "s1" } },
      );
      rerender({ id: "s2" });
      await waitFor(() => expect(result.current.lines).toEqual(["new-session"]));

      await act(async () => {
        first.resolve({ live_frames: [{ event: "text", data: { delta: "STALE" }, seq: 9 }] });
      });

      expect(result.current.lines).toEqual(["new-session"]);
    });

    it("ignores a seed response that lands after the hook was disabled", async () => {
      const first = deferred<unknown>();
      mockGetSessionState.mockImplementation(() => first.promise);

      const { result, rerender } = renderHook(
        ({ enabled }: { enabled: boolean }) => usePulseTail("s1", enabled, "running"),
        { initialProps: { enabled: true } },
      );
      rerender({ enabled: false });

      await act(async () => {
        first.resolve({ live_frames: [{ event: "text", data: { delta: "STALE" }, seq: 9 }] });
      });

      expect(result.current.lines).toEqual([]);
      expect(result.current.error).toBeNull();
    });

    it("ignores a failure that lands after the session id changed", async () => {
      const first = deferred<unknown>();
      mockGetSessionState.mockImplementation((id: string) =>
        id === "s1" ? first.promise : Promise.resolve({ live_frames: [] }),
      );

      const { result, rerender } = renderHook(
        ({ id }: { id: string }) => usePulseTail(id, true, "running"),
        { initialProps: { id: "s1" } },
      );
      rerender({ id: "s2" });
      await waitFor(() => expect(mockGetSessionState).toHaveBeenCalledTimes(2));

      await act(async () => {
        first.reject(new Error("stale boom"));
      });

      expect(result.current.error).toBeNull();
      // The failure is still logged: a swallowed fetch failure is how a card
      // silently shows nothing forever.
      expect(loggedErrors()).toContain("stale boom");
    });
  });

  describe("teardown", () => {
    it("unsubscribes and clears when disabled", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [{ event: "text", data: { delta: "alpha" }, seq: 1 }],
      });
      const { result, rerender } = renderHook(
        ({ enabled }: { enabled: boolean }) => usePulseTail("s1", enabled, "running"),
        { initialProps: { enabled: true } },
      );
      await waitFor(() => expect(result.current.lines).toEqual(["alpha"]));
      expect(unsubscribeCount).toBe(0);

      rerender({ enabled: false });

      expect(unsubscribeCount).toBe(1);
      expect(textHandlerCount()).toBe(0);
      expect(result.current.lines).toEqual([]);

      emitText("s1", "late delta");

      expect(result.current.lines).toEqual([]);
    });

    it("unsubscribes on unmount so a torn-down card cannot keep appending", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });

      const { unmount } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});
      expect(textHandlerCount()).toBe(1);

      unmount();

      expect(unsubscribeCount).toBe(1);
      expect(textHandlerCount()).toBe(0);
    });
  });
  describe("loading", () => {
    // The card's hover overlay needs to tell "the fetch has not answered yet"
    // apart from "there is genuinely nothing to show", or it flashes the
    // empty state on every single hover.

    it("is true while the seed fetch is in flight, then false once it lands", async () => {
      const seed = deferred<unknown>();
      mockGetSessionState.mockReturnValue(seed.promise);

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      expect(result.current.loading).toBe(true);

      await act(async () => {
        seed.resolve({ live_frames: [{ event: "text", data: { delta: "hi" }, seq: 1 }] });
        await seed.promise;
      });

      expect(result.current.loading).toBe(false);
    });

    it("is true while the transcript fetch is in flight on a settled row", async () => {
      const seed = deferred<unknown>();
      mockGetSession.mockReturnValue(seed.promise);

      const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

      expect(result.current.loading).toBe(true);

      await act(async () => {
        seed.resolve({ messages: [{ role: "assistant", content: "done" }] });
        await seed.promise;
      });

      expect(result.current.loading).toBe(false);
    });

    it("clears on failure too, so the error is shown instead of a spinner", async () => {
      mockGetSession.mockRejectedValue(new Error("boom"));

      const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

      await waitFor(() => expect(result.current.error).toContain("boom"));
      expect(result.current.loading).toBe(false);
    });

    it("is false when disabled — nothing is in flight", () => {
      const { result } = renderHook(() => usePulseTail("s1", false, "idle"));

      expect(result.current.loading).toBe(false);
    });
  });
});
