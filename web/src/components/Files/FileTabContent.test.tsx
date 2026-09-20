import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import FileTabContent from "./FileTabContent";
import { previewViewKey, savePreviewViewState } from "../../lib/previewViewState";

// jsdom has no PointerEvent (see UnifiedTabBar.drag.test.tsx) — the split
// divider drag relies on clientX propagation, so polyfill it before import-time
// handlers are exercised.
if (typeof window.PointerEvent === "undefined") {
  class PointerEventPolyfill extends MouseEvent {
    pointerId: number;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
    }
  }
  // @ts-expect-error assigning a minimal polyfill onto jsdom's window
  window.PointerEvent = PointerEventPolyfill;
}

// Keep the routing test free of Monaco / pdf.js / docx-preview dependencies.
vi.mock("./FileEditor", () => ({ default: () => <div data-testid="monaco" /> }));
vi.mock("../Preview/PreviewSurface", () => ({
  default: ({ kind, content, page, slide }: { kind: string; content?: string; page?: number; slide?: number }) => (
    <div
      data-testid={`preview-${kind}`}
      data-content={content ?? ""}
      data-page={String(page ?? "")}
      data-slide={String(slide ?? "")}
    />
  ),
}));
vi.mock("../Preview/LegacyOfficePane", () => ({ default: () => <div data-testid="legacy-office" /> }));

async function testIdFor(path: string): Promise<string> {
  const { container } = render(<FileTabContent path={path} content="x" />);
  // Monaco is code-split (React.lazy), so the editor surface appears only after
  // the lazy chunk resolves; the first paint is the Suspense fallback.
  await waitFor(() => expect(container.querySelector("[data-testid]")).not.toBeNull());
  const el = container.querySelector("[data-testid]");
  return el?.getAttribute("data-testid") ?? "";
}

async function renderMarkdown(content = "# Hello") {
  const utils = render(<FileTabContent path="README.md" content={content} />);
  await waitFor(() => expect(screen.getByTestId("monaco")).toBeInTheDocument());
  return utils;
}

describe("FileTabContent", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("previews binary preview-only formats instead of Monaco", async () => {
    expect(await testIdFor("docs/report.pdf")).toBe("preview-pdf");
    expect(await testIdFor("docs/spec.docx")).toBe("preview-docx");
    expect(await testIdFor("docs/deck.pptx")).toBe("preview-pptx");
    expect(await testIdFor("docs/budget.xlsx")).toBe("preview-excel");
    expect(await testIdFor("assets/logo.png")).toBe("preview-image");
    expect(await testIdFor("media/song.mp3")).toBe("preview-audio");
    expect(await testIdFor("media/clip.mp4")).toBe("preview-video");
  });

  it("falls back to the OS-open pane for legacy .doc/.ppt", async () => {
    expect(await testIdFor("docs/old.doc")).toBe("legacy-office");
    expect(await testIdFor("docs/slides.ppt")).toBe("legacy-office");
  });

  it("keeps text, code, markdown and mermaid in the editor", async () => {
    expect(await testIdFor("src/main.go")).toBe("monaco");
    expect(await testIdFor("README.md")).toBe("monaco");
    expect(await testIdFor("flow.mmd")).toBe("monaco");
    expect(await testIdFor("Makefile")).toBe("monaco");
  });
});

describe("FileTabContent markdown modes", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("defaults markdown to Edit with no rendered preview", async () => {
    await renderMarkdown();
    expect(screen.getByRole("button", { name: /edit/i })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("monaco")).toBeInTheDocument();
    expect(screen.queryByTestId("preview-markdown")).toBeNull();
  });

  it("renders a full-width preview while keeping Monaco mounted", async () => {
    await renderMarkdown("# Hello");
    fireEvent.click(screen.getByRole("button", { name: /preview/i }));

    const preview = screen.getByTestId("preview-markdown");
    expect(preview).toHaveAttribute("data-content", "# Hello");
    expect(screen.getByRole("button", { name: /preview/i })).toHaveAttribute("aria-pressed", "true");
    // The editor is hidden, not unmounted — cursor/scroll/undo survive.
    expect(screen.getByTestId("monaco")).toBeInTheDocument();
    expect(screen.getByTestId("monaco").parentElement).toHaveClass("hidden");
  });

  it("shows the editor and a resizable divider in Split mode", async () => {
    await renderMarkdown("# Hello");
    fireEvent.click(screen.getByRole("button", { name: /split/i }));

    expect(screen.getByTestId("monaco")).toBeInTheDocument();
    expect(screen.getByTestId("monaco").parentElement).not.toHaveClass("hidden");
    expect(screen.getByTestId("preview-markdown")).toBeInTheDocument();
    expect(screen.getByRole("separator", { name: /resize editor and preview/i })).toBeInTheDocument();
  });

  it("feeds live editor content into the split preview", async () => {
    const { rerender } = await renderMarkdown("# One");
    fireEvent.click(screen.getByRole("button", { name: /split/i }));
    expect(screen.getByTestId("preview-markdown")).toHaveAttribute("data-content", "# One");

    rerender(<FileTabContent path="README.md" content="# Two" />);
    await waitFor(() =>
      expect(screen.getByTestId("preview-markdown")).toHaveAttribute("data-content", "# Two"),
    );
  });

  it("resizes the split by dragging the divider and resets on double-click", async () => {
    await renderMarkdown();
    fireEvent.click(screen.getByRole("button", { name: /split/i }));

    const handle = screen.getByRole("separator", { name: /resize editor and preview/i });
    const container = handle.parentElement as HTMLElement;
    container.getBoundingClientRect = () =>
      ({ left: 0, top: 0, width: 1000, height: 600, right: 1000, bottom: 600, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;

    const editorPane = screen.getByTestId("monaco").parentElement as HTMLElement;
    expect(editorPane.style.width).toBe("50%");

    fireEvent.pointerDown(handle, { clientX: 500, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 300, pointerId: 1 });
    expect(editorPane.style.width).toBe("30%");

    // Drag stops tracking after pointerup.
    fireEvent.pointerUp(window, { pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 800, pointerId: 1 });
    expect(editorPane.style.width).toBe("30%");

    fireEvent.doubleClick(handle);
    expect(editorPane.style.width).toBe("50%");
  });

  it("treats .markdown as markdown too", async () => {
    render(<FileTabContent path="docs/guide.markdown" content="# Hi" />);
    await waitFor(() => expect(screen.getByTestId("monaco")).toBeInTheDocument());
    expect(screen.getByRole("group", { name: /markdown view mode/i })).toBeInTheDocument();
  });

  it("adds no markdown mode switch to code or plain-text tabs", async () => {
    render(<FileTabContent path="src/main.go" content="package main" />);
    await waitFor(() => expect(screen.getByTestId("monaco")).toBeInTheDocument());
    expect(screen.queryByRole("group", { name: /markdown view mode/i })).toBeNull();
    expect(screen.queryByTestId("preview-markdown")).toBeNull();
  });
});

describe("FileTabContent viewer-state persistence", () => {
  beforeEach(() => localStorage.clear());

  it("restores a PDF's last page on mount", async () => {
    savePreviewViewState(previewViewKey("docs/report.pdf", "/proj"), { page: 9 });
    render(<FileTabContent path="docs/report.pdf" projectRoot="/proj" content="" />);
    await waitFor(() => expect(screen.getByTestId("preview-pdf")).toBeInTheDocument());
    expect(screen.getByTestId("preview-pdf").getAttribute("data-page")).toBe("9");
  });

  it("keys the restored page by project so two projects' same-path PDFs differ", async () => {
    savePreviewViewState(previewViewKey("docs/report.pdf", "/proj-a"), { page: 9 });
    render(<FileTabContent path="docs/report.pdf" projectRoot="/proj-b" content="" />);
    await waitFor(() => expect(screen.getByTestId("preview-pdf")).toBeInTheDocument());
    expect(screen.getByTestId("preview-pdf").getAttribute("data-page")).toBe("1");
  });
});
