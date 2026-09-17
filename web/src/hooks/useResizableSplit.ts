import { useCallback, useEffect, useRef, useState } from "react";

const DEFAULT_RATIO = 0.5;
const MIN_RATIO = 0.2;
const MAX_RATIO = 0.8;

interface Options {
  storageKey?: string;
  defaultRatio?: number;
  minRatio?: number;
  maxRatio?: number;
}

/**
 * Drag-to-resize state for a horizontal two-pane split, expressed as the
 * **ratio** of the container width taken by the first (left) pane. A ratio
 * (rather than a fixed px width) keeps the split proportional when the window,
 * the file-tree pane, or the surrounding chrome resizes.
 *
 * The pointer contract mirrors `useResizableSidebar`: the returned
 * `onPointerDown` is wired to the divider element, which captures the pointer
 * for the duration of the drag and applies `col-resize` to the body so the
 * cursor stays correct outside the 4px handle.
 *
 * The ratio persists to localStorage so it survives a reload and is shared by
 * every markdown tab (the split is a user preference, not per-file state).
 */
export function useResizableSplit(options: Options = {}) {
  const {
    storageKey = "ocode.ui.split_ratio",
    defaultRatio = DEFAULT_RATIO,
    minRatio = MIN_RATIO,
    maxRatio = MAX_RATIO,
  } = options;

  const [ratio, setRatio] = useState<number>(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      if (stored) {
        const parsed = Number(stored);
        if (Number.isFinite(parsed) && parsed >= minRatio && parsed <= maxRatio) {
          return parsed;
        }
      }
    } catch {
      /* ignore */
    }
    return defaultRatio;
  });

  const containerRef = useRef<HTMLDivElement>(null);

  const clamp = useCallback(
    (v: number) => Math.min(maxRatio, Math.max(minRatio, v)),
    [minRatio, maxRatio],
  );

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      const container = containerRef.current;
      if (!container) return;
      e.preventDefault();
      e.stopPropagation();
      // Capture so the drag keeps tracking when the cursor leaves the handle.
      (e.target as HTMLElement).setPointerCapture?.(e.pointerId);

      document.body.style.userSelect = "none";
      document.body.style.cursor = "col-resize";

      const onMove = (ev: PointerEvent) => {
        const rect = container.getBoundingClientRect();
        if (rect.width <= 0) return;
        setRatio(clamp((ev.clientX - rect.left) / rect.width));
      };

      const onUp = () => {
        document.body.style.userSelect = "";
        document.body.style.cursor = "";
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [clamp],
  );

  useEffect(() => {
    try {
      localStorage.setItem(storageKey, String(ratio));
    } catch {
      /* ignore */
    }
  }, [storageKey, ratio]);

  const resetToDefault = useCallback(() => setRatio(defaultRatio), [defaultRatio]);

  return {
    ratio,
    containerRef,
    onPointerDown,
    resetToDefault,
    minRatio,
    maxRatio,
    defaultRatio,
  };
}
