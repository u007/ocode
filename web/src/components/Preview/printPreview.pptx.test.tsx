import { describe, expect, it, vi } from "vitest";
import { api } from "../../api/client";
import { buildPrintDocument } from "./printPreview";

// SlideCard escapes its text, so a real card can never carry markup into the
// print document. This file stands in a card that emits raw markup, to prove
// the sanitizer (not the viewer) removes it. It lives apart from
// printPreview.kinds.test.ts, which needs the real SlideCard.

vi.mock("../../api/client", () => ({ api: { fetchFileRaw: vi.fn(), getFileContent: vi.fn() } }));

vi.mock("./pptxDeck", () => ({
  parsePptx: vi.fn(async () => [{ index: 1, aspect: 1.5, blocks: [], images: [] }]),
}));

vi.mock("./PptxViewer", () => ({
  SlideCard: () => (
    <div
      dangerouslySetInnerHTML={{
        __html:
          '<p onclick="x()">hi</p><img src="x" onerror="alert(1)"><a href="javascript:alert(2)">l</a><script>alert(3)</script><span style="color:red">kept</span>',
      }}
    />
  ),
}));

describe("buildPrintDocument: pptx sanitizing", () => {
  it("removes script, event handlers and javascript: URLs from each slide, and keeps inline styles", async () => {
    vi.mocked(api.fetchFileRaw).mockResolvedValue(new ArrayBuffer(0));

    const html = await buildPrintDocument("pptx", { origin: "disk", path: "deck.pptx" }, "deck.pptx");

    expect(html).toContain('<span style="color:red">kept</span>');
    expect(html).not.toContain("alert(");
    expect(html).not.toContain("onclick");
    expect(html).not.toContain("onerror");
    expect(html).not.toContain("javascript:");
  });
});
