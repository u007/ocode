import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";

// Same heavy-dependency isolation as the other Markdown tests.
vi.mock("../../api/client", () => ({
  api: { getFileContent: vi.fn().mockResolvedValue({ content: "", is_binary: false }) },
}));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

import MarkdownViewer from "./MarkdownViewer";

// Rendered text: "Alpha beta gamma betamax." + "Alpha BETA delta."
//   case-insensitive "beta" → 3 (beta, betamax, BETA)
//   case-sensitive   "beta" → 2 (beta, betamax)
//   + whole word              → 1 (only the standalone "beta")
const MD = "# Title\n\nAlpha beta gamma betamax.\n\nAlpha BETA delta.\n";

function scrollerOf(container: HTMLElement): HTMLElement {
  const el = container.querySelector(".prose") as HTMLElement | null;
  if (!el) throw new Error("prose scroller not found");
  return el;
}

/** jsdom has no Range rects; one rect per range is enough for the overlay. */
function stubRangeRects() {
  const proto = Range.prototype as unknown as { getClientRects?: () => DOMRectList };
  const native = proto.getClientRects;
  proto.getClientRects = function () {
    const rect = { left: 10, top: 20, width: 30, height: 12, x: 10, y: 20, right: 40, bottom: 32, toJSON: () => ({}) };
    return [rect] as unknown as DOMRectList;
  };
  return () => {
    if (native) proto.getClientRects = native;
    else delete proto.getClientRects;
  };
}

let originalOffsetParent: PropertyDescriptor | undefined;

beforeEach(() => {
  // jsdom reports offsetParent === null everywhere, which the visibility gate
  // treats as "pane hidden". ChatPanel's tests stub it the same way.
  originalOffsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
  Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get: () => document.body });
});

afterEach(() => {
  if (originalOffsetParent) Object.defineProperty(HTMLElement.prototype, "offsetParent", originalOffsetParent);
  else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
});

describe("MarkdownViewer find", () => {
  it("searches the rendered prose with match case and whole word toggles", async () => {
    const view = render(<MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />);
    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;

    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.click(view.getByLabelText("Match case"));
    await waitFor(() => expect(view.container.textContent).toContain("1/2"));

    fireEvent.click(view.getByLabelText("Whole word"));
    await waitFor(() => expect(view.container.textContent).toContain("1/1"));
  });

  it("walks matches and wraps in both directions", async () => {
    const view = render(<MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />);
    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.click(view.getByLabelText("Next match"));
    await waitFor(() => expect(view.container.textContent).toContain("2/3"));
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true });
    await waitFor(() => expect(view.container.textContent).toContain("3/3"));
  });

  it("paints highlight rects and clears them when the bar closes", async () => {
    const restore = stubRangeRects();
    try {
      const view = render(<MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />);
      fireEvent.click(view.getByLabelText("Find in document"));
      fireEvent.change(view.getByLabelText("Find text"), { target: { value: "beta" } });

      await waitFor(() =>
        expect(
          Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) =>
            el.className.includes("bg-yellow-300/45"),
          ).length,
        ).toBeGreaterThan(0),
      );
      // The current match is orange, the rest yellow.
      expect(view.container.querySelector(".bg-orange-500\\/60")).toBeTruthy();

      fireEvent.click(view.getByLabelText("Close find"));
      expect(
        Array.from(view.container.querySelectorAll('[aria-hidden="true"]')).filter((el) =>
          el.className.includes("bg-yellow-300/45"),
        ).length,
      ).toBe(0);
      expect(view.container.querySelector(".bg-orange-500\\/60")).toBeNull();
    } finally {
      restore();
    }
  });

  it("claims Ctrl/⌘F only while the reader is inside the preview", async () => {
    const view = render(<MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />);
    const scroller = scrollerOf(view.container);

    // Focus elsewhere: the chat's own find bar keeps the shortcut.
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull();

    scroller.focus();
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    const input = await view.findByLabelText("Find text");
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.keyDown(window, { key: "F3" });
    await waitFor(() => expect(view.container.textContent).toContain("2/3"));
    fireEvent.keyDown(window, { key: "F3", shiftKey: true });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull());
  });

  it("re-indexes when the document content changes", async () => {
    const view = render(<MarkdownViewer path="doc.md" content={MD} onOpenFile={() => {}} />);
    fireEvent.click(view.getByLabelText("Find in document"));
    fireEvent.change(view.getByLabelText("Find text"), { target: { value: "delta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/1"));

    view.rerender(<MarkdownViewer path="doc.md" content={`${MD}\n\ndelta delta\n`} onOpenFile={() => {}} />);
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));
  });
});
