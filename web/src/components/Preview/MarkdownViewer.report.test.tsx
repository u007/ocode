import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";

vi.mock("../../api/client", () => ({
  api: { getFileContent: vi.fn().mockResolvedValue({ content: "", is_binary: false }) },
}));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

import MarkdownViewer from "./MarkdownViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc } from "../../lib/viewerContext";

const MD = "# Title\n\nAlpha beta gamma.\n";

beforeEach(() => {
  resetViewerStates();
  setViewerDoc("s1", { path: "doc.md", kind: "markdown" });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderViewer() {
  return render(
    <ViewerReportContext.Provider value="s1">
      <MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />
    </ViewerReportContext.Provider>,
  );
}

describe("MarkdownViewer report", () => {
  it("reports the nearest heading and the visible text after render", async () => {
    // Every block overlaps the viewport, so the whole document is visible.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
      top: 0, bottom: 100, left: 0, right: 0, width: 0, height: 100, x: 0, y: 0, toJSON: () => ({}),
    } as DOMRect);
    renderViewer();
    await waitFor(() => expect(readViewerState("s1")?.location.heading).toBe("Title"));
    expect(readViewerState("s1")?.visibleText).toBe("Title\nAlpha beta gamma.");
  });

  it("reports a selection made inside the viewer and clears it on an in-viewer deselect", async () => {
    const view = renderViewer();
    const paragraph = view.container.querySelector("p") as HTMLElement;
    const selection = { isCollapsed: false, anchorNode: paragraph, toString: () => "Alpha beta", getRangeAt: () => ({ getBoundingClientRect: () => ({ top: 0, left: 0, width: 0 }) }) };
    vi.spyOn(window, "getSelection").mockReturnValue(selection as unknown as Selection);

    fireEvent.mouseUp(view.container.querySelector(".prose") as HTMLElement);
    await waitFor(() => expect(readViewerState("s1")?.selection).toBe("Alpha beta"));

    vi.spyOn(window, "getSelection").mockReturnValue({ ...selection, isCollapsed: true } as unknown as Selection);
    fireEvent.mouseUp(view.container.querySelector(".prose") as HTMLElement);
    await waitFor(() => expect(readViewerState("s1")?.selection).toBeNull());
  });
});
