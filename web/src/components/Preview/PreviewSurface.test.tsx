import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
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

describe("PreviewSurface", () => {
  it("renders text kind", () => {
    const { container } = render(<PreviewSurface path="/test.go" kind="text" />);
    expect(container.querySelector(".text")).toBeInTheDocument();
  });
});
