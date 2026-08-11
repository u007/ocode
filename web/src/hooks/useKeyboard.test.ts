import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useKeyboard } from "./useKeyboard";

function dispatchKey(key: string, meta = false, ctrl = false, target?: Element) {
  const ev = new KeyboardEvent("keydown", {
    key,
    metaKey: meta,
    ctrlKey: ctrl,
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
      dispatchKey("w", true, false, terminal);
    });

    document.body.removeChild(terminal);
    expect(onCloseSession).not.toHaveBeenCalled();
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
