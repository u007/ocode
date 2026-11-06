import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
}));
vi.mock("./pptxDeck", () => ({
  parsePptx: vi.fn(async () => [
    { index: 1, blocks: [{ text: "Title" }, { text: "Body line" }], images: [], aspect: 1.6 },
    { index: 2, blocks: [{ text: "Second" }], images: [], aspect: 1.6 },
  ]),
}));

import PptxViewer from "./PptxViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

afterEach(() => {
  cleanup();
  resetViewerStates();
});

function viewer(slide: number) {
  return (
    <ViewerReportContext.Provider value="s1">
      <PptxViewer path="/deck.pptx" slide={slide} onSlideChange={() => {}} />
    </ViewerReportContext.Provider>
  );
}

describe("PptxViewer report", () => {
  it("reports the current slide's number, count and text", async () => {
    setViewerDoc("s1", { path: "/deck.pptx", kind: "pptx" });
    const view = render(viewer(1));
    await waitFor(() => expect(readViewerState("s1")?.visibleText).toBe("Title\nBody line"));
    expect(readViewerState("s1")?.location).toEqual({ slide: 1, slideCount: 2 });

    view.rerender(viewer(2));
    await waitFor(() => expect(readViewerState("s1")?.visibleText).toBe("Second"));
    expect(readViewerState("s1")?.location).toEqual({ slide: 2, slideCount: 2 });
  });
});
