import { beforeEach, describe, expect, it } from "vitest";
import {
  previewViewKey,
  loadPreviewViewState,
  savePreviewViewState,
} from "./previewViewState";

describe("previewViewKey", () => {
  it("qualifies by host and root so same-path files in different projects differ", () => {
    expect(previewViewKey("a.pdf", "/p1")).not.toBe(previewViewKey("a.pdf", "/p2"));
    expect(previewViewKey("a.pdf", "/p1", "me@ssh")).not.toBe(previewViewKey("a.pdf", "/p1"));
    expect(previewViewKey("a.pdf", "/p1", "me@ssh")).toBe("me@ssh::/p1::a.pdf");
  });

  it("degrades to the bare path with no project identity", () => {
    expect(previewViewKey("a.pdf")).toBe("a.pdf");
  });
});

describe("previewViewState store", () => {
  beforeEach(() => localStorage.clear());

  it("round-trips page/zoom/scrollTop", () => {
    savePreviewViewState("k", { page: 3, zoom: 1.5, scrollTop: 900 });
    expect(loadPreviewViewState("k")).toMatchObject({ page: 3, zoom: 1.5, scrollTop: 900 });
  });

  it("merges partial patches without dropping other fields", () => {
    savePreviewViewState("k", { page: 3, zoom: 1.5 });
    savePreviewViewState("k", { scrollTop: 420 });
    expect(loadPreviewViewState("k")).toMatchObject({ page: 3, zoom: 1.5, scrollTop: 420 });
  });

  it("returns null for an unknown key and ignores invalid entries", () => {
    expect(loadPreviewViewState("missing")).toBeNull();
    localStorage.setItem("ocode.ui.previewViewState.v1", JSON.stringify({ bad: { page: -1, zoom: "x" } }));
    expect(loadPreviewViewState("bad")).toBeNull();
  });

  it("survives corrupt JSON", () => {
    localStorage.setItem("ocode.ui.previewViewState.v1", "{not json");
    expect(loadPreviewViewState("k")).toBeNull();
    savePreviewViewState("k", { page: 2 });
    expect(loadPreviewViewState("k")).toMatchObject({ page: 2 });
  });

  it("drops prototype-pollution keys", () => {
    localStorage.setItem(
      "ocode.ui.previewViewState.v1",
      JSON.stringify({ __proto__: { page: 9 }, constructor: { page: 9 }, safe: { page: 1 } }),
    );
    expect(loadPreviewViewState("safe")).toMatchObject({ page: 1 });
    expect(loadPreviewViewState("__proto__")).toBeNull();
  });

  it("evicts the oldest entry past the cap and refreshes MRU on rewrite", () => {
    // Cap is 200. Fill it, refresh the oldest entry, then add one more: the
    // refreshed key must survive and the now-oldest (k1) must be evicted.
    for (let i = 0; i < 200; i++) savePreviewViewState(`k${i}`, { page: 1 });
    expect(loadPreviewViewState("k0")).not.toBeNull();
    savePreviewViewState("k0", { page: 2 }); // moves k0 to newest
    savePreviewViewState("k200", { page: 1 }); // 201 entries → evict oldest
    expect(loadPreviewViewState("k1")).toBeNull();
    expect(loadPreviewViewState("k0")).toMatchObject({ page: 2 });
    expect(loadPreviewViewState("k200")).not.toBeNull();
  });

  it("rejects invalid values on write so read invariants cannot be violated", () => {
    savePreviewViewState("k", { page: 2, zoom: -1, scrollTop: -5 });
    expect(loadPreviewViewState("k")).toMatchObject({ page: 2 });
    expect(loadPreviewViewState("k")).not.toHaveProperty("zoom");
    expect(loadPreviewViewState("k")).not.toHaveProperty("scrollTop");
  });

  it("does not create an entry from only-invalid values", () => {
    savePreviewViewState("k", { scrollTop: -1 });
    expect(loadPreviewViewState("k")).toBeNull();
  });
});
