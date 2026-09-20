import { useCallback, useEffect, useRef, useState } from "react";
import type * as pdfjsLib from "pdfjs-dist";

/**
 * Full-document find for the PDF viewer.
 *
 * The viewer renders every page's text as absolutely-positioned spans (one
 * span per pdf.js text item, separated by a single space text node). This hook
 * builds a per-page text index from the same `getTextContent()` items joined
 * the same way, so a match's character offsets map 1:1 onto the DOM spans —
 * which lets highlights be cut as DOM `Range`s over the live spans and turned
 * into page-relative rectangles for the overlay layer.
 *
 * The hook owns only state + rect measurement; scrolling to the current match
 * is the viewer's job (it knows the stack geometry).
 */

export type PdfMatch = { page: number; start: number; end: number };
export type PdfFindOptions = { caseSensitive: boolean; wholeWord: boolean };
/** Page-relative highlight rectangle; `matchIdx` indexes into `matches`. */
export type FindRect = { left: number; top: number; width: number; height: number; matchIdx: number };

type PageText = { text: string; spans: { start: number; len: number }[] };

export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function usePdfFind(params: {
  docRef: { current: pdfjsLib.PDFDocumentProxy | null };
  textRefs: { current: Map<number, HTMLDivElement> };
  canvasRefs: { current: Map<number, HTMLCanvasElement> };
  /** Bumped by the viewer after a page's spans are (re)built or cleared. */
  renderTick: number;
  /** Bumped by the viewer when a new document lands. */
  docVersion: number;
}) {
  const { docRef, textRefs, canvasRefs, renderTick, docVersion } = params;
  const [active, setActive] = useState(false);
  const [query, setQuery] = useState("");
  const [opts, setOpts] = useState<PdfFindOptions>({ caseSensitive: false, wholeWord: false });
  const [matches, setMatches] = useState<PdfMatch[]>([]);
  const [current, setCurrent] = useState(0);
  const [pageRects, setPageRects] = useState<Map<number, FindRect[]>>(new Map());
  const [searching, setSearching] = useState(false);

  const indexRef = useRef<{ doc: pdfjsLib.PDFDocumentProxy; pages: Map<number, PageText> } | null>(null);
  const runRef = useRef(0);
  const matchesRef = useRef<PdfMatch[]>([]);
  matchesRef.current = matches;

  // New document: drop everything, including the cached text index.
  useEffect(() => {
    indexRef.current = null;
    runRef.current++;
    setMatches([]);
    setCurrent(0);
    setPageRects(new Map());
    setActive(false);
    setQuery("");
    setSearching(false);
  }, [docVersion]);

  const show = useCallback(() => setActive(true), []);
  const hide = useCallback(() => {
    setActive(false);
    setPageRects(new Map());
  }, []);

  /** Walk to the next/previous match, wrapping at the ends. */
  const go = useCallback((dir: 1 | -1) => {
    const ms = matchesRef.current;
    if (!ms.length) return;
    setCurrent((c) => (c + dir + ms.length) % ms.length);
  }, []);

  // Search whenever the query or options change while the bar is open. The
  // per-page text index is built once per document and cached.
  useEffect(() => {
    if (!active) return;
    const doc = docRef.current;
    if (!doc || !query.trim()) {
      runRef.current++;
      setMatches([]);
      setCurrent(0);
      setSearching(false);
      return;
    }
    const run = ++runRef.current;
    let cancelled = false;
    (async () => {
      setSearching(true);
      try {
        let index = indexRef.current;
        if (!index || index.doc !== doc) {
          index = { doc, pages: new Map() };
          for (let n = 1; n <= doc.numPages; n++) {
            if (runRef.current !== run) return;
            const pg = await doc.getPage(n);
            const tc = await pg.getTextContent();
            let text = "";
            const spans: { start: number; len: number }[] = [];
            for (const item of tc.items) {
              if (!("str" in item) || !item.str) continue;
              spans.push({ start: text.length, len: item.str.length });
              text += item.str;
              text += " "; // the DOM renders a space text node between spans
            }
            index.pages.set(n, { text, spans });
          }
          if (runRef.current !== run) return;
          indexRef.current = index;
        }
        const flags = opts.caseSensitive ? "g" : "gi";
        const body = opts.wholeWord ? `\\b(?:${escapeRegExp(query)})\\b` : escapeRegExp(query);
        const re = new RegExp(body, flags);
        const found: PdfMatch[] = [];
        for (let n = 1; n <= doc.numPages; n++) {
          const pt = index.pages.get(n);
          if (!pt) continue;
          re.lastIndex = 0;
          for (let m = re.exec(pt.text); m; m = re.exec(pt.text)) {
            if (m[0].length === 0) {
              re.lastIndex++;
              continue;
            }
            found.push({ page: n, start: m.index, end: m.index + m[0].length });
          }
        }
        if (runRef.current !== run || cancelled) return;
        setMatches(found);
        setCurrent(0);
      } catch {
        if (!cancelled && runRef.current === run) {
          setMatches([]);
          setCurrent(0);
        }
      } finally {
        if (!cancelled && runRef.current === run) setSearching(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [active, query, opts, docRef]);

  // Recompute highlight rectangles from the live text layer whenever the
  // match set changes, a page is (re)rendered, or the layout was invalidated
  // (renderTick also bumps on zoom/rotate redraws). Pages whose spans are not
  // in the DOM yet simply contribute no rects until they render.
  useEffect(() => {
    if (!active || matches.length === 0) {
      if (pageRects.size) setPageRects(new Map());
      return;
    }
    const perPage = new Map<number, { m: PdfMatch; idx: number }[]>();
    matches.forEach((m, idx) => {
      const arr = perPage.get(m.page);
      if (arr) arr.push({ m, idx });
      else perPage.set(m.page, [{ m, idx }]);
    });
    const byPage = new Map<number, FindRect[]>();
    for (const [n, ms] of perPage) {
      const layer = textRefs.current.get(n);
      const canvas = canvasRefs.current.get(n);
      const pt = indexRef.current?.pages.get(n);
      if (!layer || !canvas || !pt || layer.childElementCount === 0) continue;
      const spans = Array.from(layer.querySelectorAll("span"));
      if (spans.length !== pt.spans.length) continue; // mid-rebuild; wait for the next tick
      const box = canvas.getBoundingClientRect();
      const rects: FindRect[] = [];
      const locate = (off: number) => {
        for (let i = 0; i < pt.spans.length; i++) {
          const s = pt.spans[i];
          if (off <= s.start + s.len) return { node: spans[i].firstChild, off: off - s.start };
        }
        const last = pt.spans.length - 1;
        return { node: spans[last].firstChild, off: pt.spans[last].len };
      };
      for (const { m, idx } of ms) {
        const a = locate(m.start);
        const b = locate(m.end);
        if (!a.node || !b.node) continue;
        let clientRects: DOMRectList | null;
        try {
          const range = document.createRange();
          range.setStart(a.node, a.off);
          range.setEnd(b.node, b.off);
          clientRects = range.getClientRects ? range.getClientRects() : null;
        } catch {
          clientRects = null; // detached nodes / environments without Range rects
        }
        if (!clientRects) continue;
        for (const cr of Array.from(clientRects)) {
          if (cr.width <= 0 || cr.height <= 0) continue;
          rects.push({
            left: cr.left - box.left,
            top: cr.top - box.top,
            width: cr.width,
            height: cr.height,
            matchIdx: idx,
          });
        }
      }
      if (rects.length) byPage.set(n, rects);
    }
    setPageRects(byPage);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, matches, renderTick, textRefs, canvasRefs]);

  return { active, query, setQuery, opts, setOpts, matches, current, count: matches.length, pageRects, searching, show, hide, go };
}