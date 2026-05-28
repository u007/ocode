import { useEffect, useRef } from "react";

interface ShortcutHandlers {
  onNewSession?: () => void;
  onCommandPalette?: () => void;
  onEscape?: () => void;
}

export function useKeyboard(handlers: ShortcutHandlers) {
  const ref = useRef(handlers);
  ref.current = handlers;

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onCommandPalette?.();
      }
      if (e.key === "n" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onNewSession?.();
      }
      if (e.key === "Escape") {
        ref.current.onEscape?.();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);
}
