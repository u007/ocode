import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HEARTBEAT_MS, InfoReporter, TICK_MS } from "./infoReporter";
import { logSnapshot } from "./log";
import type { WorkbookInfo } from "./port";

/** Warnings the reporter logged since the test started (the log buffer is shared, so the count is taken per test). */
const warnings = () => logSnapshot().filter((e) => e.level === "warn" && e.source === "info").length;

const INFO: WorkbookInfo = {
  fileName: "Budget.xlsx", path: "/Users/me/Budget.xlsx", host: "Excel", platform: "Mac",
  state: "saved", blank: false, readOnly: false, protectedView: false, sheets: 2,
};

interface Posted { url: string; key: string; body: WorkbookInfo }

/** A fetch that records each POST and answers with `statuses` in order (then 200). */
function fakeFetch(statuses: number[] = [], failWith?: Error) {
  const posts: Posted[] = [];
  const f = (async (url: string, init: RequestInit) => {
    if (failWith) throw failWith;
    const headers = init.headers as Record<string, string>;
    posts.push({ url, key: headers["X-Bridge-Key"], body: JSON.parse(String(init.body)) as WorkbookInfo });
    return new Response("", { status: statuses.shift() ?? 200 });
  }) as typeof fetch;
  return { f, posts };
}

/** A collector whose answer the test can change between ticks. */
function source(initial: WorkbookInfo) {
  let next: WorkbookInfo = initial;
  let failNext: Error | null = null;
  const collect = vi.fn(async () => {
    if (failNext) { const e = failNext; failNext = null; throw e; }
    return next;
  });
  return {
    collect,
    set: (i: WorkbookInfo) => { next = i; },
    failOnce: (e: Error) => { failNext = e; },
  };
}

const reporterWith = (collect: () => Promise<WorkbookInfo>, f: typeof fetch) =>
  new InfoReporter({ base: "https://bridge.test", key: "k-1", workbook: "wb 1", collect, fetchImpl: f });

describe("InfoReporter", () => {
  beforeEach(() => { vi.useFakeTimers(); });
  afterEach(() => { vi.useRealTimers(); });

  it("posts the collected state to pane-info with the key, and the workbook id in the query", async () => {
    const { f, posts } = fakeFetch();
    const src = source(INFO);
    await reporterWith(src.collect, f).tick();
    expect(posts).toEqual([{ url: "https://bridge.test/bridge/pane-info?workbook=wb%201", key: "k-1", body: INFO }]);
  });

  it("stays quiet while nothing changed, and posts a heartbeat once HEARTBEAT_MS has passed", async () => {
    const { f, posts } = fakeFetch();
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    await r.tick();
    await vi.advanceTimersByTimeAsync(HEARTBEAT_MS - 1);
    await r.tick();
    expect(posts).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    await r.tick();
    expect(posts).toHaveLength(2);
  });

  it("posts straight away when the state changed, without waiting for the heartbeat", async () => {
    const { f, posts } = fakeFetch();
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    await r.tick();
    src.set({ ...INFO, state: "modified" });
    await r.tick();
    expect(posts.map((p) => p.body.state)).toEqual(["saved", "modified"]);
  });

  it("a failed post is not counted as sent: the next tick posts the same state again", async () => {
    const { f, posts } = fakeFetch([500]);
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    await expect(r.tick()).resolves.toBeUndefined();
    expect(posts).toHaveLength(1);
    await r.tick();
    expect(posts).toHaveLength(2);
  });

  it("a network error on the post is logged and does not throw", async () => {
    const src = source(INFO);
    const before = warnings();
    const r = reporterWith(src.collect, fakeFetch([], new TypeError("fetch failed")).f);
    await expect(r.tick()).resolves.toBeUndefined();
    expect(warnings()).toBe(before + 1);
  });

  it("a failed collection is logged, nothing is posted, and the next tick collects again", async () => {
    const { f, posts } = fakeFetch();
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    src.failOnce(new Error("Excel error"));
    const before = warnings();
    await expect(r.tick()).resolves.toBeUndefined();
    expect(warnings()).toBe(before + 1);
    expect(posts).toHaveLength(0);
    expect(r.current()).toBeNull();
    await r.tick();
    expect(posts).toHaveLength(1);
    expect(r.current()).toEqual(INFO);
  });

  it("start() collects on every TICK_MS after an immediate first collection", async () => {
    const { f, posts } = fakeFetch();
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    r.start();
    await vi.advanceTimersByTimeAsync(0);
    expect(src.collect).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(TICK_MS * 2);
    expect(src.collect).toHaveBeenCalledTimes(3);
    expect(posts).toHaveLength(1);
  });

  it("current() is the latest collected state, even when posting it failed", async () => {
    const { f, posts } = fakeFetch([200, 500]);
    const src = source(INFO);
    const r = reporterWith(src.collect, f);
    await r.tick();
    src.set({ ...INFO, readOnly: true });
    await r.tick();
    expect(posts).toHaveLength(2);
    expect(r.current()?.readOnly).toBe(true);
  });
});
