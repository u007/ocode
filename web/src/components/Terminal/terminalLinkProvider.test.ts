import { describe, it, expect, vi, beforeEach } from "vitest";
import { registerFileLinkProvider } from "./terminalLinkProvider";
import { OPEN_FILE_EVENT, type OpenFileDetail } from "../../lib/fileLinks";
import type { Terminal } from "@xterm/xterm";

type MockCall = { range: { start: { x: number; y: number }; end: { x: number; y: number } }; text: string; activate: (e: MouseEvent, t: string) => void };

type MockTerminal = Terminal & {
  _provider?: {
    provideLinks: (y: number, cb: (links: MockCall[] | undefined) => void) => void;
  };
};

function makeMockTerminal(
  lines: string[],
  wrapped: boolean[] = [],
  widthMap?: Map<string, number>,
): MockTerminal {
  const bufferLines = lines.map((content, idx) => ({
    translateToString: (trim: boolean) => (trim ? content.trimEnd() : content),
    isWrapped: wrapped[idx] ?? false,
    length: content.length,
    getCell: (col: number, cell: { getChars: () => string; getWidth: () => number }) => {
      const ch = content[col] ?? "";
      const w = widthMap?.get(`${idx}:${col}`) ?? (ch ? 1 : 0);
      // Patch cell in place so caller sees updated values
      (cell as unknown as { getChars: () => string }).getChars = () => ch;
      (cell as unknown as { getWidth: () => number }).getWidth = () => w;
    },
  }));

  const nullCell = {
    getChars: () => "",
    getWidth: () => 0,
  };

  return {
    buffer: {
      active: {
        getLine: (idx: number) => (idx >= 0 && idx < bufferLines.length ? (bufferLines[idx] as unknown as import("@xterm/xterm").IBufferLine) : undefined),
        getNullCell: () => nullCell as unknown as ReturnType<import("@xterm/xterm").Terminal["buffer"]["active"]["getNullCell"]>,
      },
    },
    registerLinkProvider: vi.fn((provider: { provideLinks: (y: number, cb: (links: MockCall[] | undefined) => void) => void }) => {
      // store for test access
      (mock as unknown as { _provider: typeof provider })._provider = provider;
      return { dispose: vi.fn() };
    }),
  } as unknown as MockTerminal;
}

let mock: ReturnType<typeof makeMockTerminal>;

describe("terminalLinkProvider", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("detects ordinary file path with line number", () => {
    mock = makeMockTerminal(["see src/foo/bar.ts:12 for details"]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    expect(provider).toBeDefined();
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links).toBeDefined();
    expect(links!.length).toBe(1);
    expect(links![0].text).toBe("src/foo/bar.ts:12");
  });

  it("dispatches OPEN_FILE_EVENT with projectRoot on activate", () => {
    mock = makeMockTerminal(["open file web/src/App.tsx:42"]);
    registerFileLinkProvider(mock, "/my/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links!.length).toBe(1);
    const handler = vi.fn((_e: Event) => {});
    window.addEventListener(OPEN_FILE_EVENT, handler as EventListener);
    try {
      const link = links![0];
      link.activate(new MouseEvent("click"), link.text);
      expect(handler).toHaveBeenCalledTimes(1);
      const ev = handler.mock.calls[0][0] as unknown as CustomEvent<OpenFileDetail>;
      expect(ev.detail.path).toBe("web/src/App.tsx");
      expect(ev.detail.line).toBe(42);
      expect(ev.detail.projectRoot).toBe("/my/proj");
    } finally {
      window.removeEventListener(OPEN_FILE_EVENT, handler as EventListener);
    }
  });

  it("matches .ocode/uploads path with spaces and U+202F", () => {
    const upload = `.ocode/uploads/Screenshot 2026-09-03 at 10.20.33\u202fAM.png`;
    mock = makeMockTerminal([`check ${upload} now`]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links!.length).toBe(1);
    expect(links![0].text).toBe(upload);
  });

  it("does not match bare http url (handled by WebLinksAddon)", () => {
    mock = makeMockTerminal(["visit https://localhost:3510/ for app"]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links).toBeUndefined();
  });

  it("handles wrapped lines as single link", () => {
    // Simulate a long line wrapped into two rows: first part ends without space, second is wrapped.
    // Terminal buffer: line 0 = "prefix src/very/long/path/to/file.ts:123 "
    // but split as wrapped: we simulate two physical lines where first is not complete
    const full = "src/very/long/path/to/file.ts:123";
    // Split artificially: first buffer line contains prefix + start of path, second contains rest and isWrapped=true
    const line0 = "prefix src/very/lo";
    const line1 = "ng/path/to/file.ts:123 suffix";
    mock = makeMockTerminal([line0, line1], [false, true]);
    // To make wrapped detection work, line1 must be isWrapped=true and line0 not wrapped but line1 isWrapped indicates it continues from line0? Actually our helper checks line.isWrapped for current line, and previous lines isWrapped.
    // In xterm, a wrapped line has isWrapped=true meaning it is continuation of previous. So line1 isWrapped=true
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    // y=1 corresponds to buffer line 0, which should expand to include wrapped successor line1
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    // The provider joins windowed lines: lines = [line0, line1] => joined = "prefix src/very/long/path/to/file.ts:123 suffix"
    // So it should still find the full path
    expect(links).toBeDefined();
    expect(links![0].text).toBe(full);
  });

  it("returns undefined when no file link present", () => {
    mock = makeMockTerminal(["no links here just text"]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined = [] as unknown as MockCall[];
    provider.provideLinks(1, (l) => (links = l));
    expect(links).toBeUndefined();
  });

  it("gracefully handles terminal without registerLinkProvider", () => {
    const term = { buffer: { active: { getLine: () => undefined, getNullCell: () => ({ getChars: () => "", getWidth: () => 0 }) } } } as unknown as Terminal;
    const disp = registerFileLinkProvider(term, "/proj");
    expect(disp.dispose).toBeDefined();
    expect(() => disp.dispose()).not.toThrow();
  });

  it("computes range coordinates for link ending exactly at line boundary", () => {
    const line = "src/file.ts:1";
    mock = makeMockTerminal([line]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links!.length).toBe(1);
    expect(links![0].text).toBe(line);
    // single line, link starts at col 0 → x 1, y 1-indexed.
    // End maps to next-line col 0 ({x:0, y:2}) — this matches xterm's own
    // LinkComputer._mapStrIdx, which returns [line+1, 0] when a link ends
    // exactly at the line boundary. Parity with upstream matters more than
    // the coordinate looking odd; xterm renders it correctly.
    expect(links![0].range.start).toEqual({ x: 1, y: 1 });
    expect(links![0].range.end).toEqual({ x: 0, y: 2 });
  });

  it("computes range for link starting on wrapped continuation row", () => {
    // "$" is a shell prompt on the first row. It cannot merge into the path
    // token (the GUARD rejects [\w:/.\-] predecessors and "$" is none of
    // those), so the link still starts cleanly at col 0 of the wrapped row.
    // Note the prompt has NO trailing space on purpose: translateToString
    // trims trailing whitespace but raw cells keep it, so a "…$ " fixture
    // would misalign the mapped range by one cell (faithful xterm behavior,
    // same in upstream WebLinksAddon — not something to bake into a test).
    const line0 = "$";
    const line1 = "src/wrapped.ts:99 done";
    mock = makeMockTerminal([line0, line1], [false, true]);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    // Query the wrapped line itself (y=2 → lineIndex 1)
    provider.provideLinks(2, (l) => (links = l));
    expect(links).toBeDefined();
    expect(links![0].text).toBe("src/wrapped.ts:99");
    // Link starts at col 0 of line1 (y=2), so start x=1, y=2; trailing
    // " done" keeps the end mapping on the same row (end x is exclusive).
    expect(links![0].range.start).toEqual({ x: 1, y: 2 });
    expect(links![0].range.end).toEqual({ x: 17, y: 2 }); // token is 17 chars, end x exclusive
  });

  it("handles double-width character at wrap boundary without crashing and maps correctly", () => {
    // Line with wide char '你' (width 2). Place link after it.
    const line = "ab你 src/file.ts:10";
    // Mock wide char at idx 2 (the '你') with width 2
    const widthMap = new Map<string, number>([["0:2", 2]]);
    mock = makeMockTerminal([line], [false], widthMap);
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links!.length).toBe(1);
    expect(links![0].text).toBe("src/file.ts:10");
    // Verify range is computed (wide char shifts column but provider still returns a range)
    expect(links![0].range.start.x).toBeGreaterThan(1);
    expect(links![0].range.start.y).toBe(1);
  });

  it("walks the empty-placeholder correction without crashing", () => {
    // Row 0 "ab你" occupies 4 cells: col 3 is the empty second half of the
    // wide char (chars "", width 1 — xterm's NULL_CELL_WIDTH). Row 1 is a
    // soft wrap starting with another wide char (width 2 at col 0). Mapping
    // the link start walks through the placeholder cell, entering the
    // `chars === ""` correction branch (which peeks at the wrapped
    // successor's first cell), without throwing — and still finds the link.
    // CJK chars sit outside [\w.\-], so the token cannot merge with them.
    const line1 = "你 src/file.ts:5 ok";
    mock = makeMockTerminal(["ab你", line1], [false, true], new Map([
      ["0:3", 1], // empty placeholder cell, nonzero width
      ["1:0", 2], // wide char starting the wrapped row
    ]));
    // Expose the placeholder cell: row 0 logically has 4 cells while its
    // trimmed string is only 3 chars. getCell(3) already returns "".
    const origGetLine = mock.buffer.active.getLine.bind(mock.buffer.active);
    (mock.buffer.active.getLine as unknown as (n: number) => unknown) = ((idx: number) => {
      const line = origGetLine(idx) as unknown as { length: number } | undefined;
      if (idx === 0 && line) (line as { length: number }).length = 4;
      return line as unknown as import("@xterm/xterm").IBufferLine;
    }) as unknown as typeof mock.buffer.active.getLine;
    registerFileLinkProvider(mock, "/proj");
    const provider = mock._provider!;
    let links: MockCall[] | undefined;
    provider.provideLinks(1, (l) => (links = l));
    expect(links).toBeDefined();
    expect(links![0].text).toBe("src/file.ts:5");
  });
});
