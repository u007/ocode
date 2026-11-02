import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render as renderView, screen, waitFor } from "@testing-library/react";

// Print & Download: both reuse an independent Blob snapshot of the PDF bytes
// (pdf.js may detach the buffer handed to getDocument). Download mints an
// object URL and clicks an anchor; print opens an in-app preview dialog
// instead of triggering the native OS print dialog.

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
  const doc = { numPages: 2, cleanup: vi.fn(), getPage };
  const getDocument = vi.fn(() => ({ promise: Promise.resolve(doc) }));
  return { renderMock, getPage, doc, getDocument };
});

vi.mock("pdfjs-dist", () => ({
  GlobalWorkerOptions: { workerSrc: "" },
  Util: { transform: vi.fn() },
  getDocument: mocks.getDocument,
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: (...args: unknown[]) => mocksFetchFileRaw(...(args as [])) },
}));
const mocksFetchFileRaw = vi.fn();

import PdfViewer from "./PdfViewer";

/** jsdom URL.createObjectURL plumbing backed by a Map so each URL maps to
 *  its blob (revokeObjectURL is a no-op spy). */
function stubBlobUrls() {
  const urls: string[] = [];
  const blobs = new Map<string, Blob>();
  const nativeCreate = typeof URL.createObjectURL === "function" ? URL.createObjectURL.bind(URL) : null;
  const nativeRevoke = typeof URL.revokeObjectURL === "function" ? URL.revokeObjectURL.bind(URL) : null;
  // jsdom ships neither createObjectURL nor revokeObjectURL — install fakes.
  (URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = (b: Blob) => {
    const u = `blob:fake-${urls.length}`;
    blobs.set(u, b);
    urls.push(u);
    return u;
  };
  (URL as unknown as { revokeObjectURL: (u: string) => void }).revokeObjectURL = (u: string) => {
    blobs.delete(u);
    if (nativeRevoke) nativeRevoke(u);
  };
  return { urls, blobs, restore: () => {
    (URL as unknown as { createObjectURL: typeof nativeCreate }).createObjectURL = nativeCreate!;
    (URL as unknown as { revokeObjectURL: typeof nativeRevoke }).revokeObjectURL = nativeRevoke!;
  }};
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  mocksFetchFileRaw.mockResolvedValue(new ArrayBuffer(8));
});

afterEach(() => cleanup());

describe("PdfViewer print & download", () => {
  it("download mints an object URL from the loaded bytes and names the file", async () => {
    const { urls, blobs, restore } = stubBlobUrls();
    const view = renderView(<PdfViewer path="docs/report.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    fireEvent.click(view.getByLabelText("Download"));

    expect(urls).toHaveLength(1);
    const blob = blobs.get(urls[0])!;
    expect(blob.type).toBe("application/pdf");
    expect(click).toHaveBeenCalled();
    const a = click.mock.instances[0] as unknown as HTMLAnchorElement;
    expect(a.download).toBe("report.pdf");
    click.mockRestore();
    restore();
  });

  it("print opens the PDF in an in-app preview dialog instead of a native print popup", async () => {
    const { urls, restore } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    // The Print button now opens an in-app preview dialog instead of
    // triggering the native OS print dialog. One blob URL is minted for the
    // dialog's iframe, and no native print popup is opened.
    fireEvent.click(view.getByLabelText("Print"));

    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    const dialog = screen.getByRole("dialog");
    const iframe = dialog.querySelector("iframe");
    expect(iframe).toBeInTheDocument();
    expect(iframe?.src).toBe(urls[0]);
    expect(urls).toHaveLength(1);
    restore();
  });

  it("print dialog shows the PDF blob in an iframe when the frame cannot print", async () => {
    const { urls, restore: restoreUrls } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    // No native print() call is made anymore: clicking Print always opens the
    // in-app dialog with the blob URL.
    fireEvent.click(view.getByLabelText("Print"));

    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    const dialog = screen.getByRole("dialog");
    const iframe = dialog.querySelector("iframe");
    expect(iframe).toBeInTheDocument();
    expect(iframe?.src).toBe(urls[0]);
    expect(urls).toHaveLength(1);
    restoreUrls();
  });

  it("closing the print dialog revokes the blob URL", async () => {
    const { urls, restore } = stubBlobUrls();
    const revokeSpy = vi.spyOn(URL, "revokeObjectURL");
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    fireEvent.click(view.getByLabelText("Print"));
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());

    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    expect(revokeSpy).toHaveBeenCalledWith(urls[0]);
    restore();
  });

  it("buttons are disabled until a document has loaded", () => {
    mocksFetchFileRaw.mockReturnValue(new Promise(() => {})); // never resolves
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    expect((view.getByLabelText("Print") as HTMLButtonElement).disabled).toBe(true);
    expect((view.getByLabelText("Download") as HTMLButtonElement).disabled).toBe(true);
  });
});
