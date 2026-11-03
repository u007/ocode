import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  renderAsync: vi.fn(async (_buf: ArrayBuffer, body: HTMLElement) => {
    body.innerHTML = "<section class='docx'><p>Alpha beta gamma betamax.</p><p>Alpha BETA delta.</p></section>";
  }),
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
}));
vi.mock("docx-preview", () => ({ renderAsync: mocks.renderAsync }));

import DocxViewer from "./DocxViewer";

// Rendered text mirrors the markdown suite: case-insensitive "beta" → 3,
// case-sensitive → 2, plus whole word → 1.
function scrollerOf(container: HTMLElement): HTMLElement {
  const body = container.querySelector(".docx-body") as HTMLElement | null;
  const el = body?.parentElement ?? null;
  if (!el) throw new Error("docx scroller not found");
  return el;
}

let originalOffsetParent: PropertyDescriptor | undefined;

beforeEach(() => {
  originalOffsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
  Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get: () => document.body });
});

afterEach(() => {
  if (originalOffsetParent) Object.defineProperty(HTMLElement.prototype, "offsetParent", originalOffsetParent);
  else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
});

describe("DocxViewer find", () => {
  it("searches the rendered Word body once it has loaded", async () => {
    const view = render(<DocxViewer path="doc.docx" />);
    await waitFor(() => expect(view.container.textContent).toContain("Alpha beta"));

    fireEvent.click(view.getByLabelText("Find in document"));
    fireEvent.change(view.getByLabelText("Find text"), { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.click(view.getByLabelText("Match case"));
    await waitFor(() => expect(view.container.textContent).toContain("1/2"));

    fireEvent.click(view.getByLabelText("Whole word"));
    await waitFor(() => expect(view.container.textContent).toContain("1/1"));
  });

  it("walks matches and wraps", async () => {
    const view = render(<DocxViewer path="doc.docx" />);
    await waitFor(() => expect(view.container.textContent).toContain("Alpha beta"));
    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "beta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));

    fireEvent.click(view.getByLabelText("Next match"));
    await waitFor(() => expect(view.container.textContent).toContain("2/3"));
    fireEvent.click(view.getByLabelText("Previous match"));
    await waitFor(() => expect(view.container.textContent).toContain("1/3"));
    fireEvent.click(view.getByLabelText("Previous match"));
    await waitFor(() => expect(view.container.textContent).toContain("3/3"));
  });

  it("claims Ctrl/⌘F only while the reader is inside the preview", async () => {
    const view = render(<DocxViewer path="doc.docx" />);
    await waitFor(() => expect(view.container.textContent).toContain("Alpha beta"));

    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull();

    scrollerOf(view.container).focus();
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    const input = await view.findByLabelText("Find text");
    fireEvent.change(input, { target: { value: "delta" } });
    await waitFor(() => expect(view.container.textContent).toContain("1/1"));

    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull());
  });
});
