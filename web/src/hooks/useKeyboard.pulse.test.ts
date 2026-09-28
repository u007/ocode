import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useKeyboard } from "./useKeyboard";

/** Fire a keydown on window the way the browser would. */
function press(key: string, opts: { meta?: boolean; ctrl?: boolean; target?: Element } = {}) {
  const event = new KeyboardEvent("keydown", {
    key,
    metaKey: opts.meta ?? false,
    ctrlKey: opts.ctrl ?? false,
    bubbles: true,
    cancelable: true,
  });
  (opts.target ?? window).dispatchEvent(event);
  return event;
}

beforeEach(() => {
  vi.stubGlobal("__wails", undefined);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useKeyboard ⌘J pulse toggle", () => {
  it("calls onTogglePulse on Cmd+J", () => {
    const onTogglePulse = vi.fn();
    renderHook(() => useKeyboard({ onTogglePulse }));
    press("j", { meta: true });
    expect(onTogglePulse).toHaveBeenCalledTimes(1);
  });

  it("calls onTogglePulse on Ctrl+J", () => {
    const onTogglePulse = vi.fn();
    renderHook(() => useKeyboard({ onTogglePulse }));
    press("j", { ctrl: true });
    expect(onTogglePulse).toHaveBeenCalledTimes(1);
  });

  it("does not fire for a bare J", () => {
    const onTogglePulse = vi.fn();
    renderHook(() => useKeyboard({ onTogglePulse }));
    press("j");
    expect(onTogglePulse).not.toHaveBeenCalled();
  });

  it("does not steal the shortcut from an embedded terminal", () => {
    // Ctrl+J is readline "kill line" inside a shell. Same hazard as the
    // existing Ctrl+W guard: while the user is typing a command, a global
    // shortcut must not yank them out to another view.
    const onTogglePulse = vi.fn();
    const xterm = document.createElement("div");
    xterm.className = "xterm";
    const input = document.createElement("input");
    xterm.appendChild(input);
    document.body.appendChild(xterm);
    try {
      renderHook(() => useKeyboard({ onTogglePulse }));
      press("j", { ctrl: true, target: input });
      expect(onTogglePulse).not.toHaveBeenCalled();
    } finally {
      xterm.remove();
    }
  });

  it("still works from the terminal on Cmd+J (macOS never sends it to the pty)", () => {
    const onTogglePulse = vi.fn();
    const xterm = document.createElement("div");
    xterm.className = "xterm";
    const input = document.createElement("input");
    xterm.appendChild(input);
    document.body.appendChild(xterm);
    try {
      renderHook(() => useKeyboard({ onTogglePulse }));
      press("j", { meta: true, target: input });
      expect(onTogglePulse).toHaveBeenCalledTimes(1);
    } finally {
      xterm.remove();
    }
  });

  it("prevents the browser default so ⌘J does not also do something else", () => {
    renderHook(() => useKeyboard({ onTogglePulse: vi.fn() }));
    const event = press("j", { meta: true });
    expect(event.defaultPrevented).toBe(true);
  });

  it("is a no-op with no handler wired", () => {
    renderHook(() => useKeyboard({}));
    expect(() => press("j", { meta: true })).not.toThrow();
  });

  it("leaves the existing shortcuts working", () => {
    const onCommandPalette = vi.fn();
    const onNewSession = vi.fn();
    renderHook(() => useKeyboard({ onCommandPalette, onNewSession, onTogglePulse: vi.fn() }));
    press("k", { meta: true });
    press("n", { meta: true });
    expect(onCommandPalette).toHaveBeenCalledTimes(1);
    expect(onNewSession).toHaveBeenCalledTimes(1);
  });
});
