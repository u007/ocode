import { describe, it, expect, vi } from "vitest";
import type { Terminal } from "ghostty-web";
import { createTerminalSearch, type TerminalSearchResult } from "./terminalSearch";

function last<T>(a: T[]): T {
  return a[a.length - 1];
}

type SearchTerm = Parameters<typeof createTerminalSearch>[0];

function makeTerm(rows: string[], screenRows: number, viewportY = 0) {
  const term = {
    rows: screenRows,
    buffer: {
      active: {
        length: rows.length,
        getLine: (y: number) => (y >= 0 && y < rows.length ? { translateToString: () => rows[y] } : undefined),
      },
    },
    getViewportY: vi.fn(() => viewportY),
    scrollToLine: vi.fn(),
    scrollToBottom: vi.fn(),
    clearSelection: vi.fn(),
  };
  return term as unknown as SearchTerm & typeof term & Pick<Terminal, "rows">;
}

function setup(rows: string[], screenRows = 3, viewportY = 0) {
  const term = makeTerm(rows, screenRows, viewportY);
  const highlight = vi.fn();
  const search = createTerminalSearch(term, highlight);
  const results: TerminalSearchResult[] = [];
  search.onDidChangeResults((r) => results.push(r));
  return { term, highlight, search, results };
}

describe("terminalSearch", () => {
  const rows = ["alpha Foo", "beta", "foo foo", "gamma", "delta", "FOO"];

  it("finds case-insensitively and counts every match", () => {
    const s = setup(rows, 3, 0); // screen = rows 3..5 -> starts at first match at/below row 3
    expect(s.search.findNext("foo")).toBe(true);
    expect(last(s.results)).toEqual({ resultCount: 4, resultIndex: 3 });
    expect(s.highlight).toHaveBeenCalledWith(5, 0, 2);
  });

  it("starts at the top visible row, accounting for viewport scrollback", () => {
    const s = setup(rows, 3, 3); // scrolled back 3 -> top row 0
    s.search.findNext("foo");
    expect(last(s.results)).toEqual({ resultCount: 4, resultIndex: 0 });
    expect(s.highlight).toHaveBeenCalledWith(0, 6, 8);
  });

  it("wraps to the first match when none is at or below the top row", () => {
    const s = setup(["foo", "x", "x", "x", "x", "x"], 3, 0); // top row 3, only match at row 0
    s.search.findNext("foo");
    expect(last(s.results)).toEqual({ resultCount: 1, resultIndex: 0 });
  });

  it("advances with wrap-around on the same query", () => {
    const s = setup(rows, 3, 3);
    s.search.findNext("foo");
    s.search.findNext("foo");
    s.search.findNext("foo");
    s.search.findNext("foo");
    expect(s.results.map((r) => r.resultIndex)).toEqual([0, 1, 2, 3]);
    s.search.findNext("foo");
    expect(last(s.results)!.resultIndex).toBe(0);
  });

  it("retreats with wrap-around", () => {
    const s = setup(rows, 3, 3);
    s.search.findNext("foo");
    s.search.findPrevious("foo");
    expect(last(s.results)!.resultIndex).toBe(3);
    s.search.findPrevious("foo");
    expect(last(s.results)!.resultIndex).toBe(2);
  });

  it("scrolls to the row for scrollback matches and to the bottom for screen matches", () => {
    const s = setup(rows, 3, 3);
    s.search.findNext("foo"); // row 0, scrollback: 6 rows - 3 screen rows - 0 = 3 lines above bottom
    expect(s.term.scrollToLine).toHaveBeenCalledWith(3);
    expect(s.term.scrollToBottom).not.toHaveBeenCalled();
    s.search.findPrevious("foo"); // row 5, screen
    expect(s.term.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it("scrolls so an absolute-row match becomes the top visible row", () => {
    const long = Array.from({ length: 100 }, (_, y) => (y === 30 ? "needle" : "x"));
    const s = setup(long, 10, 0); // screen = rows 90..99, match at row 30 is scrollback
    s.search.findNext("needle");
    expect(s.term.scrollToLine).toHaveBeenCalledWith(60); // 100 - 10 - 30: row 30 is the top row
    expect(s.term.scrollToBottom).not.toHaveBeenCalled();
  });

  it("reads each buffer row once across searches until invalidate()", () => {
    const term = makeTerm(rows, 3, 3);
    const getLine = vi.spyOn(term.buffer.active, "getLine");
    const search = createTerminalSearch(term, vi.fn());
    search.findNext("foo");
    search.findNext("beta");
    expect(getLine).toHaveBeenCalledTimes(rows.length);
    search.invalidate();
    search.findNext("beta");
    expect(getLine).toHaveBeenCalledTimes(rows.length * 2);
  });

  it("clears and reports none for empty, whitespace and unmatched queries", () => {
    const s = setup(rows);
    expect(s.search.findNext("")).toBe(false);
    expect(s.search.findNext("   ")).toBe(false);
    expect(s.search.findNext("zzz")).toBe(false);
    expect(s.results).toEqual([
      { resultCount: 0, resultIndex: -1 },
      { resultCount: 0, resultIndex: -1 },
      { resultCount: 0, resultIndex: -1 },
    ]);
    expect(s.term.clearSelection).toHaveBeenCalledTimes(3);
    expect(s.highlight).not.toHaveBeenCalled();
  });

  it("treats a changed query as new and restarts from the top visible row", () => {
    const s = setup(rows, 3, 3);
    s.search.findNext("foo");
    s.search.findNext("foo");
    s.search.findNext("beta");
    expect(last(s.results)).toEqual({ resultCount: 1, resultIndex: 0 });
  });

  it("clear() clears the selection and reports none; a following find starts fresh", () => {
    const s = setup(rows, 3, 3);
    s.search.findNext("foo");
    s.search.findNext("foo");
    s.search.clear();
    expect(last(s.results)).toEqual({ resultCount: 0, resultIndex: -1 });
    expect(s.term.clearSelection).toHaveBeenCalled();
    s.search.findNext("foo");
    expect(last(s.results)!.resultIndex).toBe(0);
  });

  it("stops notifying after the listener or search is disposed", () => {
    const term = makeTerm(rows, 3, 3);
    const search = createTerminalSearch(term, vi.fn());
    const cb = vi.fn();
    const d = search.onDidChangeResults(cb);
    d.dispose();
    search.findNext("foo");
    expect(cb).not.toHaveBeenCalled();
    const cb2 = vi.fn();
    search.onDidChangeResults(cb2);
    search.dispose();
    search.findNext("foo");
    expect(cb2).not.toHaveBeenCalled();
  });
});
