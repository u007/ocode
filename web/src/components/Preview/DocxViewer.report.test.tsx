import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  renderAsync: vi.fn(async (_buf: ArrayBuffer, body: HTMLElement) => {
    body.innerHTML = "<section class='docx'><h2>Install</h2><p>Alpha beta gamma.</p></section>";
  }),
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
}));
vi.mock("docx-preview", () => ({ renderAsync: mocks.renderAsync }));

import DocxViewer from "./DocxViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

beforeEach(() => {
  resetViewerStates();
  setViewerDoc("s1", { path: "doc.docx", kind: "docx" });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("DocxViewer report", () => {
  it("reports the heading and visible text of the rendered body once it has loaded", async () => {
    // Every block overlaps the viewport, so the whole body is visible.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
      top: 0, bottom: 100, left: 0, right: 0, width: 0, height: 100, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect);
    render(
      <ViewerReportContext.Provider value="s1">
        <DocxViewer path="doc.docx" />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(readViewerState("s1")?.visibleText).toBe("Install\nAlpha beta gamma."));
    expect(readViewerState("s1")?.location.heading).toBe("Install");
  });
});
