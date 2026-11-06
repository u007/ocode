import { renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  describeViewerLocation,
  formatViewerContext,
  patchViewerState,
  readViewerController,
  readViewerState,
  registerViewerController,
  rekeyViewerState,
  resetViewerStates,
  setViewerDoc,
  subscribeViewerState,
  tidyViewerText,
  useViewerController,
  ViewerReportContext,
  VIEWER_SELECTION_MAX,
  VIEWER_VISIBLE_MAX,
  type ViewerController,
} from "./viewerContext";

describe("viewerContext store", () => {
  beforeEach(() => resetViewerStates());

  it("setViewerDoc seeds an empty view and a new path resets it", () => {
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "a.md", selection: "hello", location: { heading: "Intro" } });
    expect(readViewerState("s1")).toMatchObject({ path: "a.md", selection: "hello", location: { heading: "Intro" } });

    // Same doc: keeps the view.
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    expect(readViewerState("s1")?.selection).toBe("hello");

    // New doc: resets the view.
    setViewerDoc("s1", { path: "b.pdf", kind: "pdf" });
    expect(readViewerState("s1")).toMatchObject({ path: "b.pdf", selection: null, visibleText: null, location: {} });

    setViewerDoc("s1", null);
    expect(readViewerState("s1")).toBeNull();
  });

  it("drops a patch for another path or an unknown session", () => {
    patchViewerState("nope", { path: "a.md", selection: "x" });
    expect(readViewerState("nope")).toBeNull();

    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "stale.md", selection: "x" });
    expect(readViewerState("s1")?.selection).toBeNull();
  });

  it("merges location, keeps selection sticky and clears it only on an explicit null", () => {
    setViewerDoc("s1", { path: "r.pdf", kind: "pdf" });
    patchViewerState("s1", { path: "r.pdf", location: { page: 2, pageCount: 9 } });
    patchViewerState("s1", { path: "r.pdf", selection: "picked" });
    patchViewerState("s1", { path: "r.pdf", location: { page: 3 }, visibleText: "body" });
    expect(readViewerState("s1")).toMatchObject({ location: { page: 3, pageCount: 9 }, selection: "picked", visibleText: "body" });

    patchViewerState("s1", { path: "r.pdf", selection: null });
    expect(readViewerState("s1")?.selection).toBeNull();
    // Location merge can also blank a key explicitly.
    patchViewerState("s1", { path: "r.pdf", location: { page: undefined } });
    expect(readViewerState("s1")?.location).toEqual({ page: undefined, pageCount: 9 });
  });

  it("caps and tidies selection and visible text", () => {
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", {
      path: "a.md",
      selection: `  a   b \n\n\n c   d  \n${"x".repeat(VIEWER_SELECTION_MAX + 50)}`,
      visibleText: "y".repeat(VIEWER_VISIBLE_MAX + 1),
    });
    const s = readViewerState("s1")!;
    expect(s.selection!.startsWith("a b\nc d\n")).toBe(true);
    expect(s.selection!.length).toBe(VIEWER_SELECTION_MAX + 1); // + ellipsis
    expect(s.selection!.endsWith("…")).toBe(true);
    expect(s.visibleText!.length).toBe(VIEWER_VISIBLE_MAX + 1);
    // An all-whitespace selection becomes null, not "".
    patchViewerState("s1", { path: "a.md", selection: "   \n  " });
    expect(readViewerState("s1")?.selection).toBeNull();
  });

  it("notifies subscribers per session only", () => {
    let hits = 0;
    const off = subscribeViewerState("s1", () => hits++);
    setViewerDoc("s2", { path: "z.md", kind: "markdown" });
    expect(hits).toBe(0);
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "a.md", selection: "q" });
    expect(hits).toBe(2);
    off();
    setViewerDoc("s1", null);
    expect(hits).toBe(2);
  });

  it("carries find and control state, and a new doc clears both", () => {
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    patchViewerState("s1", { path: "a.md", find: { query: "install", count: 3, current: 2 } });
    patchViewerState("s1", { path: "a.md", control: { id: "vc_1", status: "failed", error: "no match" } });
    expect(readViewerState("s1")).toMatchObject({
      find: { query: "install", count: 3, current: 2 },
      control: { id: "vc_1", status: "failed", error: "no match" },
    });

    // A patch without the fields leaves them alone; an explicit null clears them.
    patchViewerState("s1", { path: "a.md", selection: "x" });
    expect(readViewerState("s1")?.find).toEqual({ query: "install", count: 3, current: 2 });
    patchViewerState("s1", { path: "a.md", find: null });
    expect(readViewerState("s1")?.find).toBeNull();

    setViewerDoc("s1", { path: "b.md", kind: "markdown" });
    expect(readViewerState("s1")).toMatchObject({ find: null, control: null });
  });

  it("rekeys the record and its listeners, notifying both keys", () => {
    setViewerDoc("tmp", { path: "a.md", kind: "markdown" });
    let hits = 0;
    const off = subscribeViewerState("tmp", () => hits++);
    rekeyViewerState("tmp", "real");
    expect(readViewerState("tmp")).toBeNull();
    expect(readViewerState("real")).toMatchObject({ path: "a.md" });
    expect(hits).toBe(1);

    // The moved listener still hears changes under the new key, and unsubscribes cleanly.
    patchViewerState("real", { path: "a.md", selection: "q" });
    expect(hits).toBe(2);
    off();
    patchViewerState("real", { path: "a.md", selection: "r" });
    expect(hits).toBe(2);
  });

  it("rekey is a no-op for a missing old key or equal keys", () => {
    rekeyViewerState("ghost", "real");
    expect(readViewerState("real")).toBeNull();
    setViewerDoc("s1", { path: "a.md", kind: "markdown" });
    rekeyViewerState("s1", "s1");
    expect(readViewerState("s1")).toMatchObject({ path: "a.md" });
  });
});

describe("viewer controller registry", () => {
  beforeEach(() => resetViewerStates());

  it("last registration wins and unregister removes only its own controller", () => {
    const a: ViewerController = { scroll: () => true };
    const b: ViewerController = { find: () => true };
    const unA = registerViewerController("s1", a);
    const unB = registerViewerController("s1", b);
    expect(readViewerController("s1")).toBe(b);

    unA();
    expect(readViewerController("s1")).toBe(b);
    unB();
    expect(readViewerController("s1")).toBeNull();
  });

  it("useViewerController registers under the enclosing report key and unregisters on unmount", () => {
    const wrapper = ({ children }: { children: ReactNode }) => createElement(ViewerReportContext.Provider, { value: "s1" }, children);
    const scroll = vi.fn(() => true);
    const { rerender, unmount } = renderHook(({ c }: { c: ViewerController }) => useViewerController("a.md", c), {
      wrapper,
      initialProps: { c: { scroll } },
    });
    // The registered proxy calls whichever controller the latest render supplied.
    const next = vi.fn(() => true);
    rerender({ c: { scroll: next } });
    readViewerController("s1")?.scroll?.({ line: 3 });
    expect(scroll).not.toHaveBeenCalled();
    expect(next).toHaveBeenCalledWith({ line: 3 });

    unmount();
    expect(readViewerController("s1")).toBeNull();
  });

  it("useViewerController is a no-op outside a provider", () => {
    renderHook(() => useViewerController("a.md", { scroll: () => true }));
    expect(readViewerController("s1")).toBeNull();
  });
});

describe("describeViewerLocation / formatViewerContext", () => {
  it("renders each location kind", () => {
    expect(describeViewerLocation({})).toBe("");
    expect(describeViewerLocation({ page: 3, pageCount: 12 })).toBe("p.3/12");
    expect(describeViewerLocation({ page: 3 })).toBe("p.3");
    expect(describeViewerLocation({ slide: 2, slideCount: 8 })).toBe("slide 2/8");
    expect(describeViewerLocation({ sheet: "Sheet1", range: "B2:D9" })).toBe("Sheet1!B2:D9");
    expect(describeViewerLocation({ sheet: "Sheet1" })).toBe("Sheet1");
    expect(describeViewerLocation({ heading: "Install" })).toBe("§ Install");
    expect(describeViewerLocation({ lines: { start: 10, end: 24 } })).toBe("L10-L24");
    expect(describeViewerLocation({ lines: { start: 7, end: 7 } })).toBe("L7");
    expect(describeViewerLocation({ page: 1, heading: "A" })).toBe("p.1, § A");
  });

  it("describes the editor viewport when no selection range is set", () => {
    expect(describeViewerLocation({ visibleLines: { start: 1, end: 40 } })).toBe("view L1-L40");
    expect(describeViewerLocation({ lines: { start: 10, end: 24 }, visibleLines: { start: 1, end: 40 } })).toBe("L10-L24");
    expect(describeViewerLocation({ visibleLines: { start: 5, end: 5 } })).toBe("view L5");
  });

  it("prefers selection, then the Ask-LLM fallback, then visible text, then the bare head", () => {
    const base = { path: "docs/g.md", kind: "markdown" as const, location: { heading: "Install" }, selection: null, visibleText: null, find: null, control: null, updatedAt: 0 };
    expect(formatViewerContext(base)).toBe("@docs/g.md (markdown, § Install)");
    expect(formatViewerContext({ ...base, visibleText: "body text" })).toBe('@docs/g.md (markdown, § Install) visible: "body text"');
    expect(formatViewerContext({ ...base, visibleText: "body", selection: "picked" })).toBe('@docs/g.md (markdown, § Install) selected: "picked"');
    expect(formatViewerContext({ ...base, visibleText: "body" }, "  from   chip ")).toBe('@docs/g.md (markdown, § Install) selected: "from chip"');
    expect(formatViewerContext({ ...base, location: {} })).toBe("@docs/g.md (markdown)");
  });

  it("tidyViewerText keeps single newlines and tabs, drops blank lines", () => {
    expect(tidyViewerText("a  b\n\n\n  c   d\n", 100)).toBe("a b\nc d");
    expect(tidyViewerText("abcdef", 3)).toBe("abc…");
    // TSV from a sheet selection: tabs are column boundaries, a leading empty cell survives.
    expect(tidyViewerText("name\tqty\n\tapples \t 3\n\t\t\n", 100)).toBe("name\tqty\n\tapples \t 3");
  });
});
