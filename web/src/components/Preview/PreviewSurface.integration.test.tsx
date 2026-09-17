import { describe, expect, it, vi } from "vitest";
import PreviewSurface from "./PreviewSurface";

// Stub every viewer: the real ones pull Monaco (via TextViewer → FileEditor →
// monaco-setup, which vitest's node resolver cannot load), pdf.js workers, and
// docx-preview. This test covers PreviewSurface's kind routing, nothing more.
vi.mock("./TextViewer", () => ({ default: () => null }));
vi.mock("./PdfViewer", () => ({ default: () => null }));
vi.mock("./DocxViewer", () => ({ default: () => null }));
vi.mock("./PptxViewer", () => ({ default: () => null }));
vi.mock("./ExcelViewer", () => ({ default: () => null }));
vi.mock("./MmdViewer", () => ({ default: () => null }));
vi.mock("./MarkdownViewer", () => ({ default: () => null }));
vi.mock("./ImageViewer", () => ({ default: () => null }));
vi.mock("./MediaViewer", () => ({ default: () => null }));

const kinds = ["pdf", "docx", "pptx", "excel", "mermaid", "markdown", "text", "image", "audio", "video"] as const;

describe("PreviewSurface renderer kinds", () => {
  for (const kind of kinds) {
    it(`accepts kind=${kind}`, () => {
      expect(() => PreviewSurface({ path: "/test", kind })).not.toThrow();
    });
  }
});
