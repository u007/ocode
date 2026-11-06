import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api/client";
import { patchViewerState, resetViewerStates, setViewerDoc } from "../lib/viewerContext";
import { useViewerSync } from "./useViewerSync";

vi.mock("../api/client", () => ({
  api: {
    putSessionViewer: vi.fn(() => Promise.resolve()),
    deleteSessionViewer: vi.fn(() => Promise.resolve()),
  },
}));

const put = vi.mocked(api.putSessionViewer);
const del = vi.mocked(api.deleteSessionViewer);

async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

describe("useViewerSync", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    resetViewerStates();
    put.mockClear();
    del.mockClear();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("throttles PUTs to the latest record, 300 ms trailing", () => {
    const { unmount } = renderHook(() => useViewerSync("s1"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "a.md", selection: "first" });
    patchViewerState("s1", { path: "a.md", selection: "second" });
    patchViewerState("s1", { path: "a.md", selection: "third" });
    expect(put).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(put).toHaveBeenCalledTimes(1);
    expect(put.mock.calls[0][0]).toBe("s1");
    expect(put.mock.calls[0][1]).toMatchObject({ path: "a.md", kind: "markdown", selection: "third" });
    unmount();
  });

  it("passes the host through to the request", () => {
    const { unmount } = renderHook(() => useViewerSync("s1", "user@box"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(put).toHaveBeenCalledWith("s1", expect.objectContaining({ path: "a.md" }), "user@box");
    unmount();
  });

  it("DELETEs at once when the record is cleared, and cancels a pending PUT", () => {
    const { unmount } = renderHook(() => useViewerSync("s1"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "a.md", selection: "pending" });
    setViewerDoc("s1", null);
    expect(del).toHaveBeenCalledWith("s1", undefined);

    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(put).not.toHaveBeenCalled();
    unmount();
  });

  it("skips draft sessions with no server id", () => {
    const { unmount } = renderHook(() => useViewerSync("new-abc"));
    setViewerDoc("new-abc", { path: "a.md", kind: "markdown" });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    setViewerDoc("new-abc", null);
    expect(put).not.toHaveBeenCalled();
    expect(del).not.toHaveBeenCalled();
    unmount();
  });

  it("pushes an existing record once at mount", () => {
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    const { unmount } = renderHook(() => useViewerSync("s1"));
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(put).toHaveBeenCalledTimes(1);
    unmount();
  });

  it("cancels a pending PUT on unmount", () => {
    const { unmount } = renderHook(() => useViewerSync("s1"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    unmount();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(put).not.toHaveBeenCalled();
  });

  it("logs a failed PUT with the session id and does not throw", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    put.mockImplementationOnce(() => Promise.reject(new Error("boom")));
    const { unmount } = renderHook(() => useViewerSync("s1"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    await flush();
    expect(warn).toHaveBeenCalledWith("viewer sync: PUT failed", { sessionId: "s1", error: "boom" });
    unmount();
  });

  it("logs a failed DELETE with the session id and does not throw", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    del.mockImplementationOnce(() => Promise.reject(new Error("gone")));
    const { unmount } = renderHook(() => useViewerSync("s1"));
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    setViewerDoc("s1", null);
    await flush();
    expect(warn).toHaveBeenCalledWith("viewer sync: DELETE failed", { sessionId: "s1", error: "gone" });
    unmount();
  });
});
