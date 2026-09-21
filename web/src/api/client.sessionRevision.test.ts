import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";
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
