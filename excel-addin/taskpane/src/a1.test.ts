import { describe, expect, it } from "vitest";
import { colToLetters, formatRect, lettersToCol, parseAddress, shiftFormula, MAX_ROW } from "./a1";

describe("a1", () => {
  it("converts columns both ways", () => {
    expect([1, 26, 27, 52, 702, 703, 16384].map(colToLetters)).toEqual(["A", "Z", "AA", "AZ", "ZZ", "AAA", "XFD"]);
    expect(["A", "AA", "XFD"].map(lettersToCol)).toEqual([1, 27, 16384]);
  });
  it("parses cells, ranges, absolute refs, sheets", () => {
    expect(parseAddress("B3")).toMatchObject({ c1: 2, r1: 3, c2: 2, r2: 3 });
    expect(parseAddress("$A$1:$F$24")).toMatchObject({ c1: 1, r1: 1, c2: 6, r2: 24 });
    expect(parseAddress("F24:A1")).toMatchObject({ c1: 1, r1: 1, c2: 6, r2: 24 });
    expect(parseAddress("'Stock Count'!C2:D5")).toMatchObject({ sheet: "Stock Count", c1: 3, r1: 2, c2: 4, r2: 5 });
    expect(parseAddress("B:B")).toMatchObject({ c1: 2, c2: 2, r1: 1, r2: MAX_ROW });
    expect(parseAddress("3:5")).toMatchObject({ r1: 3, r2: 5, c1: 1 });
  });
  it("rejects what is not an address", () => {
    for (const bad of ["", "A", "1A", "A0", "XFE1", "A1048577", "A1:B", "hello", "A1;B2"]) expect(parseAddress(bad)).toBeNull();
  });
  it("formats", () => {
    expect(formatRect({ c1: 1, r1: 1, c2: 1, r2: 1 })).toBe("A1");
    expect(formatRect({ sheet: "My Sheet", c1: 1, r1: 2, c2: 3, r2: 4 }, true)).toBe("'My Sheet'!A2:C4");
  });
});

describe("shiftFormula", () => {
  it("moves relative refs and keeps anchored ones", () => {
    expect(shiftFormula("=A1+B$2+$C3+$D$4", 1, 1)).toBe("=B2+C$2+$C4+$D$4");
    expect(shiftFormula("=SUM(A1:B2)", 2, 0)).toBe("=SUM(A3:B4)");
  });
  it("leaves strings, functions, names and sheet quoting alone", () => {
    expect(shiftFormula('=IF(A1="A1",LOG10(B1),"x")', 1, 0)).toBe('=IF(A2="A1",LOG10(B2),"x")');
    expect(shiftFormula("='My Sheet'!A1+Settings!$B$1", 3, 0)).toBe("='My Sheet'!A4+Settings!$B$1");
    expect(shiftFormula("=TAX_RATE*A1", 1, 0)).toBe("=TAX_RATE*A2");
  });
  it("produces #REF! when a shift leaves the sheet", () => {
    expect(shiftFormula("=A1", -1, 0)).toBe("=#REF!");
  });
});
