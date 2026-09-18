import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRemoteHostStatus } from "./useRemoteHostStatus";

const mockStatus = vi.fn();
const mockConnect = vi.fn();
const mockRestart = vi.fn();
vi.mock("../api/client", () => ({
  api: {
    getRemoteHostStatus: (...a: unknown[]) => mockStatus(...a),
    connectRemoteHost: (...a: unknown[]) => mockConnect(...a),
    restartRemoteHost: (...a: unknown[]) => mockRestart(...a),
  },
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

const base = { host: "h", connected: true, version: "1.0.0", local_version: "2.0.0", outdated: true, pid: 42 };

describe("useRemoteHostStatus", () => {
  beforeEach(() => {
    mockStatus.mockReset();
    mockConnect.mockReset();
    mockRestart.mockReset();
    reconnectHandlers.clear();
  });
  afterEach(() => vi.restoreAllMocks());

  it("does not fetch while disabled", () => {
    renderHook(() => useRemoteHostStatus("h", false));
    expect(mockStatus).not.toHaveBeenCalled();
  });

  it("fetches on mount when enabled and stores the status", async () => {
    mockStatus.mockResolvedValue(base);
    const { result } = renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(result.current.status).toEqual(base));
    expect(mockStatus).toHaveBeenCalledWith("h");
  });

  it("refetches on eventBus reconnect", async () => {
    mockStatus.mockResolvedValue(base);
    renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(mockStatus).toHaveBeenCalledTimes(1));
    act(() => reconnectHandlers.forEach((h) => h()));
    await waitFor(() => expect(mockStatus).toHaveBeenCalledTimes(2));
  });

  it("restart transitions busy through restarting back to idle with the new status", async () => {
    const restarted = { ...base, version: "2.0.0", outdated: false, pid: 99 };
    mockStatus.mockResolvedValue(base);
    let resolveRestart!: (v: typeof restarted) => void;
    mockRestart.mockReturnValue(new Promise((resolve) => { resolveRestart = resolve; }));
    const { result } = renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(result.current.status).toEqual(base));

    act(() => result.current.restart());
    expect(result.current.busy).toBe("restarting");

    await act(async () => {
      resolveRestart(restarted);
      await Promise.resolve();
    });
    await waitFor(() => expect(result.current.busy).toBe("idle"));
    expect(result.current.status).toEqual(restarted);
  });

  it("stores the error and rescans status when restart fails", async () => {
    mockStatus.mockResolvedValue(base);
    mockRestart.mockRejectedValue(new Error("kill failed (stage: remote-kill)"));
    const { result } = renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(result.current.status).toEqual(base));

    await act(async () => {
      result.current.restart();
      await Promise.resolve();
    });
    await waitFor(() => expect(result.current.error).toContain("remote-kill"));
    expect(result.current.busy).toBe("idle");
    // Initial fetch + the post-failure refresh.
    expect(mockStatus.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});
