import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRemoteTerminals } from "./useRemoteTerminals";

const mockList = vi.fn();
vi.mock("../api/client", () => ({
  api: { listRemoteTerminals: (...a: unknown[]) => mockList(...a) },
}));

const reconnectHandlers = new Set<() => void>();
const eventHandlers = new Map<string, Set<(env: unknown) => void>>();
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (h: () => void) => {
      reconnectHandlers.add(h);
      return () => reconnectHandlers.delete(h);
    },
    on: (event: string, h: (env: unknown) => void) => {
      let set = eventHandlers.get(event);
      if (!set) {
        set = new Set();
        eventHandlers.set(event, set);
      }
      set.add(h);
      return () => set!.delete(h);
    },
  },
}));

function fire(event: string) {
  for (const h of eventHandlers.get(event) ?? []) h({ event, seq: 1, data: null });
}

const entries = [{ id: "t1", title: "shell", pid: 1, started_at: "2026-09-18T00:00:00Z", attached: false }];

describe("useRemoteTerminals", () => {
  beforeEach(() => {
    mockList.mockReset();
    reconnectHandlers.clear();
    eventHandlers.clear();
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

  // A terminal opened in ANOTHER client (the desktop app) is announced by the
  // server's terminal_tabs_changed. Without this subscription the list was
  // fetched once on mount, so a browser that was already open reported
  // "0 terminals" forever — the count could never catch up with the host.
  it("refetches when another client changes the terminal tabs", async () => {
    mockList.mockResolvedValue(entries);
    const { result } = renderHook(() => useRemoteTerminals("h", "/p", true));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));

    mockList.mockResolvedValue([
      ...entries,
      { id: "t2", title: "from-desktop", pid: 2, started_at: "2026-09-18T00:00:00Z", attached: false },
    ]);
    await act(async () => {
      fire("terminal_tabs_changed");
    });

    await waitFor(() => expect(result.current.terminals.map((t) => t.id)).toEqual(["t1", "t2"]));
  });
});
