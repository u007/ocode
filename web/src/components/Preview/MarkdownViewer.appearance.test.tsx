import { describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import MarkdownViewer from "./MarkdownViewer";

// Same heavy-dependency isolation as the other Preview tests: FileEditor pulls
// Monaco (unresolvable in vitest) and MermaidViewer pulls mermaid.
vi.mock("../../api/client", () => ({ api: { getFileContent: vi.fn() } }));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

describe("MarkdownViewer appearance", () => {
  it("keeps prose-invert by default and under the dark override, drops it for light", () => {
    const def = render(<MarkdownViewer path="/d.md" onOpenFile={() => {}} content="# Title" />);
    expect(def.container.querySelector(".prose")).toHaveClass("prose-invert");
    def.unmount();

    const dark = render(
      <MarkdownViewer path="/d.md" onOpenFile={() => {}} content="# Title" appearance="dark" />,
    );
    expect(dark.container.querySelector(".prose")).toHaveClass("prose-invert");
    dark.unmount();

    const light = render(
      <MarkdownViewer path="/d.md" onOpenFile={() => {}} content="# Title" appearance="light" />,
    );
    expect(light.container.querySelector(".prose")).not.toHaveClass("prose-invert");
  });
});
