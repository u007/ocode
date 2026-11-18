import { describe, it, expect, vi, afterEach } from "vitest";
import type { Terminal } from "ghostty-web";
import {
  clearBuiltinLinkProviders,
  disableBuiltinMouseReporting,
  disableBuiltinSelectionCopy,
  pauseRenderLoop,
  resumeRenderLoop,
  resyncTerminalViewport,
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

describe("resyncTerminalViewport", () => {
  it("re-creates the canvas from the current geometry and repaints in full", () => {
    const render = vi.fn();
    const resize = vi.fn();
    const buffer = { id: "buffer" };
    const term = asTerm({
      cols: 120,
      rows: 40,
      viewportY: 0,
      renderer: { resize, render },
      wasmTerm: buffer,
    });

    resyncTerminalViewport(term);

    // `renderer.resize` is the only ghostty-web path that re-creates the backing
    // store and re-applies the device-pixel transform, which a stale hidden-tab
    // canvas needs.
    expect(resize).toHaveBeenCalledWith(120, 40);
    // Full redraw (second argument), otherwise the partial loop repaints only
    // dirty rows and leaves the rest of the canvas stale.
    expect(render).toHaveBeenCalledWith(buffer, true, 0, term);
  });

  it("is a no-op before open()", () => {
    expect(() => resyncTerminalViewport(asTerm({ cols: 80, rows: 24 }))).not.toThrow();
  });

  it("throws when the renderer loses resize (a ghostty-web bump)", () => {
    expect(() => resyncTerminalViewport(asTerm({ cols: 80, rows: 24, renderer: {}, wasmTerm: {} }))).toThrow(
      "ghostty-web internals changed: CanvasRenderer.resize is not a function",
    );
  });
});

describe("pauseRenderLoop", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("cancels the pending frame and clears the field", () => {
    const cancel = vi.fn();
    vi.stubGlobal("cancelAnimationFrame", cancel);
    const term = asTerm({ renderer: {}, animationFrameId: 7 });
    pauseRenderLoop(term);
    expect(cancel).toHaveBeenCalledWith(7);
    expect((term as unknown as { animationFrameId: unknown }).animationFrameId).toBeUndefined();
  });

  it("is a no-op when already paused", () => {
    const cancel = vi.fn();
    vi.stubGlobal("cancelAnimationFrame", cancel);
    pauseRenderLoop(asTerm({ renderer: {}, animationFrameId: undefined }));
    expect(cancel).not.toHaveBeenCalled();
  });

  it("throws when animationFrameId is not a number", () => {
    expect(() => pauseRenderLoop(asTerm({ renderer: {}, animationFrameId: "7" }))).toThrow(
      "ghostty-web internals changed: Terminal.animationFrameId is not a number or undefined",
    );
  });

  it("returns quietly without a renderer (terminal not open or disposed)", () => {
    const cancel = vi.fn();
    vi.stubGlobal("cancelAnimationFrame", cancel);
    expect(() => pauseRenderLoop(asTerm({ animationFrameId: 7 }))).not.toThrow();
    expect(cancel).not.toHaveBeenCalled();
  });
});

describe("resumeRenderLoop", () => {
  it("restarts the render loop on the terminal and repaints the whole viewport", () => {
    const render = vi.fn();
    const startRenderLoop = vi.fn();
    const buffer = { id: "buffer" };
    const term = asTerm({
      renderer: { render },
      wasmTerm: buffer,
      viewportY: 0,
      startRenderLoop,
    });

    resumeRenderLoop(term);

    expect(startRenderLoop).toHaveBeenCalledTimes(1);
    expect(startRenderLoop.mock.contexts[0]).toBe(term);
    // Full repaint (second argument): the canvas went stale while the loop was stopped.
    expect(render).toHaveBeenCalledWith(buffer, true, 0, term);
  });

  it("throws when startRenderLoop is gone", () => {
    expect(() => resumeRenderLoop(asTerm({ renderer: { render: vi.fn() }, wasmTerm: {} }))).toThrow(
      "ghostty-web internals changed: Terminal.startRenderLoop is not a function",
    );
  });

  it("returns quietly without a renderer (terminal not open or disposed)", () => {
    const startRenderLoop = vi.fn();
    expect(() => resumeRenderLoop(asTerm({ startRenderLoop }))).not.toThrow();
    expect(startRenderLoop).not.toHaveBeenCalled();
  });
});
