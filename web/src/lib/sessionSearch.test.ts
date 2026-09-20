import { describe, expect, it } from "vitest";
import {
  buildJumpTargets,
  inWindowMatchCount,
  olderPrefixFetch,
  serverIndexToLocal,
} from "./sessionSearch";
import type { Message } from "../api/types";

/** N dummy messages; only the length matters to the index math. */
function msgs(n: number): Message[] {
  return Array.from({ length: n }, (_, i) => ({ role: "user", content: `m${i}` }));
}

describe("serverIndexToLocal", () => {
  it("maps a server index inside the window to its local position", () => {
    // Window holds server messages 100..149 (50 loaded).
    expect(serverIndexToLocal(100, msgs(50), 100)).toBe(0);
    expect(serverIndexToLocal(120, msgs(50), 100)).toBe(20);
    expect(serverIndexToLocal(149, msgs(50), 100)).toBe(49);
  });

  it("returns -1 for a match older than the loaded window", () => {
    expect(serverIndexToLocal(99, msgs(50), 100)).toBe(-1);
    expect(serverIndexToLocal(0, msgs(50), 100)).toBe(-1);
  });

  it("returns -1 for a match newer than the loaded window", () => {
    expect(serverIndexToLocal(150, msgs(50), 100)).toBe(-1);
  });

  it("refuses to guess when the window anchor is unknown", () => {
    // -1 anchor (live-only slice before its first page resolved).
    expect(serverIndexToLocal(5, msgs(50), -1)).toBe(-1);
    expect(serverIndexToLocal(0, msgs(50), -1)).toBe(-1);
  });
});

describe("olderPrefixFetch", () => {
  it("loads the contiguous prefix from the match up to the window start", () => {
    // Window holds 150..199 (50 loaded); transcript total 200; match at 120.
    // offset = totalMessages - windowStart = 200 - 150 = 50 skips the loaded
    // window; limit = 150 - 120 = 30 back from the window start.
    expect(olderPrefixFetch(120, 150, 200)).toEqual({ limit: 30, offset: 50 });
  });

  it("derives offset from server counts, not the (possibly inflated) local array", () => {
    // 3 client-only ADD_MESSAGE injections sit at the tail, so the loaded
    // window is 50 server rows but 53 local messages. The fetch must still
    // skip exactly 50 (the server rows), not 53 — otherwise the prepended
    // block leaves a 3-message gap and the match lands off by 3.
    const localWithInjections = msgs(53);
    expect(localWithInjections.length).toBe(53);
    expect(olderPrefixFetch(120, 150, 200)).toEqual({ limit: 30, offset: 50 });
  });

  it("uses the window start as the walk-back anchor, not the transcript total", () => {
    // Same match, a smaller loaded window -> offset tracks the loaded rows
    // (total - windowStart), not a fixed value.
    expect(olderPrefixFetch(120, 150, 160)).toEqual({ limit: 30, offset: 10 });
  });

  it("returns null when the match is already loaded", () => {
    expect(olderPrefixFetch(160, 150, 200)).toBeNull();
  });

  it("returns null when the match is newer than the window (cannot prepend)", () => {
    // A pinned (non-tail) window cannot be extended downward by prepending.
    expect(olderPrefixFetch(150, 100, 150)).toBeNull();
  });

  it("returns null when the anchor is unknown", () => {
    expect(olderPrefixFetch(10, -1, 60)).toBeNull();
  });

  it("returns null when the anchor exceeds the server total", () => {
    // Counts disagree (mid-turn/live slice): refuse rather than prepend a gap.
    expect(olderPrefixFetch(10, 150, 100)).toBeNull();
  });

  it("returns null for a negative index", () => {
    expect(olderPrefixFetch(-1, 150, 200)).toBeNull();
  });
});

describe("inWindowMatchCount", () => {
  it("counts only the matches already mapped to a navigable entry", () => {
    // Window 100..149; matches at 3, 47, 120, 149 => two have entry positions.
    const map = new Map<number, number>([
      [120, 4],
      [149, 9],
    ]);
    expect(inWindowMatchCount([3, 47, 120, 149], map)).toBe(2);
  });

  it("does not count an in-window index with no navigable entry (sentinel)", () => {
    // 101 is inside the window but excluded from renderEntries, so it is not a
    // navigable target and must not suppress the "N total, M in view" note.
    const map = new Map<number, number>([[100, 3]]);
    expect(inWindowMatchCount([100, 101], map)).toBe(1);
  });

  it("counts nothing for an empty map", () => {
    expect(inWindowMatchCount([1, 2, 3], new Map())).toBe(0);
  });

  it("handles an empty match list", () => {
    expect(inWindowMatchCount([], new Map([[100, 1]]))).toBe(0);
  });
});

describe("buildJumpTargets", () => {
  const local = [2, 5, 9];

  it("falls back to local matches while the server result is pending", () => {
    expect(
      buildJumpTargets({
        serverIndices: null,
        entryPosByServerIndex: new Map(),
        localEntryPositions: local,
        windowStartServerIndex: 100,
      }),
    ).toEqual([
      { serverIndex: -1, entryPos: 2 },
      { serverIndex: -1, entryPos: 5 },
      { serverIndex: -1, entryPos: 9 },
    ]);
  });

  it("uses server hits (resolved and unresolved) once the query lands", () => {
    // Server knows about an off-window hit (0) plus in-window hits (100, 101).
    const map = new Map<number, number>([
      [100, 3],
      [101, 4],
    ]);
    expect(
      buildJumpTargets({
        serverIndices: [0, 100, 101],
        entryPosByServerIndex: map,
        localEntryPositions: local,
        windowStartServerIndex: 100,
      }),
    ).toEqual([
      { serverIndex: 0, entryPos: -1 },
      { serverIndex: 100, entryPos: 3 },
      { serverIndex: 101, entryPos: 4 },
    ]);
  });

  it("collapses indices that fold into the same bubble (call + result)", () => {
    // 100 and 101 both map to render-entry 3 (a tool call and its result).
    const map = new Map<number, number>([
      [100, 3],
      [101, 3],
    ]);
    expect(
      buildJumpTargets({
        serverIndices: [100, 101],
        entryPosByServerIndex: map,
        localEntryPositions: local,
        windowStartServerIndex: 100,
      }),
    ).toEqual([{ serverIndex: 100, entryPos: 3 }]);
  });

  it("ignores the server list when the window anchor is unknown", () => {
    const map = new Map<number, number>([[100, 3]]);
    expect(
      buildJumpTargets({
        serverIndices: [100],
        entryPosByServerIndex: map,
        localEntryPositions: local,
        windowStartServerIndex: -1,
      }),
    ).toEqual([
      { serverIndex: -1, entryPos: 2 },
      { serverIndex: -1, entryPos: 5 },
      { serverIndex: -1, entryPos: 9 },
    ]);
  });

  it("keeps an unresolved server hit as a fetchable target", () => {
    // Server reported a hit whose entry position did not resolve (off-window,
    // or folded into a bubble the current fold does not expose). It stays in
    // the list with entryPos -1 so jumping can fetch and then scroll.
    expect(
      buildJumpTargets({
        serverIndices: [42],
        entryPosByServerIndex: new Map(),
        localEntryPositions: local,
        windowStartServerIndex: 100,
      }),
    ).toEqual([{ serverIndex: 42, entryPos: -1 }]);
  });

  it("trusts an empty server result (no local resurrection)", () => {
    expect(
      buildJumpTargets({
        serverIndices: [],
        entryPosByServerIndex: new Map(),
        localEntryPositions: local,
        windowStartServerIndex: 100,
      }),
    ).toEqual([]);
  });
});
