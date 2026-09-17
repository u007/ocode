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

describe("PreviewSurface", () => {
  it("renders text kind", async () => {
    const { container } = render(<PreviewSurface path="/test.go" kind="text" />);
    // Viewers are code-split (React.lazy), so the routed surface appears only
    // after the lazy chunk resolves — wait for it instead of asserting on the
    // first paint, which is the Suspense fallback.
    await waitFor(() => expect(container.querySelector(".text")).toBeInTheDocument());
  });
});
