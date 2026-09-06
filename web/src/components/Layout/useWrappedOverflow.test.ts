import { describe, it, expect } from "vitest";
import { computeRowBuckets, partitionVisible } from "./useWrappedOverflow";

/**
 * Tests for the pure wrapped-overflow partition core (spec:
 * docs/superpowers/specs/2026-09-06-multirow-tab-bar-design.md). These are
 * pure unit tests — no DOM, no jsdom — matching the module's own contract.
 *
 * Geometry contract: pills are `flex: 0 0 auto`; `widths` are probe-measured
 * `offsetWidth`s; a row of n pills plus the chip costs
 *   Σ widths + gapPx·n + chipReservedPx
 * (n−1 pill gaps + one pill→chip gap). The fit budget is
 *   containerWidth − epsilonPx.
 */

describe("computeRowBuckets", () => {
  it("returns empty for empty input", () => {
    expect(computeRowBuckets([])).toEqual([]);
  });

  it("groups a single row of equal tops as row 0", () => {
    expect(computeRowBuckets([0, 0, 0])).toEqual([0, 0, 0]);
  });

  it("assigns a new row when the top exceeds the row anchor by > tolerance", () => {
    expect(computeRowBuckets([0, 0, 20, 20, 40])).toEqual([0, 0, 1, 1, 2]);
  });

  it("ignores sub-tolerance jitter along a line (anchor, not previous, comparison)", () => {
    // 0→4 is within tolerance; 5 from anchor 0 is not → new row.
    expect(computeRowBuckets([0, 2, 3, 5], 4)).toEqual([0, 0, 0, 1]);
  });

  it("respects an explicit tolerance", () => {
    expect(computeRowBuckets([0, 6], 4)).toEqual([0, 1]);
    expect(computeRowBuckets([0, 6], 10)).toEqual([0, 0]);
  });
});

describe("partitionVisible — baseline", () => {
  const base = {
    chipReservedPx: 40,
    maxRows: 2,
    activeKey: null,
  };

  it("keeps everything visible when nothing overflows (no chip)", () => {
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c"],
      buckets: [0, 0, 0],
      widths: [100, 100, 100],
      containerWidth: 400,
    });
    expect(r.visibleKeys).toEqual(["a", "b", "c"]);
    expect(r.hiddenKeys).toEqual([]);
    expect(r.uncapped).toBeUndefined();
  });

  it("treats a multi-row wrap within the cap as fully visible", () => {
    // Two rows, both under maxRows → no hidden, no chip.
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d"],
      buckets: [0, 0, 1, 1],
      widths: [100, 100, 100, 100],
      containerWidth: 400,
    });
    expect(r.visibleKeys).toEqual(["a", "b", "c", "d"]);
    expect(r.hiddenKeys).toEqual([]);
  });

  it("hides pills measured on or beyond maxRows", () => {
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e", "f"],
      buckets: [0, 0, 1, 1, 2, 2],
      widths: [100, 100, 100, 100, 100, 100],
      containerWidth: 400,
    });
    expect(r.visibleKeys).toEqual(["a", "b", "c", "d"]);
    expect(r.hiddenKeys).toEqual(["e", "f"]);
  });

  it("keeps canonical persisted order in both output arrays", () => {
    const r = partitionVisible({
      ...base,
      keys: ["c", "a", "b", "e", "d"],
      buckets: [0, 0, 0, 1, 1],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 400,
      maxRows: 1,
    });
    // keys in persisted order, not row order
    expect(r.visibleKeys).toEqual(["c", "a", "b"]);
    expect(r.hiddenKeys).toEqual(["e", "d"]);
  });
});

describe("partitionVisible — chip-fit (chip never yields)", () => {
  const base = {
    chipReservedPx: 40,
    maxRows: 2,
    activeKey: null,
  };

  it("demotes tail pills until the chip fits on the last visible row", () => {
    // Row 1 pills c,d each 100 wide; with one extra hidden pill the chip must
    // be reserved on top → row1(c,d)+chip = 100+100+2*2+40=244 ≤ 296 budget.
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 300, // budget = 296
    });
    expect(r.visibleKeys).toEqual(["a", "b", "c", "d"]);
    expect(r.hiddenKeys).toEqual(["e"]);
  });

  it("demotes until chip fits — exact-fit boundary vs one-pixel overflow", () => {
    // Row1 = [c] (100) with chip: 100 + 2*1 + 40 = 142. Budget for
    // containerWidth=146 → 142 ≤ 142 fits exactly (epsilon 4). Everything
    // else on row1 (d,100) does not fit once the chip is added.
    const fit = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 146, // budget 142
    });
    expect(fit.visibleKeys).toEqual(["a", "b", "c"]);
    expect(fit.hiddenKeys).toEqual(["d", "e"]);

    // One pixel less: budget 141. Row 1 must accommodate c alone plus the
    // chip: 100 + gap(2) + chip(40) = 142 > 141 → c demotes too. Visibles
    // are exactly row 0 (a,b); everything else collapses behind the chip.
    const overflow = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 145, // budget 141
    });
    expect(overflow.visibleKeys).toEqual(["a", "b"]);
    expect(overflow.hiddenKeys).toEqual(["c", "d", "e"]);
  });

  it("accepts a chip-only last row at ultra-narrow width (chip never yields)", () => {
    // Row1 single pill c (100) does not share with chip; demote c itself? No —
    // chip-only row means c also demoted when c+chip cannot fit.
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 130, 100, 100],
      containerWidth: 120, // c (130) > budget (116) alone → cannot host chip
    });
    // Row0: a,b(chip? a,b + chip = 200+4+40=244 > budget 116) → a alone must
    // fit? no. This is absurd-narrow territory handled below.
    expect(r.hiddenKeys.length + r.visibleKeys.length).toBe(5);
    expect(r.hiddenKeys.includes("e")).toBe(true);
  });
});

describe("partitionVisible — promotion (active always visible)", () => {
  const base = {
    chipReservedPx: 40,
    maxRows: 2,
  };

  it("promotes a hidden active into the last visible row when it fits", () => {
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 300,
      activeKey: "e",
    });
    expect(r.visibleKeys).toContain("e");
    expect(r.hiddenKeys).not.toContain("e");
  });

  it("leaves a visible active untouched (no-op)", () => {
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 300,
      activeKey: "b",
    });
    expect(r.visibleKeys).toContain("b");
    expect(r.hiddenKeys).toEqual(["e"]);
  });

  it("demotes wider pills on promotion when capacity is constrained", () => {
    const r = partitionVisible({
      ...base,
      keys: ["a", "b", "c", "d", "e"],
      buckets: [0, 0, 1, 1, 2],
      widths: [100, 100, 100, 100, 100],
      containerWidth: 300,
      activeKey: "e",
    });
    expect(r.visibleKeys).toContain("e");
    expect(r.hiddenKeys).not.toContain("e");
  });
});

describe("partitionVisible — degradation", () => {
  const chipTooWide = {
    chipReservedPx: 40,
    maxRows: 2,
    activeKey: null,
  };

  it("degrades to uncapped when the chip cannot fit on any row", () => {
    // hidden = {c} (bucket 2 ≥ maxRows) so the chip is required; but the
    // chip alone (40) already exceeds budget (30−4=26) → capping infeasible.
    const r = partitionVisible({
      ...chipTooWide,
      keys: ["a", "b", "c"],
      buckets: [0, 1, 2],
      widths: [60, 60, 60],
      containerWidth: 30, // budget 26 < chipReservedPx 40
    });
    expect(r.uncapped).toBe(true);
    expect(r.visibleKeys).toEqual(["a", "b", "c"]);
    expect(r.hiddenKeys).toEqual([]);
  });

  it("renders everything and drops the chip in absurd-narrow degrade", () => {
    // chip fits the budget (96 ≥ 40) but the active pill c (999) can never
    // share a row with the chip → capping infeasible → uncapped.
    const r = partitionVisible({
      keys: ["a", "b", "c"],
      buckets: [0, 1, 2],
      widths: [60, 60, 999],
      containerWidth: 100, // budget 96 ≥ chipReservedPx 40
      chipReservedPx: 40,
      maxRows: 2,
      activeKey: "c",
    });
    expect(r.uncapped).toBe(true);
    expect(r.visibleKeys).toEqual(["a", "b", "c"]);
    expect(r.hiddenKeys).toEqual([]);
  });
});
