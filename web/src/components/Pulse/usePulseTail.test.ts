import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { PULSE_TAIL_ENTRIES, usePulseTail, type PulseTailEntry } from "./usePulseTail";
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

/** Fire one SSE envelope at every live subscriber of `event`. */
function emit(event: string, sessionId: string, data: unknown) {
  act(() => {
    busHandlers.get(event)?.forEach((h) =>
      h({ event, session_id: sessionId, seq: 1, data } as BusEnvelope),
    );
  });
}

function emitText(sessionId: string, delta: string) {
  emit("text", sessionId, { delta });
}

/** Live subscriptions across every event the hook listens to. */
function handlerCount(): number {
  let n = 0;
  busHandlers.forEach((set) => (n += set.size));
  return n;
}

/** The hook subscribes to exactly these three events and no others. */
const LIVE_EVENT_COUNT = 3;

const texts = (...lines: string[]): PulseTailEntry[] => lines.map((text) => ({ kind: "text", text }));
const joined = (entries: PulseTailEntry[]) => entries.map((e) => e.text).join("");

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

  // Regression: the live "text" subscription is installed synchronously after
  // the seed request is fired, so chunks arriving before the seed resolves were
  // appended FIRST and the seed's own live_frames appended after them — showing
  // the same frame twice and reading out of order.
  describe("seed / live ordering", () => {
    it("orders the seed before live chunks and does not duplicate a frame", async () => {
      const seed = deferred<{ live_frames?: { event: string; data: unknown }[] }>();
      mockGetSessionState.mockReturnValue(seed.promise);

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      // The subscription is live before the seed lands, so this chunk is
      // already in flight when the seed resolves.
      emitText("s1", "world");

      await act(async () => {
        seed.resolve({
          live_frames: [
            { event: "text", data: { delta: "hello " } },
            { event: "tool_start", data: { tool: "bash" } },
          ],
        });
      });

      await waitFor(() => expect(result.current.entries.length).toBeGreaterThan(0));
      // The seed's tool_start is a tool entry between the seed text and the
      // held live chunk, so "hello " and "world" are separate text entries.
      expect(result.current.entries).toEqual([
        { kind: "text", text: "hello " },
        { kind: "tool", text: "▸ bash" },
        { kind: "text", text: "world" },
      ]);
    });

    it("keeps appending live chunks normally once the seed has landed", async () => {
      const seed = deferred<{ live_frames?: { event: string; data: unknown }[] }>();
      mockGetSessionState.mockReturnValue(seed.promise);

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {
        seed.resolve({ live_frames: [{ event: "text", data: { delta: "one " } }] });
      });
      emitText("s1", "two ");

      await waitFor(() => expect(joined(result.current.entries)).toBe("one two "));
    });
  });

  describe("disabled", () => {
    it("subscribes to nothing, fetches nothing, and stays empty", async () => {
      const { result } = renderHook(() => usePulseTail("s1", false, "running"));

      expect(result.current.entries).toEqual([]);
      expect(handlerCount()).toBe(0);
      await act(async () => {});
      expect(mockGetSessionState).not.toHaveBeenCalled();
      expect(mockGetSession).not.toHaveBeenCalled();
      expect(result.current.entries).toEqual([]);
      expect(result.current.error).toBeNull();
    });
  });

  describe("running", () => {
    it("seeds text and tool frames in order, ignoring thinking and tool_output", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [
          { event: "text", data: { delta: "alpha\nbeta" }, seq: 1 },
          { event: "thinking", data: { delta: "hmm" }, seq: 2 },
          { event: "tool_start", data: { tool: "bash", command: '{"command":"ls -la"}', call_id: "c1" }, seq: 3 },
          { event: "tool_output", data: { call_id: "c1", chunk: "noise" }, seq: 4 },
          { event: "tool_result", data: { call_id: "c1", output: "ok" }, seq: 5 },
          { event: "text", data: { delta: "\ngamma" }, seq: 6 },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      await waitFor(() =>
        expect(result.current.entries).toEqual([
          { kind: "text", text: "alpha" },
          { kind: "text", text: "beta" },
          { kind: "tool", text: "▸ bash ls -la ✓" },
          { kind: "text", text: "gamma" },
        ]),
      );
      expect(result.current.error).toBeNull();
    });

    it("appends only this session's deltas, ignoring other sessions", async () => {
      mockGetSessionState.mockResolvedValue({
        live_frames: [{ event: "text", data: { delta: "alpha" }, seq: 1 }],
      });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await waitFor(() => expect(result.current.entries).toEqual(texts("alpha")));

      emitText("other-session", "WRONG");

      expect(result.current.entries).toEqual(texts("alpha"));

      emitText("s1", "\nbeta");

      expect(result.current.entries).toEqual(texts("alpha", "beta"));
    });

    it("appends a tool_start as a tool entry, with the command clipped to 80 chars", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      emit("tool_start", "s1", { tool: "read", command: JSON.stringify({ command: "x".repeat(200) }), call_id: "c1" });
      emit("tool_start", "s1", { tool: "todoread" });

      expect(result.current.entries).toEqual([
        { kind: "tool", text: `▸ read ${"x".repeat(80)}…` },
        { kind: "tool", text: "▸ todoread" },
      ]);
    });

    it("summarizes live tool_start args: command, then file_path, else name only", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      emit("tool_start", "s1", { tool: "bash", command: '{"command":"echo hi","path":"/p"}' });
      emit("tool_start", "s1", { tool: "read", command: '{"file_path":"/a/b.go"}' });
      emit("tool_start", "s1", { tool: "bash", command: "echo raw, not json" });
      emit("tool_start", "s1", { tool: "todoread", command: "{}" });

      expect(result.current.entries).toEqual([
        { kind: "tool", text: "▸ bash echo hi" },
        { kind: "tool", text: "▸ read /a/b.go" },
        { kind: "tool", text: "▸ bash" },
        { kind: "tool", text: "▸ todoread" },
      ]);
    });

    it("continues the open text entry, and starts a new one after a tool entry", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      emitText("s1", "before ");
      emitText("s1", "tool\n");
      emit("tool_start", "s1", { tool: "bash" });
      emitText("s1", "after");

      expect(result.current.entries).toEqual([
        { kind: "text", text: "before tool" },
        { kind: "tool", text: "▸ bash" },
        { kind: "text", text: "after" },
      ]);
    });

    it("marks the matching tool entry done on tool_result, and failed on an error output", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      emit("tool_start", "s1", { tool: "bash", command: '{"command":"a"}', call_id: "c1" });
      emit("tool_start", "s1", { tool: "bash", command: '{"command":"b"}', call_id: "c2" });
      emit("tool_result", "s1", { call_id: "c2", output: "Error: exit 1" });
      emit("tool_result", "s1", { call_id: "c1", output: "fine" });

      expect(result.current.entries).toEqual([
        { kind: "tool", text: "▸ bash a ✓" },
        { kind: "tool", text: "▸ bash b ✗" },
      ]);
    });

    it("ignores a tool_result for an unknown call_id and a repeated result", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      emit("tool_start", "s1", { tool: "bash", call_id: "c1" });
      emit("tool_result", "s1", { call_id: "nope", output: "ok" });
      emit("tool_result", "s1", { output: "no id at all" });
      expect(result.current.entries).toEqual([{ kind: "tool", text: "▸ bash" }]);

      emit("tool_result", "s1", { call_id: "c1", output: "ok" });
      emit("tool_result", "s1", { call_id: "c1", output: "ok" });
      expect(result.current.entries).toEqual([{ kind: "tool", text: "▸ bash ✓" }]);
    });

    it("does not subscribe to thinking or tool_output", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });
      renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});

      expect([...busHandlers.keys()].sort()).toEqual(["text", "tool_result", "tool_start"]);
    });

    it("holds live tool events until the seed lands, then replays them after the seed", async () => {
      const seed = deferred<{ live_frames?: { event: string; data: unknown }[] }>();
      mockGetSessionState.mockReturnValue(seed.promise);
      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      // The result belongs to a call that only the SEED knows about, so applying
      // it before the seed would be dropped as unknown.
      emit("tool_result", "s1", { call_id: "c1", output: "ok" });
      await act(async () => {
        seed.resolve({
          live_frames: [{ event: "tool_start", data: { tool: "bash", call_id: "c1" } }],
        });
      });

      await waitFor(() =>
        expect(result.current.entries).toEqual([{ kind: "tool", text: "▸ bash ✓" }]),
      );
    });

    // Asserted against PULSE_TAIL_ENTRIES rather than a hardcoded 60, so the
    // test follows the constant instead of going stale when it moves.
    it("keeps only the last PULSE_TAIL_ENTRIES entries, newest last, tool entries included", async () => {
      const lines = Array.from({ length: PULSE_TAIL_ENTRIES }, (_, i) => String(i + 1));
      mockGetSessionState.mockResolvedValue({
        live_frames: [
          { event: "text", data: { delta: lines.join("\n") }, seq: 1 },
          { event: "tool_start", data: { tool: "bash" }, seq: 2 },
          { event: "text", data: { delta: "last" }, seq: 3 },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "running"));

      await waitFor(() => expect(result.current.entries).toHaveLength(PULSE_TAIL_ENTRIES));
      // Two entries were added past the cap, so the two oldest lines fell off.
      expect(result.current.entries).toEqual([
        ...texts(...lines.slice(2)),
        { kind: "tool", text: "▸ bash" },
        { kind: "text", text: "last" },
      ]);
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

      await waitFor(() => expect(result.current.entries).toEqual(texts("old", "reply")));
      // `noteRevision: false` is load-bearing, not incidental: this is a
      // speculative read of a card, and recording a baseline would stamp a
      // fresh revision over an open tab that is still showing older content.
      expect(mockGetSession).toHaveBeenCalledWith(
        "s1",
        expect.objectContaining({ limit: 200, noteRevision: false }),
      );
      expect(mockGetSessionState).not.toHaveBeenCalled();
      expect(handlerCount()).toBe(0);
    });

    it("caps a long last message at the last PULSE_TAIL_ENTRIES entries", async () => {
      const all = Array.from({ length: PULSE_TAIL_ENTRIES + 2 }, (_, i) => String(i + 1));
      mockGetSession.mockResolvedValue({
        messages: [{ role: "assistant", content: all.join("\n") }],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, status));

      await waitFor(() => expect(result.current.entries).toHaveLength(PULSE_TAIL_ENTRIES));
      expect(result.current.entries).toEqual(texts(...all.slice(-PULSE_TAIL_ENTRIES)));
    });

    it("reports a transcript-fetch failure naming the session", async () => {
      mockGetSession.mockRejectedValue(new Error("transcript boom"));

      const { result } = renderHook(() => usePulseTail("ses_xyz", true, status));

      await waitFor(() => expect(result.current.error).toContain("transcript boom"));
      expect(result.current.error).toContain("ses_xyz");
      expect(loggedErrors()).toContain("ses_xyz");
    });
  });

  describe("finished turn activity feed", () => {
    const call = (id: string, name: string, args: string) => ({ id, function: { name, arguments: args } });

    it("replays the last turn's prose and tool calls in order, marking results", async () => {
      mockGetSession.mockResolvedValue({
        messages: [
          { role: "user", content: "first question" },
          { role: "assistant", content: "OLD TURN", tool_calls: [call("old", "bash", "{}")] },
          { role: "user", content: "second question" },
          {
            role: "assistant",
            content: "looking",
            tool_calls: [call("c1", "bash", '{"command":"ls -la"}'), call("c2", "read", '{"path":"x"}')],
          },
          { role: "tool", tool_call_id: "c1", content: "total 0" },
          { role: "tool", tool_call_id: "c2", content: "Error: no such file" },
          { role: "assistant", content: "done\nall good" },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

      await waitFor(() =>
        expect(result.current.entries).toEqual([
          { kind: "text", text: "looking" },
          { kind: "tool", text: "▸ bash ls -la ✓" },
          { kind: "tool", text: "▸ read x ✗" },
          { kind: "text", text: "done" },
          { kind: "text", text: "all good" },
        ]),
      );
    });

    it("uses the whole fetched slice when it holds no user message", async () => {
      mockGetSession.mockResolvedValue({
        messages: [
          { role: "assistant", content: "", tool_calls: [call("c1", "bash", '{"command":"pwd"}')] },
          { role: "tool", tool_call_id: "c1", content: "/tmp" },
          { role: "assistant", content: "ok" },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "error"));

      await waitFor(() =>
        expect(result.current.entries).toEqual([
          { kind: "tool", text: "▸ bash pwd ✓" },
          { kind: "text", text: "ok" },
        ]),
      );
    });

    it("shows the name only when arguments are not JSON, without logging", async () => {
      mockGetSession.mockResolvedValue({
        messages: [
          { role: "user", content: "q" },
          {
            role: "assistant",
            content: "",
            tool_calls: [call("c1", "bash", "not { json"), call("c2", "todoread", '{"other":1}')],
          },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

      await waitFor(() =>
        expect(result.current.entries).toEqual([
          { kind: "tool", text: "▸ bash" },
          { kind: "tool", text: "▸ todoread" },
        ]),
      );
      expect(console.error).not.toHaveBeenCalled();
    });

    it("does not treat an [ocode: notice as the turn boundary", async () => {
      mockGetSession.mockResolvedValue({
        messages: [
          { role: "user", content: "real question" },
          { role: "assistant", content: "before notice" },
          { role: "user", content: "[ocode:event] something happened" },
          { role: "assistant", content: "after notice" },
        ],
      });

      const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

      await waitFor(() =>
        expect(result.current.entries).toEqual(texts("before notice", "after notice")),
      );
    });
  });

  it("is empty (not an error) when the transcript has no assistant message", async () => {
    mockGetSession.mockResolvedValue({ messages: [{ role: "user", content: "hi" }] });

    const { result } = renderHook(() => usePulseTail("s1", true, "idle"));

    await waitFor(() => expect(mockGetSession).toHaveBeenCalled());
    expect(result.current.entries).toEqual([]);
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
      await waitFor(() => expect(result.current.entries).toEqual(texts("new-session")));

      await act(async () => {
        first.resolve({ live_frames: [{ event: "text", data: { delta: "STALE" }, seq: 9 }] });
      });

      expect(result.current.entries).toEqual(texts("new-session"));
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

      expect(result.current.entries).toEqual([]);
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
      await waitFor(() => expect(result.current.entries).toEqual(texts("alpha")));
      expect(unsubscribeCount).toBe(0);

      rerender({ enabled: false });

      expect(unsubscribeCount).toBe(LIVE_EVENT_COUNT);
      expect(handlerCount()).toBe(0);
      expect(result.current.entries).toEqual([]);

      emitText("s1", "late delta");

      expect(result.current.entries).toEqual([]);
    });

    it("unsubscribes on unmount so a torn-down card cannot keep appending", async () => {
      mockGetSessionState.mockResolvedValue({ live_frames: [] });

      const { unmount } = renderHook(() => usePulseTail("s1", true, "running"));
      await act(async () => {});
      expect(handlerCount()).toBe(LIVE_EVENT_COUNT);

      unmount();

      expect(unsubscribeCount).toBe(LIVE_EVENT_COUNT);
      expect(handlerCount()).toBe(0);
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
