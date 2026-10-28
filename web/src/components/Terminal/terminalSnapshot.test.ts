import { afterEach, describe, expect, it, vi } from "vitest";
import { createTerminalSnapshot } from "./terminalSnapshot";
import type { Terminal } from "ghostty-web";
import { serializeTerminalBuffer } from "./terminalSerialize";
import { TERMINAL_THEME_DEFAULTS } from "./terminalOptions";

/** A mutable plain-text fake of ghostty-web's buffer: one string per row, default attributes. */
function makeFakeTerm(state: { rows: string[] }): Pick<Terminal, "buffer"> {
  const cell = (ch: string) => ({
    getChars: () => (ch === " " ? "" : ch),
    getWidth: () => 1,
    getFgColor: () => TERMINAL_THEME_DEFAULTS.foreground,
    getBgColor: () => TERMINAL_THEME_DEFAULTS.background,
    isBold: () => 0,
    isItalic: () => 0,
    isUnderline: () => 0,
    isStrikethrough: () => 0,
    isBlink: () => 0,
    isInverse: () => 0,
    isInvisible: () => 0,
    isFaint: () => 0,
  });
  return {
    buffer: {
      active: {
        get length() {
          return state.rows.length;
        },
        getLine: (y: number) => {
          const row = state.rows[y];
          if (row === undefined) return undefined;
          return { length: row.length, isWrapped: false, getCell: (x: number) => (x < row.length ? cell(row[x]) : undefined) };
        },
        getNullCell: () => cell(" "),
      },
    },
  } as unknown as Pick<Terminal, "buffer">;
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("terminal snapshot scheduling", () => {
  it("does not serialize an unchanged terminal on repeated periodic saves", () => {
    vi.useFakeTimers();
    const serialize = vi.fn();
    const snapshot = createTerminalSnapshot(serialize);
    snapshot.schedule();
    vi.runOnlyPendingTimers();
    snapshot.schedule();
    vi.runOnlyPendingTimers();
    expect(serialize).toHaveBeenCalledTimes(1);
    snapshot.dispose();
  });

  it("keeps a later parsed generation dirty after a save before parsing completes", () => {
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.flush();
    // Queuing a write does not mark dirty by itself: the panel marks the
    // snapshot after every write it performs (ghostty-web has no "write
    // parsed" event), including for a hidden terminal that never renders.
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(1);
    snapshot.markDirty();
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(2);
    snapshot.dispose();
  });

  it.each(["resize", "clear", "reset"])("saves synchronous %s invalidation once", () => {
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.flush();
    snapshot.markDirty();
    snapshot.flush();
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(2);
  });

  it("coalesces many writes into one pending snapshot", () => {
    vi.useFakeTimers();
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    for (let i = 0; i < 100; i++) {
      snapshot.markDirty();
      snapshot.schedule();
    }
    expect(vi.getTimerCount()).toBe(1);
    vi.runAllTimers();
    expect(save).toHaveBeenCalledTimes(1);
    snapshot.dispose();
  });

  it("defers rather than drops dirty work during wheel activity", () => {
    vi.useFakeTimers();
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.schedule();
    snapshot.input();
    vi.advanceTimersByTime(100);
    snapshot.markDirty();
    snapshot.input();
    vi.advanceTimersByTime(100);
    expect(save).not.toHaveBeenCalled();
    vi.advanceTimersByTime(500);
    expect(save).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    snapshot.dispose();
  });

  it("waits for a stationary selection drag to end", () => {
    vi.useFakeTimers();
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.beginSelection();
    snapshot.schedule();
    vi.advanceTimersByTime(10_000);
    expect(save).not.toHaveBeenCalled();
    snapshot.endSelection();
    vi.advanceTimersByTime(500);
    expect(save).toHaveBeenCalledTimes(1);
    snapshot.dispose();
  });

  it("flushes dirty lifecycle work during input and cancels the retry", () => {
    vi.useFakeTimers();
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.beginSelection();
    snapshot.schedule();
    vi.advanceTimersByTime(1);
    snapshot.flush();
    snapshot.dispose();
    vi.runAllTimers();
    expect(save).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
    snapshot.markDirty();
    snapshot.schedule();
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("checks input again when an idle deadline fires and cancels idle handles", () => {
    vi.useFakeTimers();
    let idleCallback: IdleRequestCallback = () => {};
    const request = vi.fn((cb: IdleRequestCallback) => { idleCallback = cb; return 42; });
    const cancel = vi.fn();
    vi.stubGlobal("requestIdleCallback", request);
    vi.stubGlobal("cancelIdleCallback", cancel);
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    snapshot.schedule();
    expect(request).toHaveBeenCalledWith(expect.any(Function), { timeout: 5000 });
    snapshot.input();
    idleCallback({ didTimeout: true, timeRemaining: () => 0 });
    expect(save).not.toHaveBeenCalled();
    vi.advanceTimersByTime(201);
    idleCallback({ didTimeout: false, timeRemaining: () => 50 });
    expect(save).toHaveBeenCalledTimes(1);
    snapshot.markDirty();
    snapshot.schedule();
    snapshot.dispose();
    expect(cancel).toHaveBeenCalledWith(42);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not acknowledge a mutation during serialization or a failed save", () => {
    const save = vi.fn();
    const snapshot = createTerminalSnapshot(save);
    save.mockImplementationOnce(() => snapshot.markDirty());
    snapshot.flush();
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(2);
    snapshot.markDirty();
    save.mockImplementationOnce(() => { throw new Error("save failed"); });
    expect(() => snapshot.flush()).toThrow("save failed");
    snapshot.flush();
    expect(save).toHaveBeenCalledTimes(4);
    snapshot.dispose();
  });

  it("tracks writes and resize against the serialized buffer without truncating history", () => {
    const state = { rows: [] as string[] };
    const term = makeFakeTerm(state);
    const save = vi.fn(() => serializeTerminalBuffer(term, TERMINAL_THEME_DEFAULTS));
    const snapshot = createTerminalSnapshot(save);
    try {
      snapshot.flush();
      // Parsing is synchronous in ghostty-web: the panel writes, then marks dirty.
      state.rows = ["first retained line", ...Array.from({ length: 200 }, () => "history line"), "last retained line"];
      snapshot.markDirty();
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(2);
      expect(save.mock.results[1].value).toContain("first retained line");
      expect(save.mock.results[1].value).toContain("last retained line");
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(2);
      // onResize marks dirty.
      snapshot.markDirty();
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(3);
      // clear / reset drop the content and mark dirty by hand.
      state.rows = ["prompt$"];
      snapshot.markDirty();
      snapshot.flush();
      expect(save.mock.results[3].value).not.toContain("first retained line");
      state.rows = [];
      snapshot.markDirty();
      snapshot.flush();
      expect(save.mock.results[4].value).not.toContain("last retained line");
    } finally {
      snapshot.dispose();
    }
  });
});
