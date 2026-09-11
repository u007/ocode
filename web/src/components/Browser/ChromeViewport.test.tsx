import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";

// jsdom has no PointerEvent (see UnifiedTabBar.drag.test.tsx) — clientX/Y are
// lost without a polyfill, which the viewport's coordinate mapping relies on.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    pointerType: string;
    isPrimary: boolean;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 0;
      this.pointerType = params.pointerType ?? "mouse";
      this.isPrimary = params.isPrimary ?? true;
    }
  }
  // @ts-expect-error assigning polyfill to the global/window
  window.PointerEvent = PointerEventPolyfill;
}

// Mock useCdpSocket: capture send calls, expose onFrame + status/error.
const mockApi = {
  send: vi.fn(),
  status: "open" as "connecting" | "open" | "reconnecting" | "closed",
  error: null as string | null,
  frameCbs: new Set<(bitmap: ImageBitmap, w: number, h: number) => void>(),
  onFrame: (cb: (bitmap: ImageBitmap, w: number, h: number) => void) => {
    mockApi.frameCbs.add(cb);
    return () => mockApi.frameCbs.delete(cb);
  },
  fileChooserCbs: new Set<(multiple: boolean) => void>(),
  onFileChooser: (cb: (multiple: boolean) => void) => {
    mockApi.fileChooserCbs.add(cb);
    return () => mockApi.fileChooserCbs.delete(cb);
  },
  selectionCbs: new Set<(text: string) => void>(),
  onSelection: (cb: (text: string) => void) => {
    mockApi.selectionCbs.add(cb);
    return () => mockApi.selectionCbs.delete(cb);
  },
  findResultCbs: new Set<(res: { query?: string; found: boolean; active: number; total: number }) => void>(),
  onFindResult: (cb: (res: { query?: string; found: boolean; active: number; total: number }) => void) => {
    mockApi.findResultCbs.add(cb);
    return () => mockApi.findResultCbs.delete(cb);
  },
  getNodeAt: vi.fn((_x: number, _y: number): Promise<{ nodeId: number }> => new Promise(() => {})),
  describeNode: vi.fn((_nodeId: number): Promise<{ nodeId: number; nodeName?: string; attributes?: string[] }> => new Promise(() => {})),
};

const mockUpload = vi.hoisted(() => vi.fn(async (_key: string, _files: File[]) => {}));
vi.mock("../../api/client", () => ({
  uploadBrowseFiles: (key: string, files: File[]) => mockUpload(key, files),
}));

vi.mock("./useCdpSocket", () => ({
  useCdpSocket: () => mockApi,
}));

import { ChromeViewport } from "./ChromeViewport";
import { browserStore, browserActions } from "../../lib/browserStore";

// Minimal ImageBitmap double (jsdom has neither createImageBitmap nor
// ImageBitmap; the component only uses .width/.height/.close() + drawImage).
function fakeBitmap(w = 640, h = 480): ImageBitmap {
  return {
    width: w,
    height: h,
    close: vi.fn(),
  } as unknown as ImageBitmap;
}

beforeEach(() => {
  vi.clearAllMocks();
  browserStore.setState(() => ({ byKey: {} }));
  mockApi.status = "open";
  mockApi.error = null;
  mockApi.frameCbs.clear();
  mockApi.fileChooserCbs.clear();
  mockApi.selectionCbs.clear();
  mockApi.findResultCbs.clear();
  mockApi.getNodeAt.mockClear();
  mockApi.describeNode.mockClear();
  mockUpload.mockClear();
  vi.useFakeTimers();
  // Stub ResizeObserver: capture the callback for manual triggering.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      cb: ResizeObserverCallback;
      constructor(cb: ResizeObserverCallback) {
        this.cb = cb;
        (globalThis as unknown as { __roCb?: ResizeObserverCallback }).__roCb = cb;
      }
      observe() {}
      unobserve() {}
      disconnect() {}
    } as unknown as typeof ResizeObserver,
  );
  // jsdom has no pointer capture; the viewport captures on pointerdown so a
  // drag that leaves the canvas still delivers move/up.
  const elProto = Element.prototype as unknown as Record<string, unknown>;
  elProto.setPointerCapture = vi.fn();
  elProto.releasePointerCapture = vi.fn();
  elProto.hasPointerCapture = vi.fn(() => true);
  // Canvas 2D context stub (jsdom lacks it): drawImage + clearRect tracked.
  const ctx = {
    drawImage: vi.fn(),
    clearRect: vi.fn(),
  };
  vi.stubGlobal("__ctx", ctx);
  const proto = HTMLCanvasElement.prototype as unknown as {
    getContext: (t: string) => unknown;
  };
  proto.getContext = () => ctx;
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function fireFrame(w = 640, h = 480) {
  act(() => {
    for (const cb of mockApi.frameCbs) cb(fakeBitmap(w, h), w, h);
  });
}

describe("ChromeViewport", () => {
  it("renders a canvas + hidden keyboard target + spinner until the first frame", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas");
    expect(canvas).toBeTruthy();
    // Keyboard/IME focus lives on a hidden textarea: a canvas cannot host an
    // input method editor or receive paste events.
    expect(container.querySelector("textarea[data-testid='cdp-keyboard']")).toBeTruthy();
    expect(container.querySelector("[data-testid='cdp-spinner']")).toBeTruthy();
    // First frame clears the spinner and sizes the canvas.
    fireFrame(640, 480);
    expect(container.querySelector("[data-testid='cdp-spinner']")).toBeNull();
    expect((canvas as HTMLCanvasElement).width).toBe(640);
    expect((canvas as HTMLCanvasElement).height).toBe(480);
  });

  it("sends resize on ResizeObserver + dpr", () => {
    render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    vi.stubGlobal("devicePixelRatio", 2);
    const roCb = (globalThis as unknown as { __roCb?: ResizeObserverCallback }).__roCb!;
    act(() => {
      roCb(
        [
          {
            contentRect: { width: 1000, height: 600 },
          } as unknown as ResizeObserverEntry,
        ],
        {} as ResizeObserver,
      );
    });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "resize", w: 1000, h: 600, dpr: 2 });
  });

  it("forwards pointer events as mouse messages and focuses the canvas", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    // jsdom getBoundingClientRect returns zeros — patch to a known rect so
    // the client→canvas mapping is exercised deterministically.
    canvas.getBoundingClientRect = () =>
      ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "down", x: 10, y: 20, button: "left", buttons: 1, clickCount: 1, modifiers: 0,
    });
    expect(document.activeElement).toBe(keyboard);
    // Moves while the button is held carry it: CDP decides "drag in
    // progress" from the move event's button/buttons, not from history.
    mockApi.send.mockClear();
    fireEvent.pointerMove(canvas, { clientX: 30, clientY: 40 });
    act(() => {
      vi.advanceTimersByTime(20);
    });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "move", x: 30, y: 40, button: "left", buttons: 1, clickCount: 0, modifiers: 0,
    });
    // The up event carries the clickCount of the click it completes.
    mockApi.send.mockClear();
    fireEvent.pointerUp(canvas, { clientX: 30, clientY: 40, button: 0, pointerId: 1 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "up", x: 30, y: 40, button: "left", buttons: 0, clickCount: 1, modifiers: 0,
    });
    // pointermove coalesces to one move per animation frame (16ms); with no
    // button held it is a hover.
    mockApi.send.mockClear();
    fireEvent.pointerMove(canvas, { clientX: 11, clientY: 21 });
    fireEvent.pointerMove(canvas, { clientX: 12, clientY: 22 });
    act(() => {
      vi.advanceTimersByTime(20);
    });
    const moves = mockApi.send.mock.calls.filter((c) => (c[0] as { t: string }).t === "mouse" && (c[0] as { kind: string }).kind === "move");
    expect(moves.length).toBe(1);
    expect(moves[0][0]).toMatchObject({ kind: "move", x: 12, y: 22, button: "none", buttons: 0 });
    // wheel
    mockApi.send.mockClear();
    fireEvent.wheel(canvas, { deltaX: 0, deltaY: 120 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "wheel", x: 0, y: 0, deltaX: 0, deltaY: 120, modifiers: 0,
    });
  });

  it("releases a held button on pointercancel and on blur, flushing the pending move first", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    fireEvent.pointerMove(canvas, { clientX: 50, clientY: 60 });
    mockApi.send.mockClear();
    fireEvent.pointerCancel(canvas, { pointerId: 1 });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "mouse", kind: "move", x: 50, y: 60, button: "left", buttons: 1, clickCount: 0, modifiers: 0 },
      { t: "mouse", kind: "up", x: 50, y: 60, button: "left", buttons: 0, clickCount: 1, modifiers: 0 },
    ]);
    // Blur mid-press releases the button and every key still down.
    fireEvent.pointerDown(canvas, { clientX: 100, clientY: 200, button: 2, pointerId: 1 });
    fireEvent.keyDown(keyboard, { key: "Shift", code: "ShiftLeft", shiftKey: true });
    mockApi.send.mockClear();
    fireEvent.blur(keyboard);
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "key", kind: "up", key: "Shift", code: "ShiftLeft", text: "", modifiers: 0 },
      { t: "mouse", kind: "up", x: 100, y: 200, button: "right", buttons: 0, clickCount: 1, modifiers: 0 },
    ]);
    // Nothing held any more: a second blur is silent.
    mockApi.send.mockClear();
    fireEvent.blur(keyboard);
    expect(mockApi.send).not.toHaveBeenCalled();
  });

  it("maps keyboard to CDP key events with modifiers bitmask", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    // Printable: text rides on the down (Chrome inserts it itself); no
    // separate "char" event.
    fireEvent.keyDown(keyboard, { key: "a", code: "KeyA" });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "key", kind: "down", key: "a", code: "KeyA", text: "a", modifiers: 0 },
    ]);
    fireEvent.keyUp(keyboard, { key: "a", code: "KeyA" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "up", key: "a", code: "KeyA", text: "a", modifiers: 0 });
    // Enter: text is \r on down.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Enter", code: "Enter" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "down", key: "Enter", code: "Enter", text: "\r", modifiers: 0 });
    // Backspace: no text; the server adds the virtual key code.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Backspace", code: "Backspace", repeat: true });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "down", key: "Backspace", code: "Backspace", text: "", modifiers: 0, autoRepeat: true });
    // Modifiers: alt=1 ctrl=2 meta=4 shift=8 (CDP)
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Tab", code: "Tab", altKey: true, ctrlKey: true, metaKey: false, shiftKey: true });
    expect(mockApi.send).toHaveBeenCalledWith(
      expect.objectContaining({ t: "key", kind: "down", key: "Tab", modifiers: 1 | 2 | 8 }),
    );
    // Ctrl/Cmd chords carry no text: Chrome would otherwise type the letter.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "a", code: "KeyA", metaKey: true });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "down", key: "a", code: "KeyA", text: "", modifiers: 4 });
  });

  it("bridges copy/cut and paste through the host clipboard", async () => {
    const writeText = vi.fn(async (_t: string) => {});
    vi.stubGlobal("navigator", { ...navigator, platform: "MacIntel", clipboard: { writeText } });
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    // Copy: selection requested BEFORE the key so cut still reports the text
    // it removes; the key itself still goes to the page (its copy handlers).
    fireEvent.keyDown(keyboard, { key: "c", code: "KeyC", metaKey: true });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "getSelection" },
      { t: "key", kind: "down", key: "c", code: "KeyC", text: "", modifiers: 4 },
    ]);
    act(() => {
      for (const cb of mockApi.selectionCbs) cb("copied text");
    });
    expect(writeText).toHaveBeenCalledWith("copied text");
    // Paste: the host paste event carries the clipboard; it becomes an
    // insertText, never a key (Chrome's own clipboard must not be pasted).
    mockApi.send.mockClear();
    const paste = new Event("paste", { bubbles: true, cancelable: true }) as Event & { clipboardData: unknown };
    paste.clipboardData = { getData: (type: string) => (type === "text/plain" ? "from host" : "") };
    act(() => {
      keyboard.dispatchEvent(paste);
    });
    expect(paste.defaultPrevented).toBe(true);
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([{ t: "insertText", text: "from host" }]);
  });

  it("commits IME composition as one insertText and skips composing keys", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    fireEvent.compositionStart(keyboard);
    fireEvent.keyDown(keyboard, { key: "Process", code: "KeyN" });
    fireEvent.keyDown(keyboard, { key: "i", code: "KeyI", isComposing: true });
    fireEvent.keyUp(keyboard, { key: "i", code: "KeyI", isComposing: true });
    expect(mockApi.send).not.toHaveBeenCalled();
    fireEvent.compositionEnd(keyboard, { data: "日本" });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([{ t: "insertText", text: "日本" }]);
    expect(keyboard.value).toBe("");
    // A dead key (Option+E on mac) starts a composition too: not forwarded.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Dead", code: "KeyE", altKey: true });
    expect(mockApi.send).not.toHaveBeenCalled();
  });

  it("translates browser-chrome shortcuts into navigation commands", () => {
    // Non-mac host: Ctrl is primary, Alt+Arrow is history.
    vi.stubGlobal("navigator", { ...navigator, platform: "Win32" });
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "F5", code: "F5" });
    fireEvent.keyDown(keyboard, { key: "ArrowLeft", code: "ArrowLeft", altKey: true });
    fireEvent.keyDown(keyboard, { key: "ArrowRight", code: "ArrowRight", altKey: true });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([{ t: "reload" }, { t: "back" }, { t: "forward" }]);
    // Shift+Alt+Arrow is a page selection chord, not history.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "ArrowLeft", code: "ArrowLeft", altKey: true, shiftKey: true });
    expect(mockApi.send).toHaveBeenCalledWith(expect.objectContaining({ t: "key", key: "ArrowLeft", modifiers: 1 | 8 }));
  });

  it("steps page zoom with Cmd/Ctrl +/-/0 and pinch (ctrl+wheel)", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Win32" });
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "=", code: "Equal", ctrlKey: true });
    fireEvent.keyDown(keyboard, { key: "=", code: "Equal", ctrlKey: true });
    fireEvent.keyDown(keyboard, { key: "-", code: "Minus", ctrlKey: true });
    fireEvent.keyDown(keyboard, { key: "0", code: "Digit0", ctrlKey: true });
    // Reset at 100% is a no-op; zoom keys never reach the page as keys.
    fireEvent.keyDown(keyboard, { key: "0", code: "Digit0", ctrlKey: true });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "zoom", factor: 1.1 },
      { t: "zoom", factor: 1.25 },
      { t: "zoom", factor: 1.1 },
      { t: "zoom", factor: 1 },
    ]);
    // Pinch: ctrl+wheel zooms continuously instead of scrolling.
    mockApi.send.mockClear();
    fireEvent.wheel(canvas, { deltaY: -50, ctrlKey: true });
    const pinch = mockApi.send.mock.calls[0][0] as { t: string; factor: number };
    expect(pinch.t).toBe("zoom");
    expect(pinch.factor).toBeCloseTo(Math.exp(0.5), 5);
    expect(mockApi.send).toHaveBeenCalledTimes(1);
    // Ctrl+= from an off-preset factor snaps to the next preset.
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "=", code: "Equal", ctrlKey: true });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "zoom", factor: 1.75 });
  });

  it("persists zoom across a viewport remount and reacts to an external reset", () => {
    vi.stubGlobal("navigator", { ...navigator, platform: "Win32" });
    browserActions.open("tab:abc");
    const { container, unmount } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    act(() => fireFrame());
    fireEvent.keyDown(keyboard, { key: "=", code: "Equal", ctrlKey: true });
    expect(browserStore.state.byKey["tab:abc"].zoom).toBe(1.1);

    // Remount (e.g. switching tabs, which remounts ChromeViewport by key): a
    // fresh CDP target starts at 100%, so the mount effect must reapply the
    // persisted zoom instead of leaving it at 100% with no explanation.
    unmount();
    mockApi.send.mockClear();
    const { container: c2 } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    act(() => fireFrame());
    expect(mockApi.send).toHaveBeenCalledWith({ t: "zoom", factor: 1.1 });

    // External reset (e.g. the address bar's zoom badge) flows back in too.
    mockApi.send.mockClear();
    act(() => {
      browserActions.setZoom("tab:abc", 1);
    });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "zoom", factor: 1 });
    void c2;
  });

  it("forwards touch contacts as touch events, not mouse presses", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const keyboard = container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());
    mockApi.send.mockClear();
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, pointerId: 7, pointerType: "touch", button: 0 });
    fireEvent.pointerDown(canvas, { clientX: 100, clientY: 200, pointerId: 8, pointerType: "touch", button: 0 });
    fireEvent.pointerMove(canvas, { clientX: 15, clientY: 25, pointerId: 7, pointerType: "touch" });
    fireEvent.pointerMove(canvas, { clientX: 110, clientY: 210, pointerId: 8, pointerType: "touch" });
    act(() => {
      vi.advanceTimersByTime(20);
    });
    fireEvent.pointerUp(canvas, { clientX: 15, clientY: 25, pointerId: 7, pointerType: "touch", button: 0 });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "touch", kind: "start", points: [{ id: 7, x: 10, y: 20 }], modifiers: 0 },
      { t: "touch", kind: "start", points: [{ id: 8, x: 100, y: 200 }], modifiers: 0 },
      // One coalesced move carrying both changed contacts.
      { t: "touch", kind: "move", points: [{ id: 7, x: 15, y: 25 }, { id: 8, x: 110, y: 210 }], modifiers: 0 },
      { t: "touch", kind: "end", points: [{ id: 7, x: 15, y: 25 }], modifiers: 0 },
    ]);
    expect(mockApi.send.mock.calls.every((c) => (c[0] as { t: string }).t !== "mouse")).toBe(true);
    // Blur cancels the contact still down.
    mockApi.send.mockClear();
    fireEvent.blur(keyboard);
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "touch", kind: "cancel", points: [{ id: 8, x: 110, y: 210 }], modifiers: 0 },
    ]);
  });

  it("replays a touch-and-hold as a right-click context menu", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, pointerId: 1, pointerType: "touch", button: 0 });
    mockApi.send.mockClear();
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(mockApi.send.mock.calls.map((c) => c[0])).toEqual([
      { t: "touch", kind: "cancel", points: [{ id: 1, x: 10, y: 20 }], modifiers: 0 },
      { t: "mouse", kind: "down", x: 10, y: 20, button: "right", buttons: 2, clickCount: 1, modifiers: 0 },
      { t: "mouse", kind: "up", x: 10, y: 20, button: "right", buttons: 0, clickCount: 1, modifiers: 0 },
    ]);
    // The contact was already cancelled by the hold; lifting it sends nothing more.
    mockApi.send.mockClear();
    fireEvent.pointerUp(canvas, { clientX: 10, clientY: 20, pointerId: 1, pointerType: "touch", button: 0 });
    expect(mockApi.send).not.toHaveBeenCalled();
  });

  it("does not fire a long-press context menu for a quick tap or a drag/scroll", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());

    // Quick tap: lifted well before the long-press threshold.
    mockApi.send.mockClear();
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, pointerId: 1, pointerType: "touch", button: 0 });
    fireEvent.pointerUp(canvas, { clientX: 10, clientY: 20, pointerId: 1, pointerType: "touch", button: 0 });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(mockApi.send.mock.calls.some((c) => (c[0] as { t: string }).t === "mouse")).toBe(false);

    // Drag/scroll: moves past the tolerance before the threshold elapses.
    mockApi.send.mockClear();
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, pointerId: 2, pointerType: "touch", button: 0 });
    fireEvent.pointerMove(canvas, { clientX: 40, clientY: 20, pointerId: 2, pointerType: "touch" });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    expect(mockApi.send.mock.calls.some((c) => (c[0] as { t: string }).t === "mouse")).toBe(false);
  });

  it("prevents the context menu", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const ev = new Event("contextmenu", { bubbles: true, cancelable: true });
    act(() => canvas.dispatchEvent(ev));
    expect(ev.defaultPrevented).toBe(true);
  });

  it("opens the host menu after resolving the node under the pointer", async () => {
    mockApi.getNodeAt.mockResolvedValue({ nodeId: 7 });
    mockApi.describeNode.mockResolvedValue({
      nodeId: 7,
      nodeName: "A",
      attributes: ["href", "https://linked.example/"],
    });
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;

    await act(async () => {
      fireEvent.contextMenu(canvas, { clientX: 40, clientY: 50 });
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(mockApi.getNodeAt).toHaveBeenCalledWith(40, 50);
    expect(mockApi.describeNode).toHaveBeenCalledWith(7);
    expect(document.body.textContent).toContain("Copy Link or Image");
  });

  it("keeps the menu open with an inline clipboard error", async () => {
    mockApi.getNodeAt.mockResolvedValue({ nodeId: 7 });
    mockApi.describeNode.mockResolvedValue({
      nodeId: 7,
      nodeName: "A",
      attributes: ["href", "https://linked.example/"],
    });
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    await act(async () => {
      fireEvent.contextMenu(canvas, { clientX: 40, clientY: 50 });
      await Promise.resolve();
      await Promise.resolve();
    });
    await act(async () => {
      fireEvent.click(Array.from(document.querySelectorAll("button")).find((button) => button.textContent === "Copy Link or Image")!);
      await Promise.resolve();
    });
    expect(writeText).toHaveBeenCalledWith("https://linked.example/");
    expect(document.body.textContent).toContain("Context lookup unavailable: clipboard write failed");
  });

  it("shows reconnecting pill and error state with open-external", () => {
    mockApi.status = "reconnecting";
    const { container, rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    expect(container.querySelector("[data-testid='cdp-reconnecting']")).toBeTruthy();
    mockApi.status = "closed";
    mockApi.error = "chrome not found — set browser.chrome_path";
    rerender(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    expect(container.textContent).toContain("chrome not found");
    const open = container.querySelector("[data-testid='cdp-open-external']") as HTMLButtonElement;
    expect(open).toBeTruthy();
    const winOpen = vi.fn();
    vi.stubGlobal("open", winOpen);
    fireEvent.click(open);
    expect(winOpen).toHaveBeenCalledWith("https://example.com/", "_blank", "noopener");
  });

  it("sends nav when the user navigates (navSeq bumps with a new url)", () => {
    const { rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/" navSeq={0} />,
    );
    rerender(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://b.com/" navSeq={1} />);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "nav", url: "https://b.com/" });
  });

  it("does NOT re-navigate when only the server-reported url changes (pushState/replaceState)", () => {
    // A page rewriting its own query string (map lat/lng/zoom, tab=...) via
    // history.replaceState surfaces as Page.navigatedWithinDocument → a
    // browse_nav event → a new store url. That is a report, not a request:
    // replaying it as {t:"nav"} would Page.navigate and reload the page on
    // every zoom/draw.
    const { rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/?zoom=15" navSeq={0} />,
    );
    mockApi.send.mockClear();
    rerender(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/?zoom=16" navSeq={0} />);
    expect(mockApi.send).not.toHaveBeenCalledWith(expect.objectContaining({ t: "nav" }));
  });

  it("re-navigates on reload (navSeq bumps with the same url)", () => {
    const { rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/" navSeq={0} />,
    );
    mockApi.send.mockClear();
    rerender(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/" navSeq={1} />);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "nav", url: "https://a.com/" });
  });

  it("navigates on FIRST mount (iframe → chrome escape hatch)", () => {
    // A surface mounted directly onto its final URL (e.g. switching a
    // dev-server page from local proxy to Chrome/CDP mode) must still issue
    // an initial {t:"nav"}; otherwise the CDP target sits on the initial
    // page and renders blank.
    render(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://dev.local/admin" navSeq={0} />);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "nav", url: "https://dev.local/admin" });
  });
});

describe("ChromeViewport file chooser", () => {
  it("opens the hidden picker on fileChooser and uploads the picked files", async () => {
    const clickSpy = vi.spyOn(HTMLInputElement.prototype, "click").mockImplementation(() => {});
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const input = container.querySelector("[data-testid='cdp-file-input']") as HTMLInputElement;
    expect(input).toBeTruthy();
    act(() => {
      for (const cb of mockApi.fileChooserCbs) cb(true);
    });
    expect(input.multiple).toBe(true);
    expect(clickSpy).toHaveBeenCalledTimes(1);

    const file = new File(["hi"], "a.txt", { type: "text/plain" });
    Object.defineProperty(input, "files", { value: [file], configurable: true });
    await act(async () => {
      fireEvent.change(input);
    });
    expect(mockUpload).toHaveBeenCalledTimes(1);
    expect(mockUpload.mock.calls[0][0]).toBe("tab:abc");
    expect(mockUpload.mock.calls[0][1].map((f) => f.name)).toEqual(["a.txt"]);
    expect(mockApi.send).not.toHaveBeenCalledWith({ t: "fileChooserCancel" });
    clickSpy.mockRestore();
  });

  it("reports a dismissed picker as fileChooserCancel", () => {
    vi.spyOn(HTMLInputElement.prototype, "click").mockImplementation(() => {});
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const input = container.querySelector("[data-testid='cdp-file-input']") as HTMLInputElement;
    act(() => {
      for (const cb of mockApi.fileChooserCbs) cb(false);
    });
    expect(input.multiple).toBe(false);
    act(() => {
      fireEvent(input, new Event("cancel", { bubbles: true }));
    });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "fileChooserCancel" });
    expect(mockUpload).not.toHaveBeenCalled();
  });

  it("ignores delayed file chooser events after the surface is backgrounded", () => {
    const clickSpy = vi.spyOn(HTMLInputElement.prototype, "click").mockImplementation(() => {});
    const { rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    rerender(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} active={false} />,
    );
    act(() => {
      for (const cb of mockApi.fileChooserCbs) cb(true);
    });
    expect(clickSpy).not.toHaveBeenCalled();
  });
});

describe("ChromeViewport find-in-page", () => {
  function renderViewport() {
    const utils = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" navSeq={0} />,
    );
    const keyboard = utils.container.querySelector("textarea[data-testid='cdp-keyboard']") as HTMLTextAreaElement;
    return { ...utils, keyboard };
  }
  function emitFindResult(res: { query?: string; found: boolean; active: number; total: number }) {
    act(() => {
      for (const cb of (mockApi as unknown as { findResultCbs: Set<(r: typeof res) => void> }).findResultCbs) cb(res);
    });
  }

  it("opens the find bar on Cmd+F without forwarding the key to the page", () => {
    const { container, keyboard } = renderViewport();
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    expect(container.querySelector("[data-testid='cdp-find-bar']")).toBeTruthy();
    expect(mockApi.send).not.toHaveBeenCalledWith(expect.objectContaining({ t: "key" }));
    // Keyup for the same chord is also swallowed, never forwarded.
    mockApi.send.mockClear();
    fireEvent.keyUp(keyboard, { key: "f", code: "KeyF", metaKey: true });
    expect(mockApi.send).not.toHaveBeenCalledWith(expect.objectContaining({ t: "key" }));
  });

  it("opens on Ctrl+F (non-Mac) and debounces typing into a find request", () => {
    const { container, keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", ctrlKey: true });
    const input = container.querySelector("[data-testid='cdp-find-input']") as HTMLInputElement;
    expect(input).toBeTruthy();
    mockApi.send.mockClear();
    fireEvent.change(input, { target: { value: "hello" } });
    expect(mockApi.send).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "hello", backwards: false, caseSensitive: false });
    emitFindResult({ query: "hello", found: true, active: 1, total: 3 });
    expect(container.querySelector("[data-testid='cdp-find-count']")?.textContent).toBe("1 of 3");
  });

  it("steps next/prev via Enter, F3 and Cmd+G, and shows No results", () => {
    const { container, keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    const input = container.querySelector("[data-testid='cdp-find-input']") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "q" } });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    mockApi.send.mockClear();
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "q", backwards: false, caseSensitive: false });
    mockApi.send.mockClear();
    fireEvent.keyDown(input, { key: "Enter", code: "Enter", shiftKey: true });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "q", backwards: true, caseSensitive: false });
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "F3", code: "F3" });
    // keyboard is blurred while the input is focused in a real browser, but the
    // hidden textarea handler still routes F3 to find-next when the bar is open.
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "q", backwards: false, caseSensitive: false });
    emitFindResult({ query: "q", found: false, active: 0, total: 0 });
    expect(container.querySelector("[data-testid='cdp-find-count']")?.textContent).toBe("No results");
  });

  it("closes on Escape from both input and textarea, clearing remote selection", () => {
    const { container, keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    expect(container.querySelector("[data-testid='cdp-find-bar']")).toBeTruthy();
    const input = container.querySelector("[data-testid='cdp-find-input']") as HTMLInputElement;
    mockApi.send.mockClear();
    fireEvent.keyDown(input, { key: "Escape", code: "Escape" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "findClose" });
    expect(container.querySelector("[data-testid='cdp-find-bar']")).toBeNull();
    // Reopen then close from the hidden textarea.
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    expect(container.querySelector("[data-testid='cdp-find-bar']")).toBeTruthy();
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Escape", code: "Escape" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "findClose" });
    expect(container.querySelector("[data-testid='cdp-find-bar']")).toBeNull();
  });

  it("toggles case sensitivity and re-searches, and next/prev buttons work", () => {
    const { container, keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    const input = container.querySelector("[data-testid='cdp-find-input']") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "Ab" } });
    act(() => {
      vi.advanceTimersByTime(300);
    });
    mockApi.send.mockClear();
    fireEvent.click(container.querySelector("[data-testid='cdp-find-case']") as HTMLElement);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "Ab", backwards: false, caseSensitive: true });
    mockApi.send.mockClear();
    fireEvent.click(container.querySelector("[data-testid='cdp-find-next']") as HTMLElement);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "Ab", backwards: false, caseSensitive: true });
    mockApi.send.mockClear();
    fireEvent.click(container.querySelector("[data-testid='cdp-find-prev']") as HTMLElement);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "find", query: "Ab", backwards: true, caseSensitive: true });
  });

  it("swallows the Enter keyup after find navigation (no stray key up to the page)", () => {
    const { keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "Enter", code: "Enter" });
    fireEvent.keyUp(keyboard, { key: "Enter", code: "Enter" });
    const keys = mockApi.send.mock.calls.filter((c) => (c[0] as { t: string }).t === "key");
    expect(keys).toEqual([]);
  });

  it("swallows the F keyup even when the modifier is released first", () => {
    const { keyboard } = renderViewport();
    mockApi.send.mockClear();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    // Modifier released before the key itself: keyup arrives without metaKey.
    fireEvent.keyUp(keyboard, { key: "f", code: "KeyF" });
    const keys = mockApi.send.mock.calls.filter((c) => (c[0] as { t: string }).t === "key");
    expect(keys).toEqual([]);
  });

  it("explicit Enter navigation supersedes the pending type-debounce (single find)", () => {
    const { container, keyboard } = renderViewport();
    fireEvent.keyDown(keyboard, { key: "f", code: "KeyF", metaKey: true });
    const input = container.querySelector("[data-testid='cdp-find-input']") as HTMLInputElement;
    mockApi.send.mockClear();
    fireEvent.change(input, { target: { value: "hello" } });
    // Before the 250ms debounce fires, the user hits Enter.
    fireEvent.keyDown(input, { key: "Enter", code: "Enter" });
    act(() => {
      vi.advanceTimersByTime(500);
    });
    const finds = mockApi.send.mock.calls.filter((c) => (c[0] as { t: string }).t === "find");
    expect(finds.length).toBe(1);
  });
});
