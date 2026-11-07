import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { applyViewerControl } from "../../lib/viewerControl";
import {
  ViewerReportContext,
  readViewerController,
  readViewerState,
  resetViewerStates,
  setViewerDoc,
} from "../../lib/viewerContext";
import { useMonacoViewerReport } from "./useMonacoViewerReport";

type Range = { startLineNumber: number; startColumn: number; endLineNumber: number; endColumn: number };

const LINES = ["alpha", "beta", "gamma", "beta again", "delta"];

/** Every occurrence of `text` in `lines`, as Monaco's findMatches reports them. */
function occurrences(lines: string[], text: string): { range: Range }[] {
  const out: { range: Range }[] = [];
  lines.forEach((line, i) => {
    let at = line.indexOf(text);
    while (text !== "" && at >= 0) {
      out.push({
        range: {
          startLineNumber: i + 1,
          startColumn: at + 1,
          endLineNumber: i + 1,
          endColumn: at + 1 + text.length,
        },
      });
      at = line.indexOf(text, at + text.length);
    }
  });
  return out;
}

/** A fake of the Monaco editor surface the hook uses, over a fixed set of lines. */
function fakeEditor(lines: string[] = LINES) {
  const model = {
    getLineCount: () => lines.length,
    getLineMaxColumn: (n: number) => lines[n - 1].length + 1,
    getValueInRange: (r: { startLineNumber: number; endLineNumber: number }) =>
      lines.slice(r.startLineNumber - 1, r.endLineNumber).join("\n"),
    findMatches: vi.fn((text: string, _a: boolean, _b: boolean, _c: boolean, _d: unknown, _e: boolean, limit?: number) =>
      occurrences(lines, text).slice(0, limit),
    ),
  };
  return {
    getModel: () => model,
    setSelection: vi.fn(),
    revealLineInCenter: vi.fn(),
    revealRangeInCenter: vi.fn(),
    getLayoutInfo: () => ({ height: 500 }),
    getScrollTop: () => 1000,
    setScrollTop: vi.fn(),
  };
}

type Editor = ReturnType<typeof fakeEditor>;

function renderMonaco(path: string, enabled = true) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <ViewerReportContext.Provider value="s1">{children}</ViewerReportContext.Provider>
  );
  return renderHook(({ enabled: on }: { enabled: boolean }) => useMonacoViewerReport(path, on), {
    wrapper,
    initialProps: { enabled },
  });
}

beforeEach(() => {
  resetViewerStates();
  setViewerDoc("s1", { path: "notes.txt", kind: "text" });
});

afterEach(() => {
  cleanup();
  resetViewerStates();
});

/** Mounts a fake editor through the hook, as FileEditor does after Monaco mounts. */
function mount(view: ReturnType<typeof renderMonaco>, ed: Editor = fakeEditor()): Editor {
  act(() => view.result.current.onEditorMount(ed as never));
  return ed;
}

describe("useMonacoViewerReport selection", () => {
  it("reports the selected whole lines and their text, and clears on collapse", () => {
    const view = renderMonaco("notes.txt");
    mount(view);

    act(() => view.result.current.onSelectionChange({ startLine: 2, endLine: 3 }));
    expect(readViewerState("s1")?.location.lines).toEqual({ start: 2, end: 3 });
    expect(readViewerState("s1")?.selection).toBe("beta\ngamma");

    act(() => view.result.current.onSelectionChange(null));
    expect(readViewerState("s1")?.location.lines).toBeUndefined();
    expect(readViewerState("s1")?.selection).toBeNull();
  });

  it("caps the selected text at 1500 characters", () => {
    const long = ["x".repeat(2000)];
    const view = renderMonaco("notes.txt");
    mount(view, fakeEditor(long));

    act(() => view.result.current.onSelectionChange({ startLine: 1, endLine: 1 }));
    expect(readViewerState("s1")?.selection).toHaveLength(1500);
  });

  it("ignores a selection before the editor has mounted", () => {
    const view = renderMonaco("notes.txt");
    act(() => view.result.current.onSelectionChange({ startLine: 1, endLine: 1 }));
    expect(readViewerState("s1")?.location.lines).toBeUndefined();
  });
});

describe("useMonacoViewerReport viewport", () => {
  it("reports the visible line span and its text", () => {
    const view = renderMonaco("notes.txt");
    act(() => view.result.current.onViewportChange({ start: 1, end: 2, text: "alpha\nbeta" }));
    expect(readViewerState("s1")?.location.visibleLines).toEqual({ start: 1, end: 2 });
    expect(readViewerState("s1")?.visibleText).toBe("alpha\nbeta");
  });
});

describe("useMonacoViewerReport controller", () => {
  it("scrolls to a line, centring it", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);
    const outcome = applyViewerControl("s1", { id: "a", action: "scroll", line: 3 });
    expect(outcome).toEqual({ id: "a", status: "applied" });
    expect(ed.revealLineInCenter).toHaveBeenCalledWith(3);
  });

  it("fails a line outside the document", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "scroll", line: 99 })).toEqual({
      id: "a",
      status: "failed",
      error: "line 99 out of range 1-5",
    });
  });

  it("scrolls by 0.8 of the editor height in the chosen direction", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);
    applyViewerControl("s1", { id: "down", action: "scroll", direction: "down" });
    expect(ed.setScrollTop).toHaveBeenLastCalledWith(1000 + 400);
    applyViewerControl("s1", { id: "up", action: "scroll", direction: "up" });
    expect(ed.setScrollTop).toHaveBeenLastCalledWith(1000 - 400);
  });

  it("reports an unsupported scroll target as not supported", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "scroll", page: 2 })).toEqual({
      id: "a",
      status: "failed",
      error: "not supported by this viewer",
    });
  });

  it("selects and reveals a line range, through the end of its last line", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);
    applyViewerControl("s1", { id: "a", action: "select", lines: { start: 2, end: 3 } });
    const range = { startLineNumber: 2, startColumn: 1, endLineNumber: 3, endColumn: "gamma".length + 1 };
    expect(ed.setSelection).toHaveBeenCalledWith(range);
    expect(ed.revealRangeInCenter).toHaveBeenCalledWith(range);
  });

  it("fails a line range that is reversed or out of bounds", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "select", lines: { start: 4, end: 2 } }).error).toBe(
      "lines 4-2 out of range 1-5",
    );
  });

  it("selects the first match of a text needle", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "select", text: "beta" }).status).toBe("applied");
    expect(ed.setSelection).toHaveBeenCalledWith({
      startLineNumber: 2,
      startColumn: 1,
      endLineNumber: 2,
      endColumn: 5,
    });
  });

  it("fails a text needle that does not occur", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "select", text: "zeta" })).toEqual({
      id: "a",
      status: "failed",
      error: "text not found",
    });
  });

  it("fails every controller call while the editor is not mounted", () => {
    renderMonaco("notes.txt");
    expect(applyViewerControl("s1", { id: "a", action: "scroll", line: 1 })).toEqual({
      id: "a",
      status: "failed",
      error: "editor not ready",
    });
  });
});

describe("useMonacoViewerReport find", () => {
  it("opens a find, steps through matches with wraparound, and closes it", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);

    expect(applyViewerControl("s1", { id: "open", action: "find", query: "beta" }).status).toBe("applied");
    expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 2, current: 1 });
    expect(ed.setSelection).toHaveBeenLastCalledWith(expect.objectContaining({ startLineNumber: 2 }));

    applyViewerControl("s1", { id: "next1", action: "find_next" });
    expect(readViewerState("s1")?.find).toEqual({ query: "beta", count: 2, current: 2 });
    expect(ed.setSelection).toHaveBeenLastCalledWith(expect.objectContaining({ startLineNumber: 4 }));

    applyViewerControl("s1", { id: "next2", action: "find_next" });
    expect(readViewerState("s1")?.find?.current).toBe(1);

    applyViewerControl("s1", { id: "prev", action: "find_prev" });
    expect(readViewerState("s1")?.find?.current).toBe(2);

    expect(applyViewerControl("s1", { id: "close", action: "find_close" }).status).toBe("applied");
    expect(readViewerState("s1")?.find).toBeNull();
  });

  it("reports zero matches without moving the selection", () => {
    const view = renderMonaco("notes.txt");
    const ed = mount(view);
    applyViewerControl("s1", { id: "open", action: "find", query: "zeta" });
    expect(readViewerState("s1")?.find).toEqual({ query: "zeta", count: 0, current: 0 });
    expect(ed.setSelection).not.toHaveBeenCalled();
  });

  it("fails next or prev when no find is open", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(applyViewerControl("s1", { id: "a", action: "find_next" })).toEqual({
      id: "a",
      status: "failed",
      error: "find not open",
    });
  });
});

describe("useMonacoViewerReport enabled gate", () => {
  it("neither reports nor registers a controller while disabled", () => {
    const view = renderMonaco("notes.txt", false);
    mount(view);
    act(() => view.result.current.onSelectionChange({ startLine: 1, endLine: 1 }));
    act(() => view.result.current.onViewportChange({ start: 1, end: 1, text: "alpha" }));
    expect(readViewerState("s1")?.location.lines).toBeUndefined();
    expect(readViewerState("s1")?.location.visibleLines).toBeUndefined();
    expect(readViewerController("s1")).toBeNull();
  });

  it("registers its controller when enabled flips on, and drops it when off", () => {
    const view = renderMonaco("notes.txt", false);
    mount(view);
    expect(readViewerController("s1")).toBeNull();

    view.rerender({ enabled: true });
    expect(readViewerController("s1")).not.toBeNull();

    view.rerender({ enabled: false });
    expect(readViewerController("s1")).toBeNull();
  });

  it("drops its controller on unmount", () => {
    const view = renderMonaco("notes.txt");
    mount(view);
    expect(readViewerController("s1")).not.toBeNull();
    view.unmount();
    expect(readViewerController("s1")).toBeNull();
  });
});
