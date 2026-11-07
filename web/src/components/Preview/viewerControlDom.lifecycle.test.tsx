import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, render } from "@testing-library/react";
import { useContext, useEffect } from "react";
import {
  ViewerReportContext,
  readViewerController,
  registerViewerController,
  useViewerController,
} from "../../lib/viewerContext";

// Mirrors MarkdownViewer: the document controller, then an edit-mode controller
// that registers on the same session key while enabled and unregisters after.
function Viewer({ forceEdit }: { forceEdit: boolean }) {
  useViewerController("doc.md", forceEdit ? {} : { scroll: () => true });
  const key = useContext(ViewerReportContext);
  useEffect(() => {
    if (!key || !forceEdit) return;
    return registerViewerController(key, { scroll: () => false });
  }, [key, forceEdit]);
  return null;
}

function renderIn(forceEdit: boolean) {
  const view = render(
    <ViewerReportContext.Provider value="s1">
      <Viewer forceEdit={forceEdit} />
    </ViewerReportContext.Provider>,
  );
  return {
    ...view,
    setForceEdit(next: boolean) {
      act(() => {
        view.rerender(
          <ViewerReportContext.Provider value="s1">
            <Viewer forceEdit={next} />
          </ViewerReportContext.Provider>,
        );
      });
    },
  };
}

afterEach(() => cleanup());

describe("useViewerController with an edit-mode controller on the same key", () => {
  it("keeps the document controller after an edit-mode controller on the same key comes and goes", () => {
    const view = renderIn(false);
    expect(readViewerController("s1")?.scroll?.({ direction: "down" })).toBe(true);

    view.setForceEdit(true);
    expect(readViewerController("s1")?.scroll?.({ direction: "down" })).toBe(false);

    view.setForceEdit(false);
    expect(readViewerController("s1")).not.toBeNull();
    expect(readViewerController("s1")?.scroll?.({ direction: "down" })).toBe(true);
  });
});
