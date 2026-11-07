import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render as renderView, waitFor } from "@testing-library/react";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc, type ViewerControlOutcome } from "../../lib/viewerContext";
import { applyViewerControl, type ViewerControlAction } from "../../lib/viewerControl";

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
        { str: "Alpha beta", fontName: "f1", transform: [10, 0, 0, 10, 0, 0], width: 100, height: 10 },
        { str: n === 2 ? ` gamma BetA ${n}delta` : ` gamma beta${n}delta`, fontName: "f1", transform: [10, 0, 0, 10, 0, 50], width: 100, height: 10 },
      ],
      styles: { f1: { fontFamily: "sans-serif", ascent: 0.9, descent: -0.2, vertical: false } },
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

function run(action: ViewerControlAction): ViewerControlOutcome {
  let outcome: ViewerControlOutcome | undefined;
  act(() => {
    outcome = applyViewerControl("s1", action);
  });
  if (!outcome) throw new Error("applyViewerControl returned nothing");
  return outcome;
}

function renderControlled(onPageChange = vi.fn()) {
  const view = renderView(
    <ViewerReportContext.Provider value="s1">
      <PdfViewer path="doc.pdf" page={1} onPageChange={onPageChange} />
    </ViewerReportContext.Provider>,
  );
  return { view, onPageChange };
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  resetViewerStates();
  setViewerDoc("s1", { path: "doc.pdf", kind: "pdf" });
  mocks.renderMock.mockImplementation(() => ({ promise: Promise.resolve(), cancel: vi.fn() }));
});

afterEach(() => cleanup());

describe("PdfViewer agent control", () => {
  it("jumps to a page through onPageChange and rejects pages outside the document", async () => {
    const { view, onPageChange } = renderControlled();
    await mount(view);
    expect(run({ id: "p1", action: "scroll", page: 2 })).toEqual({ id: "p1", status: "applied" });
    expect(onPageChange).toHaveBeenLastCalledWith(2);
    await waitFor(() => expect(readViewerState("s1")?.location.page).toBe(2));
    expect(run({ id: "p2", action: "scroll", page: 9 }).error).toBe("page 9 out of range 1-3");
  });

  it("steps one page by direction and clamps at the ends", async () => {
    const { view, onPageChange } = renderControlled();
    await mount(view);
    expect(run({ id: "d1", action: "scroll", direction: "down" }).status).toBe("applied");
    expect(onPageChange).toHaveBeenLastCalledWith(2);
    expect(run({ id: "d2", action: "scroll", direction: "up" }).status).toBe("applied");
    expect(onPageChange).toHaveBeenLastCalledWith(1);
    expect(run({ id: "d3", action: "scroll", direction: "up" }).status).toBe("applied");
    expect(onPageChange).toHaveBeenLastCalledWith(1);
  });

  it("opens find for selected text, walks matches, and reports the count", async () => {
    const { view } = renderControlled();
    await mount(view);
    expect(run({ id: "s1", action: "select", text: "beta" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 6, current: 1 }));
    expect(run({ id: "f1", action: "find_next" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find?.current).toBe(2));
    expect(run({ id: "f2", action: "find_close" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toBeNull());
  });

  it("reports heading targets as not supported by a PDF", async () => {
    const { view } = renderControlled();
    await mount(view);
    expect(run({ id: "h1", action: "scroll", heading: "Intro" }).error).toBe("not supported by this viewer");
  });
});
