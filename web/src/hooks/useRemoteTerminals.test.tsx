import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRemoteTerminals } from "./useRemoteTerminals";

const mockList = vi.fn();
vi.mock("../api/client", () => ({
  api: { listRemoteTerminals: (...a: unknown[]) => mockList(...a) },
}));

const reconnectHandlers = new Set<() => void>();
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (h: () => void) => {
      reconnectHandlers.add(h);
      return () => reconnectHandlers.delete(h);
    },
  },
}));

const entries = [{ id: "t1", title: "shell", pid: 1, started_at: "2026-09-18T00:00:00Z", attached: false }];

describe("useRemoteTerminals", () => {
  beforeEach(() => {
    mockList.mockReset();
    reconnectHandlers.clear();
  });
  afterEach(() => vi.restoreAllMocks());

  it("does not fetch while disabled", () => {
    renderHook(() => useRemoteTerminals("h", "/p", false));
    expect(mockList).not.toHaveBeenCalled();
  });

  it("fetches once enabled and stores the terminals", async () => {
    mockList.mockResolvedValue(entries);
    const { result } = renderHook(() => useRemoteTerminals("h", "/p", true));
    await waitFor(() => expect(result.current.terminals).toEqual(entries));
    expect(mockList).toHaveBeenCalledWith("h", "/p");
  });

  it("fetches when expansion becomes enabled", async () => {
    mockList.mockResolvedValue(entries);
    const { rerender } = renderHook(({ enabled }: { enabled: boolean }) => useRemoteTerminals("h", "/p", enabled), {
      initialProps: { enabled: false },
    });
    expect(mockList).not.toHaveBeenCalled();
    rerender({ enabled: true });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
  });

  it("refetches on eventBus reconnect", async () => {
    mockList.mockResolvedValue(entries);
    renderHook(() => useRemoteTerminals("h", "/p", true));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
    act(() => reconnectHandlers.forEach((h) => h()));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });
});
