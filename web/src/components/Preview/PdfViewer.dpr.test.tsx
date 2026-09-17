import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render as renderView, waitFor } from "@testing-library/react";

// PdfViewer renders pages through pdf.js. Capture the render() args so we can
// assert the backing store is scaled by devicePixelRatio (the blur fix).
const mocks = vi.hoisted(() => {
  const renderMock = vi.fn(() => ({ promise: Promise.resolve() }));
  const getViewport = vi.fn(() => ({
    width: 400,
    height: 500,
    scale: 1.5,
    transform: [1.5, 0, 0, -1.5, 0, 500],
  }));
  const getPage = vi.fn(async () => ({
    getViewport,
    render: renderMock,
    getTextContent: async () => ({ items: [] }),
  }));
  const doc = { numPages: 1, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  return { renderMock, getViewport, getPage, doc, getDocument };
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

function setDpr(value: number) {
  Object.defineProperty(window, "devicePixelRatio", { value, configurable: true, writable: true });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.getDocument.mockImplementation(() => ({ promise: Promise.resolve(mocks.doc) }));
  mocks.getPage.mockImplementation(async () => ({
    getViewport: mocks.getViewport,
    render: mocks.renderMock,
    getTextContent: async () => ({ items: [] }),
  }));
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve() }));
});

afterEach(() => {
  setDpr(1);
});

describe("PdfViewer device-pixel-ratio rendering", () => {
  it("renders the canvas backing store at devicePixelRatio while keeping CSS size 1:1 (retina crispness)", async () => {
    setDpr(2);
    const { container } = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);

    await waitFor(() => expect(container.querySelector("canvas")).toBeTruthy());
    const canvas = container.querySelector("canvas")!;
    await waitFor(() => expect(canvas.width).toBe(800));
    expect(canvas.height).toBe(1000);
    // Displayed at CSS-pixel size so the text layer (1:1 viewport) lines up.
    expect(canvas.style.width).toBe("400px");
    expect(canvas.style.height).toBe("500px");
    // pdf.js composes this transform before the viewport transform.
    expect(mocks.renderMock).toHaveBeenCalledWith(
      expect.objectContaining({ viewport: expect.objectContaining({ width: 400 }), transform: [2, 0, 0, 2, 0, 0] }),
    );
  });

  it("passes an undefined transform and unscaled backing store at dpr 1", async () => {
    setDpr(1);
    const { container } = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);

    await waitFor(() => expect(container.querySelector("canvas")).toBeTruthy());
    const canvas = container.querySelector("canvas")!;
    await waitFor(() => expect(canvas.width).toBe(400));
    expect(canvas.height).toBe(500);
    expect(canvas.style.width).toBe("400px");
    expect(mocks.renderMock).toHaveBeenCalledWith(expect.objectContaining({ transform: undefined }));
  });
});
