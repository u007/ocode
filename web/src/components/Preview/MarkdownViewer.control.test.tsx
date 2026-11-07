import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";

vi.mock("../../api/client", () => ({
  api: { getFileContent: vi.fn().mockResolvedValue({ content: "", is_binary: false }) },
}));
vi.mock("../Files/FileEditor", () => ({ default: () => null }));
vi.mock("./MermaidViewer", () => ({ default: () => null }));

import MarkdownViewer from "./MarkdownViewer";
import { api } from "../../api/client";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc, type ViewerControlOutcome } from "../../lib/viewerContext";
import { applyViewerControl, type ViewerControlAction } from "../../lib/viewerControl";

// Rendered text has "beta" three times (case-insensitive): beta, betamax, BETA.
const MD = "# Title\n\nAlpha beta gamma betamax.\n\n## Install\n\nAlpha BETA delta.\n";

/** Own-property stub on HTMLElement.prototype, undone in afterEach. jsdom has no layout or scrollBy. */
const stubs: Array<() => void> = [];
function stubElementProp(name: string, value: unknown): void {
  const had = Object.getOwnPropertyDescriptor(HTMLElement.prototype, name);
  Object.defineProperty(HTMLElement.prototype, name, { value, configurable: true, writable: true });
  stubs.push(() => {
    if (had) Object.defineProperty(HTMLElement.prototype, name, had);
    else delete (HTMLElement.prototype as unknown as Record<string, unknown>)[name];
  });
}

let scrollBy: ReturnType<typeof vi.fn>;
let scrollIntoView: ReturnType<typeof vi.fn>;

beforeEach(() => {
  resetViewerStates();
  setViewerDoc("s1", { path: "doc.md", kind: "markdown" });
  scrollBy = vi.fn();
  scrollIntoView = vi.fn();
  stubElementProp("scrollBy", scrollBy);
  stubElementProp("scrollIntoView", scrollIntoView);
  stubElementProp("clientHeight", 500);
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  while (stubs.length) stubs.pop()?.();
});

function renderViewer(content: string | undefined = MD) {
  return render(
    <ViewerReportContext.Provider value="s1">
      <MarkdownViewer path="doc.md" content={content} onOpenFile={() => {}} />
    </ViewerReportContext.Provider>,
  );
}

function run(action: ViewerControlAction): ViewerControlOutcome {
  let outcome: ViewerControlOutcome | undefined;
  act(() => {
    outcome = applyViewerControl("s1", action);
  });
  if (!outcome) throw new Error("applyViewerControl returned nothing");
  return outcome;
}

describe("MarkdownViewer agent control", () => {
  it("scrolls to a heading, and fails loudly when the heading is absent", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("h2")).not.toBeNull());
    expect(run({ id: "h1", action: "scroll", heading: "install" })).toEqual({ id: "h1", status: "applied" });
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "start" });
    expect(run({ id: "h2", action: "scroll", heading: "usage" })).toEqual({ id: "h2", status: "failed", error: "heading not found: usage" });
  });

  it("steps the scroller by 80% of its height in the given direction", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("h1")).not.toBeNull());
    expect(run({ id: "d1", action: "scroll", direction: "down" }).status).toBe("applied");
    expect(scrollBy).toHaveBeenLastCalledWith({ top: 400 });
    expect(run({ id: "d2", action: "scroll", direction: "up" }).status).toBe("applied");
    expect(scrollBy).toHaveBeenLastCalledWith({ top: -400 });
  });

  it("reports page targets as not supported by a document viewer", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("h1")).not.toBeNull());
    expect(run({ id: "p1", action: "scroll", page: 2 })).toEqual({ id: "p1", status: "failed", error: "not supported by this viewer" });
  });

  it("selects the first matching text and reports it as the selection", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("p")).not.toBeNull());
    expect(run({ id: "s1", action: "select", text: "gamma" })).toEqual({ id: "s1", status: "applied" });
    expect(window.getSelection()?.toString()).toBe("gamma");
    expect(readViewerState("s1")?.selection).toBe("gamma");
    expect(run({ id: "s2", action: "select", text: "zeta" })).toEqual({ id: "s2", status: "failed", error: "text not found" });
  });

  it("opens find, walks matches with wrap, reports the state, and closes", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("p")).not.toBeNull());
    expect(run({ id: "f1", action: "find", query: "beta" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 3, current: 1 }));
    expect(run({ id: "f2", action: "find_next" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find?.current).toBe(2));
    expect(run({ id: "f3", action: "find_close" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toBeNull());
  });

  it("fails next/prev when the find bar is not open", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("p")).not.toBeNull());
    expect(run({ id: "n1", action: "find_next" })).toEqual({ id: "n1", status: "failed", error: "find is not open" });
  });

  it("registers an empty controller for a binary file, so every action is unsupported", async () => {
    vi.mocked(api.getFileContent).mockResolvedValueOnce({ content: "", is_binary: true });
    render(
      <ViewerReportContext.Provider value="s1">
        <MarkdownViewer path="doc.md" onOpenFile={() => {}} />
      </ViewerReportContext.Provider>,
    );
    await waitFor(() => expect(run({ id: "b1", action: "scroll", direction: "down" }).status).toBe("failed"));
    expect(run({ id: "b1b", action: "scroll", direction: "down" }).error).toBe("not supported by this viewer");
  });
});
