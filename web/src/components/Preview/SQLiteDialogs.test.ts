import { describe, expect, it } from "vitest";
import { cellToInput, inputToCell } from "./SQLiteDialogs";

describe("SQLiteDialogs value codec", () => {
  it("renders cells as editable text", () => {
    expect(cellToInput(null)).toBe("");
    expect(cellToInput(5)).toBe("5");
    expect(cellToInput("x")).toBe("x");
    expect(cellToInput({ $blob: true, bytes: 3, preview: "aabb" })).toBe("");
  });

  it("coerces input by declared type", () => {
    expect(inputToCell("", "TEXT")).toBeNull();
    expect(inputToCell("42", "INTEGER")).toBe(42);
    expect(inputToCell("42.9", "INTEGER")).toBe(42);
    expect(inputToCell("4.5", "REAL")).toBe(4.5);
    expect(inputToCell("hello", "TEXT")).toBe("hello");
    // A non-numeric value in a numeric column is kept as text rather than
    // silently becoming NaN.
    expect(inputToCell("abc", "INTEGER")).toBe("abc");
  });
});
