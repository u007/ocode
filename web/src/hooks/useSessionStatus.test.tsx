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

  // ── Context-field contract ────────────────────────────────────────────
  // TUIStatus uses `json:"context_current_tokens,omitempty"` etc., so 0 is
  // *omitted* (absent) on the wire, never `null` and never `0` (omitempty
  // drops 0). `/api/sessions/{id}/context` instead always writes numeric
  // zeros: `estimated_tokens: totalChars/4` (0 for empty transcript).
  it("propagates context_current_tokens / context_max_tokens when present", async () => {
    mockGetSessionStatus.mockResolvedValue({
      session_id: "s1",
      context_current_tokens: 96352,
      context_max_tokens: 1048576,
      context_model: "opencode-go/muse-spark-1.2-contributor",
    });
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    await waitFor(() => expect(mockGetSessionStatus).toHaveBeenCalledTimes(1));
    expect(mockGetSessionStatus).toHaveBeenCalledWith("s1");
  });

  it("handles empty-message transcript (0 tokens omitted, not null)", async () => {
    // Empty session: HandleSessionContext returns estimated_tokens: 0, max_tokens: window;
    // HandleSessionStatus omits context_current_tokens/context_max_tokens when 0
    // (omitempty) — never `null`.
    mockGetSessionStatus.mockResolvedValue({
      session_id: "empty",
      context_max_tokens: 1048576,
      context_model: "opencode-go/muse-spark-1.2-contributor",
      // context_current_tokens intentionally absent — not `null`, not `0`
    });
    renderHook(() => useSessionStatus("empty"), { wrapper: Wrapper });
    await waitFor(() => expect(mockGetSessionStatus).toHaveBeenCalledTimes(1));
    expect(mockGetSessionStatus).toHaveBeenCalledWith("empty");
  });

  // ── Cross-process staleness: 15s poll + visibility ─────────────────────
  it("polls every 15s to recover from cross-process staleness (TUI vs desktop headless)", async () => {
    vi.useFakeTimers();
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1" });
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(15000);
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(15000);
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(3);
    vi.useRealTimers();
  });

  it("refreshes on visibilitychange when document becomes visible", async () => {
    // visibility listener is added in the effect; dispatching the event
    // should trigger a background refetch (fetchStatus(false)).
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1" });
    renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    Object.defineProperty(document, "visibilityState", { value: "visible", writable: true });
    document.dispatchEvent(new Event("visibilitychange"));
    await waitFor(() => expect(mockGetSessionStatus).toHaveBeenCalledTimes(2));
  });

  it("cleans up interval and visibility listener on unmount", async () => {
    vi.useFakeTimers();
    mockGetSessionStatus.mockResolvedValue({ session_id: "s1" });
    const { unmount } = renderHook(() => useSessionStatus("s1"), { wrapper: Wrapper });
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    unmount();
    await vi.advanceTimersByTimeAsync(15000);
    // No extra fetch after unmount — interval was cleared.
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    Object.defineProperty(document, "visibilityState", { value: "visible", writable: true });
    document.dispatchEvent(new Event("visibilitychange"));
    await vi.advanceTimersByTimeAsync(100);
    expect(mockGetSessionStatus).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});
