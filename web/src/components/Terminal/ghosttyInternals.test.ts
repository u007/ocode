import { describe, it, expect, vi } from "vitest";
import type { Terminal } from "ghostty-web";
import {
  clearBuiltinLinkProviders,
  disableBuiltinMouseReporting,
  disableBuiltinSelectionCopy,
  setSelectionAbsolute,
} from "./ghosttyInternals";

function asTerm(shape: Record<string, unknown>): Terminal {
  return shape as unknown as Terminal;
}

function makeSelectionManager(overrides: Record<string, unknown> = {}) {
  return {
    copyToClipboard: vi.fn(),
    selectionStart: null as { col: number; absoluteRow: number } | null,
    selectionEnd: null as { col: number; absoluteRow: number } | null,
    dirtySelectionRows: new Set<number>(),
    selectionChangedEmitter: { fire: vi.fn() },
    requestRender: vi.fn(),
    ...overrides,
  };
}

describe("disableBuiltinSelectionCopy", () => {
  it("replaces copyToClipboard with a no-op", () => {
    const sm = makeSelectionManager();
    const original = sm.copyToClipboard;
    disableBuiltinSelectionCopy(asTerm({ selectionManager: sm }));
    expect(sm.copyToClipboard).not.toBe(original);
    sm.copyToClipboard();
    expect(original).not.toHaveBeenCalled();
  });

  it("throws when the terminal is not open", () => {
    expect(() => disableBuiltinSelectionCopy(asTerm({}))).toThrow(
      "ghostty-web internals changed: Terminal.selectionManager is not an object",
    );
  });

  it("throws when copyToClipboard is gone", () => {
    expect(() => disableBuiltinSelectionCopy(asTerm({ selectionManager: {} }))).toThrow(
      "ghostty-web internals changed: SelectionManager.copyToClipboard is not a function",
    );
  });
});

describe("clearBuiltinLinkProviders", () => {
  it("disposes and removes every provider, then invalidates the cache", () => {
    const dispose = vi.fn();
    const providers: unknown[] = [{ dispose }, {}, { dispose }];
    const detector = { providers, invalidateCache: vi.fn() };
    clearBuiltinLinkProviders(asTerm({ linkDetector: detector }));
    expect(dispose).toHaveBeenCalledTimes(2);
    expect(detector.providers).toBe(providers);
    expect(providers).toHaveLength(0);
    expect(detector.invalidateCache).toHaveBeenCalledTimes(1);
  });

  it("throws when providers is not an array", () => {
    expect(() => clearBuiltinLinkProviders(asTerm({ linkDetector: { providers: new Set() } }))).toThrow(
      "ghostty-web internals changed: LinkDetector.providers is not an array",
    );
  });

  it("throws when linkDetector is missing", () => {
    expect(() => clearBuiltinLinkProviders(asTerm({}))).toThrow(
      "ghostty-web internals changed: Terminal.linkDetector is not an object",
    );
  });

  it("throws when invalidateCache is gone", () => {
    expect(() => clearBuiltinLinkProviders(asTerm({ linkDetector: { providers: [] } }))).toThrow(
      "ghostty-web internals changed: LinkDetector.invalidateCache is not a function",
    );
  });
});

describe("disableBuiltinMouseReporting", () => {
  function makeInputHandler(container: EventTarget, overrides: Record<string, unknown> = {}) {
    return {
      container,
      mousedownListener: vi.fn(),
      mouseupListener: vi.fn(),
      mousemoveListener: vi.fn(),
      wheelListener: vi.fn(),
      ...overrides,
    };
  }

  it("removes all four listeners from the container and nulls the fields", () => {
    const container = document.createElement("div");
    const removeSpy = vi.spyOn(container, "removeEventListener");
    const ih = makeInputHandler(container);
    const { mousedownListener, mouseupListener, mousemoveListener, wheelListener } = ih;
    disableBuiltinMouseReporting(asTerm({ inputHandler: ih }));
    expect(removeSpy).toHaveBeenCalledWith("mousedown", mousedownListener);
    expect(removeSpy).toHaveBeenCalledWith("mouseup", mouseupListener);
    expect(removeSpy).toHaveBeenCalledWith("mousemove", mousemoveListener);
    expect(removeSpy).toHaveBeenCalledWith("wheel", wheelListener);
    expect(removeSpy).toHaveBeenCalledTimes(4);
    expect(ih.mousedownListener).toBeNull();
    expect(ih.mouseupListener).toBeNull();
    expect(ih.mousemoveListener).toBeNull();
    expect(ih.wheelListener).toBeNull();
  });

  it("throws when the terminal is not open", () => {
    expect(() => disableBuiltinMouseReporting(asTerm({}))).toThrow(
      "ghostty-web internals changed: Terminal.inputHandler is not an object",
    );
  });

  it("throws when container is not an EventTarget", () => {
    const ih = makeInputHandler(document.createElement("div"), { container: {} });
    expect(() => disableBuiltinMouseReporting(asTerm({ inputHandler: ih }))).toThrow(
      "ghostty-web internals changed: InputHandler.container is not an EventTarget",
    );
  });

  it("throws when one listener field is null and names the field", () => {
    const ih = makeInputHandler(document.createElement("div"), { wheelListener: null });
    expect(() => disableBuiltinMouseReporting(asTerm({ inputHandler: ih }))).toThrow(
      "ghostty-web internals changed: InputHandler.wheelListener is not a function (is the terminal open?)",
    );
  });
});

describe("setSelectionAbsolute", () => {
  it("sets absolute coordinates, dirties old and new rows, renders and fires", () => {
    const sm = makeSelectionManager({
      selectionStart: { col: 1, absoluteRow: 10 },
      selectionEnd: { col: 4, absoluteRow: 12 },
    });
    setSelectionAbsolute(asTerm({ selectionManager: sm }), 2, 40, 6, 40);
    expect(sm.selectionStart).toEqual({ col: 2, absoluteRow: 40 });
    expect(sm.selectionEnd).toEqual({ col: 6, absoluteRow: 40 });
    expect([...sm.dirtySelectionRows].sort((a, b) => a - b)).toEqual([10, 11, 12, 40]);
    expect(sm.requestRender).toHaveBeenCalledTimes(1);
    expect(sm.selectionChangedEmitter.fire).toHaveBeenCalledTimes(1);
  });

  it("works when there was no previous selection and a multi-row range", () => {
    const sm = makeSelectionManager();
    setSelectionAbsolute(asTerm({ selectionManager: sm }), 0, 3, 5, 5);
    expect([...sm.dirtySelectionRows].sort((a, b) => a - b)).toEqual([3, 4, 5]);
  });

  it("throws when dirtySelectionRows is not a Set", () => {
    const sm = makeSelectionManager({ dirtySelectionRows: [] });
    expect(() => setSelectionAbsolute(asTerm({ selectionManager: sm }), 0, 0, 0, 0)).toThrow(
      "ghostty-web internals changed: SelectionManager.dirtySelectionRows is not a Set",
    );
  });

  it("throws when the emitter is missing", () => {
    const sm = makeSelectionManager({ selectionChangedEmitter: undefined });
    expect(() => setSelectionAbsolute(asTerm({ selectionManager: sm }), 0, 0, 0, 0)).toThrow(
      "ghostty-web internals changed: SelectionManager.selectionChangedEmitter.fire is not a function",
    );
  });

  it("throws when selectionStart/End are gone", () => {
    const sm = makeSelectionManager();
    delete (sm as Record<string, unknown>).selectionStart;
    expect(() => setSelectionAbsolute(asTerm({ selectionManager: sm }), 0, 0, 0, 0)).toThrow(
      "ghostty-web internals changed: SelectionManager.selectionStart/selectionEnd are missing",
    );
  });

  it("throws when requestRender is gone", () => {
    const sm = makeSelectionManager({ requestRender: undefined });
    expect(() => setSelectionAbsolute(asTerm({ selectionManager: sm }), 0, 0, 0, 0)).toThrow(
      "ghostty-web internals changed: SelectionManager.requestRender is not a function",
    );
  });
});
