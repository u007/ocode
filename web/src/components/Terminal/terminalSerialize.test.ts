import { describe, it, expect } from "vitest";
import type { Terminal } from "ghostty-web";
import { serializeTerminalBuffer } from "./terminalSerialize";

const DEFAULTS = { foreground: 0xe4e4e7, background: 0x18181b };

interface C {
  ch: string;
  w?: number;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  fg?: number;
  bg?: number;
}

/** Row DSL: a string is plain default cells; an array gives explicit cells. `~` prefix marks a wrapped row. */
type RowSpec = string | C[];

// ghostty-web's null cell: what getNullCell() returns and what getCell() synthesizes past a row's stored cells.
const NULL_FG = 0xcccccc;
const NULL_BG = 0x000000;

function nullCell() {
  return {
    getChars: () => "",
    getWidth: () => 1,
    getFgColor: () => NULL_FG,
    getBgColor: () => NULL_BG,
    isBold: () => 0,
    isItalic: () => 0,
    isUnderline: () => 0,
    isStrikethrough: () => 0,
    isBlink: () => 0,
    isInverse: () => 0,
    isInvisible: () => 0,
    isFaint: () => 0,
  };
}

function makeTerm(specs: RowSpec[], wrapped: boolean[] = []): Pick<Terminal, "buffer"> {
  const lines = specs.map((spec, idx) => {
    const cells: C[] = typeof spec === "string" ? [...spec].map((ch) => ({ ch })) : spec;
    return {
      length: cells.length,
      isWrapped: wrapped[idx] ?? false,
      getCell: (x: number) => {
        const c = cells[x];
        if (!c) return undefined;
        return {
          getChars: () => (c.ch === " " ? "" : c.ch),
          getWidth: () => c.w ?? 1,
          getFgColor: () => c.fg ?? DEFAULTS.foreground,
          getBgColor: () => c.bg ?? DEFAULTS.background,
          isBold: () => (c.bold ? 1 : 0),
          isItalic: () => (c.italic ? 1 : 0),
          isUnderline: () => (c.underline ? 1 : 0),
          isStrikethrough: () => 0,
          isBlink: () => 0,
          isInverse: () => 0,
          isInvisible: () => 0,
          isFaint: () => 0,
        };
      },
    };
  });
  return {
    buffer: { active: { length: lines.length, getLine: (y: number) => lines[y], getNullCell: () => nullCell() } },
  } as unknown as Pick<Terminal, "buffer">;
}

describe("serializeTerminalBuffer", () => {
  it("joins plain rows with CRLF and emits no escapes", () => {
    expect(serializeTerminalBuffer(makeTerm(["abc", "def"]), DEFAULTS)).toBe("abc\r\ndef");
  });

  it("trims trailing blanks and drops trailing blank rows", () => {
    expect(serializeTerminalBuffer(makeTerm(["abc   ", "      ", "      "]), DEFAULTS)).toBe("abc");
  });

  it("keeps interior blank rows", () => {
    expect(serializeTerminalBuffer(makeTerm(["a", "   ", "b"]), DEFAULTS)).toBe("a\r\n\r\nb");
  });

  it("joins wrapped rows without a line break", () => {
    expect(serializeTerminalBuffer(makeTerm(["abcd", "ef"], [false, true]), DEFAULTS)).toBe("abcdef");
  });

  it("does not trim a row that is continued by a wrapped row", () => {
    expect(serializeTerminalBuffer(makeTerm(["ab  ", "cd"], [false, true]), DEFAULTS)).toBe("ab  cd");
  });

  it("skips the zero-width tail of a wide char", () => {
    const row: C[] = [{ ch: "a" }, { ch: "你", w: 2 }, { ch: "", w: 0 }, { ch: "b" }];
    expect(serializeTerminalBuffer(makeTerm([row]), DEFAULTS)).toBe("a你b");
  });

  it("emits truecolor foreground and background as one sequence and resets at the end", () => {
    const row: C[] = [{ ch: "x", fg: 0xff0000, bg: 0x00ff00 }];
    expect(serializeTerminalBuffer(makeTerm([row]), DEFAULTS)).toBe("\x1b[38;2;255;0;0;48;2;0;255;0mx\x1b[0m");
  });

  it("combines attributes in one sequence in a fixed order", () => {
    const row: C[] = [{ ch: "x", bold: true, italic: true, underline: true, fg: 0x010203 }];
    expect(serializeTerminalBuffer(makeTerm([row]), DEFAULTS)).toBe("\x1b[1;3;4;38;2;1;2;3mx\x1b[0m");
  });

  it("resets between cells when attributes change, without redundant codes for equal cells", () => {
    const row: C[] = [{ ch: "a", bold: true }, { ch: "b", bold: true }, { ch: "c" }, { ch: "d", italic: true }];
    expect(serializeTerminalBuffer(makeTerm([row]), DEFAULTS)).toBe("\x1b[1mab\x1b[0mc\x1b[3md\x1b[0m");
  });

  it("does not trim trailing blanks that carry a background colour", () => {
    const row: C[] = [{ ch: "a" }, { ch: " ", bg: 0x112233 }];
    expect(serializeTerminalBuffer(makeTerm([row]), DEFAULTS)).toBe("a\x1b[48;2;17;34;51m \x1b[0m");
  });

  it("treats an undefined cell as a default blank", () => {
    const term = makeTerm(["ab"]);
    const line = term.buffer.active.getLine(0)!;
    const patched = { ...line, length: 4, getCell: (x: number) => line.getCell(x) };
    (term.buffer.active as unknown as { getLine: () => unknown }).getLine = () => patched;
    expect(serializeTerminalBuffer(term, DEFAULTS)).toBe("ab");
  });

  it("treats cells synthesized past the row's stored cells as blanks, not null-cell colours", () => {
    const term = makeTerm(["ab"]);
    const line = term.buffer.active.getLine(0)!;
    const patched = { ...line, length: 6, getCell: (x: number) => (x < 2 ? line.getCell(x) : nullCell()) };
    (term.buffer.active as unknown as { getLine: () => unknown }).getLine = () => patched;
    const out = serializeTerminalBuffer(term, DEFAULTS);
    expect(out).toBe("ab");
    expect(out).not.toContain("38;2;204;204;204");
  });

  it("returns empty string for an empty buffer", () => {
    expect(serializeTerminalBuffer(makeTerm([]), DEFAULTS)).toBe("");
  });
});
