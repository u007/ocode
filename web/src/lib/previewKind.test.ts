import { describe, expect, it, vi } from "vitest";
import {
  OPEN_PREVIEW_EVENT,
  PREVIEW_CONTEXT_EVENT,
  dispatchOpenPreview,
  dispatchPreviewContext,
  parsePreviewOpen,
  previewKindForPath,
  resolvePreviewDoc,
} from "./previewKind";

describe("previewKindForPath", () => {
  it("maps office and doc types", () => {
    expect(previewKindForPath("docs/deck.pptx")).toBe("pptx");
    expect(previewKindForPath("spec.docx")).toBe("docx");
    expect(previewKindForPath("report.pdf")).toBe("pdf");
    expect(previewKindForPath("flow.mmd")).toBe("mermaid");
    expect(previewKindForPath("notes.md")).toBe("markdown");
    expect(previewKindForPath("main.go")).toBe("text");
    expect(previewKindForPath("photo.PNG")).toBe("image");
    expect(previewKindForPath("budget.xlsx")).toBe("excel");
    expect(previewKindForPath("legacy.xls")).toBe("excel");
    expect(previewKindForPath("data.csv")).toBe("excel");
  });

  it("returns null for unknown or missing extensions", () => {
    expect(previewKindForPath("movie.mkv")).toBeNull();
    expect(previewKindForPath("Makefile")).toBeNull();
  });
});

describe("parsePreviewOpen", () => {
  it("returns null when no sentinel is present", () => {
    expect(parsePreviewOpen(["hello", "tool result ok"])).toBeNull();
  });

  it("parses path, kind, and page", () => {
    const got = parsePreviewOpen(["PREVIEW_OPEN:docs/deck.pptx|kind=pptx|page=3"]);
    expect(got).toEqual({ path: "docs/deck.pptx", kind: "pptx", page: 3 });
  });

  it("latest directive wins", () => {
    const got = parsePreviewOpen([
      "PREVIEW_OPEN:a.pdf|kind=pdf|page=1",
      "PREVIEW_OPEN:b.pdf|kind=pdf|page=5",
    ]);
    expect(got?.path).toBe("b.pdf");
    expect(got?.page).toBe(5);
  });

  it("clamps page to >= 1", () => {
    expect(parsePreviewOpen(["PREVIEW_OPEN:a.pdf|kind=pdf|page=0"])?.page).toBe(1);
  });
});

describe("preview events", () => {
  it("dispatchOpenPreview fires ocode:open-preview with kind", () => {
    const seen: unknown[] = [];
    const h = (e: Event) => seen.push((e as CustomEvent).detail);
    window.addEventListener(OPEN_PREVIEW_EVENT, h as EventListener);
    try {
      dispatchOpenPreview("report.pdf", 2, "/proj");
      expect(seen).toEqual([{ path: "report.pdf", kind: "pdf", page: 2, projectRoot: "/proj" }]);
    } finally {
      window.removeEventListener(OPEN_PREVIEW_EVENT, h as EventListener);
    }
  });

  it("dispatchPreviewContext fires ocode:preview-context", () => {
    const fn = vi.fn();
    const h = (e: Event) => fn((e as CustomEvent).detail);
    window.addEventListener(PREVIEW_CONTEXT_EVENT, h as EventListener);
    try {
      dispatchPreviewContext({ path: "deck.pptx", label: "slide 4", excerpt: "hello" });
      expect(fn).toHaveBeenCalledWith({ path: "deck.pptx", label: "slide 4", excerpt: "hello", projectRoot: undefined });
    } finally {
      window.removeEventListener(PREVIEW_CONTEXT_EVENT, h as EventListener);
    }
  });
});

describe("resolvePreviewDoc", () => {
  it("resolves known kinds directly", () => {
    expect(resolvePreviewDoc("report.pdf", "pdf")).toEqual({ kind: "pdf", unsupported: null });
    expect(resolvePreviewDoc("budget.xlsx", "excel")).toEqual({ kind: "excel", unsupported: null });
  });

  it("falls unknown-but-textual requests through to the text editor", () => {
    expect(resolvePreviewDoc("Main.java", "text")).toEqual({ kind: "text", unsupported: null });
    expect(resolvePreviewDoc("Makefile", "text")).toEqual({ kind: "text", unsupported: null });
  });

  it("sends legacy .doc/.ppt to the OS fallback, never a fake preview", () => {
    expect(resolvePreviewDoc("old.doc", "text")).toEqual({ kind: null, unsupported: "old.doc" });
    expect(resolvePreviewDoc("old.ppt", "text")).toEqual({ kind: null, unsupported: "old.ppt" });
    // …but the modern formats still preview.
    expect(resolvePreviewDoc("new.docx", "text").kind).toBe("docx");
    expect(resolvePreviewDoc("new.pptx", "text").kind).toBe("pptx");
  });

  it("sends non-text unknowns to the fallback", () => {
    expect(resolvePreviewDoc("movie.mkv", "pdf")).toEqual({ kind: null, unsupported: "movie.mkv" });
  });
});
