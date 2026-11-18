import { useEffect, useMemo, useReducer, useRef, useState } from "react";
import type { WorkbookImage } from "./univerData";
import type { ViewportMetrics } from "./univerMount";

export interface SheetImageOverlayProps {
  /** Already filtered to the sheet whose grid is mounted. */
  images: WorkbookImage[];
  /** Live geometry read from the mounted workbook; null before it mounts. */
  viewport: () => ViewportMetrics | null;
  /** Notified by Univer on scroll, zoom and active-sheet changes. */
  subscribe: (cb: () => void) => () => void;
}

/** Box used only when an image's anchor carries no extent (neither br nor ext). Scaled by sheet zoom. */
export const DEFAULT_IMAGE_PX = { w: 180, h: 120 };

/** Sheet-space x of a fractional column position, before zoom and scroll. */
function colPos(m: ViewportMetrics, col: number): number {
  const whole = Math.floor(col);
  return m.colStart(whole) + (col - whole) * m.colWidth(whole);
}

/** Sheet-space y of a fractional row position, before zoom and scroll. */
function rowPos(m: ViewportMetrics, row: number): number {
  const whole = Math.floor(row);
  return m.rowStart(whole) + (row - whole) * m.rowHeight(whole);
}

/** Zoomed box for an image: its two-cell extent, else its one-cell extent, else the default. */
function imageSizePx(image: WorkbookImage, m: ViewportMetrics): { w: number; h: number } {
  // The drawing's EMU extent is the image's true size; the two-cell anchor is
  // only a bounding range and stretches/distorts the picture when used as size.
  if (image.extWidth !== null && image.extHeight !== null) {
    return { w: image.extWidth * m.zoom, h: image.extHeight * m.zoom };
  }
  if (image.toCol !== null && image.toRow !== null) {
    return {
      w: (colPos(m, image.toCol) - colPos(m, image.fromCol)) * m.zoom,
      h: (rowPos(m, image.toRow) - rowPos(m, image.fromRow)) * m.zoom,
    };
  }
  // shortcut: no EMU extent and no cell range -> show a visible 180x120 box;
  // only reached for third-party writers that omit both. Upgrade if such a
  // writer becomes common and the image's native size can be decoded.
  return { w: DEFAULT_IMAGE_PX.w * m.zoom, h: DEFAULT_IMAGE_PX.h * m.zoom };
}

/**
 * Read-only layer that paints floating workbook images at their grid anchors.
 * Sibling of the Univer container: absolute, clipped, and never hit-tested, so
 * the grid keeps every pointer event.
 */
export default function SheetImageOverlay({ images, viewport, subscribe }: SheetImageOverlayProps) {
  const layerRef = useRef<HTMLDivElement | null>(null);
  const frameRef = useRef<number | null>(null);
  const [tick, bump] = useReducer((n: number) => n + 1, 0);
  const [size, setSize] = useState({ w: 0, h: 0 });

  useEffect(() => {
    const node = layerRef.current;
    if (node === null) return;
    // One frame per burst of Univer events, however many fire in the same turn.
    const schedule = () => {
      if (frameRef.current !== null) return;
      frameRef.current = requestAnimationFrame(() => {
        frameRef.current = null;
        bump();
      });
    };
    const measure = () => {
      const w = node.clientWidth;
      const h = node.clientHeight;
      setSize((prev) => (prev.w === w && prev.h === h ? prev : { w, h }));
    };
    const unsubscribe = subscribe(schedule);
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(node);
    return () => {
      unsubscribe();
      observer?.disconnect();
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    };
  }, [subscribe]);

  const boxes = useMemo(() => {
    const m = viewport();
    if (m === null) return null;
    const placed: { key: string; src: string; left: number; top: number; width: number; height: number }[] = [];
    for (let i = 0; i < images.length; i += 1) {
      const image = images[i];
      const x = m.rowHeaderWidth + (colPos(m, image.fromCol) - m.scrollX) * m.zoom;
      const y = m.columnHeaderHeight + (rowPos(m, image.fromRow) - m.scrollY) * m.zoom;
      const { w: width, h: height } = imageSizePx(image, m);
      // Entirely outside the layer: no node, no console noise.
      if (x + width <= 0 || y + height <= 0 || x >= size.w || y >= size.h) continue;
      // Not clamped to the layer: the layer is overflow-hidden, and clamping would squash the image.
      placed.push({
        key: `${image.sheetId}:${image.fromCol}:${image.fromRow}:${i}`,
        src: image.dataUrl,
        left: x,
        top: y,
        width,
        height,
      });
    }
    return placed;
  }, [images, viewport, tick, size.w, size.h]);

  return (
    <div ref={layerRef} className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden="true">
      {boxes === null && (
        <div className="absolute left-2 top-2 rounded bg-destructive px-2 py-1 text-xs text-destructive-foreground">
          Images not shown — grid geometry unavailable
        </div>
      )}
      {(boxes ?? []).map((box) => (
        <img
          key={box.key}
          src={box.src}
          alt=""
          style={{
            position: "absolute",
            left: box.left,
            top: box.top,
            width: box.width,
            height: box.height,
          }}
        />
      ))}
    </div>
  );
}
