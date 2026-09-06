import { describe, it, expect } from "vitest";
import { computeRowBuckets, partitionVisible, type PartitionInput } from "./multirowLayout";

const CHIP = 40;
const GAP = 2;
const EPS = 4;
const MAX_ROWS = 2;

function p(input: Partial<PartitionInput> & Pick<PartitionInput, "keys" | "buckets" | "widths" | "containerWidth">): PartitionInput {
  return {
    chipReservedPx: CHIP,
    gapPx: GAP,
    epsilonPx: EPS,
    maxRows: MAX_ROWS,
    activeKey: null,
    ...input,
  };
}

describe("computeRowBuckets", () => {
  it("groups offsetTops within tolerance into monotonic row indices", () => {
    // row0: top 0 (4 pills), row1: top 24 (2 pills), row2: top 48 (1 pill)
    expect(computeRowBuckets([0, 0, 1, 0, 24, 24, 48])).toEqual([0, 0, 0, 0, 1, 1, 2]);
  });

  it("uses a single cluster for tiny jitter (tolerance)", () => {
    expect(computeRowBuckets([10, 10.5, 11, 30], 2)).toEqual([0, 0, 0, 1]);
  });
});

describe("partitionVisible baseline", () => {
  it("returns everything visible when nothing overflows", () => {
    const r = partitionVisible(p({ keys: ["a", "b"], buckets: [0, 0], widths: [100, 100], containerWidth: 500 }));
    expect(r.visibleKeys).toEqual(["a", "b"]);
    expect(r.hiddenKeys).toEqual([]);
    expect(r.uncapped).toBe(false);
  });

  it("hides pills on rows >= maxRows", () => {
    // maxRows=2 → rows 0,1 visible; rows >=2 hidden (e,f).
    const r = partitionVisible(p({ keys: ["a", "b", "c", "d", "e", "f"], buckets: [0, 0, 1, 1, 2, 2], widths: [110, 110, 110, 110, 110, 110], containerWidth: 300 }));
    expect(r.visibleKeys).toEqual(["a", "b", "c", "d"]);
    expect(r.hiddenKeys).toEqual(["e", "f"]);
  });

  it("handles empty and single-tab inputs", () => {
    expect(partitionVisible(p({ keys: [], buckets: [], widths: [], containerWidth: 500 })).visibleKeys).toEqual([]);
    const single = partitionVisible(p({ keys: ["a"], buckets: [0], widths: [100], containerWidth: 500 }));
    expect(single.visibleKeys).toEqual(["a"]);
    expect(single.hiddenKeys).toEqual([]);
  });

  it("respects probe buckets regardless of ragged widths", () => {
    // c is on row 2 (bucket >= maxRows) → hidden by bucket, even though it is
    // narrower than visible b. widths only matter for chip-fit/promotion.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 2], widths: [30, 200, 20], containerWidth: 300 }));
    expect(r.visibleKeys).toEqual(["a", "b"]);
    expect(r.hiddenKeys).toEqual(["c"]);
  });
});

describe("chip-fit (chip-induced third-row prevention)", () => {
  it("demotes tail pills of the last visible row until the chip fits", () => {
    // row1 = [b, c] is too full for the chip → demote c.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 1], widths: [100, 140, 150], containerWidth: 300 }));
    expect(r.visibleKeys).toEqual(["a", "b"]);
    expect(r.hiddenKeys).toEqual(["c"]);
  });

  it("reduces the last row to chip-only when even one pill cannot fit with the chip", () => {
    // container is so narrow that row1 cannot hold b + chip → b demoted, row1 = chip only.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 2], widths: [50, 50, 50], containerWidth: 80 }));
    expect(r.visibleKeys).toEqual(["a"]);
    expect(r.hiddenKeys).toEqual(["b", "c"]); // ordered by persisted keys
  });
});

describe("chip reservation arithmetic (off-by-one gap count)", () => {
  it("counts n gaps for n pills + chip, not n-1", () => {
    // widths sum 253; with the CORRECT n-gap (2 pills → 4px) the row overflows:
    //   253 + 40 chip + 4 gaps = 297 > (300 - 4 eps) = 296  → demote c.
    // With the WRONG n-1 gap (2px) it would be 295 ≤ 296 → wrongly keep c visible.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 1], widths: [100, 120, 133], containerWidth: 300 }));
    expect(r.visibleKeys).toEqual(["a", "b"]);
    expect(r.hiddenKeys).toEqual(["c"]);
  });
});

describe("promotion (active tab always visible)", () => {
  it("is a no-op when the active tab is already visible", () => {
    // e,f are on row 2 → hidden; active a is already in row 0 → unchanged.
    const r = partitionVisible(p({ keys: ["a", "b", "c", "d", "e", "f"], buckets: [0, 0, 1, 1, 2, 2], widths: [110, 110, 110, 110, 110, 110], containerWidth: 300, activeKey: "a" }));
    expect(r.visibleKeys).toEqual(["a", "b", "c", "d"]);
    expect(r.hiddenKeys).toEqual(["e", "f"]);
  });

  it("promotes a hidden active tab, demoting the rightmost pill it displaces", () => {
    // row1 = [b] cannot hold [b, c] + chip → drop b, keep active c.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 1], widths: [100, 150, 150], containerWidth: 300, activeKey: "c" }));
    expect(r.visibleKeys).toEqual(["a", "c"]);
    expect(r.hiddenKeys).toEqual(["b"]);
  });

  it("demotes a wider promoted pill over a narrower one (greedy repack)", () => {
    // active c is wider (200) than the pill it displaces (150); active alone fits.
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 1], widths: [100, 150, 200], containerWidth: 300, activeKey: "c" }));
    expect(r.visibleKeys).toEqual(["a", "c"]);
    expect(r.hiddenKeys).toEqual(["b"]);
  });

  it("terminates by demoting every last-row pill before the active one", () => {
    // active is a hidden pill; the last row contains two pills that all must
    // give way for active to fit together with the chip.
    const r = partitionVisible(p({ keys: ["a", "b", "c", "d"], buckets: [0, 1, 1, 2], widths: [100, 120, 120, 60], containerWidth: 300, activeKey: "d" }));
    // row0=[a], row1=[b,c] (chip fits? 120+120+40+4=284≤296 → yes, no chip demote).
    // active d hidden (bucket2). candidate=[b,c,d]: 120+120+60+40+4gaps(8)=348>296 → drop c → [b,d]:120+60+40+4=224≤296 → fits.
    expect(r.visibleKeys).toEqual(["a", "b", "d"]);
    expect(r.hiddenKeys).toEqual(["c"]);
  });

  it("degrades to uncapped when a lone active pill plus the chip cannot fit", () => {
    const r = partitionVisible(p({ keys: ["a", "b", "c"], buckets: [0, 1, 2], widths: [20, 20, 20], containerWidth: 50, activeKey: "c" }));
    expect(r.uncapped).toBe(true);
    expect(r.visibleKeys).toEqual(["a", "b", "c"]);
    expect(r.hiddenKeys).toEqual([]);
  });
});
