/**
 * Lane layout for the history graph. Pure: turns a topologically ordered
 * commit list (children before parents) into one row per commit, each with the
 * edges to draw inside that row.
 *
 * Row geometry, used by GitHistoryPanel to draw the SVG:
 *  - "pass":     a lane running straight through the row (top to bottom).
 *  - "incoming": the row's top edge into the commit's node (same lane).
 *  - "parent":   from the node's centre down to the bottom edge of the row, at
 *                the lane that carries the parent (same lane for first parent).
 *
 * A hash occupies at most one lane at a time (a lane is only opened for a
 * hash no lane already waits for), so "incoming" is always a vertical line.
 */

/** Palette slots a lane cycles through. Each lane keeps its slot while open. */
export const GRAPH_PALETTE_SIZE = 8;

export interface GraphEdge {
  fromLane: number;
  toLane: number;
  /** Palette slot, 0..GRAPH_PALETTE_SIZE-1. */
  color: number;
  kind: "pass" | "parent" | "incoming";
}

export interface GraphRow {
  hash: string;
  /** Lane of this commit's node. */
  lane: number;
  /** Palette slot of this commit's lane. */
  color: number;
  /** Edges drawn inside this row. */
  edges: GraphEdge[];
  /** Lanes this row touches, counting from 0: sizes the SVG width. */
  activeLanes: number;
}

export function layoutCommitGraph(
  commits: ReadonlyArray<{ hash: string; parents: string[] }>,
): GraphRow[] {
  // lanes[i]: the hash lane i is waiting for, or null when the lane is free.
  const lanes: (string | null)[] = [];
  // colors[i]: palette slot of lane i, set when the lane is opened.
  const colors: number[] = [];
  let nextColor = 0;
  // Hashes already laid out. A parent in this set was listed earlier, so no
  // lane must wait for it: it would run straight to the bottom of the graph.
  const emitted = new Set<string>();

  const claimLane = (hash: string): number => {
    let i = lanes.indexOf(null);
    if (i === -1) {
      i = lanes.length;
      lanes.push(null);
      colors.push(0);
    }
    lanes[i] = hash;
    colors[i] = nextColor;
    nextColor = (nextColor + 1) % GRAPH_PALETTE_SIZE;
    return i;
  };

  const rows: GraphRow[] = [];
  for (const commit of commits) {
    const waitingAt = lanes.indexOf(commit.hash);
    const lane = waitingAt !== -1 ? waitingAt : claimLane(commit.hash);
    const color = colors[lane];
    const edges: GraphEdge[] = [];

    // Every other open lane runs straight through this row.
    for (let i = 0; i < lanes.length; i++) {
      if (i !== lane && lanes[i] !== null) {
        edges.push({ fromLane: i, toLane: i, color: colors[i], kind: "pass" });
      }
    }
    if (waitingAt !== -1) {
      edges.push({ fromLane: lane, toLane: lane, color, kind: "incoming" });
    }

    // The node's lane is released, then the parents claim lanes. The first
    // parent takes this lane over unless another lane already waits for it;
    // every later parent reuses its lane or opens one.
    lanes[lane] = null;
    commit.parents.forEach((parent, index) => {
      if (emitted.has(parent)) return;
      const existing = lanes.indexOf(parent);
      let to: number;
      if (existing !== -1) {
        to = existing;
      } else if (index === 0) {
        lanes[lane] = parent;
        to = lane;
      } else {
        to = claimLane(parent);
      }
      edges.push({ fromLane: lane, toLane: to, color: colors[to], kind: "parent" });
    });

    emitted.add(commit.hash);
    // Measured before trimming, so an edge into a lane freed in this row still
    // counts toward the width.
    const activeLanes = Math.max(lane, ...edges.map((e) => Math.max(e.fromLane, e.toLane))) + 1;
    rows.push({ hash: commit.hash, lane, color, edges, activeLanes });

    while (lanes.length > 0 && lanes[lanes.length - 1] === null) {
      lanes.pop();
      colors.pop();
    }
  }
  return rows;
}
