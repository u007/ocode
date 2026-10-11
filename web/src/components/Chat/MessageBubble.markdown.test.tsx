import { useState } from "react";
import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AssistantText } from "./MessageBubble";

// jsdom does not resolve Tailwind, so these tests pin the class CONTRACT and the
// DOM structure of the markdown renderers, mirroring MessageBubble.links.test.tsx.

describe("AssistantText markdown code rendering", () => {
  it("renders a language-less fenced block as a block, not an inline chip", () => {
    const { container } = render(
      <AssistantText content={"```\ndist/x\n├── a\n└── b\n```"} />,
    );
    const blockCode = container.querySelector("pre code");
    expect(blockCode).not.toBeNull();
    // The inline chip's signature classes must NOT leak onto a fenced block.
    // Before the fix, `isInline = !className` was true for a fence with no info
    // string, so the whole block got the chip (border painted per line fragment,
    // inline padding, no highlighting).
    expect(blockCode!.className).not.toContain("px-1.5");
    expect(blockCode!.className).not.toContain("py-0.5");
    expect(blockCode!.textContent).toContain("dist/x");
  });

  it("still renders inline code as the chip", () => {
    const { container } = render(
      <AssistantText content={"use `foo()` here"} />,
    );
    const inline = container.querySelector("code");
    expect(inline).not.toBeNull();
    expect(inline!.className).toContain("px-1.5");
    expect(inline!.className).toContain("border-border");
  });

  it("cancels the typography plugin's literal backticks on inline code", () => {
    // @tailwindcss/typography wraps inline <code> in literal backticks via
    // code::before/after; the bordered chip makes them redundant.
    const { container } = render(<AssistantText content={"a `b` c"} />);
    const inline = container.querySelector("code")!;
    expect(inline.className).toContain("before:content-none");
    expect(inline.className).toContain("after:content-none");
  });

  it("does not leak react-markdown's node prop onto the DOM", () => {
    const { container } = render(<AssistantText content={"```\nx\n```"} />);
    expect(container.querySelector("pre code")!.hasAttribute("node")).toBe(
      false,
    );
  });
});

// Regression for the "selection flickers to other places while dragging"
// report. AssistantText re-renders on every streaming delta and on every
// ChatPanel render; when the `components` object was rebuilt inline each render,
// react-markdown saw new component types and React UNMOUNTED + REMOUNTED the
// whole rendered subtree, destroying any in-progress text selection. Hoisting
// the config to module scope keeps the DOM node identity stable.
function RerenderHarness({ content }: { content: string }) {
  const [tick, setTick] = useState(0);
  return (
    <div>
      <button type="button" onClick={() => setTick((t) => t + 1)}>
        bump
      </button>
      <span data-testid="tick">{tick}</span>
      <AssistantText content={content} />
    </div>
  );
}

describe("AssistantText render stability", () => {
  it("preserves the rendered code DOM node across re-renders", () => {
    const { container, getByRole, getByTestId } = render(
      <RerenderHarness
        content={"before\n\n```\nline one\nline two\n```\n\nafter"}
      />,
    );
    const before = container.querySelector("pre code");
    expect(before).not.toBeNull();
    fireEvent.click(getByRole("button", { name: "bump" }));
    expect(getByTestId("tick").textContent).toBe("1");
    const after = container.querySelector("pre code");
    expect(after).toBe(before);
  });

  it("preserves inline-code DOM identity across re-renders", () => {
    const { container, getByRole } = render(
      <RerenderHarness content={"use `foo()` and `bar()` here"} />,
    );
    const before = container.querySelector("code");
    fireEvent.click(getByRole("button", { name: "bump" }));
    expect(container.querySelector("code")).toBe(before);
  });
});
