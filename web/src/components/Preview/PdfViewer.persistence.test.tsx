import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";

// Viewer-state persistence: a project switch UNMOUNTS the sidebar's viewer (the
// panel is keyed by the active session tab), so the PDF must resume at the
// stored zoom and scroll offset instead of resetting to 100% / page 1.
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
import { savePreviewViewState, previewViewKey } from "../../lib/previewViewState";

const PAGE_H = 500;
const PAGE_GAP = 16;

async function mount(view: { container: HTMLElement }, expectedWidth = 400) {
  await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(12));
  await waitFor(() => expect((view.container.querySelectorAll("canvas")[0] as HTMLCanvasElement).width).toBe(expectedWidth));
  return view.container.querySelector('[aria-label="PDF pages"]') as HTMLElement;
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
});
afterEach(() => cleanup());

describe("PdfViewer view-state persistence", () => {
  it("restores the stored zoom on mount instead of resetting to 100%", async () => {
    savePreviewViewState(previewViewKey("doc.pdf", "/proj"), { zoom: 1.25 });
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj" page={1} onPageChange={() => {}} />);
    // Zoom 1.25 → the window rasterizes at scale 1.875 (base width 400 × 1.25 = 500).
    await mount(view, 500);
    expect(view.container.textContent).toContain("125%");
  });

  it("restores the stored scroll offset when it lands within the target page", async () => {
    // Page 3's band at zoom 1: top = 2×(500+16)+8 = 1040.
    const page3Top = 2 * (PAGE_H + PAGE_GAP) + 8;
    savePreviewViewState(previewViewKey("doc.pdf", "/proj"), { page: 3, scrollTop: page3Top + 120 });
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj" page={3} onPageChange={() => {}} />);
    const scroller = await mount(view);
    // The restore runs once geometry exists; give it a tick. Do NOT stamp
    // scrollTop afterwards — that would erase what the restore set.
    await waitFor(() => expect(scroller.scrollTop).toBe(page3Top + 120));
  });


  it("persists zoom and scroll as the reader zooms in", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj" page={1} onPageChange={() => {}} />);
    await mount(view);
    fireEvent.click(view.getByLabelText("Zoom in"));
    // Debounced save (250ms).
    await waitFor(() => {
      const raw = window.localStorage.getItem("ocode.ui.previewViewState.v1");
      expect(raw).toBeTruthy();
      const entry = JSON.parse(raw as string)[previewViewKey("doc.pdf", "/proj")];
      expect(entry.zoom).toBe(1.25);
    });
  });

  it("keys state by host+root so two projects' same-path files do not share a position", async () => {
    savePreviewViewState(previewViewKey("doc.pdf", "/proj-a"), { zoom: 1.25 });
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj-b" page={1} onPageChange={() => {}} />);
    await mount(view);
    // /proj-b has no stored state → default zoom.
    expect(view.container.textContent).toContain("100%");
  });

  it("persists the scroll offset on the debounced save", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    scroller.scrollTop = 520;
    fireEvent.scroll(scroller);
    await waitFor(() => {
      const raw = window.localStorage.getItem("ocode.ui.previewViewState.v1");
      expect(raw).toBeTruthy();
      const entry = JSON.parse(raw as string)[previewViewKey("doc.pdf", "/proj")];
      expect(entry.scrollTop).toBe(520);
    });
  });

  it("flushes a pending debounced save on unmount", async () => {
    const view = renderView(<PdfViewer path="doc.pdf" projectRoot="/proj" page={1} onPageChange={() => {}} />);
    const scroller = await mount(view);
    scroller.scrollTop = 640;
    fireEvent.scroll(scroller);
    // Unmount BEFORE the 250ms debounce fires: the cleanup must flush, not drop.
    view.unmount();
    const raw = window.localStorage.getItem("ocode.ui.previewViewState.v1");
    expect(raw).toBeTruthy();
    const entry = JSON.parse(raw as string)[previewViewKey("doc.pdf", "/proj")];
    expect(entry.scrollTop).toBe(640);
  });
});
