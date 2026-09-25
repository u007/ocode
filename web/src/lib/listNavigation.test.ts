import { describe, expect, it } from "vitest";
import { resolveListNavigation } from "./listNavigation";

describe("resolveListNavigation", () => {
  it("does nothing for an empty or disabled list", () => {
    expect(resolveListNavigation({ current: -1, count: 0, direction: "next" })).toEqual({ type: "none" });
    expect(resolveListNavigation({ current: 0, count: 3, direction: "next", disabled: true })).toEqual({ type: "none" });
  });

  it("jumps to the first and last rows", () => {
    expect(resolveListNavigation({ current: 2, count: 4, direction: "first" })).toEqual({ type: "focus", index: 0 });
    expect(resolveListNavigation({ current: 0, count: 4, direction: "last" })).toEqual({ type: "focus", index: 3 });
  });

  it("enters a list from either boundary when no row is active", () => {
    expect(resolveListNavigation({ current: -1, count: 4, direction: "next" })).toEqual({ type: "focus", index: 0 });
    expect(resolveListNavigation({ current: -1, count: 4, direction: "previous" })).toEqual({ type: "focus", index: 3 });
  });

  it("moves one row at a time through the middle of a list", () => {
    expect(resolveListNavigation({ current: 0, count: 4, direction: "next" })).toEqual({ type: "focus", index: 1 });
    expect(resolveListNavigation({ current: 3, count: 4, direction: "previous" })).toEqual({ type: "focus", index: 2 });
  });

  it("clamps at the boundaries instead of wrapping", () => {
    expect(resolveListNavigation({ current: 3, count: 4, direction: "next" })).toEqual({ type: "none" });
    expect(resolveListNavigation({ current: 0, count: 4, direction: "previous" })).toEqual({ type: "none" });
  });

  it("requests the next page only when the caller says more rows exist", () => {
    expect(resolveListNavigation({ current: 3, count: 4, direction: "next", hasMore: true })).toEqual({
      type: "load-more",
      nextIndex: 4,
    });
  });

  it("returns to the search input at the first-row boundary when available", () => {
    expect(resolveListNavigation({ current: 0, count: 4, direction: "previous", hasInput: true })).toEqual({
      type: "return-to-input",
    });
    expect(resolveListNavigation({ current: 0, count: 4, direction: "previous" })).toEqual({ type: "none" });
  });
});
