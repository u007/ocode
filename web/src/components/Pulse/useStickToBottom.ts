import { useEffect, useRef, type RefObject, type UIEvent } from "react";

/** Within this many px of the bottom still counts as "following the stream". */
const STICK_TO_BOTTOM_PX = 8;

/**
 * Keep a scroll region pinned to its newest content unless the user scrolled
 * up to read. `dep` is whatever changes when content is appended; the returned
 * `onScroll` goes on the region and records whether it is at the bottom.
 * Tracked in a ref, not state: it changes on every scroll event and nothing
 * renders from it. Scrolling back to the bottom resumes following.
 */
export function useStickToBottom(
  ref: RefObject<HTMLElement | null>,
  dep: unknown,
): { onScroll: (e: UIEvent<HTMLElement>) => void } {
  const atBottomRef = useRef(true);
  useEffect(() => {
    const el = ref.current;
    if (el && atBottomRef.current) el.scrollTop = el.scrollHeight;
  }, [ref, dep]);
  return {
    onScroll: (e) => {
      const el = e.currentTarget;
      atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < STICK_TO_BOTTOM_PX;
    },
  };
}
