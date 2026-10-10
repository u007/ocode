import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useKeyboard } from "./useKeyboard";

function dispatchKey(key: string, meta = false, ctrl = false, target?: Element, shift = false, alt = false) {
  const ev = new KeyboardEvent("keydown", {
    key,
    metaKey: meta,
    ctrlKey: ctrl,
    shiftKey: shift,
    altKey: alt,
    bubbles: true,
    cancelable: true,
  });
  const scope = target ?? window;
  scope.dispatchEvent(ev);
  return ev;
}

beforeEach(() => {
  // The desktop shell injects window._wails; simulate that so the
  // Cmd/Ctrl+W close-session shortcut is active.
  (window as unknown as { _wails?: unknown })._wails = {};
});

afterEach(() => {
  delete (window as unknown as { _wails?: unknown })._wails;
  vi.restoreAllMocks();
});

describe("useKeyboard assistant toggle (Cmd/Ctrl+Shift+A)", () => {
  it("toggles the assistant on Cmd+Shift+A and Ctrl+Shift+A, and prevents the default", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));

    let meta!: KeyboardEvent;
    let ctrl!: KeyboardEvent;
    act(() => {
      meta = dispatchKey("a", true, false, undefined, true);
      ctrl = dispatchKey("a", false, true, undefined, true);
    });

    expect(onToggleAssistant).toHaveBeenCalledTimes(2);
    expect(meta.defaultPrevented).toBe(true);
    expect(ctrl.defaultPrevented).toBe(true);
  });

  it("accepts the uppercase key that Shift produces", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));

    act(() => {
      dispatchKey("A", false, true, undefined, true);
    });

    expect(onToggleAssistant).toHaveBeenCalledTimes(1);
  });

  it("leaves plain Cmd/Ctrl+A, and the Shift+Alt and Ctrl-only forms, to the page", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));

    act(() => {
      dispatchKey("a", true);
      dispatchKey("a", false, true);
      dispatchKey("a", false, true, undefined, true, true);
      dispatchKey("a", false, false, undefined, true);
    });

    expect(onToggleAssistant).not.toHaveBeenCalled();
  });

  it("does not act on a key another handler already took", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));
    const guard = (e: KeyboardEvent) => e.preventDefault();
    window.addEventListener("keydown", guard, { capture: true });

    act(() => {
      dispatchKey("a", false, true, undefined, true);
    });
    window.removeEventListener("keydown", guard, { capture: true });

    expect(onToggleAssistant).not.toHaveBeenCalled();
  });

  it("leaves Ctrl+Shift+A to the terminal and the code editor, which use it", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));
    const xterm = document.createElement("div");
    xterm.className = "xterm";
    const monaco = document.createElement("div");
    monaco.className = "monaco-editor";
    const inner = document.createElement("textarea");
    xterm.appendChild(inner);
    monaco.appendChild(document.createElement("textarea"));
    document.body.append(xterm, monaco);

    act(() => {
      dispatchKey("a", false, true, inner, true);
      dispatchKey("a", false, true, monaco.querySelector("textarea") ?? undefined, true);
    });
    xterm.remove();
    monaco.remove();

    expect(onToggleAssistant).not.toHaveBeenCalled();
  });

  it("still toggles from the terminal or editor on Cmd+Shift+A, which those never receive", () => {
    const onToggleAssistant = vi.fn();
    renderHook(() => useKeyboard({ onToggleAssistant }));
    const xterm = document.createElement("div");
    xterm.className = "xterm";
    const inner = document.createElement("textarea");
    xterm.appendChild(inner);
    document.body.appendChild(xterm);

    act(() => {
      dispatchKey("a", true, false, inner, true);
    });
    xterm.remove();

    expect(onToggleAssistant).toHaveBeenCalledTimes(1);
  });
});

describe("useKeyboard", () => {
  it("calls onCloseSession for Cmd+W and Ctrl+W in the desktop shell", () => {
    const onCloseSession = vi.fn();
    renderHook(() => useKeyboard({ onCloseSession }));

    act(() => {
      dispatchKey("w", true);
      dispatchKey("w", false, true);
    });

    expect(onCloseSession).toHaveBeenCalledTimes(2);
  });

  it("does not bind close-session in a plain browser (no window._wails)", () => {
    delete (window as unknown as { _wails?: unknown })._wails;
    const onCloseSession = vi.fn();
    renderHook(() => useKeyboard({ onCloseSession }));

    act(() => {
      dispatchKey("w", true);
      dispatchKey("w", false, true);
    });

    expect(onCloseSession).not.toHaveBeenCalled();
  });

  it("does not steal Ctrl+W while typing inside the embedded terminal", () => {
    const onCloseSession = vi.fn();
    renderHook(() => useKeyboard({ onCloseSession }));

    const terminal = document.createElement("div");
    terminal.className = "xterm";
    document.body.appendChild(terminal);

    act(() => {
      dispatchKey("w", false, true, terminal);
    });

    document.body.removeChild(terminal);
    expect(onCloseSession).not.toHaveBeenCalled();
  });

  it("closes via Cmd+W even when the embedded terminal has focus", () => {
    // Cmd+W (metaKey) is never sent to the pty, so it should still close the
    // frontmost tab while typing in the terminal — only Ctrl+W is the
    // shell's delete-previous-word.
    const onCloseSession = vi.fn();
    renderHook(() => useKeyboard({ onCloseSession }));

    const terminal = document.createElement("div");
    terminal.className = "xterm";
    document.body.appendChild(terminal);

    act(() => {
      dispatchKey("w", true, false, terminal);
    });

    document.body.removeChild(terminal);
    expect(onCloseSession).toHaveBeenCalledTimes(1);
  });

  it("does not call onCloseSession for plain W", () => {
    const onCloseSession = vi.fn();
    renderHook(() => useKeyboard({ onCloseSession }));

    act(() => {
      dispatchKey("w");
    });

    expect(onCloseSession).not.toHaveBeenCalled();
  });

  it("still fires the existing New Session shortcut", () => {
    const onNewSession = vi.fn();
    renderHook(() => useKeyboard({ onNewSession }));

    act(() => {
      dispatchKey("n", true);
    });

    expect(onNewSession).toHaveBeenCalledTimes(1);
  });
});
