import { useCallback, useEffect, useRef, useState } from "react";
import {
  buildTextIndex,
  findMatches,
  rangeForMatch,
  type FindOptions,
  type TextIndex,
  type TextMatch,
} from "./findDom";

/** Page-relative highlight rectangle; `matchIdx` indexes into `matches`. */
export type FindRect = { left: number; top: number; width: number; height: number; matchIdx: number };

/** Safety cap: an overlay div per rect on a pathological query must not tank
 *  the frame. Matches beyond this still get counted and walked. */
const MAX_HIGHLIGHT_RECTS = 4000;

/**
 * In-document find for DOM-rendered previews (Markdown prose, Word body).
 *
 * Mirrors `usePdfFind`'s contract — query + options, a match list, a current
 * index that wraps, and highlight rectangles — but derives the index from the
 * rendered text nodes instead of pdf.js text items. Offsets are content-
 * relative, so a rect layer placed inside the scroller follows scrolling
 * without remeasuring.
 */
export function useDomFind(params: {
  /** The scroll container (also the rect layer's containing block). */
  containerRef: React.RefObject<HTMLElement | null>;
  /** Element whose text is searchable (may equal the container). */
  textRootRef: React.RefObject<HTMLElement | null>;
  /** Bump on any content change (markdown source identity, docx render tick). */
  version: unknown;
  /** False while the viewer is in an edit mode where the DOM is not the doc. */
  enabled?: boolean;
}) {
  const { containerRef, textRootRef, version, enabled = true } = params;
  const [active, setActive] = useState(false);
  const [query, setQuery] = useState("");
  const [opts, setOpts] = useState<FindOptions>({ caseSensitive: false, wholeWord: false });
  const [matches, setMatches] = useState<TextMatch[]>([]);
  const [current, setCurrent] = useState(0);
  const [rects, setRects] = useState<FindRect[]>([]);

  const indexRef = useRef<TextIndex | null>(null);
  const matchesRef = useRef<TextMatch[]>([]);
  matchesRef.current = matches;

  const show = useCallback(() => setActive(true), []);
  const hide = useCallback(() => {
    setActive(false);
    setRects([]);
    setMatches([]);
    setCurrent(0);
    indexRef.current = null;
  }, []);

  const toggleOption = useCallback((key: keyof FindOptions) => {
    setOpts((o) => ({ ...o, [key]: !o[key] }));
  }, []);

  /** Walk to the next/previous match, wrapping at the ends. */
  const go = useCallback((dir: 1 | -1) => {
    const n = matchesRef.current.length;
    if (!n) return;
    setCurrent((c) => (c + dir + n) % n);
  }, []);

  // (Re)search whenever the bar is open and the query, options or document
  // changed. The index is rebuilt with the document: nodes from a previous
  // render are detached and would throw when a Range is set on them.
  useEffect(() => {
    if (!active || !enabled || !query) {
      indexRef.current = null;
      setMatches((m) => (m.length ? [] : m));
      setCurrent(0);
      return;
    }
    const root = textRootRef.current;
    if (!root) return;
    const index = buildTextIndex(root);
    indexRef.current = index;
    setMatches(findMatches(index, query, opts));
    setCurrent(0);
  }, [active, enabled, query, opts.caseSensitive, opts.wholeWord, version, textRootRef]);

  // Measure highlight rectangles from the live ranges. Content-relative, so
  // scrolling does not invalidate them; a resize or a re-render does.
  useEffect(() => {
    if (!active || !matches.length) {
      setRects((r) => (r.length ? [] : r));
      return;
    }
    const container = containerRef.current;
    const index = indexRef.current;
    if (!container || !index) return;

    let raf = 0;
    const measure = () => {
      const box = container.getBoundingClientRect();
      const next: FindRect[] = [];
      for (let i = 0; i < matches.length; i++) {
        if (next.length >= MAX_HIGHLIGHT_RECTS) break;
        const range = rangeForMatch(index, matches[i]);
        const clientRects = range?.getClientRects?.();
        if (!clientRects) continue;
        for (const cr of Array.from(clientRects)) {
          if (cr.width <= 0 || cr.height <= 0) continue;
          next.push({
            left: cr.left - box.left + container.scrollLeft,
            top: cr.top - box.top + container.scrollTop,
            width: cr.width,
            height: cr.height,
            matchIdx: i,
          });
          if (next.length >= MAX_HIGHLIGHT_RECTS) break;
        }
      }
      setRects(next);
    };
    raf = requestAnimationFrame(measure);

    let ro: ResizeObserver | null = null;
    const root = textRootRef.current;
    if (typeof ResizeObserver !== "undefined") {
      ro = new ResizeObserver(() => {
        cancelAnimationFrame(raf);
        raf = requestAnimationFrame(measure);
      });
      ro.observe(container);
      if (root && root !== container) ro.observe(root);
    }
    return () => {
      cancelAnimationFrame(raf);
      ro?.disconnect();
    };
  }, [active, matches, version, containerRef, textRootRef]);

  // Bring the current match into view. Uses the measured rect (content
  // coordinates) rather than a Range, so it stays correct while the document
  // is still settling.
  useEffect(() => {
    if (!active || !matches.length) return;
    const container = containerRef.current;
    const rect = rects.find((r) => r.matchIdx === current);
    if (!container || !rect) return;
    const top = Math.max(0, rect.top - container.clientHeight * 0.35);
    if (typeof container.scrollTo === "function") container.scrollTo({ top, behavior: "smooth" });
    else container.scrollTop = top;
  }, [active, current, matches, rects, containerRef]);

  return {
    active,
    show,
    hide,
    query,
    setQuery,
    opts,
    toggleOption,
    matches,
    count: matches.length,
    current,
    go,
    rects,
  };
}
