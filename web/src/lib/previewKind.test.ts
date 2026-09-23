import { describe, expect, it, vi } from "vitest";
import {
  OPEN_PREVIEW_EVENT,
  PREVIEW_CONTEXT_EVENT,
  dispatchOpenPreview,
  dispatchPreviewContext,
  isLegacyOfficePath,
  isMarkdownPath,
  parsePreviewOpen,
  previewKindForPath,
  previewOnlyKindForPath,
  resolvePreviewDoc,
} from "./previewKind";

describe("previewKindForPath", () => {
  it("maps office and doc types", () => {
    expect(previewKindForPath("docs/deck.pptx")).toBe("pptx");
    expect(previewKindForPath("spec.docx")).toBe("docx");
    expect(previewKindForPath("report.pdf")).toBe("pdf");
    expect(previewKindForPath("flow.mmd")).toBe("mermaid");
    expect(previewKindForPath("notes.md")).toBe("markdown");
    expect(previewKindForPath("docs/page.mdx")).toBe("markdown");
    expect(previewKindForPath("main.go")).toBe("text");
    expect(previewKindForPath("photo.PNG")).toBe("image");
    expect(previewKindForPath("budget.xlsx")).toBe("excel");
    expect(previewKindForPath("legacy.xls")).toBe("excel");
    expect(previewKindForPath("data.csv")).toBe("excel");
    expect(previewKindForPath("song.mp3")).toBe("audio");
    expect(previewKindForPath("clip.mp4")).toBe("video");
    expect(previewKindForPath("screen.webm")).toBe("video");
  });

  it("defaults textual and extensionless paths to text", () => {
    // Regression: a narrow text allowlist used to bounce every one of these to
    // "Format Not Supported" in the sidebar preview (and refuse them in the
    // preview_open tool). Text is the default now; the content endpoint's
    // NUL-byte sniff is the real binary gate.
    expect(previewKindForPath("schema.sql")).toBe("text");
    expect(previewKindForPath("Main.java")).toBe("text");
    expect(previewKindForPath("script.sh")).toBe("text");
    expect(previewKindForPath("app.rb")).toBe("text");
    expect(previewKindForPath("config.toml")).toBe("text");
    expect(previewKindForPath("infra.tf")).toBe("text");
    expect(previewKindForPath("pom.xml")).toBe("text");
    expect(previewKindForPath("style.ini")).toBe("text");
    expect(previewKindForPath("query.graphql")).toBe("text");
    expect(previewKindForPath("Makefile")).toBe("text");
    expect(previewKindForPath("LICENSE")).toBe("text");
    expect(previewKindForPath(".gitignore")).toBe("text");
    expect(previewKindForPath("src/.env")).toBe("text");
    expect(previewKindForPath("types.d.ts")).toBe("text");
  });

  it("returns null only for known binary formats with no renderer", () => {
    expect(previewKindForPath("movie.mkv")).toBeNull();
    expect(previewKindForPath("movie.avi")).toBeNull();
    expect(previewKindForPath("app.exe")).toBeNull();
    expect(previewKindForPath("bundle.zip")).toBeNull();
    expect(previewKindForPath("archive.tar.gz")).toBeNull();
    expect(previewKindForPath("old.doc")).toBeNull();
    expect(previewKindForPath("font.ttf")).toBeNull();
    expect(previewKindForPath("db.sqlite")).toBeNull();
    // Mixed-case extensions are lowercased before the denylist lookup.
    expect(previewKindForPath("debug.dSYM")).toBeNull();
  });
});

describe("previewOnlyKindForPath", () => {
  it("routes binary preview formats away from Monaco", () => {
    expect(previewOnlyKindForPath("report.pdf")).toBe("pdf");
    expect(previewOnlyKindForPath("spec.docx")).toBe("docx");
    expect(previewOnlyKindForPath("deck.pptx")).toBe("pptx");
    expect(previewOnlyKindForPath("budget.xlsx")).toBe("excel");
    expect(previewOnlyKindForPath("data.csv")).toBe("excel");
    expect(previewOnlyKindForPath("photo.PNG")).toBe("image");
    expect(previewOnlyKindForPath("icon.svg")).toBe("image");
    expect(previewOnlyKindForPath("song.mp3")).toBe("audio");
    expect(previewOnlyKindForPath("voice.m4a")).toBe("audio");
    expect(previewOnlyKindForPath("clip.mp4")).toBe("video");
    expect(previewOnlyKindForPath("screen.webm")).toBe("video");
  });

  it("keeps editable and unrenderable paths in the editor", () => {
    expect(previewOnlyKindForPath("main.go")).toBeNull();
    expect(previewOnlyKindForPath("notes.md")).toBeNull();
    expect(previewOnlyKindForPath("page.mdx")).toBeNull();
    expect(previewOnlyKindForPath("flow.mmd")).toBeNull();
    expect(previewOnlyKindForPath("legacy.doc")).toBeNull();
    expect(previewOnlyKindForPath("movie.mkv")).toBeNull();
    expect(previewOnlyKindForPath("movie.avi")).toBeNull();
    expect(previewOnlyKindForPath("Makefile")).toBeNull();
  });
});

// Regression: `.mdx` is Markdown-source-plus-JSX. It must route exactly like
// `.md` — editable in Monaco with the Files-tab Edit/Preview/Split switch —
// and never fall into the preview-only (binary) branch.
describe("isMarkdownPath", () => {
  it("covers the Markdown family including MDX", () => {
    expect(isMarkdownPath("notes.md")).toBe(true);
    expect(isMarkdownPath("notes.markdown")).toBe(true);
    expect(isMarkdownPath("docs/page.mdx")).toBe(true);
    expect(isMarkdownPath("PAGE.MDX")).toBe(true);
    expect(isMarkdownPath("main.go")).toBe(false);
    expect(isMarkdownPath("flow.mmd")).toBe(false);
    expect(isMarkdownPath("Makefile")).toBe(false);
  });
});

describe("isLegacyOfficePath", () => {
  it("detects .doc/.ppt only", () => {
    expect(isLegacyOfficePath("old.doc")).toBe(true);
    expect(isLegacyOfficePath("OLD.PPT")).toBe(true);
    expect(isLegacyOfficePath("spec.docx")).toBe(false);
    expect(isLegacyOfficePath("deck.pptx")).toBe(false);
    expect(isLegacyOfficePath("report.pdf")).toBe(false);
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
    expect(resolvePreviewDoc("report.pdf")).toEqual({ kind: "pdf", unsupported: null });
    expect(resolvePreviewDoc("budget.xlsx")).toEqual({ kind: "excel", unsupported: null });
  });

  it("falls unknown-but-textual requests through to the text editor", () => {
    expect(resolvePreviewDoc("Main.java")).toEqual({ kind: "text", unsupported: null });
    expect(resolvePreviewDoc("Makefile")).toEqual({ kind: "text", unsupported: null });
  });

  it("sends legacy .doc/.ppt to the OS fallback, never a fake preview", () => {
    expect(resolvePreviewDoc("old.doc")).toEqual({ kind: null, unsupported: "old.doc" });
    expect(resolvePreviewDoc("old.ppt")).toEqual({ kind: null, unsupported: "old.ppt" });
    // …but the modern formats still preview.
    expect(resolvePreviewDoc("new.docx").kind).toBe("docx");
    expect(resolvePreviewDoc("new.pptx").kind).toBe("pptx");
  });

  it("sends non-text unknowns to the fallback", () => {
    expect(resolvePreviewDoc("movie.mkv")).toEqual({ kind: null, unsupported: "movie.mkv" });
  });
});
