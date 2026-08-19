import { useState, useRef, useEffect, useCallback } from "react";
import { createPortal } from "react-dom";

export interface ContextMenuItem {
  label: string;
  icon?: React.ReactNode;
  onClick: () => void;
  destructive?: boolean;
  disabled?: boolean;
  separator?: boolean;
}

interface ContextMenuProps {
  items: ContextMenuItem[];
  children: React.ReactNode;
  onOpen?: () => void;
}

export function ContextMenu({ items, children, onOpen }: ContextMenuProps) {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ x: 0, y: 0 });
  const menuRef = useRef<HTMLDivElement>(null);

  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setPosition({ x: e.clientX, y: e.clientY });
    setOpen(true);
    onOpen?.();
  }, [onOpen]);

  useEffect(() => {
    if (!open) return;

    const handleClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const handleEscape = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };

    document.addEventListener("mousedown", handleClick);
    document.addEventListener("keydown", handleEscape);
    return () => {
      document.removeEventListener("mousedown", handleClick);
      document.removeEventListener("keydown", handleEscape);
    };
  }, [open]);

  // Clamp menu position to viewport
  const clampedPosition = {
    x: Math.min(position.x, window.innerWidth - 200),
    y: Math.min(position.y, window.innerHeight - items.length * 36),
  };

  return (
    <>
      <div onContextMenu={handleContextMenu} className="contents">
        {children}
      </div>
      {open &&
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
                      : "text-foreground hover:bg-accent"
                  } ${item.disabled ? "opacity-50 pointer-events-none" : ""}`}
                  onClick={() => {
                    item.onClick();
                    setOpen(false);
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
