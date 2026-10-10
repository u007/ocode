import { useEffect, useRef } from "react";
import { isDesktopShell } from "@/lib/desktopShell";
import type { FocusedKind } from "../lib/viewPersistence";

interface ShortcutHandlers {
  onNewSession?: () => void;
  onNewTerminal?: () => void;
  onCommandPalette?: () => void;
  onFilePicker?: () => void;
  onSave?: () => void;
  onEscape?: () => void;
  onCloseSession?: () => void;
  /** Which tab kind is frontmost on the merged sessions bar. When "browser"
   *  (and activeBrowserId is set) Cmd/Ctrl+W closes the browser tab instead
   *  of the session tab. */
  focusedKind?: FocusedKind;
  /** The focused browser tab's id, when a browser tab is focused. */
  activeBrowserId?: string | null;
  onCloseBrowserTab?: (id: string) => void;
  /** Toggle the Pulse dashboard. Wired to Cmd/Ctrl+J (see the handler). */
  onTogglePulse?: () => void;
  /** Toggle the Pulse assistant window. Wired to Cmd/Ctrl+Shift+A. */
  onToggleAssistant?: () => void;
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
        const h = ref.current;
        if (h.focusedKind === "browser" && h.activeBrowserId) {
          h.onCloseBrowserTab?.(h.activeBrowserId);
        } else {
          h.onCloseSession?.();
        }
      }
      if (e.key === "j" && (e.metaKey || e.ctrlKey)) {
        // Same hazard as the Ctrl+W guard above: Ctrl+J is readline "kill
        // line" inside a shell, so a global shortcut must not yank the user
        // out to another view while they are typing a command. Cmd+J is never
        // sent to the pty on macOS, so it still works from the terminal.
        const target = e.target as Element | null;
        if (!e.metaKey && target instanceof Element && target.closest(".xterm")) return;
        e.preventDefault();
        ref.current.onTogglePulse?.();
      }
      // Cmd/Ctrl+Shift+A. Shift is required so Cmd/Ctrl+A keeps its select-all
      // in every field, and the key is compared lowercased because Shift makes
      // e.key "A". Ctrl+Shift+A is also the xterm and Monaco block-comment
      // combo on Linux and Windows, so on the Ctrl path an event from either
      // editor is left alone. Cmd+Shift+A reaches neither. An event another
      // handler already took is never acted on again.
      if (
        !e.defaultPrevented &&
        e.key.toLowerCase() === "a" &&
        (e.metaKey || e.ctrlKey) &&
        e.shiftKey &&
        !e.altKey
      ) {
        const target = e.target as Element | null;
        const inEditor = target instanceof Element && target.closest(".xterm, .monaco-editor") !== null;
        if (!(e.ctrlKey && !e.metaKey && inEditor)) {
          e.preventDefault();
          ref.current.onToggleAssistant?.();
        }
      }
      if (e.key === "Escape") {
        ref.current.onEscape?.();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);
}
