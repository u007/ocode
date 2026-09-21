import { afterEach, describe, expect, it } from "vitest";
import {
  noteSessionRevision,
  sessionRevisionMoved,
  clearSessionRevision,
  resetSessionRevisions,
} from "./sessionRevision";

afterEach(() => resetSessionRevisions());

describe("sessionRevision", () => {
  it("treats an unknown baseline as nothing to do (no fetch recorded yet)", () => {
    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);
  });

  it("reports no movement when the revision matches the fetched baseline", () => {
    noteSessionRevision("s1", undefined, "rev-1");
    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);
  });

  it("reports movement when the stored revision changed", () => {
    noteSessionRevision("s1", undefined, "rev-1");
    expect(sessionRevisionMoved("s1", undefined, "rev-2")).toBe(true);
  });

  it("ignores an absent revision (bridged/in-memory session)", () => {
    noteSessionRevision("s1", undefined, "rev-1");
    expect(sessionRevisionMoved("s1", undefined, undefined)).toBe(false);
  });

  it("keeps local and remote-host sessions independent", () => {
    noteSessionRevision("s1", undefined, "local-1");
    noteSessionRevision("s1", "devbox", "remote-1");
    expect(sessionRevisionMoved("s1", undefined, "local-2")).toBe(true);
    expect(sessionRevisionMoved("s1", "devbox", "remote-1")).toBe(false);
    expect(sessionRevisionMoved("s1", "devbox", "remote-2")).toBe(true);
  });

  it("clears the baseline when a fetch reports no revision", () => {
    noteSessionRevision("s1", undefined, "rev-1");
    noteSessionRevision("s1", undefined, undefined);
    expect(sessionRevisionMoved("s1", undefined, "rev-1")).toBe(false);
  });

  it("drops a session's baseline on clear (tab closed)", () => {
    noteSessionRevision("s1", undefined, "rev-1");
    clearSessionRevision("s1");
    expect(sessionRevisionMoved("s1", undefined, "rev-2")).toBe(false);
  });
});
