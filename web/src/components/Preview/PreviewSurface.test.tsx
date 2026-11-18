import { describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import PreviewSurface from "./PreviewSurface";

// TextViewer mounts Monaco (unresolvable in vitest); stub the viewers so this
// test covers PreviewSurface's kind routing without the editor dependency.
vi.mock("./TextViewer", () => ({ default: () => <div className="text" /> }));
vi.mock("./PdfViewer", () => ({ default: () => null }));
vi.mock("./DocxViewer", () => ({ default: () => null }));
vi.mock("./PptxViewer", () => ({ default: () => null }));
vi.mock("./ExcelViewer", () => ({ default: () => null }));
vi.mock("./MmdViewer", () => ({ default: () => null }));
vi.mock("./MarkdownViewer", () => ({ default: () => null }));
vi.mock("./ImageViewer", () => ({ default: () => null }));
vi.mock("./MediaViewer", () => ({ default: () => null }));
// HTML keeps the real fetch contract in HtmlViewer's own test; here we only
// pin that PreviewSurface forwards the anchor + revision and leaves `content`
// undefined for hosts without a live editor.
vi.mock("./HtmlViewer", () => ({
  default: ({ path, projectRoot, revision, content }: { path: string; projectRoot?: string; revision?: number; content?: string }) => (
    <div
      className="html"
      data-path={path}
      data-root={projectRoot ?? ""}
      data-revision={revision === undefined ? "none" : String(revision)}
      data-controlled={content === undefined ? "false" : "true"}
    />
  ),
}));

describe("PreviewSurface", () => {
  it("renders text kind", async () => {
    const { container } = render(<PreviewSurface path="/test.go" kind="text" />);
    // Viewers are code-split (React.lazy), so the routed surface appears only
    // after the lazy chunk resolves — wait for it instead of asserting on the
    // first paint, which is the Suspense fallback.
    await waitFor(() => expect(container.querySelector(".text")).toBeInTheDocument());
  });

  it("routes html to the viewer with the anchor and revision, without live content", async () => {
    const { container } = render(
      <PreviewSurface path="/proj/mockups/page.html" kind="html" projectRoot="/proj" revision={3} />,
    );
    await waitFor(() => expect(container.querySelector(".html")).toBeInTheDocument());
    const el = container.querySelector(".html") as HTMLElement;
    expect(el.dataset.root).toBe("/proj");
    expect(el.dataset.revision).toBe("3");
    expect(el.dataset.controlled).toBe("false");
  });
});
