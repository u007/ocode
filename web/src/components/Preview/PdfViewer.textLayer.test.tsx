import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render as renderView, waitFor } from "@testing-library/react";

// Text-layer geometry: the selectable spans must sit on the rasterized
// glyphs. pdf.js reports a text item's origin at its *baseline*, so a span
// placed at that y sits one font-height below the ink; the span's top must be
// the baseline minus the font ascent. The font family is copied from the
// item's style so the DOM text advances like the raster, and the span is
// stretched (transform-origin 0 0) to the item's PDF width.
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

afterEach(() => cleanup());

describe("PdfViewer text layer geometry", () => {
  it("places each span at baseline minus ascent, in the item's font, stretched from its origin", async () => {
    const view = renderView(<PdfViewer path="/a.pdf" page={1} onPageChange={() => {}} active />);
    const span = await waitFor(() => {
      const s = view.container.querySelector(".pdf-text-layer span") as HTMLElement | null;
      expect(s).not.toBeNull();
      return s as HTMLElement;
    });
    // viewport [1.5,0,0,-1.5,0,500] × item [10,0,0,10,20,50] → tx = [15,0,0,-15,30,425]
    // fontHeight = hypot(0,-15) = 15; jsdom has no canvas metrics, so the
    // ascent ratio comes from the style (0.9): top = 425 - 13.5.
    expect(span.style.left).toBe("30px");
    expect(span.style.top).toBe("411.5px");
    expect(span.style.fontSize).toBe("15px");
    expect(span.style.fontFamily).toBe("serif");
    expect(span.style.transformOrigin).toBe("0 0");
  });
});
