import { useEffect, useRef } from "react";

interface ShortcutHandlers {
  onNewSession?: () => void;
  onCommandPalette?: () => void;
  onFilePicker?: () => void;
  onSave?: () => void;
  onEscape?: () => void;
  onCloseSession?: () => void;
}

/**
 * True when running inside the ocode desktop shell's webview. The Wails
 * runtime core is injected into every webview page; a plain browser tab never
 * has it. Used to gate Cmd/Ctrl+W: in a browser that key closes the browser
 * tab at the OS level and cannot be intercepted, so binding it would double
 * close (session + browser tab) — the shortcut only makes sense on desktop.
 */
function isDesktopShell() {
  return (
    typeof window !== "undefined" &&
    typeof (window as unknown as { _wails?: unknown })._wails !== "undefined"
  );
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
      if (e.key === "p" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onFilePicker?.();
      }
      if (e.key === "s" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onSave?.();
      }
      if (e.key === "n" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onNewSession?.();
      }
      if (e.key === "w" && (e.metaKey || e.ctrlKey) && isDesktopShell()) {
        // Don't steal Ctrl+W from the embedded terminal (xterm uses it as
        // readline "delete previous word" while typing).
        const target = e.target as Element | null;
        if (target instanceof Element && target.closest(".xterm")) return;
        e.preventDefault();
        ref.current.onCloseSession?.();
      }
      if (e.key === "Escape") {
        ref.current.onEscape?.();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);
}
