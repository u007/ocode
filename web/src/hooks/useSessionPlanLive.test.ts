import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { TodoUpdatedEvent } from "../api/types";
import { setSessionPlan } from "../lib/sessionPlan";
import { useSessionPlanLive } from "./useSessionPlanLive";

const getSessionTodo = vi.fn();
vi.mock("../api/client", () => ({
  api: { getSessionTodo: (...args: unknown[]) => getSessionTodo(...args) },
}));
vi.mock("../lib/eventBus", () => ({
  eventBus: { onReconnect: () => () => {} },
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
});
