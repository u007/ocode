import { describe, expect, it } from "vitest";
import { layoutCommitGraph, GRAPH_PALETTE_SIZE } from "./commitGraph";

const c = (hash: string, ...parents: string[]) => ({ hash, parents });

describe("layoutCommitGraph", () => {
  it("keeps a linear chain on one lane with one colour", () => {
    const rows = layoutCommitGraph([c("a", "b"), c("b", "c"), c("c")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0, 0]);
    expect(new Set(rows.map((r) => r.color)).size).toBe(1);
    expect(rows.map((r) => r.activeLanes)).toEqual([1, 1, 1]);
    // The root's lane is released, so nothing is drawn below it.
    expect(rows[2].edges).toEqual([
      { fromLane: 0, toLane: 0, color: rows[2].color, kind: "incoming" },
    ]);
    // Each non-root row draws a parent edge down the same lane.
    expect(rows[0].edges).toEqual([
      { fromLane: 0, toLane: 0, color: rows[0].color, kind: "parent" },
    ]);
  });

  it("draws a branch and its merge back into the trunk on two lanes", () => {
    // M merges A and B; A and B both descend from R.
    const rows = layoutCommitGraph([c("M", "A", "B"), c("A", "R"), c("B", "R"), c("R")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0, 1, 0]);

    const merge = rows[0];
    expect(merge.activeLanes).toBe(2);
    expect(merge.edges).toContainEqual(
      expect.objectContaining({ fromLane: 0, toLane: 1, kind: "parent" }),
    );

    // While A is laid out, B's lane runs straight through.
    expect(rows[1].edges).toContainEqual(
      expect.objectContaining({ fromLane: 1, toLane: 1, kind: "pass" }),
    );

    // B's parent edge merges its lane back into R's lane.
    expect(rows[2].edges).toContainEqual(
      expect.objectContaining({ fromLane: 1, toLane: 0, kind: "parent" }),
    );
    expect(rows[3].edges).toEqual([
      expect.objectContaining({ fromLane: 0, toLane: 0, kind: "incoming" }),
    ]);
  });

  it("counts a lane freed in the row toward activeLanes", () => {
    // B's lane (1) converges into R at lane 0 and is freed in that row.
    const rows = layoutCommitGraph([c("M", "A", "B"), c("A", "R"), c("B", "R"), c("R")]);
    expect(rows[2].activeLanes).toBe(2);
  });

  it("tolerates a parent listed earlier by opening no lane for it", () => {
    // y's parent x was already laid out, so y must not leave a lane waiting.
    const rows = layoutCommitGraph([c("x"), c("y", "x"), c("z")]);
    expect(rows[1].edges).toEqual([]);
    expect(rows[2].lane).toBe(0);
    expect(rows[2].edges).toEqual([]);
  });

  it("frees the lane of a root commit so the next unrelated root reuses it", () => {
    const rows = layoutCommitGraph([c("r1"), c("r2")]);
    expect(rows.map((r) => r.lane)).toEqual([0, 0]);
    expect(rows.map((r) => r.activeLanes)).toEqual([1, 1]);
  });

  it("keeps a parent that is still in the log on its own lane", () => {
    // A parent listed later waits on its lane, so the pass edge is drawn.
    const rows = layoutCommitGraph([c("t", "p"), c("u"), c("p")]);
    expect(rows[1].lane).toBe(1);
    expect(rows[1].edges).toContainEqual(
      expect.objectContaining({ fromLane: 0, toLane: 0, kind: "pass" }),
    );
  });

  it("cycles the palette once every slot has been used", () => {
    const roots = Array.from({ length: GRAPH_PALETTE_SIZE + 1 }, (_, i) => c(`r${i}`));
    const colors = layoutCommitGraph(roots).map((r) => r.color);
    expect(colors).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 0]);
  });

  it("returns no rows for an empty log", () => {
    expect(layoutCommitGraph([])).toEqual([]);
  });
});
