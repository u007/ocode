import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render as renderView, waitFor } from "@testing-library/react";

// Print & Download: both reuse an independent Blob snapshot of the PDF bytes
// (pdf.js may detach the buffer handed to getDocument). Download mints an
// object URL and clicks an anchor; print hosts the blob in a hidden iframe
// and calls print() on its window, with popup-tab and download fallbacks.

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
  (URL as unknown as { revokeObjectURL: (u: string) => void }).revokeObjectURL = () => {};
  const restore = () => {
    const mutable = URL as unknown as { createObjectURL?: unknown; revokeObjectURL?: unknown };
    delete mutable.createObjectURL;
    delete mutable.revokeObjectURL;
    if (nativeCreate) mutable.createObjectURL = nativeCreate;
    if (nativeRevoke) mutable.revokeObjectURL = nativeRevoke;
  };
  return { urls, blobs, restore };
}
/** iframe.contentWindow.print is jsdom-unreachable (null window) — the code
 *  catches and opens a tab; capture window.open instead. */
function stubWindowOpen(result: Window | null) {
  const open = vi.spyOn(window, "open").mockReturnValue(result as Window);
  return { open, restore: () => open.mockRestore() };
}
/** Click Print and return the hidden frame it appends. */
function startPrint(view: ReturnType<typeof renderView>): HTMLIFrameElement {
  fireEvent.click(view.getByLabelText("Print"));
  const frame = document.querySelector("iframe");
  if (!frame) throw new Error("print did not append an iframe");
  return frame;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
  mocksFetchFileRaw.mockResolvedValue(new ArrayBuffer(8));
});

afterEach(() => {
  cleanup();
  // Print frames live on document.body, outside RTL's container; a case that
  // ends before its teardown must not leak a frame or fake timers into the next.
  for (const frame of document.querySelectorAll("iframe")) frame.remove();
  vi.useRealTimers();
});

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

  it("print opens the PDF in a fallback tab on environments without frame print", async () => {
    const { urls, restore } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    const appendSpy = vi.spyOn(document.body, "appendChild").mockImplementation((node: Node) => {
      // Call through so React's real DOM insertions still happen.
      return HTMLElement.prototype.appendChild.call(document.body, node);
    });
    // jsdom's real window.open logs "Not implemented" and returns undefined —
    // which our code treats as popup-blocked and falls back to download.
    // Stub it to a "allowed popup" return so exactly one URL gets minted.
    const openSpy = vi.spyOn(window, "open").mockImplementation((() => ({})) as unknown as typeof window.open);

    vi.useFakeTimers();
    fireEvent.click(view.getByLabelText("Print"));
    // jsdom never fires iframe onload for blob: URLs, so the 30s load watchdog
    // is what removes the frame and opens a tab. A slow load must not trigger it early.
    act(() => {
      vi.advanceTimersByTime(1100);
    });
    expect(document.querySelector("iframe")).not.toBeNull();
    expect(openSpy).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(30_000);
    });
    vi.useRealTimers();
    expect(urls).toHaveLength(1);
    // The watchdog removed the frame and opened a tab (popup allowed here).
    expect(document.querySelector("iframe")).toBeNull();
    expect(openSpy).toHaveBeenCalledTimes(1);

    appendSpy.mockRestore();
    restore();
  });

  it("print falls back to a popup tab when the frame cannot print", async () => {
    const { restore: restoreUrls } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    const { open, restore: restoreOpen } = stubWindowOpen({} as Window);
    vi.useFakeTimers();
    const frame = startPrint(view);
    // The frame loads, then its print() throws → popup tab after the settle delay.
    Object.defineProperty(frame, "contentWindow", {
      value: {
        focus: vi.fn(),
        addEventListener: vi.fn(),
        print: () => {
          throw new Error("print refused");
        },
      },
    });
    act(() => {
      fireEvent.load(frame);
      vi.advanceTimersByTime(500);
    });
    vi.useRealTimers();
    expect(open).toHaveBeenCalledWith(expect.any(String), "_blank");
    restoreOpen();
    restoreUrls();
  });

  it("print falls back to a tab when the loaded frame has no window", async () => {
    const { restore: restoreUrls } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));

    const { open, restore: restoreOpen } = stubWindowOpen({} as Window);
    vi.useFakeTimers();
    const frame = startPrint(view);
    // A blob: frame can load with no accessible window (WKWebView). The click must
    // still reach the popup/download fallback instead of doing nothing.
    Object.defineProperty(frame, "contentWindow", { value: null });
    act(() => {
      fireEvent.load(frame);
      vi.advanceTimersByTime(500);
    });
    vi.useRealTimers();
    expect(open).toHaveBeenCalledWith(expect.any(String), "_blank");
    expect(document.querySelector("iframe")).toBeNull();
    restoreOpen();
    restoreUrls();
  });

  it("print runs after the settle delay and tears down on afterprint, not at 60s", async () => {
    const { urls, restore } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));
    const revoke = vi.spyOn(URL, "revokeObjectURL");

    vi.useFakeTimers();
    const frame = startPrint(view);
    const win = Object.assign(new EventTarget(), { focus: vi.fn(), print: vi.fn() });
    Object.defineProperty(frame, "contentWindow", { value: win });
    act(() => {
      fireEvent.load(frame);
      vi.advanceTimersByTime(499);
    });
    expect(win.print).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(win.print).toHaveBeenCalledTimes(1);

    // The dialog may stay open well past 60s: nothing is torn down yet.
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(document.querySelector("iframe")).not.toBeNull();
    expect(revoke).not.toHaveBeenCalled();

    act(() => {
      win.dispatchEvent(new Event("afterprint"));
    });
    vi.useRealTimers();
    expect(document.querySelector("iframe")).toBeNull();
    expect(revoke).toHaveBeenCalledWith(urls[0]);
    revoke.mockRestore();
    restore();
  });

  it("print tears the frame down at the 10 min safety timeout when afterprint never fires", async () => {
    const { urls, restore } = stubBlobUrls();
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    await waitFor(() => expect(view.container.querySelectorAll("canvas").length).toBe(2));
    const revoke = vi.spyOn(URL, "revokeObjectURL");

    vi.useFakeTimers();
    const frame = startPrint(view);
    Object.defineProperty(frame, "contentWindow", {
      value: Object.assign(new EventTarget(), { focus: vi.fn(), print: vi.fn() }),
    });
    act(() => {
      fireEvent.load(frame);
      vi.advanceTimersByTime(600_000 - 1);
    });
    expect(document.querySelector("iframe")).not.toBeNull();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    vi.useRealTimers();
    expect(document.querySelector("iframe")).toBeNull();
    expect(revoke).toHaveBeenCalledWith(urls[0]);
    revoke.mockRestore();
    restore();
  });

  it("buttons are disabled until a document has loaded", () => {
    mocksFetchFileRaw.mockReturnValue(new Promise(() => {})); // never resolves
    const view = renderView(<PdfViewer path="doc.pdf" page={1} onPageChange={() => {}} />);
    expect((view.getByLabelText("Print") as HTMLButtonElement).disabled).toBe(true);
    expect((view.getByLabelText("Download") as HTMLButtonElement).disabled).toBe(true);
  });
});
