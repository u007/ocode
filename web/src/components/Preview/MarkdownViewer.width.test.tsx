import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, render } from "@testing-library/react";
import MarkdownViewer from "./MarkdownViewer";
import {
  PREVIEW_WIDTH_STORAGE_KEY,
  __resetPreviewWidthForTests,
  setPreviewWidthPreset,
} from "../../lib/previewWidth";

// Same heavy-dependency isolation as the other Preview tests: FileEditor pulls
// Monaco (unresolvable in vitest) and MermaidViewer pulls mermaid. Neither is
// exercised by the controlled-content path this test renders.
vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

beforeEach(() => {
  window.localStorage.clear();
  __resetPreviewWidthForTests();
});

function renderDoc() {
  return render(
    <MarkdownViewer path="/doc.md" onOpenFile={() => {}} content={"# Title\n\nBody text."} />,
  );
}

describe("MarkdownViewer reading column", () => {
  // Regression: @tailwindcss/typography's `.prose` sets `max-width: 65ch`, which
  // left an off-centre empty gutter on the right of the markdown preview. The
  // scroller must still opt out, because the column width below is applied to a
  // CHILD — a 65ch cap on `.prose` would win over it.
  it("keeps typography from capping the scroller", () => {
    const { container } = renderDoc();
    const prose = container.querySelector(".prose");
    expect(prose).not.toBeNull();
    expect(prose).toHaveClass("max-w-none");
  });

  it("renders a centred paper column with a horizontal margin", () => {
    setPreviewWidthPreset("letter");
    const { container } = renderDoc();

    const column = container.querySelector("[data-preview-column]");
    expect(column).not.toBeNull();
    expect(column).toHaveAttribute("data-preview-column", "letter");
    expect(column).toHaveClass("max-w-[816px]");
    expect(column).toHaveClass("mx-auto");
    // The margin that survives when the pane is narrower than the sheet. It is
    // fluid (clamped 12–32px) because the Files-tab split view can be narrow.
    expect(column).toHaveClass("px-[max(0.75rem,min(2rem,4%))]");
  });

  it("uses the A4 column for the A4/F4 preset", () => {
    setPreviewWidthPreset("a4");
    const { container } = renderDoc();

    const column = container.querySelector("[data-preview-column]");
    expect(column).toHaveAttribute("data-preview-column", "a4");
    expect(column).toHaveClass("max-w-[794px]");
  });

  it("keeps the edge-to-edge fill when the reader picks Full", () => {
    setPreviewWidthPreset("full");
    const { container } = renderDoc();

    const column = container.querySelector("[data-preview-column]");
    expect(column).toHaveAttribute("data-preview-column", "full");
    expect(column).toHaveClass("max-w-none");
    expect(column).not.toHaveClass("max-w-[816px]");
  });

  // The menu and the viewer share one store, so picking a preset must move a
  // MOUNTED viewer. A no-op `subscribe` would pass every other case in this
  // file, because they set the preset before rendering.
  it("re-renders the column when the preference changes while mounted", () => {
    const { container } = renderDoc();
    expect(container.querySelector("[data-preview-column]")).toHaveAttribute("data-preview-column", "letter");

    act(() => setPreviewWidthPreset("a4"));

    expect(container.querySelector("[data-preview-column]")).toHaveAttribute("data-preview-column", "a4");
  });

  // Cross-window sync: another window writing the key must re-render this one.
  // This also pins that the storage handler calls emit() — asserting the module
  // cache alone would pass with emit() deleted.
  it("re-renders when another window writes the preference", () => {
    const { container } = renderDoc();
    expect(container.querySelector("[data-preview-column]")).toHaveAttribute("data-preview-column", "letter");

    act(() => {
      window.dispatchEvent(
        new StorageEvent("storage", { key: PREVIEW_WIDTH_STORAGE_KEY, newValue: "full" }),
      );
    });

    expect(container.querySelector("[data-preview-column]")).toHaveAttribute("data-preview-column", "full");
  });
});
