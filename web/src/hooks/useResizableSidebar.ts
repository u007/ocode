import { useState, useCallback, useRef, useEffect } from "react";

const STORAGE_KEY = "ocode.ui.sidebar_width";
const DEFAULT_WIDTH = 240; // w-60 equivalent
const MIN_WIDTH = 160;
const MAX_WIDTH = 500;

/**
 * Hook that manages a resizable sidebar width with drag-to-resize and
 * localStorage persistence. Returns the current width, a ref for the
 * drag handle element, and handlers wired to pointer events.
 */
export function useResizableSidebar() {
  const [width, setWidth] = useState<number>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) {
        const parsed = Number(stored);
        if (Number.isFinite(parsed) && parsed >= MIN_WIDTH && parsed <= MAX_WIDTH) {
          return parsed;
        }
      }
    } catch { /* ignore */ }
    return DEFAULT_WIDTH;
  });

  const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
  const handleRef = useRef<HTMLDivElement>(null);

  const clamp = useCallback((v: number) => Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, v)), []);

  const setWidthClamped = useCallback((w: number) => {
    setWidth(clamp(w));
  }, [clamp]);

  const onPointerDown = useCallback(
    (e: React.PointerEvent) => {
      e.preventDefault();
      e.stopPropagation();

      // Capture the pointer so we get events even if the cursor leaves the handle
      (e.target as HTMLElement).setPointerCapture(e.pointerId);

      dragRef.current = { startX: e.clientX, startWidth: width };

      // Prevent text selection during drag
      document.body.style.userSelect = "none";
      document.body.style.cursor = "col-resize";

      const onMove = (ev: PointerEvent) => {
        if (!dragRef.current) return;
        const delta = ev.clientX - dragRef.current.startX;
        const newWidth = clamp(dragRef.current.startWidth + delta);
        setWidth(newWidth);
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
    [width, clamp],
  );

  // Persist on width change
  useEffect(() => {
    try {
      localStorage.setItem(STORAGE_KEY, String(width));
    } catch { /* ignore */ }
  }, [width]);

  const resetToDefault = useCallback(() => {
    setWidth(DEFAULT_WIDTH);
  }, []);

  return {
    width,
    handleRef,
    onPointerDown,
    setWidth: setWidthClamped,
    resetToDefault,
    minWidth: MIN_WIDTH,
    maxWidth: MAX_WIDTH,
    defaultWidth: DEFAULT_WIDTH,
  };
}
