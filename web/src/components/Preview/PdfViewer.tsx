import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import * as pdfjsLib from "pdfjs-dist";
import workerSrc from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { api } from "../../api/client";
import { ensureReadableStreamAsyncIterator } from "../../lib/readableStreamAsyncIterator";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";

// WebKit has no ReadableStream async iteration, which pdf.js's getTextContent()
// needs (`for await … of streamTextContent()`). Install it before the first
// document loads; no-op on Chrome/Firefox. See the shim for the full story.
ensureReadableStreamAsyncIterator();

// pdf.js runs page raster + text extraction in a worker; the bundled worker
// URL keeps preview offline-capable (no CDN, same as monaco-setup).
if (typeof window !== "undefined" && pdfjsLib.GlobalWorkerOptions.workerSrc !== workerSrc) {
  pdfjsLib.GlobalWorkerOptions.workerSrc = workerSrc;
}

const PAGE_SCALE = 1.5;
/** Vertical gap between stacked pages. Must match the flex `gap` below. */
const PAGE_GAP = 16;
/** The scroll container's `p-2` padding, in px. */
const CONTAINER_PAD = 8;
/** Pages on each side of the visible one that are kept rasterized. */
const RENDER_AHEAD = 2;
/** Pages beyond this distance from the visible one are unrendered (canvas
 *  backing stores are ~4 MB/page at dpr 2, so an unbounded set is a leak). */
const KEEP_ALIVE = 6;
/** Bound on the background pass that measures every page. Past this the
 *  remaining pages get page 1's geometry until they are rendered. */
const MAX_MEASURE = 200;
const DEFAULT_DIM = { w: 612 * PAGE_SCALE, h: 792 * PAGE_SCALE };

type PageDim = { w: number; h: number };

async function measurePage(doc: pdfjsLib.PDFDocumentProxy, n: number): Promise<PageDim | null> {
  try {
    const pg = await doc.getPage(n);
    const viewport = pg.getViewport({ scale: PAGE_SCALE });
    return { w: viewport.width, h: viewport.height };
  } catch {
    return null; // destroyed document / bad page — keep the placeholder size
  }
}

function isCancelledRender(e: unknown): boolean {
  // pdf.js rejects an in-flight render with RenderingCancelledException when
  // the page is unrendered or the document is torn down. That is expected.
  return !!e && typeof e === "object" && (e as { name?: string }).name === "RenderingCancelledException";
}

/**
 * Continuous multi-page PDF viewer (pdf.js, Mozilla — stable/maintained).
 * Every page is stacked in one scroll container, each rasterized to a canvas
 * with an overlaid selectable text layer, so scrolling reads like a native
 * reader. Pages are drawn lazily for a window around the visible page and
 * released once far out of view; the page currently in view drives the
 * Ask-LLM citation label ("p.3") and the `onPageChange` callback.
 *
 * The pager buttons/keyboard still navigate: they scroll to the target page
 * and report it, so they work even when the host does not feed `page` back
 * (e.g. the standalone Preview tab).
 */
export default function PdfViewer({
  path,
  projectRoot,
  projectHost,
  page,
  onPageChange,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  page: number;
  onPageChange: (page: number) => void;
}) {
  const [total, setTotal] = useState(0);
  /** Per-page geometry (CSS px at PAGE_SCALE), used to lay the stack out
   *  before a page is rasterized so the scrollbar is stable. */
  const [dims, setDims] = useState<PageDim[]>([]);
  const [visiblePage, setVisiblePage] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const docRef = useRef<pdfjsLib.PDFDocumentProxy | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const canvasRefs = useRef(new Map<number, HTMLCanvasElement>());
  const textRefs = useRef(new Map<number, HTMLDivElement>());
  // Stable per-page ref callbacks: an inline `(el) => …` would be a fresh
  // function on every render, and React detaches/reattaches refs whose identity
  // changed — which would blank the ref maps mid-render whenever the host
  // re-renders this viewer (it does, on every reported page change).
  const canvasRefCbs = useRef(new Map<number, (el: HTMLCanvasElement | null) => void>());
  const textRefCbs = useRef(new Map<number, (el: HTMLDivElement | null) => void>());
  const renderedRef = useRef(new Set<number>());
  const renderTasksRef = useRef(new Map<number, pdfjsLib.RenderTask>());
  const totalRef = useRef(0);
  const dimsRef = useRef<PageDim[]>([]);
  const visibleRef = useRef(1);
  const pagePropRef = useRef(page);
  // Set once the first page placement has happened for the current document;
  // before that the viewer must not treat the `page` prop as a scroll request.
  const placedRef = useRef(false);

  const { ref: selContainerRef, sel, clear } = usePreviewSelection<HTMLDivElement>(() => `p.${visibleRef.current}`);

  totalRef.current = total;
  dimsRef.current = dims;
  visibleRef.current = visiblePage;
  pagePropRef.current = page;

  // The scroll container is both the selection anchor and our geometry source.
  const attachScroll = useCallback(
    (el: HTMLDivElement | null) => {
      scrollRef.current = el;
      selContainerRef.current = el;
    },
    [selContainerRef],
  );

  /** Stable ref callback per page so re-renders don't detach live canvases. */
  const canvasRefFor = useCallback((n: number) => {
    let cb = canvasRefCbs.current.get(n);
    if (!cb) {
      cb = (el: HTMLCanvasElement | null) => {
        if (el) canvasRefs.current.set(n, el);
        else canvasRefs.current.delete(n);
      };
      canvasRefCbs.current.set(n, cb);
    }
    return cb;
  }, []);

  /** Stable ref callback per page for the selectable text layer. */
  const textRefFor = useCallback((n: number) => {
    let cb = textRefCbs.current.get(n);
    if (!cb) {
      cb = (el: HTMLDivElement | null) => {
        if (el) textRefs.current.set(n, el);
        else textRefs.current.delete(n);
      };
      textRefCbs.current.set(n, cb);
    }
    return cb;
  }, []);

  /** Top of each page in stack coordinates (page 1 starts at 0). */
  const offsets = useMemo(() => {
    const out: number[] = new Array(dims.length);
    let y = 0;
    for (let i = 0; i < dims.length; i++) {
      out[i] = y;
      y += dims[i].h + PAGE_GAP;
    }
    return out;
  }, [dims]);
  const offsetsRef = useRef(offsets);
  offsetsRef.current = offsets;

  const clampPage = useCallback((n: number) => {
    const max = totalRef.current || 1;
    return Math.min(Math.max(1, Math.floor(n) || 1), max);
  }, []);

  const unrenderPage = useCallback((n: number) => {
    renderedRef.current.delete(n);
    const task = renderTasksRef.current.get(n);
    if (task) {
      try {
        task.cancel();
      } catch {
        /* already settled */
      }
      renderTasksRef.current.delete(n);
    }
    const canvas = canvasRefs.current.get(n);
    if (canvas) {
      canvas.width = 0;
      canvas.height = 0;
    }
    const layer = textRefs.current.get(n);
    if (layer) layer.innerHTML = "";
  }, []);

  const renderPage = useCallback(async (n: number) => {
    const doc = docRef.current;
    if (!doc || renderedRef.current.has(n)) return;
    renderedRef.current.add(n);
    try {
      const pg = await doc.getPage(n);
      if (docRef.current !== doc) {
        renderedRef.current.delete(n);
        return;
      }
      const canvas = canvasRefs.current.get(n);
      if (!canvas) {
        renderedRef.current.delete(n);
        return;
      }
      // Render the raster at the device pixel ratio (cap 3 to bound memory)
      // so retina/hi-DPI screens get a crisp page. Without this the canvas
      // backing store is 1× CSS pixels and the display upscales it by
      // devicePixelRatio — the "blurry PDF" bug on Mac/WKWebView. The backing
      // store is dpr× the CSS size; the text layer keeps the 1:1 viewport so
      // glyph positions stay aligned with the rendered page.
      const dpr = Math.min(Math.max(window.devicePixelRatio || 1, 1), 3);
      const viewport = pg.getViewport({ scale: PAGE_SCALE });
      canvas.width = Math.floor(viewport.width * dpr);
      canvas.height = Math.floor(viewport.height * dpr);
      canvas.style.width = `${viewport.width}px`;
      canvas.style.height = `${viewport.height}px`;
      const known = dimsRef.current[n - 1];
      if (known && (known.w !== viewport.width || known.h !== viewport.height)) {
        const next = dimsRef.current.slice();
        next[n - 1] = { w: viewport.width, h: viewport.height };
        dimsRef.current = next;
        setDims(next);
      }
      const task = pg.render({
        canvas,
        viewport,
        transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : undefined,
      });
      renderTasksRef.current.set(n, task);
      await task.promise;
      renderTasksRef.current.delete(n);
      if (docRef.current !== doc) return;
      const tc = await pg.getTextContent();
      const target = textRefs.current.get(n);
      if (docRef.current !== doc || !target) return;
      target.innerHTML = "";
      target.style.width = `${viewport.width}px`;
      target.style.height = `${viewport.height}px`;
      for (const item of tc.items) {
        if (!("str" in item) || !item.str) continue;
        const tx = pdfjsLib.Util.transform(viewport.transform, item.transform);
        const span = document.createElement("span");
        span.textContent = item.str;
        span.style.cssText = `position:absolute;left:${tx[4]}px;top:${tx[5]}px;font-size:${Math.abs(tx[0]) || 10}px;line-height:1;white-space:pre;`;
        target.appendChild(span);
        target.appendChild(document.createTextNode(" "));
      }
    } catch (e) {
      renderTasksRef.current.delete(n);
      renderedRef.current.delete(n);
      if (isCancelledRender(e)) return;
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  /** Rasterize the window around `cur` and release pages that fell behind. */
  const ensureWindow = useCallback(
    (cur: number) => {
      const max = totalRef.current;
      if (!max) return;
      const start = Math.max(1, cur - RENDER_AHEAD);
      const end = Math.min(max, cur + RENDER_AHEAD);
      for (let i = start; i <= end; i++) void renderPage(i);
      for (const n of Array.from(renderedRef.current)) {
        if (n < cur - KEEP_ALIVE || n > cur + KEEP_ALIVE) unrenderPage(n);
      }
    },
    [renderPage, unrenderPage],
  );

  const scrollToPage = useCallback((n: number) => {
    const el = scrollRef.current;
    const off = offsetsRef.current;
    if (!el || !off.length) return;
    el.scrollTop = Math.max(0, off[n - 1] + CONTAINER_PAD);
  }, []);

  /** Navigate to a page: scroll there, update the label, notify the host. */
  const gotoPage = useCallback(
    (n: number) => {
      const target = clampPage(n);
      scrollToPage(target);
      visibleRef.current = target;
      setVisiblePage(target);
      ensureWindow(target);
      onPageChange(target);
    },
    [clampPage, ensureWindow, onPageChange, scrollToPage],
  );

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || !totalRef.current) return;
    const off = offsetsRef.current;
    // The page whose band contains a probe ~35% down the viewport is "current".
    const probe = el.scrollTop - CONTAINER_PAD + el.clientHeight * 0.35;
    let n = 1;
    for (let i = 0; i < off.length; i++) {
      if (off[i] <= probe) n = i + 1;
      else break;
    }
    if (n !== visibleRef.current) {
      visibleRef.current = n;
      setVisiblePage(n);
      onPageChange(n);
    }
    ensureWindow(n);
  }, [ensureWindow, onPageChange]);

  // Load (or reload) the document. `page` is read through a ref: a page change
  // must not tear down and refetch the PDF.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setTotal(0);
    setDims([]);
    setVisiblePage(clampPage(pagePropRef.current));
    placedRef.current = false;
    renderedRef.current.clear();
    for (const task of renderTasksRef.current.values()) {
      try {
        task.cancel();
      } catch {
        /* already settled */
      }
    }
    renderTasksRef.current.clear();

    (async () => {
      try {
        const buf = await api.fetchFileRaw(path, projectRoot, projectHost);
        const doc = await pdfjsLib.getDocument({ data: buf }).promise;
        if (cancelled) {
          void doc.cleanup();
          return;
        }
        docRef.current = doc;
        const start = clampPage(pagePropRef.current);
        const first = await measurePage(doc, start);
        if (cancelled) return;
        const initial: PageDim[] = new Array(doc.numPages).fill(first ?? DEFAULT_DIM);
        setDims(initial);
        setTotal(doc.numPages);
        setLoading(false);

        // Measure the other pages in the background so the stack geometry is
        // right for mixed-size documents without blocking first paint.
        const limit = Math.min(doc.numPages, MAX_MEASURE);
        const rest = initial.slice();
        let next = 1;
        const worker = async () => {
          for (;;) {
            const n = next++;
            if (n > limit || cancelled) return;
            if (n === start) continue; // already measured
            const dim = await measurePage(doc, n);
            if (dim) rest[n - 1] = dim;
          }
        };
        await Promise.all(Array.from({ length: Math.min(8, limit) }, worker));
        if (!cancelled) setDims(rest.slice());
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      }
    })();

    return () => {
      cancelled = true;
      for (const task of renderTasksRef.current.values()) {
        try {
          task.cancel();
        } catch {
          /* already settled */
        }
      }
      renderTasksRef.current.clear();
      renderedRef.current.clear();
      const doc = docRef.current;
      docRef.current = null;
      if (doc) void doc.cleanup();
    };
    // `page` intentionally omitted — see the comment above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, projectRoot, projectHost]);

  // First placement, once the page geometry exists.
  useEffect(() => {
    if (placedRef.current || !total || dims.length === 0) return;
    placedRef.current = true;
    const target = clampPage(pagePropRef.current);
    scrollToPage(target);
    visibleRef.current = target;
    setVisiblePage(target);
    ensureWindow(target);
  }, [total, dims.length, clampPage, ensureWindow, scrollToPage]);

  // Follow an external page request (pager, citation jump). Keyed on the prop
  // alone: a geometry correction must never yank a reader who scrolled away.
  useEffect(() => {
    if (!placedRef.current || !total) return;
    const target = clampPage(pagePropRef.current);
    if (target === visibleRef.current) return;
    scrollToPage(target);
    visibleRef.current = target;
    setVisiblePage(target);
    ensureWindow(target);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page]);

  const safe = total ? Math.min(Math.max(1, visiblePage), total) : 1;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
        <button type="button" disabled={!total || safe <= 1} onClick={() => gotoPage(safe - 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Previous page">←</button>
        <span className="text-muted-foreground">{total ? `${safe} / ${total}` : "…"}</span>
        <button type="button" disabled={!total || safe >= total} onClick={() => gotoPage(safe + 1)} className="rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40" aria-label="Next page">→</button>
      </div>
      <div
        ref={attachScroll}
        onScroll={onScroll}
        className="relative min-h-0 flex-1 overflow-auto bg-muted/20 p-2"
        aria-label="PDF pages"
        onKeyDown={(e) => {
          if (e.key === "ArrowLeft") gotoPage(safe - 1);
          else if (e.key === "ArrowRight") gotoPage(safe + 1);
        }}
        tabIndex={0}
      >
        {loading && <div className="p-2 text-xs text-muted-foreground">Loading PDF…</div>}
        {error && <div className="p-2 text-xs text-red-400">PDF failed: {error}</div>}
        <div className="relative mx-auto flex w-fit flex-col" style={{ gap: PAGE_GAP }}>
          {dims.map((d, i) => {
            const n = i + 1;
            return (
              <div key={n} className="relative">
                <canvas
                  ref={canvasRefFor(n)}
                  style={{ width: d.w, height: d.h }}
                  className="block rounded border border-border bg-white"
                />
                {/* Selectable text layer over the raster: transparent glyphs, visible selection. */}
                <div
                  ref={textRefFor(n)}
                  className="pdf-text-layer absolute left-0 top-0 overflow-hidden select-text"
                  aria-hidden={false}
                />
              </div>
            );
          })}
        </div>
      </div>
      <style>{`.pdf-text-layer span{color:transparent;}.pdf-text-layer span::selection{background:rgba(59,130,246,.35);}`}</style>
      {sel && <SelectionToolbar sel={sel} path={path} label={`p.${safe}`} projectRoot={projectRoot} projectHost={projectHost} onDone={clear} />}
    </div>
  );
}
