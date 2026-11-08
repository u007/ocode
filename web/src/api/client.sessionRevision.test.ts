import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

// getSession writes the transcript through to the offline cache. The write is
// a dynamic import that is not awaited, so the tests flush it before asserting.
const offline = vi.hoisted(() => ({
  syncSessionToCache: vi.fn(async (_detail: unknown) => {}),
}));
vi.mock("../lib/offlineSync", () => ({
  syncSessionToCache: offline.syncSessionToCache,
}));
import {
  noteSessionRevision,
  sessionRevisionMoved,
  resetSessionRevisions,
} from "../lib/sessionRevision";

// The cross-process revalidation poll depends on ONE thing the api layer
// guarantees: every transcript fetch records the stored revision it fetched
// at. If that wiring is dropped, `revalidateSession` silently stops detecting
// out-of-process writes, so it is pinned here rather than only indirectly.
function jsonResponse(body: string): Response {
  return new Response(body, { status: 200, headers: { "Content-Type": "application/json" } });
}

function detailBody(revision: string): string {
  return JSON.stringify({
    id: "s1",
    title: "",
    created_at: "",
    updated_at: "",
    messages: [],
    total: 0,
    revision,
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  resetSessionRevisions();
});

describe("api.getSession records the stored revision", () => {
  it("notes the detail revision so a later poll can detect a change", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-1"))));
    await api.getSession("s1");

    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);
    expect(sessionRevisionMoved("s1", undefined, "rev-2")).toBe(true);
  });

  it("keys the baseline by host and leaves other hosts untouched", async () => {
    noteSessionRevision("s1", undefined, "rev-local");
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-h"))));
    await api.getSession("s1", { limit: 10 }, "devbox");

    expect(sessionRevisionMoved("s1", "devbox", "rev-h")).toBe(false);
    expect(sessionRevisionMoved("s1", "devbox", "rev-other")).toBe(true);
    // The local baseline is independent of the remote fetch.
    expect(sessionRevisionMoved("s1", undefined, "rev-local")).toBe(false);
  });
});

describe("api.getSession opt-out from the revision baseline", () => {
  it("leaves an existing baseline alone when noteRevision is false", async () => {
    // A speculative read (the Pulse dashboard hovering a card) must not stamp a
    // fresh revision over an OPEN TAB that is still showing older content:
    // the poll would then see "no change" and never repair the stale tab.
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-1"))));
    await api.getSession("s1");
    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);

    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-2"))));
    await api.getSession("s1", { noteRevision: false });

    // The baseline is still rev-1, so a rev-2 poll is still seen as a move.
    expect(sessionRevisionMoved("s1", undefined, "rev-2")).toBe(true);
  });

  it("a normal fetch still records, so the opt-out cannot disable the feature", async () => {
    // No baseline yet, so nothing to detect. One normal fetch must establish it.
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-1"))));
    await api.getSession("s1", { noteRevision: false });
    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);

    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(detailBody("rev-1"))));
    await api.getSession("s1");
    expect(sessionRevisionMoved("s1", undefined, "rev-2")).toBe(true);
  });
});

describe("api.getSession offline write-through only caches a whole transcript", () => {
  const flush = () => new Promise((resolve) => setTimeout(resolve, 10));

  function transcriptBody(messageCount: number, total: number): string {
    return JSON.stringify({
      id: "s1",
      title: "",
      created_at: "",
      updated_at: "",
      messages: Array.from({ length: messageCount }, (_, i) => ({ role: "user", content: `m${i}` })),
      total,
      revision: "rev",
    });
  }

  function serve(body: string) {
    vi.stubGlobal("fetch", vi.fn(async () => jsonResponse(body)));
  }

  beforeEach(() => {
    offline.syncSessionToCache.mockClear();
  });

  it("caches a full read with no window options", async () => {
    serve(transcriptBody(3, 3));
    await api.getSession("s1");
    await flush();
    expect(offline.syncSessionToCache).toHaveBeenCalledTimes(1);
    expect(offline.syncSessionToCache.mock.calls[0][0]).toMatchObject({ id: "s1", total: 3 });
  });

  it("caches a head window that already holds the whole transcript", async () => {
    // The normal open path passes a limit. When the limit reaches the end,
    // the read is still the whole transcript and must stay cached.
    serve(transcriptBody(3, 3));
    await api.getSession("s1", { limit: 100 });
    await flush();
    expect(offline.syncSessionToCache).toHaveBeenCalledTimes(1);
  });

  it("skips a window that starts past the head (offset > 0)", async () => {
    serve(transcriptBody(2, 5));
    await api.getSession("s1", { limit: 2, offset: 2 });
    await flush();
    expect(offline.syncSessionToCache).not.toHaveBeenCalled();
  });

  it("skips a head window that stops short of the total", async () => {
    serve(transcriptBody(20, 200));
    await api.getSession("s1", { limit: 20 });
    await flush();
    expect(offline.syncSessionToCache).not.toHaveBeenCalled();
  });
});
