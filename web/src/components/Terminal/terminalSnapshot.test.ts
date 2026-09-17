import { afterEach, describe, expect, it, vi } from "vitest";
import { createTerminalSnapshot } from "./terminalSnapshot";
import { Terminal } from "@xterm/xterm";
import { SerializeAddon } from "@xterm/addon-serialize";

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
    // Queuing term.write does not mark dirty. The public onWriteParsed event
    // arrives later, including when a hidden terminal never renders.
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

  it("coalesces many parsed writes into one pending snapshot", () => {
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

  it("tracks real xterm parser completion and resize without rendering or truncating history", async () => {
    const term = new Terminal({ cols: 80, rows: 24, scrollback: 100_000, allowProposedApi: true });
    const addon = new SerializeAddon();
    term.loadAddon(addon);
    const save = vi.fn(() => addon.serialize({ scrollback: term.options.scrollback }));
    const snapshot = createTerminalSnapshot(save);
    const parsed = term.onWriteParsed(snapshot.markDirty);
    const resized = term.onResize(snapshot.markDirty);
    try {
      snapshot.flush();
      // No open()/render(): models hidden keep-alive terminals. The first
      // flush after write still sees the old generation until parsing ends.
      const text = "first retained line\r\n" + "history line\r\n".repeat(200) + "last retained line";
      const completed = new Promise<void>((resolve) => {
        const listener = term.onWriteParsed(() => { listener.dispose(); resolve(); });
      });
      term.write(text);
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(1);
      await completed;
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(2);
      expect(save.mock.results[1].value).toContain("first retained line");
      expect(save.mock.results[1].value).toContain("last retained line");
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(2);
      term.resize(90, 30);
      snapshot.flush();
      expect(save).toHaveBeenCalledTimes(3);
      term.clear();
      snapshot.markDirty();
      snapshot.flush();
      expect(save.mock.results[3].value).not.toContain("first retained line");
      term.reset();
      snapshot.markDirty();
      snapshot.flush();
      expect(save.mock.results[4].value).not.toContain("last retained line");
    } finally {
      snapshot.dispose();
      parsed.dispose();
      resized.dispose();
      term.dispose();
    }
  });
});
