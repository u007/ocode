import { useState, useRef, useEffect, useCallback } from "react";
import { createPortal } from "react-dom";

export interface ContextMenuItem {
  label: string;
  icon?: React.ReactNode;
  /** Return false (or resolve false) to keep the menu open, e.g. to show an error. */
  onClick: () => void | boolean | Promise<void | boolean>;
  destructive?: boolean;
  disabled?: boolean;
  separator?: boolean;
}

interface ContextMenuProps {
  items: ContextMenuItem[];
  /** Controlled mode, used by browser surfaces that own the menu state. */
  open?: boolean;
  position?: { x: number; y: number };
  onClose?: () => void;
  /** Legacy wrapper mode for existing application context menus. */
  children?: React.ReactNode;
  onOpen?: () => void;
}

export function ContextMenu({ items, open, position, onClose, children, onOpen }: ContextMenuProps) {
  const [internalOpen, setInternalOpen] = useState(false);
  const [internalPosition, setInternalPosition] = useState({ x: 0, y: 0 });
  const menuRef = useRef<HTMLDivElement>(null);
  const controlled = open !== undefined;
  const visibleOpen = controlled ? open : internalOpen;
  const visiblePosition = position ?? internalPosition;
  const close = useCallback(() => {
    setInternalOpen(false);
    onClose?.();
  }, [onClose]);

  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setInternalPosition({ x: e.clientX, y: e.clientY });
    setInternalOpen(true);
    onOpen?.();
  }, [onOpen]);

  useEffect(() => {
    if (!visibleOpen) return;

    const handleClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        close();
      }
    };
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };

    document.addEventListener("mousedown", handleClick);
    document.addEventListener("keydown", handleEscape);
    return () => {
      document.removeEventListener("mousedown", handleClick);
      document.removeEventListener("keydown", handleEscape);
    };
  }, [visibleOpen, close]);

  // Clamp menu position to viewport
  const clampedPosition = {
    x: Math.max(0, Math.min(visiblePosition.x, window.innerWidth - 200)),
    y: Math.max(0, Math.min(visiblePosition.y, window.innerHeight - items.length * 36)),
  };

  return (
    <>
      {children && (
        <div onContextMenu={handleContextMenu} className="contents">
          {children}
        </div>
      )}
      {visibleOpen &&
        createPortal(
          <div
            ref={menuRef}
            className="fixed z-50 min-w-[180px] bg-popover border border-border rounded-md shadow-md py-1 animate-in fade-in-0 zoom-in-95"
            style={{ left: clampedPosition.x, top: clampedPosition.y }}
          >
            {items.map((item, i) => {
              if (item.separator) {
                return <div key={i} className="h-px bg-border my-1" />;
              }
              return (
                <button
                  key={i}
                  className={`w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left ${
                    item.destructive
                      ? "text-destructive hover:bg-destructive/10"
                      : "text-foreground hover:bg-accent hover:text-accent-foreground"
                  } ${item.disabled ? "opacity-50 pointer-events-none" : ""}`}
                  disabled={item.disabled}
                  onClick={async () => {
                    if (item.disabled) return;
                    const shouldClose = await item.onClick();
                    if (shouldClose !== false) close();
                  }}
                >
                  {item.icon && <span className="w-4 h-4 shrink-0">{item.icon}</span>}
                  {item.label}
                </button>
              );
            })}
          </div>,
          document.body,
        )}
    </>
  );
}
