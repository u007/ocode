import { describe, it, expect, vi, beforeEach } from "vitest";
import { registerFileLinkProvider, registerUrlLinkProvider } from "./terminalLinkProvider";
import { OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";
import type { ILink, ILinkProvider, Terminal } from "ghostty-web";

interface FakeLine {
  translateToString: (trim: boolean) => string;
  isWrapped: boolean;
  length: number;
  getCell: (col: number) => { getChars: () => string; getWidth: () => number } | undefined;
}

interface MockTerminal {
  term: Terminal;
  lines: FakeLine[];
  provider: () => ILinkProvider;
}

function makeMockTerminal(lines: string[], wrapped: boolean[] = [], widthMap?: Map<string, number>): MockTerminal {
  const bufferLines: FakeLine[] = lines.map((content, idx) => {
    const line: FakeLine = {
      translateToString: (trim: boolean) => (trim ? content.trimEnd() : content),
      isWrapped: wrapped[idx] ?? false,
      length: content.length,
      getCell: (col: number) => {
        if (col >= line.length) return undefined;
        const ch = content[col] ?? "";
        const w = widthMap?.get(`${idx}:${col}`) ?? (ch ? 1 : 0);
        return { getChars: () => ch, getWidth: () => w };
      },
    };
    return line;
  });

  let registered: ILinkProvider | undefined;
  const term = {
    buffer: {
      active: {
        getLine: (idx: number) => (idx >= 0 && idx < bufferLines.length ? bufferLines[idx] : undefined),
      },
    },
    registerLinkProvider: vi.fn((p: ILinkProvider) => {
      registered = p;
    }),
  } as unknown as Terminal;

  return {
    term,
    lines: bufferLines,
    provider: () => {
      if (!registered) throw new Error("no provider registered");
      return registered;
    },
  };
}

function linksAt(m: MockTerminal, row: number): ILink[] | undefined {
  let out: ILink[] | undefined = [] as ILink[];
  m.provider().provideLinks(row, (l) => (out = l));
  return out;
}

function captureOpenFile(run: () => void): CustomEvent<OpenFileDetail>[] {
  const events: CustomEvent<OpenFileDetail>[] = [];
  const handler = (e: Event) => events.push(e as CustomEvent<OpenFileDetail>);
  window.addEventListener(OPEN_FILE_EVENT, handler);
  try {
    run();
  } finally {
    window.removeEventListener(OPEN_FILE_EVENT, handler);
  }
  return events;
}

describe("registerFileLinkProvider", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("detects ordinary file path with line number", () => {
    const m = makeMockTerminal(["see src/foo/bar.ts:12 for details"]);
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 0);
    expect(links!.length).toBe(1);
    expect(links![0].text).toBe("src/foo/bar.ts:12");
  });

  it("dispatches OPEN_FILE_EVENT with projectRoot on ctrl click", () => {
    const m = makeMockTerminal(["open file web/src/App.tsx:42"]);
    registerFileLinkProvider(m.term, "/my/proj");
    const links = linksAt(m, 0)!;
    expect(links.length).toBe(1);
    const events = captureOpenFile(() => links[0].activate(new MouseEvent("click", { ctrlKey: true })));
    expect(events).toHaveLength(1);
    expect(events[0].detail.path).toBe("web/src/App.tsx");
    expect(events[0].detail.line).toBe(42);
    expect(events[0].detail.projectRoot).toBe("/my/proj");
  });

  it("dispatches OPEN_FILE_EVENT with metaKey", () => {
    const m = makeMockTerminal(["open file web/src/App.tsx:42"]);
    registerFileLinkProvider(m.term, "/my/proj");
    const links = linksAt(m, 0)!;
    const events = captureOpenFile(() => links[0].activate(new MouseEvent("click", { metaKey: true })));
    expect(events).toHaveLength(1);
  });

  it("does not dispatch OPEN_FILE_EVENT on ordinary click", () => {
    const m = makeMockTerminal(["open file web/src/App.tsx:42"]);
    registerFileLinkProvider(m.term, "/my/proj");
    const links = linksAt(m, 0)!;
    const events = captureOpenFile(() => links[0].activate(new MouseEvent("click")));
    expect(events).toHaveLength(0);
  });

  it("does not dispatch when the guard reports a drag", () => {
    const m = makeMockTerminal(["open file web/src/App.tsx:42"]);
    registerFileLinkProvider(m.term, "/my/proj", () => true);
    const links = linksAt(m, 0)!;
    const events = captureOpenFile(() => links[0].activate(new MouseEvent("click", { ctrlKey: true })));
    expect(events).toHaveLength(0);
  });

  it("omits projectRoot when no project path is given", () => {
    const m = makeMockTerminal(["open file a/b.ts:1"]);
    registerFileLinkProvider(m.term, undefined);
    const links = linksAt(m, 0)!;
    const events = captureOpenFile(() => links[0].activate(new MouseEvent("click", { ctrlKey: true })));
    expect(events[0].detail.projectRoot).toBeUndefined();
  });

  it("matches .ocode/uploads path with spaces and U+202F", () => {
    const upload = `.ocode/uploads/Screenshot 2026-09-03 at 10.20.33 AM.png`;
    const m = makeMockTerminal([`check ${upload} now`]);
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 0)!;
    expect(links.length).toBe(1);
    expect(links[0].text).toBe(upload);
  });

  it("does not match a bare http url (handled by the URL provider)", () => {
    const m = makeMockTerminal(["visit https://localhost:3510/ for app"]);
    registerFileLinkProvider(m.term, "/proj");
    expect(linksAt(m, 0)).toBeUndefined();
  });

  it("handles wrapped lines as single link", () => {
    // Row 1 is a soft-wrapped continuation of row 0, so the path spans both rows.
    const m = makeMockTerminal(["prefix src/very/lo", "ng/path/to/file.ts:123 suffix"], [false, true]);
    registerFileLinkProvider(m.term, "/proj");
    // Row 0 expands downward into its wrapped successor.
    const links = linksAt(m, 0)!;
    expect(links[0].text).toBe("src/very/long/path/to/file.ts:123");
    expect(links[0].range.start).toEqual({ x: 7, y: 0 });
    expect(links[0].range.end).toEqual({ x: 21, y: 1 });
  });

  it("returns undefined when no file link present", () => {
    const m = makeMockTerminal(["no links here just text"]);
    registerFileLinkProvider(m.term, "/proj");
    expect(linksAt(m, 0)).toBeUndefined();
  });

  it("returns a no-op disposable (ghostty-web unregisters providers on term.dispose())", () => {
    const m = makeMockTerminal(["x"]);
    const disp = registerFileLinkProvider(m.term, "/proj");
    expect(() => disp.dispose()).not.toThrow();
  });

  it("computes 0-based inclusive range for a link ending exactly at the row boundary", () => {
    const line = "src/file.ts:1";
    const m = makeMockTerminal([line]);
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 0)!;
    expect(links[0].text).toBe(line);
    // mapStrIdx reports the exclusive end of a boundary-ending link as column 0 of the next
    // row (like xterm's LinkComputer); the range must instead end on the last cell of its row.
    expect(links[0].range.start).toEqual({ x: 0, y: 0 });
    expect(links[0].range.end).toEqual({ x: line.length - 1, y: 0 });
  });

  it("computes range for link starting on wrapped continuation row", () => {
    // "$" cannot merge into the path token, so the link starts cleanly at col 0 of the wrapped
    // row. The prompt has NO trailing space on purpose: translateToString trims trailing
    // whitespace but raw cells keep it, which would misalign the mapped range by one cell.
    const m = makeMockTerminal(["$", "src/wrapped.ts:99 done"], [false, true]);
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 1)!;
    expect(links[0].text).toBe("src/wrapped.ts:99");
    expect(links[0].range.start).toEqual({ x: 0, y: 1 });
    expect(links[0].range.end).toEqual({ x: 16, y: 1 }); // 17 chars, inclusive end
  });

  it("handles a double-width character before the link", () => {
    const line = "ab你 src/file.ts:10";
    const m = makeMockTerminal([line], [false], new Map([["0:2", 2]]));
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 0)!;
    expect(links.length).toBe(1);
    expect(links[0].text).toBe("src/file.ts:10");
    expect(links[0].range.start.x).toBeGreaterThan(0);
    expect(links[0].range.start.y).toBe(0);
  });

  it("walks the empty-placeholder correction without crashing", () => {
    // Row 0 "ab你" occupies 4 cells: col 3 is the empty second half of the wide char (chars "",
    // width 1). Row 1 is a soft wrap starting with another wide char (width 2 at col 0). Mapping
    // the link start walks through the placeholder cell, entering the `chars === ""` correction
    // branch (which peeks at the wrapped successor's first cell), and still finds the link.
    const m = makeMockTerminal(
      ["ab你", "你 src/file.ts:5 ok"],
      [false, true],
      new Map([
        ["0:3", 1],
        ["1:0", 2],
      ]),
    );
    m.lines[0].length = 4;
    registerFileLinkProvider(m.term, "/proj");
    const links = linksAt(m, 0)!;
    expect(links[0].text).toBe("src/file.ts:5");
  });

  it("stops mapping at a missing cell instead of throwing", () => {
    const m = makeMockTerminal(["see src/a.ts:1 here"]);
    m.lines[0].getCell = () => undefined;
    registerFileLinkProvider(m.term, "/proj");
    expect(() => linksAt(m, 0)).not.toThrow();
  });
});

describe("registerUrlLinkProvider", () => {
  it("detects an https url and reports a 0-based inclusive range", () => {
    const m = makeMockTerminal(["go to https://example.com/a?b=1 now"]);
    registerUrlLinkProvider(m.term, vi.fn());
    const links = linksAt(m, 0)!;
    expect(links).toHaveLength(1);
    expect(links[0].text).toBe("https://example.com/a?b=1");
    expect(links[0].range.start).toEqual({ x: 6, y: 0 });
    expect(links[0].range.end).toEqual({ x: 6 + "https://example.com/a?b=1".length - 1, y: 0 });
  });

  it("opens on a plain left click", () => {
    const m = makeMockTerminal(["see http://localhost:3510/x ok"]);
    const open = vi.fn();
    registerUrlLinkProvider(m.term, open);
    linksAt(m, 0)![0].activate(new MouseEvent("click", { button: 0 }));
    expect(open).toHaveBeenCalledWith("http://localhost:3510/x");
  });

  it("opens on ctrl/cmd left click too", () => {
    const m = makeMockTerminal(["see http://localhost:3510/x ok"]);
    const open = vi.fn();
    registerUrlLinkProvider(m.term, open);
    const link = linksAt(m, 0)![0];
    link.activate(new MouseEvent("click", { ctrlKey: true }));
    link.activate(new MouseEvent("click", { metaKey: true }));
    expect(open).toHaveBeenCalledTimes(2);
  });

  it("does not open when the guard reports a drag", () => {
    const m = makeMockTerminal(["see http://localhost:3510/x ok"]);
    const open = vi.fn();
    registerUrlLinkProvider(m.term, open, () => true);
    linksAt(m, 0)![0].activate(new MouseEvent("click", { button: 0 }));
    expect(open).not.toHaveBeenCalled();
  });

  it("does not open on a non-left button", () => {
    const m = makeMockTerminal(["see http://localhost:3510/x ok"]);
    const open = vi.fn();
    registerUrlLinkProvider(m.term, open);
    linksAt(m, 0)![0].activate(new MouseEvent("click", { button: 2 }));
    expect(open).not.toHaveBeenCalled();
  });

  it("strips trailing punctuation and shrinks the range", () => {
    const line = "(see https://example.com/path).";
    const m = makeMockTerminal([line]);
    const open = vi.fn();
    registerUrlLinkProvider(m.term, open);
    const link = linksAt(m, 0)![0];
    expect(link.text).toBe("https://example.com/path");
    expect(link.range.start.x).toBe(5);
    expect(link.range.end.x).toBe(5 + "https://example.com/path".length - 1);
    link.activate(new MouseEvent("click", { button: 0 }));
    expect(open).toHaveBeenCalledWith("https://example.com/path");
  });

  it("strips a run of mixed trailing punctuation", () => {
    const m = makeMockTerminal(["a https://x.io/y?!.,"]);
    registerUrlLinkProvider(m.term, vi.fn());
    expect(linksAt(m, 0)![0].text).toBe("https://x.io/y");
  });

  it("joins a url wrapped across rows into one link", () => {
    const m = makeMockTerminal(["see https://exa", "mple.com/long done"], [false, true]);
    registerUrlLinkProvider(m.term, vi.fn());
    const link = linksAt(m, 0)![0];
    expect(link.text).toBe("https://example.com/long");
    expect(link.range.start).toEqual({ x: 4, y: 0 });
    expect(link.range.end).toEqual({ x: 12, y: 1 });
  });

  it("finds several urls on one row and none when absent", () => {
    const m = makeMockTerminal(["http://a.io and https://b.io"]);
    registerUrlLinkProvider(m.term, vi.fn());
    expect(linksAt(m, 0)!.map((l) => l.text)).toEqual(["http://a.io", "https://b.io"]);
    const none = makeMockTerminal(["nothing here"]);
    registerUrlLinkProvider(none.term, vi.fn());
    expect(linksAt(none, 0)).toBeUndefined();
  });
});
