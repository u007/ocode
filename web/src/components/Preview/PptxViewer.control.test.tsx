import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen } from "@testing-library/react";

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
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc, type ViewerControlOutcome } from "../../lib/viewerContext";
import { applyViewerControl, type ViewerControlAction } from "../../lib/viewerControl";

afterEach(() => {
  cleanup();
  resetViewerStates();
  vi.restoreAllMocks();
});

beforeEach(() => {
  setViewerDoc("s1", { path: "/deck.pptx", kind: "pptx" });
});

function renderViewer(slide: number, onSlideChange = vi.fn()) {
  const view = render(
    <ViewerReportContext.Provider value="s1">
      <PptxViewer path="/deck.pptx" slide={slide} onSlideChange={onSlideChange} />
    </ViewerReportContext.Provider>,
  );
  return { view, onSlideChange };
}

function run(action: ViewerControlAction): ViewerControlOutcome {
  let outcome: ViewerControlOutcome | undefined;
  act(() => {
    outcome = applyViewerControl("s1", action);
  });
  if (!outcome) throw new Error("applyViewerControl returned nothing");
  return outcome;
}

describe("PptxViewer agent control", () => {
  it("moves between slides through onSlideChange, clamping steps and rejecting bad numbers", async () => {
    const { onSlideChange } = renderViewer(1);
    await screen.findByText("Slide 1 / 2");
    expect(run({ id: "g1", action: "scroll", slide: 2 })).toEqual({ id: "g1", status: "applied" });
    expect(onSlideChange).toHaveBeenLastCalledWith(2);
    expect(run({ id: "g2", action: "scroll", direction: "up" })).toEqual({ id: "g2", status: "applied" });
    expect(onSlideChange).toHaveBeenLastCalledWith(1);
    expect(run({ id: "g3", action: "scroll", direction: "up" })).toEqual({ id: "g3", status: "applied" });
    expect(onSlideChange).toHaveBeenLastCalledWith(1);
    expect(run({ id: "g4", action: "scroll", slide: 9 }).error).toBe("slide 9 out of range 1-2");
  });

  it("selects text on the current slide and reports it, failing when absent", async () => {
    renderViewer(1);
    await screen.findByText("Slide 1 / 2");
    expect(run({ id: "s1", action: "select", text: "Body line" }).status).toBe("applied");
    expect(readViewerState("s1")?.selection).toBe("Body line");
    expect(run({ id: "s2", action: "select", text: "Second" }).error).toBe("text not found");
  });

  it("reports find as not supported, since the deck has no find engine", async () => {
    renderViewer(1);
    await screen.findByText("Slide 1 / 2");
    expect(run({ id: "f1", action: "find", query: "Title" }).error).toBe("not supported by this viewer");
  });
});
