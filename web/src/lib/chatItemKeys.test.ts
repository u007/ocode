import { describe, expect, it } from "vitest";
import { transcriptItemKey, UNKNOWN_WINDOW_START } from "./chatItemKeys";

describe("transcriptItemKey", () => {
  it("is stable across a snapshot that replaces every message object", () => {
    // The turn-boundary MERGE_SNAPSHOT re-fetches a tail page: same window
    // start, same row count, but every Message is a brand-new object. Global
    // position must not move, or the virtualizer's itemSizeCache is thrown
    // away at the end of every turn and the list height collapses.
    const before = [120, 121, 122].map((i) => transcriptItemKey(120, i - 120));
    const after = [120, 121, 122].map((i) => transcriptItemKey(120, i - 120));
    expect(after).toEqual(before);
    expect(before).toEqual([120, 121, 122]);
  });

  it("does not move existing rows when older messages are prepended", () => {
    // PREPEND_MESSAGES walks the window start back by exactly the number of
    // prepended rows while every local index shifts forward by the same
    // amount, so the global position of a pre-existing row is unchanged.
    const windowStartBefore = 100;
    const localBefore = 12; // a row already on screen
    const prepended = 40;

    const localAfter = localBefore + prepended;
    const windowStartAfter = windowStartBefore - prepended;

    expect(transcriptItemKey(windowStartAfter, localAfter)).toBe(
      transcriptItemKey(windowStartBefore, localBefore),
    );
  });

  it("falls back to the local position while the window start is unknown", () => {
    // A slice built purely from SSE deltas has no page yet, so the window
    // start is the store's negative "unknown" sentinel. It must never be
    // added to the local index, or a key would be negative and could collide
    // with an unrelated row.
    expect(transcriptItemKey(UNKNOWN_WINDOW_START, 3)).toBe(3);
    expect(transcriptItemKey(UNKNOWN_WINDOW_START, 3)).toBeGreaterThanOrEqual(0);
  });

  it("keeps distinct rows distinct", () => {
    const keys = [0, 1, 2, 3].map((i) => transcriptItemKey(500, i));
    expect(new Set(keys).size).toBe(keys.length);
  });
});
