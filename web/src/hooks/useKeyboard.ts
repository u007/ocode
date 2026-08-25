import { useEffect, useRef } from "react";
import { isDesktopShell } from "@/lib/desktopShell";

interface ShortcutHandlers {
  onNewSession?: () => void;
  onNewTerminal?: () => void;
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
      if (e.key === "t" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        ref.current.onNewTerminal?.();
      }
      if (e.key === "w" && (e.metaKey || e.ctrlKey) && isDesktopShell()) {
        // Don't steal Ctrl+W from the embedded terminal (readline "delete
        // previous word" while typing). Cmd+W (metaKey) is never sent to the
        // pty, so it still closes the frontmost tab even when the terminal
        // has focus.
        const target = e.target as Element | null;
        if (!e.metaKey && target instanceof Element && target.closest(".xterm")) return;
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
