import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import * as pdfjsLib from "pdfjs-dist";
import workerSrc from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { api } from "../../api/client";
import { ensureReadableStreamAsyncIterator } from "../../lib/readableStreamAsyncIterator";
import { SelectionToolbar, usePreviewSelection } from "./SelectionToolbar";
import { usePdfFind } from "./usePdfFind";
import { loadPreviewViewState, savePreviewViewState, previewViewKey } from "../../lib/previewViewState";

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
/** Zoom bounds, as multipliers of the default view (25% – 400%). */
const MIN_ZOOM = 0.25;
const MAX_ZOOM = 4;
/** Multiplicative step for keyboard/button zoom. */
const ZOOM_STEP = 1.25;
/** Wheel zoom sensitivity: ratio = exp(-deltaY / WHEEL_ZOOM_DIVISOR). */
const WHEEL_ZOOM_DIVISOR = 450;

type PageDim = { w: number; h: number };
/** How to re-map the scroll position after a zoom layout change. */
type ZoomRemap = {
  ratio: number;
  /** Content-space point that must stay put (scroll + cursor offset). */
  anchorX: number;
  anchorY: number;
  /** Same point's offset inside the viewport. */
  cursorX: number;
  cursorY: number;
};

async function measurePage(doc: pdfjsLib.PDFDocumentProxy, n: number, rotation: number): Promise<PageDim | null> {
  try {
    const pg = await doc.getPage(n);
    const viewport = pg.getViewport({ scale: PAGE_SCALE, rotation: (((pg.rotate as number) || 0) + rotation) % 360 });
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

function isEditableTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el || !el.tagName) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable;
}

/** One-time probe: can this environment give us a 2D context for blitting?
 *  jsdom (and other no-canvas environments) answer false, so the viewer skips
 *  the copy and just sizes the backing store — enough for tests. */
let blitSupport: boolean | null = null;
function canBlit(): boolean {
  if (blitSupport === null) {
    try {
      // jsdom has no canvas implementation (getContext returns null and logs a
      // "Not implemented" error) — skip the copy there; sizing the backing
      // store is all the tests need.
      blitSupport = typeof navigator !== "undefined" && /jsdom/i.test(navigator.userAgent)
        ? false
        : !!document.createElement("canvas").getContext("2d");
    } catch {
      blitSupport = false;
    }
  }
  return blitSupport;
}

/**
 * Continuous multi-page PDF viewer (pdf.js, Mozilla — stable/maintained).
 * Every page is stacked in one scroll container, each rasterized to a canvas
 * with an overlaid selectable text layer, so scrolling reads like a native
 * reader. Pages are drawn lazily for a window around the visible page and
 * released once far out of view; the page currently in view drives the
 * Ask-LLM citation label ("p.3") and the `onPageChange` callback.
 *
 * Zoom: pages are laid out from zoom-invariant base geometry (`dims` at the
 * default view) times a `zoom` multiplier, so zooming re-renders only the
 * visible window and re-maps the scroll position around an anchor — the
 * mouse cursor for ctrl/⌘+wheel and space+wheel, the current reading
 * position for keyboard/button zoom. Held Space turns the wheel into
 * zoom-at-cursor and left-drag into panning. `usePdfFind` adds full-document
 * search with highlights.
 */
export default function PdfViewer({
  path,
  projectRoot,
  projectHost,
  page,
  onPageChange,
  active = true,
}: {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  page: number;
  onPageChange: (page: number) => void;
  /** False while the owning tab/pane is hidden. Hidden viewers must not answer
   *  global find shortcuts (every visited pane stays mounted across tabs and
   *  projects) and release their rasterized window so a backgrounded PDF does
   *  not pin tens of MB of canvas backing stores indefinitely. */
  active?: boolean;
}) {
  const [total, setTotal] = useState(0);
  /** Per-page geometry in CSS px at the default view (PAGE_SCALE, zoom 1).
   *  Zoom-invariant: the displayed size is base × zoom, so zooming never
   *  needs re-measuring — pdf.js viewports are linear in scale. */
  const [dims, setDims] = useState<PageDim[]>([]);
  /** User zoom multiplier: 1 = 100% (the default view). */
  const [zoom, setZoom] = useState(1);
  /** User rotation offset in degrees (0/90/180/270), added to each page's
   *  inherent /Rotate. */
  const [, setRotation] = useState(0);  // rotation lives in rotationRef; state only forces a re-render
  const [visiblePage, setVisiblePage] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  /** Bumped whenever a page's text spans are (re)built or cleared; drives the
   *  find-highlight overlay re-measure. */
  const [renderTick, setRenderTick] = useState(0);
  /** Bumped when a new document lands; resets the find bar state. */
  const [docVersion, setDocVersion] = useState(0);
  const [spaceHeld, setSpaceHeld] = useState(false);
  const [dragging, setDragging] = useState(false);

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
  /** Generation of the render pipeline. Bumped on zoom/rotate/reload so a
   *  render that started under an older generation never mutates shared state
   *  (it was cancelled; a newer render owns the page). */
  const renderGenRef = useRef(0);
  const zoomRef = useRef(1);
  const rotationRef = useRef(0);
  /** Zoom scroll remap, consumed in a layout effect once the new geometry
   *  has been committed (before paint, so there is no flicker). */
  const pendingRemapRef = useRef<ZoomRemap | null>(null);
  const spaceRef = useRef(false);
  const dragRef = useRef<{ x: number; y: number; sl: number; st: number } | null>(null);
  /** Snapshot of the loaded PDF's bytes as a Blob. pdf.js may transfer the
   *  ArrayBuffer handed to getDocument (detaching it), so print/download work
   *  from an independent copy taken at load. The object URL is minted per
   *  action and revoked immediately after use. */
  const blobRef = useRef<Blob | null>(null);

  const find = usePdfFind({ docRef, textRefs, canvasRefs, renderTick, docVersion });
  const findInputRef = useRef<HTMLInputElement | null>(null);

  const { ref: selContainerRef, sel, clear } = usePreviewSelection<HTMLDivElement>(() => `p.${visibleRef.current}`);

  /** Storage key for this file's viewer state (page/zoom/scroll). A host- or
   *  root-qualified key keeps a remote project's file distinct from a local
   *  one with the same path. Restoring the reading position matters most in the
   *  sidebar, whose host remounts the whole panel (and this viewer) on every
   *  project/session switch. */
  const viewKeyRef = useRef("");
  viewKeyRef.current = previewViewKey(path, projectRoot, projectHost);
  /** Armed once the first placement + restore pass has run for the current
   *  document, so the initial state (pre-restore) never overwrites the stored
   *  position. */
  const restoreDoneRef = useRef(false);
  const saveTimerRef = useRef<number | null>(null);
  /** Last observed scroll offset. Kept in a ref because the scroll container's
   *  ref is detached before passive-effect cleanup on unmount, so the final
   *  flush could not read it otherwise. */
  const lastScrollTopRef = useRef(0);

  totalRef.current = total;
  dimsRef.current = dims;
  visibleRef.current = visiblePage;
  pagePropRef.current = page;
  zoomRef.current = zoom;

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
      y += dims[i].h * zoom + PAGE_GAP;
    }
    return out;
  }, [dims, zoom]);
  const offsetsRef = useRef(offsets);
  offsetsRef.current = offsets;

  const clampPage = useCallback((n: number) => {
    const max = totalRef.current || 1;
    return Math.min(Math.max(1, Math.floor(n) || 1), max);
  }, []);

  /** Persist the reading position (page + zoomed scroll offset). Debounced:
   *  scroll fires at frame rate. No-op until the first-placement restore pass
   *  has run, so pre-restore state never overwrites the stored position. */
  const schedulePersist = useCallback(() => {
    if (!restoreDoneRef.current) return;
    // Capture the file identity NOW. The setTimeout resolves ~250ms later; if
    // the document changed in between, reading viewKeyRef.current inside the
    // callback would persist THIS document's page/scroll under the NEXT file's
    // key (e.g. re-previewing B within 250ms of scrolling A).
    const key = viewKeyRef.current;
    if (saveTimerRef.current !== null) window.clearTimeout(saveTimerRef.current);
    saveTimerRef.current = window.setTimeout(() => {
      saveTimerRef.current = null;
      savePreviewViewState(key, {
        page: visibleRef.current,
        zoom: zoomRef.current,
        scrollTop: lastScrollTopRef.current,
      });
    }, 250);
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
    setRenderTick((t) => t + 1); // spans are gone → drop this page's highlights
  }, []);

  const renderPage = useCallback(async (n: number) => {
    const doc = docRef.current;
    if (!doc || renderedRef.current.has(n)) return;
    const gen = renderGenRef.current;
    renderedRef.current.add(n);
    let task: pdfjsLib.RenderTask | null = null;
    try {
      const pg = await doc.getPage(n);
      if (docRef.current !== doc || gen !== renderGenRef.current) {
        if (gen === renderGenRef.current) renderedRef.current.delete(n);
        return;
      }
      const canvas = canvasRefs.current.get(n);
      if (!canvas) {
        if (gen === renderGenRef.current) renderedRef.current.delete(n);
        return;
      }
      // Render the raster at the device pixel ratio (cap 3 to bound memory)
      // so retina/hi-DPI screens get a crisp page. Without this the canvas
      // backing store is 1× CSS pixels and the display upscales it by
      // devicePixelRatio — the "blurry PDF" bug on Mac/WKWebView. The backing
      // store is dpr× the CSS size; the text layer keeps the 1:1 viewport so
      // glyph positions stay aligned with the rendered page.
      const dpr = Math.min(Math.max(window.devicePixelRatio || 1, 1), 3);
      const zoomNow = zoomRef.current;
      const viewport = pg.getViewport({
        scale: PAGE_SCALE * zoomNow,
        rotation: (((pg.rotate as number) || 0) + rotationRef.current) % 360,
      });
      const known = dimsRef.current[n - 1];
      if (known && (Math.abs(known.w - viewport.width / zoomNow) > 0.5 || Math.abs(known.h - viewport.height / zoomNow) > 0.5)) {
        const next = dimsRef.current.slice();
        next[n - 1] = { w: viewport.width / zoomNow, h: viewport.height / zoomNow };
        dimsRef.current = next;
        setDims(next);
      }
      // Draw into an offscreen canvas first, then blit: sizing the live canvas
      // (canvas.width = …) clears its bitmap, which would blank the page for
      // the whole async render when zooming/rotating. The old bitmap stays
      // on-screen (CSS-stretched) until the new raster lands.
      const off = document.createElement("canvas");
      off.width = Math.floor(viewport.width * dpr);
      off.height = Math.floor(viewport.height * dpr);
      task = pg.render({
        canvas: off,
        viewport,
        transform: dpr !== 1 ? [dpr, 0, 0, dpr, 0, 0] : undefined,
      });
      renderTasksRef.current.set(n, task);
      await task.promise;
      if (renderTasksRef.current.get(n) === task) renderTasksRef.current.delete(n);
      if (docRef.current !== doc || gen !== renderGenRef.current) return;
      canvas.width = off.width;
      canvas.height = off.height;
      if (canBlit()) {
        const ctx = canvas.getContext("2d");
        if (ctx) ctx.drawImage(off, 0, 0);
      }
      const tc = await pg.getTextContent();
      const target = textRefs.current.get(n);
      if (docRef.current !== doc || gen !== renderGenRef.current || !target) return;
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
      setRenderTick((t) => t + 1); // spans changed → re-measure find highlights
    } catch (e) {
      if (task && renderTasksRef.current.get(n) === task) renderTasksRef.current.delete(n);
      if (gen === renderGenRef.current) renderedRef.current.delete(n);
      if (isCancelledRender(e)) return;
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  /** Rasterize the window around `cur` and release pages that fell behind.
   *  No-op while the pane is hidden: the release effect below empties the
   *  window, and re-rendering it on a scroll/layout signal from a display:none
   *  pane would just re-pin the memory we are trying to free. */
  const ensureWindow = useCallback(
    (cur: number) => {
      if (!active) return;
      const max = totalRef.current;
      if (!max) return;
      const start = Math.max(1, cur - RENDER_AHEAD);
      const end = Math.min(max, cur + RENDER_AHEAD);
      for (let i = start; i <= end; i++) void renderPage(i);
      for (const n of Array.from(renderedRef.current)) {
        if (n < cur - KEEP_ALIVE || n > cur + KEEP_ALIVE) unrenderPage(n);
      }
    },
    [active, renderPage, unrenderPage],
  );

  // Release the rasterized window while this pane is hidden. display:none does
  // not free canvas backing stores, and every visited editor tab stays mounted
  // across tabs and projects, so without this a backgrounded PDF would pin
  // roughly `RENDER_AHEAD*2+1` × (dpr² × page area) of memory for the life of
  // the app. Only the heavy per-page rasters are dropped; the cheap state
  // (page/zoom/scroll, and the parsed document) is kept, and the window is
  // rebuilt on show. The pdf.js document itself is intentionally retained —
  // re-parsing it on every tab switch would make a switch back slow; that
  // residual cost is accepted (the dominant cost is the page canvases).
  useEffect(() => {
    if (active) {
      ensureWindow(visibleRef.current);
      return;
    }
    for (const n of Array.from(renderedRef.current)) unrenderPage(n);
  }, [active, ensureWindow, unrenderPage]);

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
      lastScrollTopRef.current = scrollRef.current?.scrollTop ?? lastScrollTopRef.current;
      visibleRef.current = target;
      setVisiblePage(target);
      ensureWindow(target);
      onPageChange(target);
      schedulePersist();
    },
    [clampPage, ensureWindow, onPageChange, schedulePersist, scrollToPage],
  );

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || !totalRef.current) return;
    const off = offsetsRef.current;
    // Track the raw offset for the unmount flush (refs are detached first).
    lastScrollTopRef.current = el.scrollTop;
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
    schedulePersist();
  }, [ensureWindow, onPageChange, schedulePersist]);

  /** Reading-position anchor for keyboard/button zoom: keep the viewport's
   *  top-left content point fixed. */
  const preserveAnchor = useCallback((): Omit<ZoomRemap, "ratio"> => {
    const el = scrollRef.current;
    return el
      ? { anchorX: el.scrollLeft, anchorY: el.scrollTop, cursorX: 0, cursorY: 0 }
      : { anchorX: 0, anchorY: 0, cursorX: 0, cursorY: 0 };
  }, []);

  /** Apply a zoom factor, re-queue the window at the new scale, and schedule
   *  a scroll re-map around `remap` once the new geometry commits. Old
   *  bitmaps stay on-screen (CSS-stretched) until each page re-renders. */
  const applyZoom = useCallback(
    (factor: number, remap: Omit<ZoomRemap, "ratio">) => {
      const old = zoomRef.current;
      const next = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, old * factor));
      if (next === old) return;
      zoomRef.current = next;
      pendingRemapRef.current = { ratio: next / old, ...remap };
      renderGenRef.current++;
      for (const t of renderTasksRef.current.values()) {
        try {
          t.cancel();
        } catch {
          /* already settled */
        }
      }
      renderTasksRef.current.clear();
      renderedRef.current.clear();
      setZoom(next);
      ensureWindow(visibleRef.current);
      schedulePersist();
    },
    [ensureWindow, schedulePersist],
  );

  /** Zoom with the point under the cursor kept fixed (ctrl/⌘+wheel, space+wheel). */
  const zoomAtCursor = useCallback(
    (factor: number, e: { clientX: number; clientY: number }) => {
      const el = scrollRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const cx = e.clientX - rect.left;
      const cy = e.clientY - rect.top;
      applyZoom(factor, { anchorX: el.scrollLeft + cx, anchorY: el.scrollTop + cy, cursorX: cx, cursorY: cy });
    },
    [applyZoom],
  );

  const resetZoom = useCallback(() => {
    applyZoom(1 / zoomRef.current, preserveAnchor());
  }, [applyZoom, preserveAnchor]);

  const fitWidth = useCallback(() => {
    const el = scrollRef.current;
    const d = dimsRef.current[visibleRef.current - 1];
    if (!el || !d || !d.w) return;
    const target = Math.max(0, el.clientWidth - CONTAINER_PAD * 2 - 2) / d.w;
    applyZoom(target / zoomRef.current, preserveAnchor());
  }, [applyZoom, preserveAnchor]);

  const fitPage = useCallback(() => {
    const el = scrollRef.current;
    const d = dimsRef.current[visibleRef.current - 1];
    if (!el || !d || !d.w || !d.h) return;
    const targetW = Math.max(0, el.clientWidth - CONTAINER_PAD * 2 - 2) / d.w;
    const targetH = Math.max(0, el.clientHeight - CONTAINER_PAD * 2 - PAGE_GAP) / d.h;
    applyZoom(Math.min(targetW, targetH) / zoomRef.current, preserveAnchor());
  }, [applyZoom, preserveAnchor]);

  /** Re-measure every page's base geometry (used after rotation changes). */
  const remeasureAll = useCallback(async () => {
    const doc = docRef.current;
    if (!doc) return;
    const limit = Math.min(doc.numPages, MAX_MEASURE);
    for (let n = 1; n <= limit; n++) {
      const dim = await measurePage(doc, n, rotationRef.current);
      if (!dim) continue;
      setDims((prev) => {
        const cur = prev[n - 1];
        if (cur && Math.abs(cur.w - dim.w) < 0.01 && Math.abs(cur.h - dim.h) < 0.01) return prev;
        const next = prev.slice();
        next[n - 1] = dim;
        return next;
      });
    }
  }, []);

  /** File name for print/download: the basename of the previewed path. */
  const pdfFileName = useMemo(() => {
    const clean = path.split(/[\\/]/).pop() || "document.pdf";
    return /\.pdf$/i.test(clean) ? clean : `${clean}.pdf`;
  }, [path]);

  /** Download the original PDF bytes under their file name. */
  const downloadPdf = useCallback(() => {
    const blob = blobRef.current;
    if (!blob) return;
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = pdfFileName;
    document.body.appendChild(a);
    a.click();
    a.remove();
    // Safari/WKWebView keep navigating to about:blank if the URL is revoked
    // synchronously; give the download a beat to start.
    window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }, [pdfFileName]);

  /** Print via a hidden iframe hosting the PDF blob: the browser's own PDF
   *  engine renders it and its print dialog opens. Fallback chain: iframe →
   *  popup tab → forced download (popup blocked). */
  const printPdf = useCallback(() => {
    const blob = blobRef.current;
    if (!blob) return;
    const url = URL.createObjectURL(blob);
    const frame = document.createElement("iframe");
    frame.style.position = "fixed";
    frame.style.right = "0";
    frame.style.bottom = "0";
    frame.style.width = "1px";
    frame.style.height = "1px";
    frame.style.opacity = "0";
    frame.style.border = "0";
    frame.src = url;
    frame.setAttribute("aria-hidden", "true");
    frame.tabIndex = -1;
    frame.onload = () => {
      try {
        frame.contentWindow?.focus();
        frame.contentWindow?.print();
      } catch {
        // Cross-origin/sandboxed frame refused print() — open a tab instead.
        window.open(url, "_blank");
      }
      window.setTimeout(() => {
        frame.remove();
        URL.revokeObjectURL(url);
      }, 60_000); // keep the frame alive while the dialog is open
    };
    document.body.appendChild(frame);
    // WKWebView sometimes never fires onload for blob: frames; if the print
    // dialog did not take over within a second, surface the PDF in a tab.
    window.setTimeout(() => {
      if (frame.isConnected) {
        frame.remove();
        const win = window.open(url, "_blank");
        if (!win) downloadPdf(); // popups blocked → last resort: download
        else window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
      }
    }, 1000);
  }, [downloadPdf]);

  const rotate = useCallback(() => {
    const doc = docRef.current;
    if (!doc || !totalRef.current) return;
    rotationRef.current = (rotationRef.current + 90) % 360;
    setRotation(rotationRef.current);
    renderGenRef.current++;
    for (const t of renderTasksRef.current.values()) {
      try {
        t.cancel();
      } catch {
        /* already settled */
      }
    }
    renderTasksRef.current.clear();
    for (const n of Array.from(renderedRef.current)) unrenderPage(n);
    void (async () => {
      await remeasureAll();
      scrollToPage(visibleRef.current);
      ensureWindow(visibleRef.current);
    })();
  }, [ensureWindow, remeasureAll, scrollToPage, unrenderPage]);

  /** Consume the zoom scroll remap after the new geometry is committed but
   *  before paint — no visible jump. */
  useLayoutEffect(() => {
    const p = pendingRemapRef.current;
    if (!p) return;
    pendingRemapRef.current = null;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollLeft = Math.max(0, p.anchorX * p.ratio - p.cursorX);
    el.scrollTop = Math.max(0, p.anchorY * p.ratio - p.cursorY);
  });

  // Keep the current find match ~⅓ down the viewport; when its rects are not
  // measured yet (page not rendered), just navigate to the match's page. The
  // page label + host callback are updated here too — relying on the ensuing
  // scroll event would make match navigation racy (jsdom never fires it, and
  // programmatic scrolls coalesce).
  const findMatch = find.count ? find.matches[find.current] : undefined;
  useEffect(() => {
    if (!find.active || !findMatch) return;
    const el = scrollRef.current;
    if (!el) return;
    const r = find.pageRects.get(findMatch.page)?.find((x) => x.matchIdx === find.current);
    if (r) {
      el.scrollTop = Math.max(0, offsetsRef.current[findMatch.page - 1] + r.top - el.clientHeight / 3);
    } else if (visibleRef.current !== findMatch.page) {
      scrollToPage(findMatch.page);
    }
    if (visibleRef.current !== findMatch.page) {
      visibleRef.current = findMatch.page;
      setVisiblePage(findMatch.page);
      onPageChange(findMatch.page);
    }
    ensureWindow(findMatch.page);
  }, [find.active, find.current, find.pageRects, findMatch, scrollToPage, ensureWindow, onPageChange]);

  // ctrl/⌘+wheel (and trackpad pinch, which arrives as ctrl+wheel) zoom at
  // the cursor; holding Space turns the plain wheel into zoom-at-cursor too.
  // Non-passive so preventDefault can stop the browser page zoom.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey && !spaceRef.current) return;
      e.preventDefault();
      const dy = e.deltaMode === 1 ? e.deltaY * 16 : e.deltaY;
      const factor = Math.exp(-dy / WHEEL_ZOOM_DIVISOR);
      if (Math.abs(factor - 1) < 0.004) return;
      zoomAtCursor(factor, e);
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [zoomAtCursor]);

  // Space held anywhere (even if the keyup lands outside the viewer) must not
  // leave the modifier stuck on.
  useEffect(() => {
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.key === " " || e.code === "Space") {
        spaceRef.current = false;
        setSpaceHeld(false);
      }
    };
    const onBlur = () => {
      spaceRef.current = false;
      setSpaceHeld(false);
      dragRef.current = null;
      setDragging(false);
    };
    window.addEventListener("keyup", onKeyUp);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  // Find shortcuts work even when the viewer itself doesn't have focus.
  // Ctrl/⌘F opens the bar (browser find stays default inside editable
  // targets), F3/Shift+F3 walk matches, Escape closes.
  useEffect(() => {
    // Only the visible viewer may claim these window-level shortcuts. With
    // every visited pane mounted (hidden) across tabs/projects, an ungated
    // listener would flip find.active on all of them for one Ctrl+F and make N
    // viewers race to handle F3.
    if (!active) return;
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (mod && !e.altKey && (e.key === "f" || e.key === "F") && !isEditableTarget(e.target)) {
        e.preventDefault();
        find.show();
      } else if (e.key === "F3" && find.active) {
        e.preventDefault();
        find.go(e.shiftKey ? -1 : 1);
      } else if (e.key === "Escape" && find.active && !isEditableTarget(e.target)) {
        find.hide();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [active, find.show, find.go, find.hide, find.active]);

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
    // A debounced save may still be in flight from the PREVIOUS document; drop
    // it (its key is captured, but it would still write stale scroll state) and
    // reset the scroll baseline so it cannot leak into this file's entry.
    if (saveTimerRef.current !== null) {
      window.clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
    lastScrollTopRef.current = 0;
    renderedRef.current.clear();
    for (const task of renderTasksRef.current.values()) {
      try {
        task.cancel();
      } catch {
        /* already settled */
      }
    }
    renderTasksRef.current.clear();
    renderGenRef.current++;
    // Restore this file's last zoom so a remounted viewer (sidebar project
    // switch, tab revisit) does not silently drop back to 100%. Scroll is
    // restored in the first-placement effect, once the geometry exists.
    const stored = loadPreviewViewState(viewKeyRef.current);
    const restoredZoom = stored?.zoom && stored.zoom > 0 ? Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, stored.zoom)) : 1;
    zoomRef.current = restoredZoom;
    setZoom(restoredZoom);
    rotationRef.current = 0;
    setRotation(0);
    pendingRemapRef.current = null;
    restoreDoneRef.current = false;
    setDocVersion((v) => v + 1); // resets the find state

    (async () => {
      try {
        const buf = await api.fetchFileRaw(path, projectRoot, projectHost);
        // pdf.js may transfer the buffer handed to it; keep an independent
        // Blob copy for print/download before that can happen.
        blobRef.current = new Blob([buf.slice(0)], { type: "application/pdf" });
        const doc = await pdfjsLib.getDocument({ data: buf }).promise;
        if (cancelled) {
          void doc.cleanup();
          return;
        }
        docRef.current = doc;
        const start = clampPage(pagePropRef.current);
        const first = await measurePage(doc, start, rotationRef.current);
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
            const dim = await measurePage(doc, n, rotationRef.current);
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
      blobRef.current = null;
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

  // First placement, once the page geometry exists. Also restores the stored
  // scroll offset so a remounted viewer resumes mid-page, not just at the
  // page's top — then arms persistence (before this, the initial state would
  // overwrite the stored position with pre-restore values).
  useEffect(() => {
    if (placedRef.current || !total || dims.length === 0) return;
    placedRef.current = true;
    const target = clampPage(pagePropRef.current);
    scrollToPage(target);
    const stored = loadPreviewViewState(viewKeyRef.current);
    const el = scrollRef.current;
    if (el && stored?.scrollTop && stored.scrollTop > 0) {
      // Only trust a stored offset that is consistent with the page we landed
      // on; a zoom change or a re-measure would otherwise put us on the wrong
      // page. `scrollTop` is stored in the current (zoomed) content space.
      const off = offsetsRef.current;
      const pageTop = off[target - 1] + CONTAINER_PAD;
      const pageBottom = (off[target] ?? Number.POSITIVE_INFINITY) - PAGE_GAP;
      if (stored.scrollTop >= pageTop && stored.scrollTop < pageBottom) {
        el.scrollTop = stored.scrollTop;
      }
    }
    lastScrollTopRef.current = el?.scrollTop ?? 0;
    visibleRef.current = target;
    setVisiblePage(target);
    ensureWindow(target);
    restoreDoneRef.current = true;
  }, [total, dims.length, clampPage, ensureWindow, scrollToPage]);

  // Flush any pending save on unmount so a project switch (which unmounts the
  // sidebar's viewer) still records where the reader was. Uses the ref-tracked
  // offset, not the scroll element: React detaches refs before passive-effect
  // cleanup runs, so `scrollRef.current` is already null here.
  useEffect(() => {
    return () => {
      if (saveTimerRef.current !== null) {
        window.clearTimeout(saveTimerRef.current);
        saveTimerRef.current = null;
        savePreviewViewState(viewKeyRef.current, {
          page: visibleRef.current,
          zoom: zoomRef.current,
          scrollTop: lastScrollTopRef.current,
        });
      }
    };
  }, []);





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
  const idleBtn = "rounded px-1.5 py-0.5 hover:bg-muted disabled:opacity-40";
  const toggleOn = "rounded px-1.5 py-0.5 bg-muted text-foreground";
  const toggleOff = "rounded px-1.5 py-0.5 text-muted-foreground hover:bg-muted";
  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
        <button type="button" disabled={!total || safe <= 1} onClick={() => gotoPage(safe - 1)} className={idleBtn} aria-label="Previous page">←</button>
        <span className="text-muted-foreground">{total ? `${safe} / ${total}` : "…"}</span>
        <button type="button" disabled={!total || safe >= total} onClick={() => gotoPage(safe + 1)} className={idleBtn} aria-label="Next page">→</button>
        <span className="mx-1 h-4 w-px bg-border" aria-hidden="true" />
        <button type="button" disabled={!total} onClick={() => applyZoom(1 / ZOOM_STEP, preserveAnchor())} className={idleBtn} aria-label="Zoom out" title="Zoom out (Ctrl/⌘ −)">−</button>
        <button type="button" disabled={!total} onClick={resetZoom} className={`${idleBtn} min-w-10 text-center`} aria-label="Reset zoom" title="Reset zoom (Ctrl/⌘ 0)">
          {Math.round(zoom * 100)}%
        </button>
        <button type="button" disabled={!total} onClick={() => applyZoom(ZOOM_STEP, preserveAnchor())} className={idleBtn} aria-label="Zoom in" title="Zoom in (Ctrl/⌘ +)">+</button>
        <button type="button" disabled={!total} onClick={fitWidth} className={idleBtn} aria-label="Fit width" title="Fit page width">Fit</button>
        <button type="button" disabled={!total} onClick={fitPage} className={idleBtn} aria-label="Fit page" title="Fit whole page">Page</button>
        <button type="button" disabled={!total} onClick={rotate} className={idleBtn} aria-label="Rotate clockwise" title="Rotate 90° clockwise">⟳</button>
        <span className="flex-1" />
        <button type="button" disabled={!total} onClick={printPdf} className={idleBtn} aria-label="Print" title="Print (browser print dialog)">Print</button>
        <button type="button" disabled={!total} onClick={downloadPdf} className={idleBtn} aria-label="Download" title={`Download ${pdfFileName}`}>Download</button>
        <button type="button" disabled={!total} onClick={find.show} className={idleBtn} aria-label="Find in document" title="Find (Ctrl/⌘ F)">Find</button>
      </div>
      {find.active && (
        <div role="search" className="flex shrink-0 items-center gap-1.5 border-b border-border px-2 py-1 text-xs">
          <input
            ref={findInputRef}
            autoFocus
            value={find.query}
            onChange={(e) => find.setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                find.go(e.shiftKey ? -1 : 1);
              } else if (e.key === "Escape") {
                e.preventDefault();
                find.hide();
              }
            }}
            placeholder="Find in document"
            aria-label="Find text"
            className="h-6 w-44 rounded border border-border bg-transparent px-1.5 outline-none focus:ring-1 focus:ring-ring"
          />
          <span aria-live="polite" className="min-w-12 text-center text-muted-foreground">
            {find.searching ? "…" : `${find.count ? find.current + 1 : 0}/${find.count}`}
          </span>
          <button type="button" disabled={!find.count} onClick={() => find.go(-1)} className={idleBtn} aria-label="Previous match" title="Previous match (Shift+F3)">↑</button>
          <button type="button" disabled={!find.count} onClick={() => find.go(1)} className={idleBtn} aria-label="Next match" title="Next match (Enter, F3)">↓</button>
          <button
            type="button"
            aria-label="Match case"
            title="Match case"
            aria-pressed={find.opts.caseSensitive}
            className={find.opts.caseSensitive ? toggleOn : toggleOff}
            onClick={() => find.setOpts((o) => ({ ...o, caseSensitive: !o.caseSensitive }))}
          >
            Aa
          </button>
          <button
            type="button"
            aria-label="Whole word"
            title="Whole word"
            aria-pressed={find.opts.wholeWord}
            className={find.opts.wholeWord ? toggleOn : toggleOff}
            onClick={() => find.setOpts((o) => ({ ...o, wholeWord: !o.wholeWord }))}
          >
            ab
          </button>
          <button type="button" onClick={find.hide} className={idleBtn} aria-label="Close find" title="Close (Esc)">✕</button>
        </div>
      )}
      <div
        ref={attachScroll}
        onScroll={onScroll}
        className={`relative min-h-0 flex-1 overflow-auto bg-muted/20 p-2 ${spaceHeld && !dragging ? "cursor-grab select-none" : ""} ${dragging ? "cursor-grabbing select-none" : ""}`}
        aria-label="PDF pages"
        onKeyDown={(e) => {
          const mod = e.ctrlKey || e.metaKey;
          if (e.key === "+" || e.key === "=") {
            e.preventDefault();
            applyZoom(ZOOM_STEP, preserveAnchor());
          } else if (e.key === "-") {
            e.preventDefault();
            applyZoom(1 / ZOOM_STEP, preserveAnchor());
          } else if (mod && e.key === "0") {
            e.preventDefault();
            resetZoom();
          } else if (e.key === "Home") {
            e.preventDefault();
            gotoPage(1);
          } else if (e.key === "End") {
            e.preventDefault();
            gotoPage(totalRef.current || safe);
          } else if (e.key === "ArrowLeft") {
            gotoPage(safe - 1);
          } else if (e.key === "ArrowRight") {
            gotoPage(safe + 1);
          } else if (e.key === "Escape" && find.active) {
            e.preventDefault();
            find.hide();
          } else if ((e.key === " " || e.code === "Space") && !mod && !e.altKey && !isEditableTarget(e.target)) {
            // Space is a modifier here (zoom/pan); eat the default page-scroll.
            e.preventDefault();
            if (!e.repeat) {
              spaceRef.current = true;
              setSpaceHeld(true);
            }
          }
        }}
        onKeyUp={(e) => {
          if (e.key === " " || e.code === "Space") {
            spaceRef.current = false;
            setSpaceHeld(false);
          }
        }}
        onPointerDown={(e) => {
          if (!spaceRef.current || e.button !== 0 || e.ctrlKey || e.metaKey || e.altKey) return;
          const el = scrollRef.current;
          if (!el) return;
          dragRef.current = { x: e.clientX, y: e.clientY, sl: el.scrollLeft, st: el.scrollTop };
          try {
            el.setPointerCapture?.(e.pointerId);
          } catch {
            /* no pointer capture (jsdom) — events still bubble to the handler */
          }
          setDragging(true);
          e.preventDefault();
        }}
        onPointerMove={(e) => {
          const d = dragRef.current;
          const el = scrollRef.current;
          if (!d || !el) return;
          el.scrollLeft = d.sl - (e.clientX - d.x);
          el.scrollTop = d.st - (e.clientY - d.y);
        }}
        onPointerUp={() => {
          dragRef.current = null;
          setDragging(false);
        }}
        onPointerCancel={() => {
          dragRef.current = null;
          setDragging(false);
        }}
        tabIndex={0}
      >
        {loading && <div className="p-2 text-xs text-muted-foreground">Loading PDF…</div>}
        {error && <div className="p-2 text-xs text-red-400">PDF failed: {error}</div>}
        <div className="relative mx-auto flex w-fit flex-col" style={{ gap: PAGE_GAP }}>
          {dims.map((d, i) => {
            const n = i + 1;
            const rects = find.active ? find.pageRects.get(n) : undefined;
            return (
              <div key={n} className="relative">
                <canvas
                  ref={canvasRefFor(n)}
                  style={{ width: d.w * zoom, height: d.h * zoom }}
                  className="block rounded border border-border bg-white"
                />
                {/* Find highlights: page-relative rects measured from the text
                    layer; the current match is orange, the rest yellow. */}
                {rects?.map((r, ri) => (
                  <div
                    key={ri}
                    aria-hidden="true"
                    className={`pointer-events-none absolute rounded-sm ${r.matchIdx === find.current ? "bg-orange-500/60" : "bg-yellow-300/45"}`}
                    style={{ left: r.left, top: r.top, width: r.width, height: r.height }}
                  />
                ))}
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
