import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import MarkdownViewer from "./MarkdownViewer";

// Same heavy-dependency isolation as the other Preview tests: FileEditor pulls
// Monaco (unresolvable in vitest) and MermaidViewer pulls mermaid.
vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({
  default: ({ code }: { code: string }) => <div data-testid="mermaid-viewer" data-code={code} />,
}));

/**
 * A ```mermaid fence must render its diagram where the author wrote it, not as
 * a strip above the document and not as a code block. Every fence gets its own
 * viewer, in document order.
 */
describe("MarkdownViewer mermaid fences", () => {
  it("renders each diagram in place, in document order", () => {
    const markdown = [
      "# Doc",
      "",
      "intro",
      "",
      "```mermaid",
      "graph TD; A-->B",
      "```",
      "",
      "middle",
      "",
      "```mermaid",
      "graph LR; C-->D",
      "```",
      "",
      "tail",
      "",
    ].join("\n");

    const { container } = render(<MarkdownViewer path="d.md" onOpenFile={() => {}} content={markdown} />);

    const viewers = container.querySelectorAll('[data-testid="mermaid-viewer"]');
    expect(viewers).toHaveLength(2);
    expect(viewers[0]?.getAttribute("data-code")).toBe("graph TD; A-->B");
    expect(viewers[1]?.getAttribute("data-code")).toBe("graph LR; C-->D");

    // Interleaved with the prose around each fence, in source order.
    const sequence = Array.from(container.querySelectorAll<HTMLElement>("p, [data-testid='mermaid-viewer']")).map((el) =>
      el.getAttribute("data-testid") === "mermaid-viewer"
        ? `mmd:${el.getAttribute("data-code")}`
        : (el.textContent ?? "").trim(),
    );
    expect(sequence).toEqual(["intro", "mmd:graph TD; A-->B", "middle", "mmd:graph LR; C-->D", "tail"]);

    // The fence is replaced, not printed as a code block.
    expect(container.querySelector("pre code.language-mermaid")).toBeNull();
    expect(container.querySelector(".not-prose")).not.toBeNull();
  });

  it("leaves other fenced code blocks as code", () => {
    const { container } = render(
      <MarkdownViewer path="d.md" onOpenFile={() => {}} content={"```js\nconst x = 1\n```\n"} />,
    );

    expect(container.querySelector("pre code.language-js")?.textContent).toBe("const x = 1\n");
    expect(container.querySelector('[data-testid="mermaid-viewer"]')).toBeNull();
  });

  it("renders no viewer for a document without a mermaid fence", () => {
    const { container } = render(<MarkdownViewer path="d.md" onOpenFile={() => {}} content={"# Only prose\n"} />);

    expect(container.querySelector('[data-testid="mermaid-viewer"]')).toBeNull();
    expect(container.querySelector("h1")?.textContent).toBe("Only prose");
  });
});
