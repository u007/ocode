import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";

// Continuous-scroll behavior: the viewer stacks every page in one scroller,
// rasterizes a window around the visible page, releases pages that fall behind,
// and reports the visible page to the host (pager + "p.N" citation label).
const mocks = vi.hoisted(() => {
  const renderMock = vi.fn(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  const getPage = vi.fn(async () => ({
    getViewport: () => ({ width: 400, height: 500, scale: 1.5, transform: [1.5, 0, 0, -1.5, 0, 500] }),
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

/** Stamp scroll geometry onto an element (jsdom has no layout). */
function setScrollMetrics(el: HTMLElement, top: number, height: number) {
  Object.defineProperty(el, "scrollTop", { value: top, configurable: true, writable: true });
  Object.defineProperty(el, "clientHeight", { value: height, configurable: true });
}

async function mount(container: HTMLElement) {
  const scroller = container.querySelector('[aria-label="PDF pages"]') as HTMLElement;
  const canvases = () => Array.from(container.querySelectorAll("canvas"));
  await waitFor(() => expect(canvases().length).toBe(12));
  // Page 1 (and the ±2 window) rasterize once the geometry is known.
  await waitFor(() => expect(canvases()[0].width).toBe(400));
  await waitFor(() => expect(canvases()[2].width).toBe(400));
  return { scroller, canvases };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  mocks.getPage.mockImplementation(async () => ({
    getViewport: () => ({ width: 400, height: 500, scale: 1.5, transform: [1.5, 0, 0, -1.5, 0, 500] }),
    render: mocks.renderMock,
    getTextContent: async () => ({ items: [] }),
  }));
  mocks.getDocument.mockImplementation(() => ({ promise: Promise.resolve(mocks.doc) }));
});

afterEach(() => cleanup());

describe("PdfViewer continuous scroll", () => {
  it("stacks every page and rasterizes only the window around the visible page", async () => {
    const { container } = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const { canvases } = await mount(container);

    // Every page gets a canvas element up front (geometry-stable scrollbar),
    // but only pages 1-3 were drawn: 1 ± RENDER_AHEAD.
    expect(canvases().length).toBe(12);
    expect(mocks.renderMock).toHaveBeenCalledTimes(3);
    expect(canvases()[3].width).toBe(300); // untouched default backing store
  });

  it("reports the page in view as the user scrolls", async () => {
    const onPageChange = vi.fn();
    const { container } = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={onPageChange} />);
    const { scroller } = await mount(container);

    // Probe sits 35% down the viewport: scrollTop 250 with clientHeight 800
    // → probe 522, past page 2's top (500 + 16) but before page 3's (1032).
    setScrollMetrics(scroller, 250, 800);
    fireEvent.scroll(scroller);

    expect(onPageChange).toHaveBeenLastCalledWith(2);
    expect(container.textContent).toContain("2 / 12");
  });

  it("pager scrolls to the next page and releases pages left far behind", async () => {
    const onPageChange = vi.fn();
    const { container } = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={onPageChange} />);
    const { scroller, canvases } = await mount(container);

    setScrollMetrics(scroller, 0, 800);
    // Jump to the last page (offset 11 × (500 + 16) = 5676).
    setScrollMetrics(scroller, 11 * (PAGE_H + PAGE_GAP), 800);
    fireEvent.scroll(scroller);

    expect(onPageChange).toHaveBeenLastCalledWith(12);
    await waitFor(() => expect(canvases()[11].width).toBe(400));
    // Pages 1-3 are > KEEP_ALIVE (6) away → backing stores released.
    await waitFor(() => expect(canvases()[0].width).toBe(0));
    expect(canvases()[1].width).toBe(0);
    expect(canvases()[2].width).toBe(0);

    // The pager still navigates: click "Next page" from a fresh viewer and the
    // counter moves even though the host never feeds `page` back.
    cleanup();
    const again = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    const mounted = await mount(again.container);
    fireEvent.click(again.getByLabelText("Next page"));
    expect(again.container.textContent).toContain("2 / 12");
    expect(mounted.scroller.scrollTop).toBe(PAGE_H + PAGE_GAP + 8);
  });
});
