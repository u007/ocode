import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import PreviewSurface from "./PreviewSurface";

// Viewers are code-split and irrelevant here — only the root wrapper that scopes
// the appearance palette is under test.
vi.mock("./TextViewer", () => ({ default: () => null }));
vi.mock("./PdfViewer", () => ({ default: () => null }));
vi.mock("./DocxViewer", () => ({ default: () => null }));
vi.mock("./PptxViewer", () => ({ default: () => null }));
vi.mock("./ExcelViewer", () => ({ default: () => null }));
vi.mock("./MmdViewer", () => ({ default: () => null }));
vi.mock("./MarkdownViewer", () => ({ default: () => null }));
vi.mock("./JsonViewer", () => ({ default: () => null }));
vi.mock("./HtmlViewer", () => ({ default: () => null }));
vi.mock("./ImageViewer", () => ({ default: () => null }));
vi.mock("./MediaViewer", () => ({ default: () => null }));

describe("PreviewSurface appearance scope", () => {
  it("wraps the surface in the requested palette class", () => {
    const light = render(<PreviewSurface path="/a.md" kind="markdown" appearance="light" />);
    expect(light.container.firstElementChild).toHaveClass("editor-appearance-light");
    light.unmount();

    const dark = render(<PreviewSurface path="/a.md" kind="markdown" appearance="dark" />);
    expect(dark.container.firstElementChild).toHaveClass("editor-appearance-dark");
    dark.unmount();

    // No appearance prop (sidebar / Preview-tab hosts) → inherit the app theme.
    const none = render(<PreviewSurface path="/a.md" kind="markdown" />);
    const root = none.container.firstElementChild as HTMLElement;
    expect(root).not.toHaveClass("editor-appearance-light");
    expect(root).not.toHaveClass("editor-appearance-dark");
  });
});
