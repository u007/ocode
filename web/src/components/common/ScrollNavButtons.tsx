import { useCallback, useEffect, useState, type RefObject } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";

/** Distance (px) from an edge within which that edge counts as reached. */
const EDGE_THRESHOLD = 24;

export interface ScrollNavButtonsProps {
  /** The scrollable element these buttons drive. */
  scrollRef: RefObject<HTMLElement | null>;
  /** Re-evaluate visibility when this changes (e.g. a newly selected item). */
  watch?: unknown;
  /** Extra classes merged onto the floating button column. */
  className?: string;
}

/**
 * Floating "scroll to top" / "scroll to bottom" buttons for a scrollable
 * surface (chat transcript, agent run list/detail). Visibility is derived from
 * the container's own geometry so the pair is self-managing:
 *
 *  - a `scroll` listener catches user/programmatic scrolls;
 *  - a `ResizeObserver` catches the container's box changing;
 *  - a subtree `MutationObserver` catches *content* growth, which is the case
 *    that matters for streaming surfaces — the transcript can grow while the
 *    scroll offset is unchanged, so no `scroll` event would ever fire and the
 *    top button would stay hidden until the user nudged the viewport.
 *
 * Renders nothing when the content fits (no scrolling possible) or when the
 * viewport already sits at the only edge there is — an at-top short list shows
 * no chrome, an at-bottom long transcript shows only "scroll to top".
 *
 * The buttons live inside the caller's `relative` container; the column is
 * `pointer-events-none` with `pointer-events-auto` on the buttons so the
 * unused gap between them never swallows clicks meant for the content below.
 */
export default function ScrollNavButtons({ scrollRef, watch, className }: ScrollNavButtonsProps) {
  const [atTop, setAtTop] = useState(true);
  const [atBottom, setAtBottom] = useState(true);

  const recompute = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    setAtTop(el.scrollTop <= EDGE_THRESHOLD);
    setAtBottom(distanceFromBottom <= EDGE_THRESHOLD);
  }, [scrollRef]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    recompute();
    el.addEventListener("scroll", recompute, { passive: true });
    // ResizeObserver is not implemented by jsdom (and not guaranteed in every
    // embedded webview), so it is optional; the MutationObserver still covers
    // content-driven growth where one is available.
    const ro = typeof ResizeObserver !== "undefined" ? new ResizeObserver(recompute) : null;
    ro?.observe(el);
    const mo = typeof MutationObserver !== "undefined" ? new MutationObserver(recompute) : null;
    mo?.observe(el, { childList: true, subtree: true, characterData: true });
    return () => {
      el.removeEventListener("scroll", recompute);
      ro?.disconnect();
      mo?.disconnect();
    };
  }, [scrollRef, recompute, watch]);

  const scrollTo = useCallback(
    (edge: "top" | "bottom") => {
      const el = scrollRef.current;
      if (!el) return;
      const top = edge === "top" ? 0 : el.scrollHeight;
      // Element.prototype.scrollTo is missing in jsdom; fall back to the
      // property assignment so the affordance degrades instead of throwing.
      if (typeof el.scrollTo === "function") {
        el.scrollTo({ top, behavior: "smooth" });
      } else {
        el.scrollTop = top;
      }
    },
    [scrollRef],
  );

  if (atTop && atBottom) return null;

  return (
    <div
      className={`pointer-events-none absolute bottom-4 right-4 z-10 flex flex-col gap-2 ${
        className ?? ""
      }`}
    >
      {!atTop && (
        <button
          type="button"
          onClick={() => scrollTo("top")}
          title="Scroll to top"
          aria-label="Scroll to top"
          className="pointer-events-auto flex h-9 w-9 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-lg transition-colors hover:bg-accent/90"
        >
          <ArrowUp className="h-4 w-4" />
        </button>
      )}
      {!atBottom && (
        <button
          type="button"
          onClick={() => scrollTo("bottom")}
          title="Scroll to bottom"
          aria-label="Scroll to bottom"
          className="pointer-events-auto flex h-9 w-9 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-lg transition-colors hover:bg-accent/90"
        >
          <ArrowDown className="h-4 w-4" />
        </button>
      )}
    </div>
  );
}
