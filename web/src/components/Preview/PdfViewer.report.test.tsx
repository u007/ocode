import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render as renderView, waitFor } from "@testing-library/react";

// Same pdf.js stand-in as the text layer geometry suite: one page whose text
// layer holds "Alpha beta".
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
    getTextContent: async () => ({
      items: [{ str: "Alpha beta", fontName: "g_d0_f1", transform: [10, 0, 0, 10, 20, 50], width: 100, height: 10 }],
      styles: { g_d0_f1: { fontFamily: "serif", ascent: 0.9, descent: -0.2, vertical: false } },
    }),
  }));
  const doc = { numPages: 1, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  return { renderMock, getPage, doc, getDocument };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  Util: { transform: (m1: number[], m2: number[]) => [m1[0] * m2[0] + m1[2] * m2[1], m1[1] * m2[0] + m1[3] * m2[1], m1[0] * m2[2] + m1[2] * m2[3], m1[1] * m2[2] + m1[3] * m2[3], m1[0] * m2[4] + m1[2] * m2[5] + m1[4], m1[1] * m2[4] + m1[3] * m2[5] + m1[5]] },
  getDocument: mocks.getDocument,
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
  apiPath: (p: string) => p,
}));

import PdfViewer from "./PdfViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

afterEach(() => {
  cleanup();
  resetViewerStates();
});

describe("PdfViewer report", () => {
  it("reports the page, page count and the visible page's text layer", async () => {
    setViewerDoc("s1", { path: "/a.pdf", kind: "pdf" });
    renderView(
      <ViewerReportContext.Provider value="s1">
        <PdfViewer path="/a.pdf" page={1} onPageChange={() => {}} active />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(readViewerState("s1")?.visibleText).toBe("Alpha beta"));
    expect(readViewerState("s1")?.location).toEqual({ page: 1, pageCount: 1 });
  });
});
