import { useCallback, useEffect, useRef, useState } from "react";

const DEFAULT_RATIO = 0.5;
const MIN_RATIO = 0.2;
const MAX_RATIO = 0.8;
const KEYBOARD_STEP = 0.02;

type Orientation = "horizontal" | "vertical";

interface Options {
  storageKey?: string;
  defaultRatio?: number;
  minRatio?: number;
  maxRatio?: number;
  orientation?: Orientation;
}

/**
 * Drag-to-resize state for a two-pane split, expressed as the **ratio** of the
 * container size taken by the first pane (left for horizontal, top for
 * vertical). A ratio (rather than a fixed px size) keeps the split proportional
 * when the window or container resizes.
 *
 * The pointer contract mirrors `useResizableSidebar`: the returned
 * `onPointerDown` is wired to the divider element, which captures the pointer
 * for the duration of the drag and applies a resize cursor to the body so the
 * cursor stays correct outside the handle.
 *
 * The returned `onKeyDown` handles arrow keys for accessibility (decrease /
 * increase the first pane by `KEYBOARD_STEP`).
 *
 * The ratio persists to localStorage so it survives a reload and is shared by
 * every instance using the same storageKey (the split is a user preference,
 * not per-instance state).
 */
export function useResizableSplit(options: Options = {}) {
  const {
    storageKey = "ocode.ui.split_ratio",
    defaultRatio = DEFAULT_RATIO,
    minRatio = MIN_RATIO,
    maxRatio = MAX_RATIO,
    orientation = "horizontal",
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

  const ratioRef = useRef(ratio);
  ratioRef.current = ratio;

  const containerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{ start: number; startRatio: number } | null>(null);

  const clamp = useCallback((v: number) => Math.min(maxRatio, Math.max(minRatio, v)), [minRatio, maxRatio]);

  const resizeCursor = orientation === "vertical" ? "row-resize" : "col-resize";

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      const container = containerRef.current;
      if (!container) return;
      e.preventDefault();
      e.stopPropagation();
      (e.target as HTMLElement).setPointerCapture?.(e.pointerId);

      const isVertical = orientation === "vertical";
      dragRef.current = {
        start: isVertical ? e.clientY : e.clientX,
        startRatio: ratioRef.current,
      };

      // Prevent text selection during drag
      document.body.style.userSelect = "none";
      document.body.style.cursor = resizeCursor;

      const onMove = (ev: PointerEvent) => {
        const c = containerRef.current;
        if (!c || !dragRef.current) return;
        const r = c.getBoundingClientRect();
        const size = isVertical ? r.height : r.width;
        if (size <= 0) return;
        const delta = (isVertical ? ev.clientY : ev.clientX) - dragRef.current.start;
        setRatio(clamp(dragRef.current.startRatio + delta / size));
      };

      const onUp = () => {
        dragRef.current = null;
        document.body.style.userSelect = "";
        document.body.style.cursor = "";
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [orientation, clamp, resizeCursor],
  );

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      let delta: number | null = null;
      if (orientation === "horizontal") {
        if (e.key === "ArrowLeft") delta = -KEYBOARD_STEP;
        else if (e.key === "ArrowRight") delta = KEYBOARD_STEP;
      } else {
        if (e.key === "ArrowUp") delta = -KEYBOARD_STEP;
        else if (e.key === "ArrowDown") delta = KEYBOARD_STEP;
      }
      if (delta === null) return;
      e.preventDefault();
      setRatio((prev) => clamp(prev + delta));
    },
    [orientation, clamp],
  );

  // Persist on every ratio change (drag and keyboard both flow through setRatio).
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
    onKeyDown,
    resetToDefault,
    minRatio,
    maxRatio,
    defaultRatio,
    orientation,
  };
}
