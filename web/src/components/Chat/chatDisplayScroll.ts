export interface ChatDisplayScrollAnchor {
  index: number;
  /** Reader offset inside the anchored row, in virtualizer scroll coordinates. */
  distance: number;
}

export type ChatDisplayOffsetResolver = (
  index: number,
) => readonly [number, "auto" | "start" | "end" | "center"] | undefined;

export type ChatDisplayIndexScroller = {
  scrollToIndex: (index: number, options: { align: "start" }) => void;
};

/** Capture the first visible row and the reader's pixel position inside it. */
export function captureChatDisplayAnchor(
  scrollTop: number,
  firstVisibleIndex: number | undefined,
  getOffsetForIndex: ChatDisplayOffsetResolver,
): ChatDisplayScrollAnchor | null {
  if (firstVisibleIndex === undefined) return null;
  const resolved = getOffsetForIndex(firstVisibleIndex);
  if (!resolved) return null;
  return { index: firstVisibleIndex, distance: scrollTop - resolved[0] };
}

/** Restore the same row and same in-row pixel position after remeasurement. */
export function restoreChatDisplayAnchor(
  scrollElement: { scrollTop: number },
  anchor: ChatDisplayScrollAnchor | null,
  virtualizer: ChatDisplayIndexScroller & {
    getOffsetForIndex: ChatDisplayOffsetResolver;
  },
): void {
  if (!anchor) return;
  virtualizer.scrollToIndex(anchor.index, { align: "start" });
  const resolved = virtualizer.getOffsetForIndex(anchor.index);
  if (!resolved) return;
  scrollElement.scrollTop = resolved[0] + anchor.distance;
}
