import { useState, useCallback, useRef, useEffect } from "react";

const DEFAULT_WIDTH = 240; // w-60 equivalent
const MIN_WIDTH = 160;
const MAX_WIDTH = 500;

interface Options {
  storageKey?: string;
  defaultWidth?: number;
  minWidth?: number;
  maxWidth?: number;
  collapsible?: boolean;
}

/**
 * Hook that manages a resizable sidebar width with drag-to-resize and
 * localStorage persistence. Returns the current width, a ref for the
 * drag handle element, and handlers wired to pointer events.
 */
export function useResizableSidebar(options: Options = {}) {
  const {
    storageKey = "ocode.ui.sidebar_width",
    defaultWidth = DEFAULT_WIDTH,
    minWidth = MIN_WIDTH,
    maxWidth = MAX_WIDTH,
    collapsible = false,
  } = options;

  const collapsedKey = `${storageKey}.collapsed`;

  const [width, setWidth] = useState<number>(() => {
    try {
      const stored = localStorage.getItem(storageKey);
      if (stored) {
        const parsed = Number(stored);
        if (Number.isFinite(parsed) && parsed >= minWidth && parsed <= maxWidth) {
          return parsed;
        }
      }
    } catch { /* ignore */ }
    return defaultWidth;
  });

  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (!collapsible) return false;
    try {
      return localStorage.getItem(collapsedKey) === "1";
    } catch {
      return false;
    }
  });

  const dragRef = useRef<{ startX: number; startWidth: number } | null>(null);
  const handleRef = useRef<HTMLDivElement>(null);

  const clamp = useCallback((v: number) => Math.min(maxWidth, Math.max(minWidth, v)), [minWidth, maxWidth]);

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
      localStorage.setItem(storageKey, String(width));
    } catch { /* ignore */ }
  }, [storageKey, width]);

  useEffect(() => {
    if (!collapsible) return;
    try {
      localStorage.setItem(collapsedKey, collapsed ? "1" : "0");
    } catch { /* ignore */ }
  }, [collapsible, collapsedKey, collapsed]);

  const resetToDefault = useCallback(() => {
    setWidth(defaultWidth);
  }, [defaultWidth]);

  const toggleCollapsed = useCallback(() => {
    setCollapsed((c) => !c);
  }, []);

  return {
    width,
    handleRef,
    onPointerDown,
    setWidth: setWidthClamped,
    resetToDefault,
    minWidth,
    maxWidth,
    defaultWidth,
    collapsed,
    setCollapsed,
    toggleCollapsed,
  };
}
