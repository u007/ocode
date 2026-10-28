import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRemoteHostStatus, __resetRemoteHostStatusForTests } from "./useRemoteHostStatus";
import {
  noteRemoteHostUnreachable,
  REMOTE_HOST_RETRY_MS,
  remoteHostRetryInMs,
} from "../lib/remoteHostGate";

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
    __resetRemoteHostStatusForTests();
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

  it("clears a retry window as soon as a status poll reports the host connected", async () => {
    mockStatus.mockResolvedValue(base);
    renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(mockStatus).toHaveBeenCalledTimes(1));

    // A transient failure armed the host-wide window. Every ordinary read is
    // skipped while it is armed, so the status probe is the only thing that can
    // notice the host recovered — and it must do so without waiting out the
    // whole window.
    noteRemoteHostUnreachable("h");
    expect(remoteHostRetryInMs("h")).toBeGreaterThan(0);

    await act(async () => {
      for (const handler of reconnectHandlers) handler();
    });
    await waitFor(() => expect(remoteHostRetryInMs("h")).toBe(0));
    expect(mockStatus.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("refetches on eventBus reconnect", async () => {
    mockStatus.mockResolvedValue(base);
    renderHook(() => useRemoteHostStatus("h", true));
    await waitFor(() => expect(mockStatus).toHaveBeenCalledTimes(1));
    act(() => reconnectHandlers.forEach((h) => h()));
    await waitFor(() => expect(mockStatus).toHaveBeenCalledTimes(2));
  });

  // The periodic poll goes through the shared per-host queue. After a failed
  // poll the host is left alone for the retry window. The status endpoint is
  // still probed (it is an in-memory registry read, and it is the only way the
  // window can close early), but a probe that reports DISCONNECTED must leave
  // the window armed, and ordinary reads stay skipped until it expires.
  it("keeps a failed host's window armed while the status probe reports disconnected", async () => {
    vi.useFakeTimers();
    try {
      // A 502 is how the proxy reports an unreachable host; only that opens the window.
      mockStatus.mockRejectedValueOnce(Object.assign(new Error("remote connect failed"), { status: 502 }));
      mockStatus.mockResolvedValue({ ...base, connected: false });
      renderHook(() => useRemoteHostStatus("h", true));
      await act(async () => {
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(mockStatus).toHaveBeenCalledTimes(1);
      expect(remoteHostRetryInMs("h")).toBeGreaterThan(0);

      // A poll inside the retry window probes status (cheap), sees it is still
      // disconnected, and leaves the window armed.
      await act(async () => {
        vi.advanceTimersByTime(30_000);
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(remoteHostRetryInMs("h")).toBeGreaterThan(0);

      // Once the window expires, the poll asks again. Advancing by the whole
      // window crosses two 30s poll ticks, so the host is asked again.
      const callsDuringWindow = mockStatus.mock.calls.length;
      await act(async () => {
        vi.advanceTimersByTime(REMOTE_HOST_RETRY_MS);
        await Promise.resolve();
        await Promise.resolve();
      });
      expect(mockStatus.mock.calls.length).toBeGreaterThan(callsDuringWindow);
    } finally {
      vi.useRealTimers();
    }
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
