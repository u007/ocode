import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { ChatProvider } from "../stores/chatStore";
import { useSessionStatus } from "./useSessionStatus";

const mockGetSessionStatus = vi.fn();
vi.mock("../api/client", () => ({
  api: { getSessionStatus: (...a: unknown[]) => mockGetSessionStatus(...a) },
}));

const reconnectHandlers = new Set<() => void>();
vi.mock("../lib/eventBus", () => ({
  eventBus: {
    onReconnect: (h: () => void) => {
      reconnectHandlers.add(h);
      return () => reconnectHandlers.delete(h);
    },
    on: () => () => {},
  },
}));

function Wrapper({ children }: { children: ReactNode }) {
  return <ChatProvider>{children}</ChatProvider>;
}

describe("useSessionStatus", () => {
  beforeEach(() => {
    mockGetSessionStatus.mockReset();
    reconnectHandlers.clear();
  });
  afterEach(() => vi.restoreAllMocks());

  it("fetches status for a real session id and populates that session's slice", async () => {
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1", main_model: "m1" });
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    expect(mockGetSessionStatus).toHaveBeenCalledWith("s1");
    await waitFor(() => expect(mockGetSessionStatus).toHaveBeenCalled());
  });

  it("switching the session id triggers exactly one new fetch", async () => {
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1" });
    const { rerender } = renderHook(({ id }: { id: string | null }) => useSessionStatus(id), {
      initialProps: { id: "s1" },
      wrapper: Wrapper,
    });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    await Promise.resolve();
    rerender({ id: "s2" });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(2);
    expect(mockGetSessionStatus).toHaveBeenLastCalledWith("s2");
  });

  it("placeholder new-* tabs skip the fetch entirely", () => {
    renderHook(() => useSessionStatus("new-123"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).not.toHaveBeenCalled();
  });

  it("null session id skips the fetch", () => {
    renderHook(() => useSessionStatus(null), { wrapper: Wrapper });
    expect(mockGetSessionStatus).not.toHaveBeenCalled();
  });

  it("refreshes status on eventBus reconnect", async () => {
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1" });
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    reconnectHandlers.forEach((h) => h());
    await Promise.resolve();
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(2);
  });

  it("clears the loading flag even when the fetch fails", async () => {
    mockGetSessionStatus.mockRejectedValue(new Error("500"));
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    await waitFor(() => expect(warn).toHaveBeenCalled());
    expect(warn.mock.calls[0][0]).toContain("s1");
    warn.mockRestore();
  });
});
