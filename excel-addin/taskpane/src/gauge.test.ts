import { describe, expect, it } from "vitest";
import { contextLevel } from "./gauge";

describe("context gauge level", () => {
  it("is ok below 70%, warn from 70%, bad from 90%", () => {
    expect(contextLevel(0, 1000)).toBe("ok");
    expect(contextLevel(699, 1000)).toBe("ok");
    expect(contextLevel(700, 1000)).toBe("warn");
    expect(contextLevel(899, 1000)).toBe("warn");
    expect(contextLevel(900, 1000)).toBe("bad");
    expect(contextLevel(1000, 1000)).toBe("bad");
  });
  it("refuses a window that is not positive", () => {
    expect(() => contextLevel(1, 0)).toThrow(RangeError);
    expect(() => contextLevel(1, Number.NaN)).toThrow(RangeError);
  });
});
