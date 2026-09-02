import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, fireEvent, act } from "@testing-library/react";

// jsdom has no PointerEvent (see UnifiedTabBar.drag.test.tsx) — clientX/Y are
// lost without a polyfill, which the viewport's coordinate mapping relies on.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    isPrimary: boolean;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 0;
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
};

const mockUpload = vi.hoisted(() => vi.fn(async (_key: string, _files: File[]) => {}));
vi.mock("../../api/client", () => ({
  uploadBrowseFiles: (key: string, files: File[]) => mockUpload(key, files),
}));

vi.mock("./useCdpSocket", () => ({
  useCdpSocket: () => mockApi,
}));

import { ChromeViewport } from "./ChromeViewport";

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
  mockApi.status = "open";
  mockApi.error = null;
  mockApi.frameCbs.clear();
  mockApi.fileChooserCbs.clear();
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
  it("renders a focusable canvas + spinner until the first frame", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    const canvas = container.querySelector("canvas");
    expect(canvas).toBeTruthy();
    expect(canvas!.getAttribute("tabindex")).toBe("0");
    expect(container.querySelector("[data-testid='cdp-spinner']")).toBeTruthy();
    // First frame clears the spinner and sizes the canvas.
    fireFrame(640, 480);
    expect(container.querySelector("[data-testid='cdp-spinner']")).toBeNull();
    expect((canvas as HTMLCanvasElement).width).toBe(640);
    expect((canvas as HTMLCanvasElement).height).toBe(480);
  });

  it("sends resize on ResizeObserver + dpr", () => {
    render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
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
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    // jsdom getBoundingClientRect returns zeros — patch to a known rect so
    // the client→canvas mapping is exercised deterministically.
    canvas.getBoundingClientRect = () =>
      ({ left: 0, top: 0, width: 1000, height: 600 }) as DOMRect;
    act(() => fireFrame());
    fireEvent.pointerDown(canvas, { clientX: 10, clientY: 20, button: 0, pointerId: 1 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "down", x: 10, y: 20, button: "left", clickCount: 1, modifiers: 0,
    });
    expect(document.activeElement).toBe(canvas);
    // The up event carries the clickCount of the click it completes.
    fireEvent.pointerUp(canvas, { clientX: 10, clientY: 20, button: 0 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "up", x: 10, y: 20, button: "left", clickCount: 1, modifiers: 0,
    });
    // pointermove coalesces to one move per animation frame (16ms).
    mockApi.send.mockClear();
    fireEvent.pointerMove(canvas, { clientX: 11, clientY: 21 });
    fireEvent.pointerMove(canvas, { clientX: 12, clientY: 22 });
    act(() => {
      vi.advanceTimersByTime(20);
    });
    const moves = mockApi.send.mock.calls.filter((c) => (c[0] as { t: string }).t === "mouse" && (c[0] as { kind: string }).kind === "move");
    expect(moves.length).toBe(1);
    expect(moves[0][0]).toMatchObject({ kind: "move", x: 12, y: 22 });
    // wheel
    mockApi.send.mockClear();
    fireEvent.wheel(canvas, { deltaX: 0, deltaY: 120 });
    expect(mockApi.send).toHaveBeenCalledWith({
      t: "mouse", kind: "wheel", x: 0, y: 0, deltaX: 0, deltaY: 120, modifiers: 0,
    });
  });

  it("maps keyboard to CDP key events with modifiers bitmask", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    act(() => fireFrame());
    mockApi.send.mockClear();
    fireEvent.keyDown(canvas, { key: "a", code: "KeyA" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "down", key: "a", code: "KeyA", text: "a", modifiers: 0 });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "char", key: "a", code: "KeyA", text: "a", modifiers: 0 });
    fireEvent.keyUp(canvas, { key: "a", code: "KeyA" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "up", key: "a", code: "KeyA", text: "a", modifiers: 0 });
    // Enter: text is \r on down, no char
    mockApi.send.mockClear();
    fireEvent.keyDown(canvas, { key: "Enter", code: "Enter" });
    expect(mockApi.send).toHaveBeenCalledWith({ t: "key", kind: "down", key: "Enter", code: "Enter", text: "\r", modifiers: 0 });
    // Modifiers: alt=1 ctrl=2 meta=4 shift=8 (CDP)
    mockApi.send.mockClear();
    fireEvent.keyDown(canvas, { key: "Tab", code: "Tab", altKey: true, ctrlKey: true, metaKey: false, shiftKey: true });
    expect(mockApi.send).toHaveBeenCalledWith(
      expect.objectContaining({ t: "key", kind: "down", key: "Tab", modifiers: 1 | 2 | 8 }),
    );
  });

  it("prevents the context menu", () => {
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    const canvas = container.querySelector("canvas") as HTMLCanvasElement;
    const ev = new Event("contextmenu", { bubbles: true, cancelable: true });
    canvas.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
  });

  it("shows reconnecting pill and error state with open-external", () => {
    mockApi.status = "reconnecting";
    const { container, rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    expect(container.querySelector("[data-testid='cdp-reconnecting']")).toBeTruthy();
    mockApi.status = "closed";
    mockApi.error = "chrome not found — set browser.chrome_path";
    rerender(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
    );
    expect(container.textContent).toContain("chrome not found");
    const open = container.querySelector("[data-testid='cdp-open-external']") as HTMLButtonElement;
    expect(open).toBeTruthy();
    const winOpen = vi.fn();
    vi.stubGlobal("open", winOpen);
    fireEvent.click(open);
    expect(winOpen).toHaveBeenCalledWith("https://example.com/", "_blank", "noopener");
  });

  it("sends nav when the url prop changes", () => {
    const { rerender } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://a.com/" />,
    );
    rerender(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://b.com/" />);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "nav", url: "https://b.com/" });
  });

  it("navigates on FIRST mount (iframe → chrome escape hatch)", () => {
    // A surface mounted directly onto its final URL (e.g. switching a
    // dev-server page from local proxy to Chrome/CDP mode) must still issue
    // an initial {t:"nav"}; otherwise the CDP target sits on the initial
    // page and renders blank.
    render(<ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://dev.local/admin" />);
    expect(mockApi.send).toHaveBeenCalledWith({ t: "nav", url: "https://dev.local/admin" });
  });
});

describe("ChromeViewport file chooser", () => {
  it("opens the hidden picker on fileChooser and uploads the picked files", async () => {
    const clickSpy = vi.spyOn(HTMLInputElement.prototype, "click").mockImplementation(() => {});
    const { container } = render(
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
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
      <ChromeViewport stateKey="tab:abc" browseBase="http://b" url="https://example.com/" />,
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
});
