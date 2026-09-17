import { describe, expect, it, vi } from "vitest";
import { render, waitFor } from "@testing-library/react";
import FileTabContent from "./FileTabContent";

// Keep the routing test free of Monaco / pdf.js / docx-preview dependencies.
vi.mock("./FileEditor", () => ({ default: () => <div data-testid="monaco" /> }));
vi.mock("../Preview/PreviewSurface", () => ({
  default: ({ kind }: { kind: string }) => <div data-testid={`preview-${kind}`} />,
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

describe("FileTabContent", () => {
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
