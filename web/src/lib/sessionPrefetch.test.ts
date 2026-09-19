import { afterEach, describe, expect, it, vi } from "vitest";
import type { SessionDetail } from "../api/types";

const getSession = vi.fn();
vi.mock("../api/client", () => ({
  api: { getSession: (...a: unknown[]) => getSession(...a) },
}));

import {
  SESSION_PREFETCH_LIMIT,
  SESSION_PREFETCH_TTL_MS,
  dropPrefetchedSession,
  prefetchSession,
  takePrefetchedSession,
} from "./sessionPrefetch";

const detail = (id: string): SessionDetail =>
  ({ id, title: id, created_at: "", updated_at: "", messages: [], total: 0 }) as SessionDetail;

afterEach(() => {
  getSession.mockReset();
  vi.useRealTimers();
  dropPrefetchedSession("s1");
  dropPrefetchedSession("s2");
});

describe("sessionPrefetch", () => {
  it("starts one request per session and joins repeated hovers", () => {
    getSession.mockReturnValue(new Promise(() => {}));
    prefetchSession("s1");
    prefetchSession("s1");
    prefetchSession("s1");
    expect(getSession).toHaveBeenCalledTimes(1);
    expect(getSession).toHaveBeenCalledWith("s1", { limit: SESSION_PREFETCH_LIMIT }, undefined);
  });

  it("hands the warm promise to the consumer exactly once", async () => {
    getSession.mockResolvedValue(detail("s1"));
    prefetchSession("s1");

    const first = takePrefetchedSession("s1");
    expect(first).toBeDefined();
    await expect(first).resolves.toMatchObject({ id: "s1" });

    // Single-use: a second mount must not silently reuse a consumed preview.
    expect(takePrefetchedSession("s1")).toBeUndefined();
  });

  it("ignores entries older than the TTL so the caller refetches", () => {
    vi.useFakeTimers();
    getSession.mockReturnValue(new Promise(() => {}));
    prefetchSession("s1");

    vi.advanceTimersByTime(SESSION_PREFETCH_TTL_MS + 1);
    expect(takePrefetchedSession("s1")).toBeUndefined();
  });

  it("never prefetches draft tabs, empty ids, or null", () => {
    prefetchSession("new-123");
    prefetchSession("");
    prefetchSession(null);
    prefetchSession(undefined);
    expect(getSession).not.toHaveBeenCalled();
    expect(takePrefetchedSession("new-123")).toBeUndefined();
  });

  it("does not surface an unhandled rejection when nobody consumes the prefetch", async () => {
    const failure = new Error("offline");
    getSession.mockRejectedValue(failure);
    prefetchSession("s2");
    // The swallowed rejection must not fail the test run (vitest turns
    // unhandled rejections into failures).
    await Promise.resolve();
    expect(getSession).toHaveBeenCalledTimes(1);
  });

  it("still lets a consumer observe the prefetch failure", async () => {
    const failure = new Error("offline");
    getSession.mockRejectedValue(failure);
    prefetchSession("s2");
    await expect(takePrefetchedSession("s2")).rejects.toThrow("offline");
  });

  it("routes a remote prefetch through the project host and never serves it to local", async () => {
    getSession.mockResolvedValue(detail("s1"));
    prefetchSession("s1", "james@217.216.72.49");

    // Same id, different host: the warm entry must not be served.
    expect(takePrefetchedSession("s1")).toBeUndefined();
    // Matching host consumes it.
    const warm = takePrefetchedSession("s1", "james@217.216.72.49");
    expect(warm).toBeDefined();
    await expect(warm).resolves.toMatchObject({ id: "s1" });
    expect(getSession).toHaveBeenCalledWith("s1", { limit: SESSION_PREFETCH_LIMIT }, "james@217.216.72.49");
  });
});
