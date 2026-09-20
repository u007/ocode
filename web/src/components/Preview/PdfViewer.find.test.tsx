import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";

// Find toolbar: query → full-document regex (case/whole-word toggles), match
// walking with wrapping, highlight rects measured from the live text layer,
// and shortcuts (Ctrl/⌘F open, F3/Shift+F3 walk, Esc close).
//
// The stubbed page text is "Alpha beta" + " gamma betaNdelta" per page (page
// 2's second item is "BetA"), so case-insensitive "beta" finds 2 matches/page
// (6 over 3 pages), case-sensitive finds 5 ("BetA" excluded), and whole-word
// case-sensitive finds 3 ("beta1delta"/"beta3delta" fail the \b boundary —
// digit is not a word char).
const mocks = vi.hoisted(() => {
  const renderMock = vi.fn(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  const getPage = vi.fn(async (n: number) => ({
    getViewport: ({ scale }: { scale: number }) => ({
      width: 400 * (scale / 1.5),
      height: 500 * (scale / 1.5),
      scale,
      transform: [scale, 0, 0, -scale, 0, 500 * (scale / 1.5)],
    }),
    rotate: 0,
    render: renderMock,
    getTextContent: async () => ({
      items: [
        { str: "Alpha beta", transform: [10, 0, 0, 10, 0, 0], width: 100, height: 10 },
        { str: n === 2 ? ` gamma BetA ${n}delta` : ` gamma beta${n}delta`, transform: [10, 0, 0, 10, 0, 50], width: 100, height: 10 },
      ],
    }),
  }));
  const doc = { numPages: 3, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  return { renderMock, getPage, doc, getDocument };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  // Real pdf.js matrix product — the viewer transforms text items with this
  // to place spans, so a vi.fn() stub would crash the text-layer build.
  Util: { transform: (m1: number[], m2: number[]) => [m1[0] * m2[0] + m1[2] * m2[1], m1[1] * m2[0] + m1[3] * m2[1], m1[0] * m2[2] + m1[2] * m2[3], m1[1] * m2[2] + m1[3] * m2[3], m1[0] * m2[4] + m1[2] * m2[5] + m1[4], m1[1] * m2[4] + m1[3] * m2[5] + m1[5]] },
  getDocument: mocks.getDocument,
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
  apiPath: (p: string) => p,
}));

import PdfViewer from "./PdfViewer";

async function mount(view: { container: HTMLElement }) {
  await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(3));
  await waitFor(() => expect(view.container.querySelectorAll("canvas")[0].width).toBe(400));
  return view.container.querySelector('[aria-label="PDF pages"]') as HTMLElement;
}

function setScrollMetrics(el: HTMLElement, top: number, height: number) {
  Object.defineProperty(el, "scrollTop", { value: top, configurable: true, writable: true });
  Object.defineProperty(el, "clientHeight", { value: height, configurable: true });
}

/** jsdom's Range.getClientRects is undefined; stub one rect per span text. */
function stubRangeRects(rect: { left: number; top: number; width: number; height: number }) {
  const proto = Range.prototype as unknown as { getClientRects?: () => DOMRectList; getBoundingClientRect?: () => DOMRect };
  const nativeRects = proto.getClientRects;
  const nativeBox = proto.getBoundingClientRect;
  proto.getClientRects = function (this: Range) {
    return {
      length: 1,
      item: () => null,
      [Symbol.iterator]: [][Symbol.iterator],
      0: { ...rect, x: rect.left, y: rect.top, right: rect.left + rect.width, bottom: rect.top + rect.height, toJSON: () => ({}) },
    } as unknown as DOMRectList;
  };
  proto.getClientRects.toString = () => "function getClientRects() { [native code] }";
  proto.getBoundingClientRect = function (this: Range) {
    return { ...rect, x: rect.left, y: rect.top, right: rect.left + rect.width, bottom: rect.top + rect.height, toJSON: () => ({}) } as DOMRect;
  };
  return () => {
    if (nativeRects) proto.getClientRects = nativeRects;
    else delete proto.getClientRects;
    if (nativeBox) proto.getBoundingClientRect = nativeBox;
    else delete proto.getBoundingClientRect;
  };
}

/** Stamp a fixed bounding box on every canvas + text layer (jsdom has none). */
function stubLayerRects(container: HTMLElement) {
  const box = { left: 0, top: 0, width: 400, height: 500, x: 0, y: 0, right: 400, bottom: 500, toJSON: () => ({}) };
  for (const el of container.querySelectorAll("canvas, .pdf-text-layer")) {
    Object.defineProperty(el, "getBoundingClientRect", { value: () => box as DOMRect, configurable: true });
  }
}

beforeEach(() => {
  vi.clearAllMocks();
  // The viewer persists zoom/scroll per file; a case must not inherit the
  // previous one's position (all cases reuse path="doc.pdf").
  localStorage.clear();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
});

afterEach(() => cleanup());

describe("PdfViewer find", () => {
  it("searches the whole document and walks matches with wrapping", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);

    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });

    // 2 case-insensitive matches per page × 3 pages.
    await waitFor(() => expect(view.container.textContent).toContain("1/6"));

    fireEvent.click(view.getByLabelText("Next match"));
    expect(view.container.textContent).toContain("2/6");
    fireEvent.click(view.getByLabelText("Previous match"));
    expect(view.container.textContent).toContain("1/6");
    // Wrap backwards from match 1 → match 6.
    fireEvent.click(view.getByLabelText("Previous match"));
    expect(view.container.textContent).toContain("6/6");
  });

  it("match case and whole word toggles change the result set", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);
    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/6"));

    // Case-sensitive: page 2's second occurrence is "BetA", so only 5 of the
    // 6 case-insensitive matches survive (2+1+2).
    fireEvent.click(view.getByLabelText("Match case"));
    await waitFor(() => expect(view.container.textContent).toContain("/5"));

    // Whole word on top of case-sensitive: "beta1delta"/"beta3delta" fail the
    // \b boundary (digit is not a word char), leaving the standalone "Alpha
    // beta" on each page → 3.
    fireEvent.click(view.getByLabelText("Whole word"));
    // Re-search resets the walk to the first match; 3 remain.
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));
  });

  it("highlight rects land on the matching pages and the current match is marked", async () => {
    const onPageChange = vi.fn();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={onPageChange} />);
    const scroller = await mount(view);
    setScrollMetrics(scroller, 0, 800);
    stubLayerRects(view.container);
    const restore = stubRangeRects({ left: 50, top: 100, width: 30, height: 12 });

    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) => el.className.includes("bg-yellow-300/45")).length).toBeGreaterThan(0));

    // The first match (page 1) is the current one: orange, not yellow.
    expect(view.container.querySelector(".bg-orange-500\\/60")).toBeTruthy();

    // Walking to match 3 (page 2) scrolls to page 2 and reports it.
    fireEvent.click(view.getByLabelText("Next match"));
    fireEvent.click(view.getByLabelText("Next match"));
    await waitFor(() => expect(view.container.textContent).toContain("2 / 3"));
    await waitFor(() => expect(onPageChange).toHaveBeenLastCalledWith(2));
    restore();
  });

  it("closing the bar drops all highlights", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);
    stubLayerRects(view.container);
    const restore = stubRangeRects({ left: 50, top: 100, width: 30, height: 12 });

    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) => el.className.includes("bg-yellow-300/45")).length).toBeGreaterThan(0));

    fireEvent.click(view.getByLabelText("Close find"));
    expect(Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) => el.className.includes("bg-yellow-300/45")).length).toBe(0);
    expect(Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) => el.className.includes("bg-orange-500/60")).length).toBe(0);
    restore();
  });

  it("Ctrl/⌘F opens, F3 walks, Esc closes", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await mount(view);
    expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull();

    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("6"));

    fireEvent.keyDown(window, { key: "F3" });
    await waitFor(() => expect(view.container.textContent).toContain("2/6"));
    fireEvent.keyDown(window, { key: "F3", shiftKey: true });
    await waitFor(() => expect(view.container.textContent).toContain("1/6"));

    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull());
  });

  it("a hidden (inactive) viewer ignores the global find shortcuts", async () => {
    // Every visited pane stays mounted across tabs/projects; only the visible
    // one may claim Ctrl+F, or one keypress would flip find open on all of them.
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active={false} />);
    // Do NOT use mount(): a hidden viewer deliberately never rasterizes, so
    // there is no canvas width to wait for.
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(3));

    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    await waitFor(() => expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull());

    // Show it: the shortcut works again.
    view.rerender(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active />);
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    await waitFor(() => expect(view.container.querySelector('input[aria-label="Find text"]')).not.toBeNull());
  });

  it("releases its rendered canvases while inactive and rebuilds on show", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active />);
    await mount(view);
    await waitFor(() => expect((view.container.querySelectorAll("canvas")[0] as HTMLCanvasElement).width).toBe(400));

    view.rerender(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active={false} />);
    await waitFor(() => {
      const widths = Array.from(view.container.querySelectorAll("canvas")).map((c) => (c as HTMLCanvasElement).width);
      expect(widths.every((w) => w === 0)).toBe(true);
    });

    view.rerender(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active />);
    await waitFor(() => expect((view.container.querySelectorAll("canvas")[0] as HTMLCanvasElement).width).toBe(400));
  });

  it("does not rasterize a hidden pane at mount", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} active={false} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(3));
    // Give the (immediate) doc load + placement a chance to run; ensureWindow
    // must refuse to rasterize while inactive.
    await new Promise((r) => setTimeout(r, 30));
    // The <canvas> default is 300; a rasterized page is 400 (base width).
    const widths = Array.from(view.container.querySelectorAll("canvas")).map((c) => (c as HTMLCanvasElement).width);
    expect(widths.some((w) => w === 400)).toBe(false);
  });
});