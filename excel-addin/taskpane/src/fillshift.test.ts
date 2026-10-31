import { describe, expect, it } from "vitest";
import { shiftFormula } from "./a1";
import { FakePort } from "./fakePort";
import { Journal, MemoryJournalStore } from "./tools/journal";
import type { ToolCtx } from "./tools/util";
import { fillFormula } from "./tools/write";

describe("shiftFormula: whole-column references", () => {
  it.each([
    ["=SUM(B:B)", 0, 1, "=SUM(C:C)"],
    ["=SUM($B:$B)", 0, 1, "=SUM($B:$B)"],
    ["=SUM(B:D)", 0, 1, "=SUM(C:E)"],
    ["=SUM($B:D)", 0, 1, "=SUM($B:E)"],
    ["=SUM(B:$D)", 0, 1, "=SUM(C:$D)"],
    ["=SUM(B:B)", 5, 0, "=SUM(B:B)"], // a row shift never moves a column reference
    ["=SUM(b:b)", 0, 2, "=SUM(D:D)"], // output is upper-case, like cell references
  ])("%s shifted by (row %i, col %i) is %s", (formula, dRow, dCol, want) => {
    expect(shiftFormula(formula, dRow, dCol)).toBe(want);
  });

  it.each([
    ["=SUM(A:A)", 0, -1, "=SUM(#REF!)"],
    ["=SUM(XFD:XFD)", 0, 1, "=SUM(#REF!)"],
    ["=SUM(B:XFD)", 0, 1, "=SUM(#REF!)"], // one end off the sheet makes the whole range #REF!
    ["=SUM($A:B)", 0, -1, "=SUM($A:A)"], // anchored start stays put, the other end shifts in bounds
    ["=SUM($A:$B)", -5, -5, "=SUM($A:$B)"], // fully anchored never overflows
  ])("%s shifted by (row %i, col %i) gives %s", (formula, dRow, dCol, want) => {
    expect(shiftFormula(formula, dRow, dCol)).toBe(want);
  });
});

describe("shiftFormula: whole-row references", () => {
  it.each([
    ["=SUM(2:2)", 1, 0, "=SUM(3:3)"],
    ["=SUM($2:$5)", 1, 0, "=SUM($2:$5)"],
    ["=SUM(2:$5)", 1, 0, "=SUM(3:$5)"],
    ["=SUM(2:5)", 1, 0, "=SUM(3:6)"],
    ["=SUM(2:2)", 0, 3, "=SUM(2:2)"], // a column shift never moves a row reference
  ])("%s shifted by (row %i, col %i) is %s", (formula, dRow, dCol, want) => {
    expect(shiftFormula(formula, dRow, dCol)).toBe(want);
  });

  it.each([
    ["=SUM(2:2)", -2, 0, "=SUM(#REF!)"],
    ["=SUM(1:2)", -1, 0, "=SUM(#REF!)"],
    ["=SUM(1048576:1048576)", 1, 0, "=SUM(#REF!)"],
    ["=SUM(2:1048576)", 1, 0, "=SUM(#REF!)"],
    ["=SUM($1:$2)", -5, 0, "=SUM($1:$2)"],
  ])("%s shifted by (row %i, col %i) gives %s", (formula, dRow, dCol, want) => {
    expect(shiftFormula(formula, dRow, dCol)).toBe(want);
  });
});

describe("shiftFormula: sheet-qualified whole references", () => {
  it("shifts after an unquoted or quoted sheet name", () => {
    expect(shiftFormula("=Sheet1!B:B", 0, 1)).toBe("=Sheet1!C:C");
    expect(shiftFormula("='My Sheet'!2:2", 1, 0)).toBe("='My Sheet'!3:3");
    expect(shiftFormula("=SUM('My Sheet'!$B:B)+Sheet1!2:$5", 1, 1)).toBe("=SUM('My Sheet'!$B:C)+Sheet1!3:$5");
  });
});

describe("shiftFormula: no misreads", () => {
  it("still shifts ordinary cell ranges as before", () => {
    expect(shiftFormula("=SUM(A1:B2)", 2, 0)).toBe("=SUM(A3:B4)");
    expect(shiftFormula("=SUM(A1:B2)", 1, 1)).toBe("=SUM(B2:C3)");
    expect(shiftFormula("=SUM($A$1:B2)", 1, 1)).toBe("=SUM($A$1:C3)");
  });

  it("leaves a time-like token inside a string literal alone", () => {
    expect(shiftFormula('="12:30"', 1, 1)).toBe('="12:30"');
    expect(shiftFormula('=IF(A1="2:2",B:B,"B:B")', 1, 1)).toBe('=IF(B2="2:2",C:C,"B:B")');
  });

  it("does not read the row of a cell reference or a decimal as a row range", () => {
    expect(shiftFormula("=A1*1.5+2", 1, 0)).toBe("=A2*1.5+2");
    expect(shiftFormula("=ZZ1:ZZ2", 0, 0)).toBe("=ZZ1:ZZ2");
  });
});

describe("fill_formula with whole-column and whole-row references", () => {
  const setup = () => {
    const port = new FakePort().addSheet("Summary", [["a", 1, 2, 3]]);
    const ctx: ToolCtx = { port, journal: new Journal(new MemoryJournalStore()), signal: new AbortController().signal };
    return { port, ctx };
  };

  it("fills D2:F2 with =SUM(B:B) as C:C-style shifted formulas", async () => {
    const { port, ctx } = setup();
    await fillFormula(ctx, { sheet: "Summary", address: "D2:F2", formula: "=SUM(B:B)", overwrite: false });
    expect((await port.read("Summary", "D2:F2")).formulas[0]).toEqual(["=SUM(B:B)", "=SUM(C:C)", "=SUM(D:D)"]);
  }, 30_000);

  it("fills D2:D4 with =SUM(2:2) as row-shifted formulas", async () => {
    const { port, ctx } = setup();
    await fillFormula(ctx, { sheet: "Summary", address: "D2:D4", formula: "=SUM(2:2)", overwrite: false });
    expect((await port.read("Summary", "D2:D4")).formulas.map((r) => r[0])).toEqual(["=SUM(2:2)", "=SUM(3:3)", "=SUM(4:4)"]);
  }, 30_000);
});
