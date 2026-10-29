import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { TodoUpdatedEvent } from "../api/types";
import { setSessionPlan } from "../lib/sessionPlan";
import { useSessionPlanLive } from "./useSessionPlanLive";

const getSessionTodo = vi.fn();
const reconnect = vi.hoisted(() => ({ handlers: [] as Array<() => void> }));
vi.mock("../api/client", () => ({
  api: { getSessionTodo: (...args: unknown[]) => getSessionTodo(...args) },
}));
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (handler: () => void) => {
      reconnect.handlers.push(handler);
      return () => {};
    },
  },
}));

function plan(id: string, revision: number, done = 0): TodoUpdatedEvent {
  return {
    session_id: id,
    revision,
    done,
    total: 2,
    current: "",
    items: [
      { id: "t1", text: "first", state: done > 0 ? "done" : "in_progress" },
      { id: "t2", text: "second", state: "pending" },
    ],
  };
}

describe("useSessionPlanLive", () => {
  beforeEach(() => {
    getSessionTodo.mockReset();
    reconnect.handlers.length = 0;
  });

  it("seeds the session's plan from the server", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-hook-seed", 1));
    const { result } = renderHook(() => useSessionPlanLive("ses-hook-seed"));

    await waitFor(() => expect(result.current?.revision).toBe(1));
    expect(getSessionTodo).toHaveBeenCalledWith("ses-hook-seed", undefined);
  });

  it("follows a newer frame and ignores a stale revision", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-hook-rev", 1));
    const { result } = renderHook(() => useSessionPlanLive("ses-hook-rev"));
    await waitFor(() => expect(result.current?.revision).toBe(1));

    act(() => setSessionPlan("ses-hook-rev", plan("ses-hook-rev", 3, 1)));
    expect(result.current?.revision).toBe(3);
    expect(result.current?.done).toBe(1);

    // A frame published before the one already held must not move it back.
    act(() => setSessionPlan("ses-hook-rev", plan("ses-hook-rev", 2)));
    expect(result.current?.revision).toBe(3);
    expect(result.current?.done).toBe(1);
  });

  it("keeps sessions isolated", async () => {
    getSessionTodo.mockImplementation(async (id: string) => plan(id, 1));
    const a = renderHook(() => useSessionPlanLive("ses-hook-a"));
    const b = renderHook(() => useSessionPlanLive("ses-hook-b"));
    await waitFor(() => expect(a.result.current?.session_id).toBe("ses-hook-a"));
    await waitFor(() => expect(b.result.current?.session_id).toBe("ses-hook-b"));

    act(() => setSessionPlan("ses-hook-b", plan("ses-hook-b", 2, 1)));

    expect(a.result.current?.revision).toBe(1);
    expect(b.result.current?.revision).toBe(2);
  });

  it("passes the remote host to the seed request", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-hook-remote", 1));
    renderHook(() => useSessionPlanLive("ses-hook-remote", "host-1"));

    await waitFor(() => expect(getSessionTodo).toHaveBeenCalledWith("ses-hook-remote", "host-1"));
  });

  it("reseeds on reconnect even when the store already holds a plan", async () => {
    getSessionTodo.mockResolvedValueOnce(plan("ses-hook-reconnect", 1));
    const { result } = renderHook(() => useSessionPlanLive("ses-hook-reconnect"));
    await waitFor(() => expect(result.current?.revision).toBe(1));

    act(() => setSessionPlan("ses-hook-reconnect", plan("ses-hook-reconnect", 2, 1)));
    expect(result.current?.revision).toBe(2);

    // A missed `todo_updated` is not replayed, so the reconnect must fetch.
    getSessionTodo.mockResolvedValueOnce(plan("ses-hook-reconnect", 3, 1));
    act(() => reconnect.handlers.forEach((handler) => handler()));
    await waitFor(() => expect(result.current?.revision).toBe(3));
  });

  it("issues one seed request for two mounted consumers", async () => {
    getSessionTodo.mockResolvedValue(plan("ses-hook-dedupe", 1));
    const a = renderHook(() => useSessionPlanLive("ses-hook-dedupe"));
    const b = renderHook(() => useSessionPlanLive("ses-hook-dedupe"));

    await waitFor(() => expect(a.result.current?.revision).toBe(1));
    await waitFor(() => expect(b.result.current?.revision).toBe(1));
    expect(getSessionTodo).toHaveBeenCalledTimes(1);
  });

  it("does not let a slower seed overwrite a frame that arrived first", async () => {
    let resolveSeed!: (value: TodoUpdatedEvent) => void;
    getSessionTodo.mockReturnValueOnce(
      new Promise<TodoUpdatedEvent>((resolve) => {
        resolveSeed = resolve;
      }),
    );
    const { result } = renderHook(() => useSessionPlanLive("ses-hook-race"));

    act(() => setSessionPlan("ses-hook-race", plan("ses-hook-race", 2, 1)));
    expect(result.current?.revision).toBe(2);

    await act(async () => {
      resolveSeed(plan("ses-hook-race", 1));
    });
    expect(result.current?.revision).toBe(2);
  });
});
