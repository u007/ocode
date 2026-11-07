import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import HtmlViewer from "./HtmlViewer";
import { HTML_FIND_RESULT } from "./htmlFindAgent";
import { ViewerReportContext, readViewerState, resetViewerStates, setViewerDoc, type ViewerControlOutcome } from "../../lib/viewerContext";
import { applyViewerControl, type ViewerControlAction } from "../../lib/viewerControl";

/** jsdom gives a srcDoc iframe no usable contentWindow; stub the one the find hook posts to. */
async function installFrame(view: ReturnType<typeof render>) {
  const iframe = view.getByTitle("HTML preview") as HTMLIFrameElement;
  const win = { postMessage: vi.fn() };
  Object.defineProperty(iframe, "contentWindow", { configurable: true, value: win });
  // jsdom fires a load for a srcdoc frame after mount; the hook re-sends its query on load.
  await new Promise<void>((resolve) => setTimeout(resolve, 60));
  return win;
}

/** The in-frame agent's reply, delivered with the frame as the message source. */
function reply(win: { postMessage: unknown }, count: number, current: number) {
  const event = new MessageEvent("message", { data: { [HTML_FIND_RESULT]: true, count, current } });
  Object.defineProperty(event, "source", { configurable: true, value: win });
  act(() => {
    window.dispatchEvent(event);
  });
}

function run(action: ViewerControlAction): ViewerControlOutcome {
  let outcome: ViewerControlOutcome | undefined;
  act(() => {
    outcome = applyViewerControl("s1", action);
  });
  if (!outcome) throw new Error("applyViewerControl returned nothing");
  return outcome;
}

beforeEach(() => {
  resetViewerStates();
  setViewerDoc("s1", { path: "doc.html", kind: "html" });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("HtmlViewer agent control", () => {
  it("drives find inside the frame and reports the frame's count", async () => {
    const view = render(
      <ViewerReportContext.Provider value="s1">
        <HtmlViewer path="doc.html" content="<p>beta beta beta</p>" />
      </ViewerReportContext.Provider>,
    );
    const win = await installFrame(view);

    expect(run({ id: "f1", action: "find", query: "beta" }).status).toBe("applied");
    reply(win, 3, 0);
    await waitFor(() => expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 3, current: 1 }));

    expect(run({ id: "f2", action: "find_next" }).status).toBe("applied");
    reply(win, 3, 1);
    await waitFor(() => expect(readViewerState("s1")?.find?.current).toBe(2));

    expect(run({ id: "f3", action: "find_close" }).status).toBe("applied");
    await waitFor(() => expect(readViewerState("s1")?.find).toBeNull());
  });

  it("reports scroll and select as not supported in the sandboxed frame", async () => {
    const view = render(
      <ViewerReportContext.Provider value="s1">
        <HtmlViewer path="doc.html" content="<p>text</p>" />
      </ViewerReportContext.Provider>,
    );
    await installFrame(view);
    expect(run({ id: "s1", action: "select", text: "text" }).error).toBe("not supported by this viewer");
    expect(run({ id: "s2", action: "scroll", direction: "down" }).error).toBe("not supported by this viewer");
  });
});
