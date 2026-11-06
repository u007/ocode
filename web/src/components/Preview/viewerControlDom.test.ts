import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { domViewerController, findHeading, findStateFor, firstMatchRange, normalizeMatchText, runFindCommand, selectRange, type FindEngine } from "./viewerControlDom";

function root(markup: string): HTMLDivElement {
  const el = document.createElement("div");
  el.innerHTML = markup;
  document.body.appendChild(el);
  return el;
}

function engine(over: Partial<FindEngine> = {}): FindEngine {
  return { active: true, count: 2, show: vi.fn(), hide: vi.fn(), setQuery: vi.fn(), go: vi.fn(), ...over };
}

beforeEach(() => {
  document.body.innerHTML = "";
  window.getSelection()?.removeAllRanges();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("normalizeMatchText", () => {
  it("collapses whitespace and lowercases", () => {
    expect(normalizeMatchText("  Install \n  Guide ")).toBe("install guide");
  });
});

describe("findHeading", () => {
  it("returns the first heading whose text contains the needle, ignoring case and wrapping", () => {
    const r = root("<h1>Title</h1><p>x</p><h2>Install\n   Guide</h2><h3>Install again</h3>");
    expect(findHeading(r, "install GUIDE")?.tagName).toBe("H2");
  });

  it("returns null when no heading matches and ignores non-heading text", () => {
    const r = root("<p>Install guide</p><h2>Usage</h2>");
    expect(findHeading(r, "install")).toBeNull();
  });

  it("throws on an empty heading instead of matching the first one", () => {
    expect(() => findHeading(root("<h1>Title</h1>"), "   ")).toThrow("heading must not be empty");
  });
});

describe("firstMatchRange", () => {
  it("returns a range over the first case-insensitive match, across inline elements", () => {
    const r = root("<p>Alpha <em>Beta</em> gamma</p><p>beta again</p>");
    expect(firstMatchRange(r, "alpha beta")?.toString()).toBe("Alpha Beta");
    expect(firstMatchRange(r, "BETA")?.toString()).toBe("Beta");
  });

  it("returns null when the text is absent or only spans a block boundary", () => {
    const r = root("<p>end</p><p>start</p>");
    expect(firstMatchRange(r, "missing")).toBeNull();
    expect(firstMatchRange(r, "end start")).toBeNull();
  });

  it("throws on an empty query", () => {
    expect(() => firstMatchRange(root("<p>x</p>"), "")).toThrow("text must not be empty");
  });
});

describe("selectRange", () => {
  it("sets the browser selection to the range and scrolls its element into view", () => {
    const original = HTMLElement.prototype.scrollIntoView;
    const scrollIntoView = vi.fn();
    HTMLElement.prototype.scrollIntoView = scrollIntoView;
    try {
      const range = firstMatchRange(root("<p>Alpha beta</p>"), "beta");
      selectRange(range as Range);
      expect(window.getSelection()?.toString()).toBe("beta");
      expect(scrollIntoView).toHaveBeenCalledWith({ block: "center" });
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });
});

describe("runFindCommand", () => {
  it("open shows the bar and sets the query", () => {
    const e = engine({ active: false });
    expect(runFindCommand(e, { op: "open", query: "beta" })).toBe(true);
    expect(e.show).toHaveBeenCalled();
    expect(e.setQuery).toHaveBeenCalledWith("beta");
  });

  it("open with an empty query throws", () => {
    expect(() => runFindCommand(engine(), { op: "open", query: "" })).toThrow("find query must not be empty");
  });

  it("next and prev step the engine in the right direction", () => {
    const e = engine();
    runFindCommand(e, { op: "next" });
    runFindCommand(e, { op: "prev" });
    expect(e.go).toHaveBeenNthCalledWith(1, 1);
    expect(e.go).toHaveBeenNthCalledWith(2, -1);
  });

  it("next fails loudly when the bar is closed or has no matches", () => {
    expect(() => runFindCommand(engine({ active: false }), { op: "next" })).toThrow("find is not open");
    expect(() => runFindCommand(engine({ count: 0 }), { op: "prev" })).toThrow("no matches to step through");
  });

  it("close hides the engine", () => {
    const e = engine();
    expect(runFindCommand(e, { op: "close" })).toBe(true);
    expect(e.hide).toHaveBeenCalled();
  });
});

describe("findStateFor", () => {
  it("is null with the bar closed and 1-based current with matches", () => {
    expect(findStateFor({ active: false, query: "x", count: 3, current: 1 })).toBeNull();
    expect(findStateFor({ active: true, query: "x", count: 3, current: 1 })).toEqual({ query: "x", count: 3, current: 2 });
    expect(findStateFor({ active: true, query: "x", count: 0, current: 0 })).toEqual({ query: "x", count: 0, current: 0 });
  });
});

describe("domViewerController", () => {
  function setup(markup: string) {
    const scroller = root(`<div>${markup}</div>`);
    const scrollerEl = scroller as HTMLElement;
    scrollerEl.scrollBy = vi.fn();
    Object.defineProperty(scrollerEl, "clientHeight", { value: 500, configurable: true });
    const report = vi.fn();
    const e = engine();
    const controller = domViewerController({
      scroller: { current: scrollerEl },
      textRoot: { current: scrollerEl },
      find: e,
      report,
    });
    return { scrollerEl, report, e, controller };
  }

  it("scrolls one page step by direction and leaves page/slide/line unsupported", () => {
    const { scrollerEl, controller } = setup("<p>x</p>");
    expect(controller.scroll?.({ direction: "down" })).toBe(true);
    expect(scrollerEl.scrollBy).toHaveBeenCalledWith({ top: 400 });
    expect(controller.scroll?.({ direction: "up" })).toBe(true);
    expect(scrollerEl.scrollBy).toHaveBeenLastCalledWith({ top: -400 });
    expect(controller.scroll?.({ page: 2 })).toBe(false);
  });

  it("scrolls to a heading and throws when it is absent", () => {
    const original = HTMLElement.prototype.scrollIntoView;
    const scrollIntoView = vi.fn();
    HTMLElement.prototype.scrollIntoView = scrollIntoView;
    try {
      const { controller } = setup("<h2>Install</h2>");
      expect(controller.scroll?.({ heading: "install" })).toBe(true);
      expect(scrollIntoView).toHaveBeenCalledWith({ block: "start" });
      expect(() => controller.scroll?.({ heading: "usage" })).toThrow("heading not found: usage");
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });

  it("selects the first text match and reports it, throwing when absent", () => {
    const original = HTMLElement.prototype.scrollIntoView;
    HTMLElement.prototype.scrollIntoView = vi.fn();
    try {
      const { controller, report } = setup("<p>Alpha beta</p>");
      expect(controller.select?.({ text: "beta" })).toBe(true);
      expect(window.getSelection()?.toString()).toBe("beta");
      expect(report).toHaveBeenCalledWith({ selection: "beta" });
      expect(() => controller.select?.({ text: "zeta" })).toThrow("text not found");
      expect(controller.select?.({ lines: { start: 1, end: 2 } })).toBe(false);
    } finally {
      HTMLElement.prototype.scrollIntoView = original;
    }
  });

  it("routes find commands to the engine", () => {
    const { controller, e } = setup("<p>x</p>");
    expect(controller.find?.({ op: "close" })).toBe(true);
    expect(e.hide).toHaveBeenCalled();
  });
});
