import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import MarkdownViewer from "./MarkdownViewer";

// Same heavy-dependency isolation as the other Preview tests: FileEditor pulls
// Monaco (unresolvable in vitest) and MermaidViewer pulls mermaid. Neither is
// exercised by the controlled-content path this test renders.
vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

describe("MarkdownViewer full-width prose", () => {
  // Regression: @tailwindcss/typography's `.prose` sets `max-width: 65ch`, which
  // left a large empty gutter on the right of the markdown preview. The chat
  // bubble (MessageBubble.tsx) already opts out with `max-w-none`; the preview
  // container must too.
  it("opts out of the prose max-width so it fills the preview pane", () => {
    const { container } = render(
      <MarkdownViewer path="/doc.md" onOpenFile={() => {}} content={"# Title\n\nBody text."} />,
    );

    const prose = container.querySelector(".prose");
    expect(prose).not.toBeNull();
    expect(prose).toHaveClass("max-w-none");
  });
});
