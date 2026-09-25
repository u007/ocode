import { describe, expect, it } from "vitest";
import { absoluteRestoreTarget } from "./inputRestore";

describe("absoluteRestoreTarget", () => {
  it("translates a loaded-window entry to its absolute server index", () => {
    expect(absoluteRestoreTarget(290, 7)).toBe(297);
  });

  it("fails closed when the loaded window has no server anchor", () => {
    expect(absoluteRestoreTarget(-1, 7)).toBeNull();
  });

  it("fails closed for invalid or overflowing indices", () => {
    expect(absoluteRestoreTarget(0, -1)).toBeNull();
    expect(absoluteRestoreTarget(Number.MAX_SAFE_INTEGER, 2)).toBeNull();
  });
});
