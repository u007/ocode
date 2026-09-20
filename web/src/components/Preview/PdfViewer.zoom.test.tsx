import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";

// Zoom + space interactions: zoom state re-scales the layout from
// zoom-invariant base dims, re-renders the window at the new scale, remaps
// scroll around the anchor (cursor for wheel zoom, reading position for keys),
// and Space turns the wheel into zoom-at-cursor and left-drag into panning.
const mocks = vi.hoisted(() => {
  const renderMock = vi.fn(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  const getPage = vi.fn(async () => ({
    getViewport: ({ scale }: { scale: number }) => ({
      width: 400 * (scale / 1.5),
      height: 500 * (scale / 1.5),
      scale,
      transform: [scale, 0, 0, -scale, 0, 500 * (scale / 1.5)],
    }),
    rotate: 0,
    render: renderMock,
    getTextContent: async () => ({ items: [] }),
  }));
  const doc = { numPages: 12, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  return { renderMock, getPage, doc, getDocument };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  Util: { transform: vi.fn() },
  getDocument: mocks.getDocument,
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
  apiPath: (p: string) => p,
}));

import PdfViewer from "./PdfViewer";

const PAGE_H = 500;
const PAGE_GAP = 16;

/** jsdom has no PointerEvent: dispatch a MouseEvent so button/clientX exist. */
function firePointer(el: HTMLElement, type: "down" | "move" | "up" | "cancel", x: number, y: number) {
  fireEvent(el, new MouseEvent(`pointer${type}`, { bubbles: true, cancelable: true, button: 0, clientX: x, clientY: y }));
}

function setScrollMetrics(el: HTMLElement, top: number, height: number) {
  Object.defineProperty(el, "scrollTop", { value: top, configurable: true, writable: true });
  Object.defineProperty(el, "clientHeight", { value: height, configurable: true });
}

async function mount(result: { container: HTMLElement; getByLabelText: (id: string) => HTMLElement }) {
  await waitFor(() => expect(result.container.querySelectorAll("canvas").length).toBe(12));
  await waitFor(() => expect(result.container.querySelectorAll("canvas")[0].width).toBe(400));
  return result.container.querySelector('[aria-label="PDF pages"]') as HTMLElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  // The viewer persists zoom/scroll per file; a case must not inherit the
  // previous one's position (all cases reuse path="doc.pdf").
  localStorage.clear();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
});

afterEach(() => cleanup());

describe("PdfViewer zoom", () => {
  it("zoom-in button re-renders the window at the larger scale and shows the % label", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);

    fireEvent.click(view.getByLabelText("Zoom in"));
    expect(view.container.textContent).toContain("125%");
    // Page 1's canvas is CSS-scaled from base dims (400×500) × 1.25, and the
    // window re-rasterizes at the larger viewport (scale 1.5 × 1.25 = 1.875).
    const canvas = view.container.querySelectorAll("canvas")[0] as HTMLCanvasElement;
    await waitFor(() => expect(canvas.style.width).toBe("500px"));
    await waitFor(() =>
      expect(mocks.renderMock).toHaveBeenCalledWith(expect.objectContaining({ viewport: expect.objectContaining({ width: 500, height: 625 }) })),
    );
    expect(canvas.width).toBe(500);
  });

  it("zoom clamps at 25% and 400%", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);
    for (let i = 0; i < 12; i++) fireEvent.click(view.getByLabelText("Zoom out"));
    await waitFor(() => expect(view.container.textContent).toContain("25%"));
    for (let i = 0; i < 24; i++) fireEvent.click(view.getByLabelText("Zoom in"));
    await waitFor(() => expect(view.container.textContent).toContain("400%"));
  });

  it("Ctrl/⌘+wheel zooms around the cursor point", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    // Cursor 100px down inside the viewport; content point 130 (scrollTop 30).
    Object.defineProperty(scroller, "getBoundingClientRect", {
      value: () => ({ left: 0, top: 0, width: 800, height: 800 }) as DOMRect,
      configurable: true,
    });
    setScrollMetrics(scroller, 30, 800);

    Object.defineProperty(scroller, "scrollLeft", { value: 60, configurable: true, writable: true });
    fireEvent.wheel(scroller, { deltaY: -120, ctrlKey: true, clientX: 50, clientY: 100 });

    // factor = exp(120/450) ≈ 1.306 → 100% ≈ 131%.
    await waitFor(() => expect(view.container.textContent).toMatch(/13[01]/));
    // Anchor: content point stays put on BOTH axes —
    // top: (30+100) × ratio − 100 ≈ 70; left: (60+50) × ratio − 50 ≈ 94.
    await waitFor(() => expect(scroller.scrollTop).toBeCloseTo(130 * Math.exp(120 / 450) - 100, 0));
    await waitFor(() => expect(scroller.scrollLeft).toBeCloseTo(110 * Math.exp(120 / 450) - 50, 0));
  });

  it("plain wheel is untouched (no zoom, browser scrolls)", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    setScrollMetrics(scroller, 30, 800);
    fireEvent.wheel(scroller, { deltaY: -500 });
    expect(view.container.textContent).toContain("100%");
  });

  it("Space+wheel zooms at the cursor like ctrl+wheel", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    Object.defineProperty(scroller, "getBoundingClientRect", {
      value: () => ({ left: 0, top: 0, width: 800, height: 800 }) as DOMRect,
      configurable: true,
    });
    setScrollMetrics(scroller, 30, 800);

    scroller.focus();
    fireEvent.keyDown(scroller, { key: " ", code: "Space" });
    expect(scroller.className).toContain("cursor-grab");

    fireEvent.wheel(scroller, { deltaY: -120, clientX: 50, clientY: 100 });
    await waitFor(() => expect(view.container.textContent).toMatch(/13[01]/));
    await waitFor(() => expect(scroller.scrollTop).toBeCloseTo(130 * Math.exp(120 / 450) - 100, 0));

    fireEvent.keyUp(scroller, { key: " ", code: "Space" });
    expect(scroller.className).not.toContain("cursor-grab");
  });

  it("Space+drag pans by the pointer delta", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    Object.defineProperty(scroller, "scrollLeft", { value: 40, configurable: true, writable: true });
    setScrollMetrics(scroller, 100, 800);

    scroller.focus();
    fireEvent.keyDown(scroller, { key: " ", code: "Space" });
    firePointer(scroller, "down", 200, 300);
    expect(scroller.className).toContain("cursor-grabbing");

    firePointer(scroller, "move", 150, 220);
    // Pan: scroll follows −(delta) → left 40+50=90, top 100+80=180.
    expect(scroller.scrollLeft).toBe(90);
    expect(scroller.scrollTop).toBe(180);

    firePointer(scroller, "up", 150, 220);
    expect(scroller.className).not.toContain("cursor-grabbing");
  });

  it("keyboard +/−/0 zoom with Home/End paging", async () => {
    const onPageChange = vi.fn();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={onPageChange} />);
    const scroller = await mount(view);
    scroller.focus();

    fireEvent.keyDown(scroller, { key: "+" });
    await waitFor(() => expect(view.container.textContent).toContain("125%"));
    fireEvent.keyDown(scroller, { key: "0", ctrlKey: true });
    await waitFor(() => expect(view.container.textContent).toContain("100%"));
    fireEvent.keyDown(scroller, { key: "-" });
    await waitFor(() => expect(view.container.textContent).toContain("80%"));

    // At 80% zoom the stack is 0.8×: pages shrink, the gap does not.
    setScrollMetrics(scroller, 0, 800);
    fireEvent.keyDown(scroller, { key: "End" });
    expect(onPageChange).toHaveBeenLastCalledWith(12);
    // offsets step = page_h*0.8 + full gap, 11 steps to page 12's top.
    expect(scroller.scrollTop).toBe(Math.round(11 * (0.8 * PAGE_H + PAGE_GAP)) + 8);
    fireEvent.keyDown(scroller, { key: "Home" });
    expect(onPageChange).toHaveBeenLastCalledWith(1);
  });

  it("keyboard zoom preserves the reading position (viewport top anchor)", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    setScrollMetrics(scroller, 250, 800);
    scroller.focus();
    fireEvent.keyDown(scroller, { key: "+" });
    // Top-left anchor: scrollTop scales by the ratio, cursor offset 0.
    await waitFor(() => expect(scroller.scrollTop).toBeCloseTo(250 * 1.25, 0));
  });
});