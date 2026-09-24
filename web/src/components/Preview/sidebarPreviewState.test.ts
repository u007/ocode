import { describe, it, expect, beforeEach } from "vitest";
import {
  loadSidebarPreviewState,
  saveSidebarPreviewState,
  rekeySidebarPreviewState,
} from "./sidebarPreviewState";

beforeEach(() => {
  window.localStorage.clear();
});

describe("sidebarPreviewState", () => {
  it("saves and loads per side stateKey (per session surface)", () => {
    saveSidebarPreviewState("side:chat:s1", { surface: "preview", path: "a.md", kind: "markdown", page: 2 });
    expect(loadSidebarPreviewState("side:chat:s1")).toMatchObject({ surface: "preview", path: "a.md", page: 2 });
    // A different session has no slot.
    expect(loadSidebarPreviewState("side:chat:s2")).toBeNull();
    // No key at all → nothing.
    expect(loadSidebarPreviewState(null)).toBeNull();
    expect(loadSidebarPreviewState(undefined)).toBeNull();
  });

  it("keeps two sessions in the same project isolated", () => {
    saveSidebarPreviewState("side:chat:s1", { surface: "preview", path: "a.md", kind: "markdown" });
    saveSidebarPreviewState("side:chat:s2", { surface: "browser" });
    expect(loadSidebarPreviewState("side:chat:s1")?.path).toBe("a.md");
    expect(loadSidebarPreviewState("side:chat:s2")?.path).toBeUndefined();
    expect(loadSidebarPreviewState("side:chat:s2")?.surface).toBe("browser");
  });

  it("rekey moves a session's slot to the new key", () => {
    saveSidebarPreviewState("side:chat:new-1", { surface: "preview", path: "a.md", kind: "markdown", page: 4 });
    rekeySidebarPreviewState("side:chat:new-1", "side:chat:ses_real");
    expect(loadSidebarPreviewState("side:chat:new-1")).toBeNull();
    expect(loadSidebarPreviewState("side:chat:ses_real")).toMatchObject({ path: "a.md", page: 4 });
  });

  it("rekey is a no-op for absent sources, existing targets, and identical/empty ids", () => {
    // absent source
    rekeySidebarPreviewState("side:chat:none", "side:chat:x");
    expect(loadSidebarPreviewState("side:chat:x")).toBeNull();

    // existing target wins
    saveSidebarPreviewState("side:chat:old", { surface: "preview", path: "old.md", kind: "markdown" });
    saveSidebarPreviewState("side:chat:new", { surface: "preview", path: "new.md", kind: "markdown" });
    rekeySidebarPreviewState("side:chat:old", "side:chat:new");
    expect(loadSidebarPreviewState("side:chat:new")?.path).toBe("new.md");
    expect(loadSidebarPreviewState("side:chat:old")?.path).toBe("old.md");

    // identical / empty
    rekeySidebarPreviewState("side:chat:new", "side:chat:new");
    rekeySidebarPreviewState("", "side:chat:new");
    expect(loadSidebarPreviewState("side:chat:new")?.path).toBe("new.md");
  });
});
