import { useEffect, useRef, type RefObject, type UIEvent } from "react";

/** Within this many px of the bottom still counts as "following the stream". */
const STICK_TO_BOTTOM_PX = 8;

/** Which edge a nav affordance jumped to. */
export type StickToBottomEdge = "top" | "bottom";

export interface StickToBottomHandle {
  /** Goes on the scroll region: records whether the reader is at the bottom. */
  onScroll: (e: UIEvent<HTMLElement>) => void;
  /**
   * Report an explicit nav-button jump. The caller has already scrolled the
   * element (ScrollNavButtons does that), so this only realigns the follow
   * intent: "bottom" resumes the auto-scroll immediately instead of waiting
   * for the smooth scroll to finish, and "top" stops an incoming update from
   * yanking the reader back down before the first scroll event lands.
   */
  onNavigate: (edge: StickToBottomEdge) => void;
}

/**
 * Keep a scroll region pinned to its newest content unless the user scrolled
 * up to read. `dep` is whatever changes when content is appended; the returned
 * `onScroll` goes on the region and records whether it is at the bottom.
 * Tracked in a ref, not state: it changes on every scroll event and nothing
 * renders from it. Scrolling back to the bottom resumes following.
 *
 * A collapsed/expanded surface (an agent card whose transcript mounts only
 * when open) must pass the dep only while mounted — e.g.
 * `open ? messages : undefined` — so the first pin runs once the scroller
 * actually exists, not against a null ref.
 */
export function useStickToBottom(
  ref: RefObject<HTMLElement | null>,
  dep: unknown,
): StickToBottomHandle {
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
    onNavigate: (edge) => {
      atBottomRef.current = edge === "bottom";
    },
  };
}
