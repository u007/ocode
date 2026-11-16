import { describe, expect, it } from "vitest";
import { humanizeDuration } from "./humanize";

describe("humanizeDuration", () => {
  it("renders sub-second durations as milliseconds", () => {
    expect(humanizeDuration(0)).toBe("0ms");
    expect(humanizeDuration(850)).toBe("850ms");
  });

  it("renders under a minute with one decimal", () => {
    expect(humanizeDuration(1000)).toBe("1.0s");
    expect(humanizeDuration(4200)).toBe("4.2s");
    expect(humanizeDuration(59_000)).toBe("59.0s");
  });

  it("renders minutes with seconds when nonzero", () => {
    expect(humanizeDuration(60_000)).toBe("1m");
    expect(humanizeDuration(123_000)).toBe("2m 3s");
  });

  it("renders hours with minutes when nonzero", () => {
    expect(humanizeDuration(3_600_000)).toBe("1h");
    expect(humanizeDuration(3_840_000)).toBe("1h 4m");
  });

  it("clamps invalid input", () => {
    expect(humanizeDuration(-5)).toBe("0ms");
    expect(humanizeDuration(Number.NaN)).toBe("0ms");
  });
});
