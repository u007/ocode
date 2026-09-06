import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { computeRowBuckets, partitionVisible, type PartitionResult } from "./multirowLayout";

export interface UseWrappedOverflowArgs {
  /** Ordered tab keys (persisted order). */
  orderedKeys: string[];
  /** The tab that must stay visible; `null` when nothing is active. */
  activeKey: string | null;
  /** Non-null while a dnd drag is in flight (freezes the split). */
  activeDragId: string | null;
  /** String encoding geometry-affecting pill content (titles, badges). */
  contentSignature: string;
  /** Ref to the flex-wrap container (the pill area). */
  containerRef: React.RefObject<HTMLElement | null>;
  maxRows?: number;
  chipReservedPx?: number;
}

export interface UseWrappedOverflowResult {
  /** True → render ALL pills (plus a chip probe) in a sensors-disabled frame. */
  probe: boolean;
  visibleKeys: string[];
  hiddenKeys: string[];
  uncapped: boolean;
  /** Attach to each pill's `ref` so the hook can measure it in the probe frame. */
  registerPillRef: (key: string) => (el: HTMLElement | null) => void;
}

/**
 * Measures a wrapping tab area and decides the visible/hidden split.
 *
 * Probe/final duality: when the split is unset or dirty the hook reports
 * `probe: true`; the caller renders every pill (with `pointer-events-none`,
 * sensors disabled) inside the real flex container. A synchronous
 * `useLayoutEffect` reads `offsetTop` (row buckets) and `offsetWidth`, runs
 * `partitionVisible`, and flushes the final (visible-only + real chip) frame
 * before the browser paints — so the probe frame is never visible.
 *
 * Re-measures on: `orderedKeys` identity, `activeKey`, `contentSignature`,
 * container width (`ResizeObserver` + `window.resize` fallback). The split is
 * FROZEN while `activeDragId != null` (a drop can change wrapping, so the
 * recalc lands on drag end).
 */
export function useWrappedOverflow(args: UseWrappedOverflowArgs): UseWrappedOverflowResult {
  const { orderedKeys, activeKey, activeDragId, contentSignature, containerRef, maxRows = 2, chipReservedPx = 40 } = args;

  const [partition, setPartition] = useState<PartitionResult | null>(null);
  const pillEls = useRef(new Map<string, HTMLElement>());
  const [resizeBump, setResizeBump] = useState(0);

  const prev = useRef({
    keySig: "",
    activeKey: "",
    contentSignature: "",
    bump: 0,
    dragStarted: false,
  });

  const keySig = orderedKeys.join("\u0000");

  const registerPillRef = useCallback((key: string) => (el: HTMLElement | null) => {
    if (el) pillEls.current.set(key, el);
    else pillEls.current.delete(key);
  }, []);

  // Dirty if the split's geometric inputs changed since the last measurement.
  const p = prev.current;
  const inputsChanged =
    keySig !== p.keySig ||
    (activeKey ?? "") !== p.activeKey ||
    contentSignature !== p.contentSignature ||
    resizeBump !== p.bump;
  const dragJustEnded = activeDragId === null && p.dragStarted;
  const frozen = activeDragId != null;
  const probe = !frozen && (partition === null || inputsChanged || dragJustEnded);

  useLayoutEffect(() => {
    const isDragging = activeDragId != null;
    prev.current.dragStarted = isDragging;
    if (isDragging) return; // frozen: keep the split until the drag settles.

    if (!probe) {
      // Not dirty — but keep prev.dragStarted aligned.
      return;
    }

    // ResizeObserver setup only when we actually probe. Re-register each time
    // the container element identity changes.
    const container = containerRef.current;
    if (!container) return;

    const tops: number[] = [];
    const widths: number[] = [];
    const keysMeasured: string[] = [];
    for (const key of orderedKeys) {
      const el = pillEls.current.get(key);
      if (!el) continue;
      keysMeasured.push(key);
      tops.push(el.offsetTop);
      widths.push(el.offsetWidth);
    }
    if (keysMeasured.length === 0) return;

    const buckets = computeRowBuckets(tops);
    const result = partitionVisible({
      keys: keysMeasured,
      buckets,
      widths,
      containerWidth: container.clientWidth,
      chipReservedPx,
      maxRows,
      activeKey,
    });

    setPartition(result);
    prev.current.keySig = keySig;
    prev.current.activeKey = activeKey ?? "";
    prev.current.contentSignature = contentSignature;
    prev.current.bump = resizeBump;
  }, [probe, orderedKeys, activeKey, activeDragId, containerRef, chipReservedPx, maxRows, contentSignature, keySig, resizeBump]);

  // Re-measure on container width changes.
  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => {
      setResizeBump((n) => n + 1);
    });
    ro.observe(container);
    return () => ro.disconnect();
  }, [containerRef]);

  // Fallback for environments without ResizeObserver.
  useLayoutEffect(() => {
    if (typeof ResizeObserver !== "undefined") return;
    const onResize = () => setResizeBump((n) => n + 1);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const result = partition ?? { visibleKeys: orderedKeys, hiddenKeys: [], uncapped: false };
  return {
    probe,
    visibleKeys: result.visibleKeys,
    hiddenKeys: result.hiddenKeys,
    uncapped: result.uncapped,
    registerPillRef,
  };
}
