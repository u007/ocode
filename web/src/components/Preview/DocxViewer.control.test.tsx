import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  renderAsync: vi.fn(async (_buf: ArrayBuffer, body: HTMLElement) => {
    body.innerHTML = "<section class='docx'><h2>Install</h2><p>Alpha beta gamma.</p></section>";
  }),
}));

vi.mock("../../api/client", () => ({
  api: { fetchFileRaw: vi.fn().mockResolvedValue(new ArrayBuffer(0)) },
}));
vi.mock("docx-preview", () => ({ renderAsync: mocks.renderAsync }));

import DocxViewer from "./DocxViewer";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc, type ViewerControlOutcome } from "../../lib/viewerContext";
import { applyViewerControl, type ViewerControlAction } from "../../lib/viewerControl";

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
  setViewerDoc("s1", { path: "doc.docx", kind: "docx" });
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

function renderViewer() {
  return render(
    <ViewerReportContext.Provider value="s1">
      <DocxViewer path="doc.docx" />
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

describe("DocxViewer agent control", () => {
  it("scrolls to a heading and steps by direction", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("h2")).not.toBeNull());
    expect(run({ id: "h1", action: "scroll", heading: "INSTALL" })).toEqual({ id: "h1", status: "applied" });
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "start" });
    expect(run({ id: "h2", action: "scroll", heading: "usage" }).error).toBe("heading not found: usage");
    expect(run({ id: "d1", action: "scroll", direction: "down" }).status).toBe("applied");
    expect(scrollBy).toHaveBeenLastCalledWith({ top: 400 });
  });

  it("selects the first matching text and reports it", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("p")).not.toBeNull());
    expect(run({ id: "s1", action: "select", text: "beta" }).status).toBe("applied");
    expect(readViewerState("s1")?.selection).toBe("beta");
    expect(run({ id: "s2", action: "select", text: "zeta" }).error).toBe("text not found");
  });

  it("opens find on the rendered body, reports the count, and closes", async () => {
    renderViewer();
    await waitFor(() => expect(document.querySelector("p")).not.toBeNull());
    expect(run({ id: "f1", action: "find", query: "beta" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 1, current: 1 }));
    expect(run({ id: "f2", action: "find_close" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toBeNull());
  });
});
