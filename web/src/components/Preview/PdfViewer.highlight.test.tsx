import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";
import { PDFDocument } from "pdf-lib";
import { sha256Hex } from "./pdfAnnotate";

// Highlight selected text: the reader maps the selection's client rects onto the
// rendered page canvas, converts them to PDF points through pdf.js, and saves a
// copy of the file against the hash of the bytes it loaded. jsdom has no layout
// or Range.getClientRects, so the geometry is stubbed where a save is expected.

const mocks = vi.hoisted(() => {
  const renderMock = vi.fn((_args: unknown) => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  const getPage = vi.fn(async () => ({
    getViewport: ({ scale }: { scale: number }) => ({
      width: 400 * (scale / 1.5),
      height: 500 * (scale / 1.5),
      scale,
      transform: [scale, 0, 0, -scale, 0, 500 * (scale / 1.5)],
      // Maps the viewport's pixel space back to PDF points; the real mapping is
      // exercised by the pdfAnnotate tests, this only has to be invertible.
      convertToPdfPoint: (x: number, y: number) => [x, 500 - y],
    }),
    rotate: 0,
    render: renderMock,
    cleanup: vi.fn(() => true),
    getTextContent: async () => ({ items: [] }),
  }));
  const doc = { numPages: 2, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  const fetchFileRaw = vi.fn();
  const saveFileRaw = vi.fn();
  return { renderMock, getPage, doc, getDocument, fetchFileRaw, saveFileRaw };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  Util: { transform: vi.fn() },
  getDocument: mocks.getDocument,
}));

vi.mock("../../api/client", () => ({
  api: {
    fetchFileRaw: (...args: unknown[]) => mocks.fetchFileRaw(...args),
    saveFileRaw: (...args: unknown[]) => mocks.saveFileRaw(...args),
  },
}));

import PdfViewer from "./PdfViewer";

async function makePdfBytes(): Promise<Uint8Array> {
  const doc = await PDFDocument.create();
  doc.addPage([400, 500]);
  doc.addPage([400, 500]);
  return doc.save();
}

/** Restores any prototype a test replaced, so one case cannot leak geometry into the next. */
const restorers: (() => void)[] = [];

beforeEach(() => {
  vi.clearAllMocks();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
});

afterEach(() => {
  while (restorers.length) restorers.pop()?.();
  cleanup();
});

/** Selects the whole scroll container so the selection lies inside the viewer,
 *  and stubs the geometry a browser would provide: canvases at a fixed box, and
 *  the selection rects. */
function stubLayoutAndSelect(view: ReturnType<typeof renderView>, rect: { left: number; top: number; width: number; height: number }) {
  const canvasBox = { left: 0, top: 0, width: 400, height: 500, right: 400, bottom: 500, x: 0, y: 0, toJSON: () => ({}) };
  const spy = vi.spyOn(HTMLCanvasElement.prototype, "getBoundingClientRect").mockReturnValue(canvasBox as DOMRect);
  restorers.push(() => spy.mockRestore());

  const rangeProto = Range.prototype as unknown as { getClientRects?: () => unknown };
  const hadRects = Object.prototype.hasOwnProperty.call(rangeProto, "getClientRects");
  const previousRects = rangeProto.getClientRects;
  const rectOut = { ...rect, right: rect.left + rect.width, bottom: rect.top + rect.height, x: rect.left, y: rect.top, toJSON: () => ({}) };
  rangeProto.getClientRects = () => [rectOut];
  restorers.push(() => {
    if (hadRects) rangeProto.getClientRects = previousRects;
    else delete rangeProto.getClientRects;
  });

  const scroll = view.getByLabelText("PDF pages");
  const range = document.createRange();
  range.selectNodeContents(scroll);
  const sel = window.getSelection();
  if (!sel) throw new Error("jsdom has no Selection");
  sel.removeAllRanges();
  sel.addRange(range);
  restorers.push(() => window.getSelection()?.removeAllRanges());
}

describe("PdfViewer highlight selected text", () => {
  it("hides the Highlight button unless allowHighlight is set", async () => {
    mocks.fetchFileRaw.mockResolvedValue((await makePdfBytes()).buffer);
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    // The toolbar itself is up (Find is always shown); only the rewrite action is absent.
    expect(view.getByRole("button", { name: "Find in document" })).toBeInTheDocument();
    expect(view.queryByRole("button", { name: "Highlight selection" })).toBeNull();
  });

  it("renders a Highlight button that is enabled once the document is loaded", async () => {
    mocks.fetchFileRaw.mockResolvedValue((await makePdfBytes()).buffer);
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} allowHighlight />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    const button = view.getByRole("button", { name: "Highlight selection" });
    expect(button).toBeEnabled();
    expect(button.getAttribute("title")).toBe("Highlight selected text");
  });

  it("keeps the selection when the button is pressed (mousedown is cancelled)", async () => {
    mocks.fetchFileRaw.mockResolvedValue((await makePdfBytes()).buffer);
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} allowHighlight />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    // fireEvent returns false when the handler called preventDefault.
    const notCancelled = fireEvent.mouseDown(view.getByRole("button", { name: "Highlight selection" }));
    expect(notCancelled).toBe(false);
  });

  it("does nothing and saves nothing when no text is selected", async () => {
    mocks.fetchFileRaw.mockResolvedValue((await makePdfBytes()).buffer);
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} allowHighlight />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    window.getSelection()?.removeAllRanges();
    fireEvent.click(view.getByRole("button", { name: "Highlight selection" }));

    expect(await view.findByText("Select text on a page first")).toBeInTheDocument();
    expect(mocks.saveFileRaw).not.toHaveBeenCalled();
  });

  it("saves a highlighted copy against the hash of the loaded bytes", async () => {
    const original = await makePdfBytes();
    mocks.fetchFileRaw.mockResolvedValue(original.buffer.slice(0));
    const view = renderView(<PdfViewer path="docs/report.pdf" projectRoot="/proj" page={1} onPageChange={() => {}} allowHighlight />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    mocks.saveFileRaw.mockResolvedValue({ path: "docs/report.pdf", saved: true });
    stubLayoutAndSelect(view, { left: 20, top: 40, width: 120, height: 14 });
    fireEvent.click(view.getByRole("button", { name: "Highlight selection" }));

    await waitFor(() => expect(mocks.saveFileRaw).toHaveBeenCalledTimes(1));
    const [path, bytes, opts] = mocks.saveFileRaw.mock.calls[0] as [string, ArrayBuffer, Record<string, unknown>];
    expect(path).toBe("docs/report.pdf");
    expect(bytes).toBeInstanceOf(ArrayBuffer);
    expect(opts.projectRoot).toBe("/proj");
    expect(opts.expectedHash).toBe(await sha256Hex(original));

    // The saved file is the original plus one highlight: still two pages, larger.
    const saved = await PDFDocument.load(new Uint8Array(bytes));
    expect(saved.getPageCount()).toBe(2);
    expect(bytes.byteLength).toBeGreaterThan(original.byteLength);
  });

  it("reports a failed save as a note and leaves the document loaded", async () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    restorers.push(() => errorSpy.mockRestore());
    mocks.fetchFileRaw.mockResolvedValue((await makePdfBytes()).buffer);
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} allowHighlight />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    mocks.saveFileRaw.mockRejectedValue(new Error("hash mismatch"));
    stubLayoutAndSelect(view, { left: 20, top: 40, width: 120, height: 14 });
    fireEvent.click(view.getByRole("button", { name: "Highlight selection" }));

    expect(await view.findByText("Highlight failed: hash mismatch")).toBeInTheDocument();
    expect(errorSpy).toHaveBeenCalledWith("pdf highlight failed", expect.any(Error));
    // The reader keeps its pages; the failure is not the viewer's fatal error.
    expect(view.container.querySelectorAll("canvas").length).toBe(2);
    expect(view.queryByText(/PDF failed:/)).toBeNull();
  });
});
