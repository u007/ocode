import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import HtmlViewer from "./HtmlViewer";
import { HTML_FIND_MESSAGE, HTML_FIND_RESULT } from "./htmlFindAgent";

/** jsdom gives a srcDoc iframe no usable contentWindow; stub the one the hook
 *  posts to, which is also the `source` the frame would report. */
async function installFrame(view: ReturnType<typeof render>) {
  const iframe = view.getByTitle("HTML preview") as HTMLIFrameElement;
  const win = { postMessage: vi.fn() };
  Object.defineProperty(iframe, "contentWindow", { configurable: true, value: win });
  // jsdom fires a load for a srcdoc frame after mount. The hook re-runs the
  // query on load, which would reset the count mid-test, so wait it out (or
  // give up after a beat when it already fired).
  await new Promise<void>((resolve) => {
    let settled = false;
    const done = () => {
      if (settled) return;
      settled = true;
      iframe.removeEventListener("load", done);
      resolve();
    };
    iframe.addEventListener("load", done);
    setTimeout(done, 60);
  });
  return { iframe, win };
}

let originalOffsetParent: PropertyDescriptor | undefined;

beforeEach(() => {
  // jsdom has no layout; the preview's visibility gate reads offsetParent.
  originalOffsetParent = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetParent");
  Object.defineProperty(HTMLElement.prototype, "offsetParent", { configurable: true, get: () => document.body });
});

afterEach(() => {
  if (originalOffsetParent) Object.defineProperty(HTMLElement.prototype, "offsetParent", originalOffsetParent);
  else delete (HTMLElement.prototype as unknown as Record<string, unknown>).offsetParent;
});

/** Did the parent ever post a message matching these fields? Order-agnostic:
 *  the hook may re-send a search around a step, which is not what is under test. */
function sent(win: { postMessage: ReturnType<typeof vi.fn> }, expected: Record<string, unknown>) {
  return win.postMessage.mock.calls.some(([msg]) => {
    if (!msg || typeof msg !== "object") return false;
    return Object.entries(expected).every(([k, v]) => (msg as Record<string, unknown>)[k] === v);
  });
}

function dispatchResult(win: { postMessage: unknown }, count: number, current: number) {
  const event = new MessageEvent("message", { data: { [HTML_FIND_RESULT]: true, count, current } });
  Object.defineProperty(event, "source", { configurable: true, value: win });
  act(() => {
    window.dispatchEvent(event);
  });
}

describe("HtmlViewer find", () => {
  it("drives the in-frame agent over postMessage", async () => {
    const view = render(<HtmlViewer content="<h1>Alpha beta gamma</h1>" />);
    const { win } = await installFrame(view);

    fireEvent.click(view.getByLabelText("Find in document"));
    await waitFor(() =>
      expect(win.postMessage).toHaveBeenCalledWith(
        expect.objectContaining({ [HTML_FIND_MESSAGE]: true, action: "search" }),
        "*",
      ),
    );

    fireEvent.change(view.getByLabelText("Find text"), { target: { value: "beta" } });
    await waitFor(() => expect(sent(win, { action: "search", query: "beta" })).toBe(true));

    fireEvent.click(view.getByLabelText("Match case"));
    await waitFor(() =>
      expect(sent(win, { action: "search", query: "beta", caseSensitive: true })).toBe(true),
    );
  });

  it("shows the frame's match count and steps matches", async () => {
    const view = render(<HtmlViewer content="<p>one two two three</p>" />);
    const { win } = await installFrame(view);

    fireEvent.click(view.getByLabelText("Find in document"));
    fireEvent.change(view.getByLabelText("Find text"), { target: { value: "two" } });
    dispatchResult(win, 2, 0);
    await waitFor(() => expect(view.container.textContent).toContain("1/2"));

    fireEvent.click(view.getByLabelText("Next match"));
    await waitFor(() => expect(sent(win, { action: "step", dir: 1 })).toBe(true));
    dispatchResult(win, 2, 1);
    await waitFor(() => expect(view.container.textContent).toContain("2/2"));
  });

  it("closes the bar and tells the frame to drop highlights", async () => {
    const view = render(<HtmlViewer content="<p>text</p>" />);
    const { win } = await installFrame(view);

    fireEvent.click(view.getByLabelText("Find in document"));
    fireEvent.click(view.getByLabelText("Close find"));
    await waitFor(() => expect(sent(win, { action: "close" })).toBe(true));
    expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull();
  });

  it("re-searches after the frame reloads", async () => {
    const view = render(<HtmlViewer content="<p>text</p>" />);
    const { iframe, win } = await installFrame(view);

    fireEvent.click(view.getByLabelText("Find in document"));
    const input = view.getByLabelText("Find text");
    fireEvent.change(input, { target: { value: "text" } });
    const calls = vi.mocked(win.postMessage).mock.calls.length;

    // srcDoc edits reload the frame; the load event must re-send the query.
    fireEvent.load(iframe);
    await waitFor(() => expect(vi.mocked(win.postMessage).mock.calls.length).toBeGreaterThan(calls));
    expect(sent(win, { action: "search", query: "text" })).toBe(true);
  });

  it("claims Ctrl/⌘F only while the reader is inside the preview", async () => {
    const view = render(<HtmlViewer content="<p>text</p>" />);
    await installFrame(view);

    window.getSelection()?.removeAllRanges();
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    expect(view.container.querySelector('input[aria-label="Find text"]')).toBeNull();

    // A selection anchored inside the preview (the same signal the browser
    // gives when the reader has clicked into the frame) claims the shortcut.
    const button = view.getByLabelText("Find in document");
    window.getSelection()?.selectAllChildren(button);
    fireEvent.keyDown(window, { key: "f", ctrlKey: true });
    await view.findByLabelText("Find text");
    window.getSelection()?.removeAllRanges();
  });

  it("appends the find agent to the rendered document", () => {
    const view = render(<HtmlViewer content="<html><body><p>hello</p></body></html>" />);
    const iframe = view.getByTitle("HTML preview");
    const srcDoc = iframe.getAttribute("srcDoc") ?? "";
    expect(srcDoc).toContain("<p>hello</p>");
    expect(srcDoc).toContain("__ocodeFind");
    expect(srcDoc.indexOf("<script>")).toBeLessThan(srcDoc.indexOf("</body>"));
  });
});
