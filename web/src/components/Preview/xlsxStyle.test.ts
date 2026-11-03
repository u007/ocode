import { describe, expect, it } from "vitest";
import { BooleanNumber, BorderStyleTypes, HorizontalAlign, VerticalAlign, WrapStrategy } from "@univerjs/core";
import type { BorderStyle } from "exceljs";
import {
  colWidthPx,
  DEFAULT_THEME_PALETTE,
  excelSerial,
  formatCell,
  formatNumber,
  isNumericValue,
  parseThemePalette,
  resolveColor,
  rowHeightPx,
  univerCellStyle,
} from "./xlsxStyle";

/** Minimal theme part with a distinct colour per slot, so a mis-ordered lookup shows up. */
const THEME_XML = `<?xml version="1.0"?>
<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Test">
<a:themeElements><a:clrScheme name="Test">
<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1>
<a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>
<a:dk2><a:srgbClr val="1F497D"/></a:dk2>
<a:lt2><a:srgbClr val="EEECE1"/></a:lt2>
<a:accent1><a:srgbClr val="4F81BD"/></a:accent1>
<a:accent2><a:srgbClr val="C0504D"/></a:accent2>
<a:accent3><a:srgbClr val="9BBB59"/></a:accent3>
<a:accent4><a:srgbClr val="8064A2"/></a:accent4>
<a:accent5><a:srgbClr val="4BACC6"/></a:accent5>
<a:accent6><a:srgbClr val="F79646"/></a:accent6>
<a:hlink><a:srgbClr val="0000FF"/></a:hlink>
<a:folHlink><a:srgbClr val="800080"/></a:folHlink>
</a:clrScheme></a:themeElements></a:theme>`;

describe("parseThemePalette", () => {
  it("maps Excel theme indices, not XML order: 0=lt1, 1=dk1, 2=lt2, 3=dk2", () => {
    const palette = parseThemePalette(THEME_XML);
    expect(palette[0]).toBe("#ffffff");
    expect(palette[1]).toBe("#000000");
    expect(palette[2]).toBe("#eeece1");
    expect(palette[3]).toBe("#1f497d");
    expect(palette[4]).toBe("#4f81bd");
    expect(palette[11]).toBe("#800080");
    expect(palette).toHaveLength(12);
  });

  it("uses the Office default palette when the workbook has no theme", () => {
    expect(parseThemePalette(undefined)).toEqual([...DEFAULT_THEME_PALETTE]);
    expect(DEFAULT_THEME_PALETTE[4]).toBe("#4472c4");
  });

  it("throws on a theme with a missing colour slot instead of guessing", () => {
    const broken = THEME_XML.replace(/<a:accent3>.*<\/a:accent3>/, "");
    expect(() => parseThemePalette(broken)).toThrow(/accent3/);
  });
});

describe("resolveColor", () => {
  const palette = parseThemePalette(THEME_XML);

  it("converts an ARGB value to #rrggbb, dropping alpha", () => {
    expect(resolveColor({ argb: "FFFF0000" }, palette)).toBe("#ff0000");
    expect(resolveColor({ argb: "FF00FF00" }, palette)).toBe("#00ff00");
  });

  it("resolves a theme index to a hex colour", () => {
    expect(resolveColor({ theme: 0 }, palette)).toBe("#ffffff");
    expect(resolveColor({ theme: 1 }, palette)).toBe("#000000");
    expect(resolveColor({ theme: 4 }, palette)).toBe("#4f81bd");
  });

  it("lightens a theme colour with a positive tint, per channel", () => {
    // 4F81BD + 0.4 * (255 - c): 79->149, 129->179, 189->215.
    expect(resolveColor({ theme: 4, tint: 0.4 }, palette)).toBe("#95b3d7");
  });

  it("darkens a theme colour with a negative tint, per channel", () => {
    // 4F81BD * 0.75: 79->59, 129->97, 189->142.
    expect(resolveColor({ theme: 4, tint: -0.25 }, palette)).toBe("#3b618e");
  });

  it("rounds the tinted channel to the nearest unit", () => {
    // White darkened by 0.5 is 127.5 per channel, which rounds to 128.
    expect(resolveColor({ theme: 0, tint: -0.5 }, palette)).toBe("#808080");
  });

  it("resolves the legacy indexed palette", () => {
    expect(resolveColor({ indexed: 2 }, palette)).toBe("#ff0000");
    expect(resolveColor({ indexed: 22 }, palette)).toBe("#c0c0c0");
    expect(resolveColor({ indexed: 65 }, palette)).toBe("#ffffff");
  });

  it("treats indexed 64 (system foreground) as automatic", () => {
    expect(resolveColor({ indexed: 64 }, palette)).toBeUndefined();
  });

  it("returns undefined for an absent colour", () => {
    expect(resolveColor(undefined, palette)).toBeUndefined();
    expect(resolveColor({}, palette)).toBeUndefined();
  });

  it("throws on a theme index outside the palette", () => {
    expect(() => resolveColor({ theme: 99 }, palette)).toThrow(/theme index 99/);
  });

  it("throws on an indexed colour outside Excel's palette", () => {
    expect(() => resolveColor({ indexed: 200 }, palette)).toThrow(/indexed colour 200/);
  });

  it("throws on a malformed ARGB value", () => {
    expect(() => resolveColor({ argb: "red" }, palette)).toThrow(/invalid ARGB/);
  });
});

describe("univerCellStyle", () => {
  const palette = parseThemePalette(THEME_XML);

  it("maps bold, italic, size and font name", () => {
    const style = univerCellStyle({ font: { bold: true, italic: true, size: 14, name: "Arial" } }, palette, false);
    expect(style.bl).toBe(BooleanNumber.TRUE);
    expect(style.it).toBe(BooleanNumber.TRUE);
    expect(style.fs).toBe(14);
    expect(style.ff).toBe("Arial");
  });

  it("sets underline and strike as their own decorations", () => {
    const style = univerCellStyle({ font: { underline: true, strike: true } }, palette, false);
    expect(style.ul).toEqual({ s: BooleanNumber.TRUE });
    expect(style.st).toEqual({ s: BooleanNumber.TRUE });
  });

  it("leaves the default theme-1 font colour unset so the sheet's theme can apply", () => {
    const style = univerCellStyle({ font: { color: { theme: 1 } } }, palette, false);
    expect(style.cl).toBeUndefined();
  });

  it("keeps an explicit font colour", () => {
    const style = univerCellStyle({ font: { color: { argb: "FFFF0000" } } }, palette, false);
    expect(style.cl).toEqual({ rgb: "#ff0000" });
  });

  it("forces black text on a solid fill that has no explicit font colour", () => {
    const style = univerCellStyle(
      { font: { color: { theme: 1 } }, fill: { type: "pattern", pattern: "solid", fgColor: { argb: "FFFFFF00" } } },
      palette,
      false,
    );
    expect(style.bg).toEqual({ rgb: "#ffff00" });
    expect(style.cl).toEqual({ rgb: "#000000" });
  });

  it("keeps an explicit font colour on a filled cell", () => {
    const style = univerCellStyle(
      { font: { color: { argb: "FFFFFFFF" } }, fill: { type: "pattern", pattern: "solid", fgColor: { theme: 4 } } },
      palette,
      false,
    );
    expect(style.cl).toEqual({ rgb: "#ffffff" });
  });

  it("draws only solid fills", () => {
    const style = univerCellStyle({ fill: { type: "pattern", pattern: "gray125", fgColor: { argb: "FFFF0000" } } }, palette, false);
    expect(style.bg).toBeUndefined();
  });

  it("maps border styles to Univer border styles and colours, per side", () => {
    const black = { argb: "FF000000" };
    const style = univerCellStyle(
      {
        border: {
          top: { style: "thin", color: black },
          right: { style: "medium", color: black },
          bottom: { style: "double", color: black },
          left: { style: "thick" },
        },
      },
      palette,
      false,
    );
    expect(style.bd?.t).toEqual({ s: BorderStyleTypes.THIN, cl: { rgb: "#000000" } });
    expect(style.bd?.r).toEqual({ s: BorderStyleTypes.MEDIUM, cl: { rgb: "#000000" } });
    expect(style.bd?.b).toEqual({ s: BorderStyleTypes.DOUBLE, cl: { rgb: "#000000" } });
    // An unset colour is Excel's automatic, which is black.
    expect(style.bd?.l).toEqual({ s: BorderStyleTypes.THICK, cl: { rgb: "#000000" } });
  });

  it("leaves a side without a border style alone so the grid shows", () => {
    const style = univerCellStyle({ border: { top: { style: "thin" } } }, palette, false);
    expect(style.bd?.l).toBeUndefined();
  });

  it("throws on an unknown border style", () => {
    const bogus: string = "wiggly";
    expect(() => univerCellStyle({ border: { top: { style: bogus as BorderStyle } } }, palette, false)).toThrow(
      /unknown border style "wiggly"/,
    );
  });

  it("wraps wrapped text and lets unwrapped text overflow, as Excel does", () => {
    expect(univerCellStyle({ alignment: { wrapText: true } }, palette, false).tb).toBe(WrapStrategy.WRAP);
    expect(univerCellStyle({}, palette, false).tb).toBe(WrapStrategy.OVERFLOW);
  });

  it("right-aligns numbers under General and leaves text to the sheet default", () => {
    expect(univerCellStyle({}, palette, true).ht).toBe(HorizontalAlign.RIGHT);
    expect(univerCellStyle({}, palette, false).ht).toBeUndefined();
  });

  it("uses an explicit horizontal alignment over the value type", () => {
    expect(univerCellStyle({ alignment: { horizontal: "center" } }, palette, true).ht).toBe(HorizontalAlign.CENTER);
  });

  it("aligns to the bottom by default, as Excel does", () => {
    expect(univerCellStyle({}, palette, false).vt).toBe(VerticalAlign.BOTTOM);
    expect(univerCellStyle({ alignment: { vertical: "top" } }, palette, false).vt).toBe(VerticalAlign.TOP);
  });
});

describe("sizes", () => {
  it("converts Excel column width in characters to pixels", () => {
    expect(colWidthPx(25)).toBe(180);
    expect(colWidthPx(12)).toBe(89);
  });

  it("uses Excel's default 8.43-character width (64px) when a column has no width", () => {
    expect(colWidthPx(undefined)).toBe(64);
  });

  it("converts row height from points to pixels", () => {
    expect(rowHeightPx(40)).toBeCloseTo(53.333, 3);
    expect(rowHeightPx(15)).toBe(20);
  });
});

describe("isNumericValue", () => {
  it("is true for numbers and dates", () => {
    expect(isNumericValue(3)).toBe(true);
    expect(isNumericValue(new Date(Date.UTC(2024, 0, 15)))).toBe(true);
  });

  it("is true for a formula whose cached result is numeric", () => {
    expect(isNumericValue({ formula: "SUM(A1:A3)", result: 6 })).toBe(true);
  });

  it("is false for text, booleans, blanks and formulas with text or error results", () => {
    expect(isNumericValue("3")).toBe(false);
    expect(isNumericValue(true)).toBe(false);
    expect(isNumericValue(null)).toBe(false);
    expect(isNumericValue({ formula: 'A1&"x"', result: "3x" })).toBe(false);
    expect(isNumericValue({ formula: "1/0", result: { error: "#DIV/0!" } })).toBe(false);
  });
});

describe("excelSerial", () => {
  const MS_PER_DAY = 24 * 3600 * 1000;
  // ExcelJS reads a serial as days from 1899-12-30 (1900 system) or 1462 days
  // later (1904 system). These helpers reproduce that read so the inverse can
  // be checked against the exact value a file stores.
  const readAs1900 = (v: number): Date => new Date(Date.UTC(1899, 11, 30) + v * MS_PER_DAY);
  const readAs1904 = (v: number): Date => new Date(Date.UTC(1899, 11, 30) + (v + 1462) * MS_PER_DAY);

  it("is the exact inverse of ExcelJS's 1900-system read, including the 1900 leap-year serials", () => {
    for (const v of [0.5, 1, 1.25, 59, 60, 61, 45306]) {
      expect(excelSerial(readAs1900(v), false)).toBeCloseTo(v, 9);
    }
  });

  it("is the exact inverse of ExcelJS's 1904-system read", () => {
    for (const v of [0, 0.5, 1, 45306]) {
      expect(excelSerial(readAs1904(v), true)).toBeCloseTo(v, 9);
    }
  });

  it("matches the known serial for a modern date", () => {
    expect(excelSerial(new Date(Date.UTC(2024, 0, 15)), false)).toBe(45306);
  });

  it("throws on a date before the epoch instead of writing a negative serial", () => {
    expect(() => excelSerial(new Date(Date.UTC(1899, 11, 29)), false)).toThrow(/before the 1900 date system/);
  });
});

describe("formatCell and formatNumber", () => {
  it("applies the number format to a number", () => {
    expect(formatCell(1234.5, "#,##0.00", false)).toBe("1,234.50");
  });

  it("shows a General number without a format", () => {
    expect(formatCell(1234.5, undefined, false)).toBe("1234.5");
    expect(formatCell(0.1 + 0.2, undefined, false)).toBe("0.3");
  });

  it("formats a date value by its serial number in the 1900 system", () => {
    // 45306 is 2024-01-15 in the 1900 date system.
    expect(formatCell(45306, "yyyy-mm-dd", false)).toBe("2024-01-15");
  });

  it("formats serials 59, 60 and 61 around the phantom 1900 leap day", () => {
    expect(formatCell(59, "yyyy-mm-dd", false)).toBe("1900-02-28");
    expect(formatCell(61, "yyyy-mm-dd", false)).toBe("1900-03-01");
  });

  it("formats a serial in the 1904 system with the 1904 epoch", () => {
    expect(formatCell(0, "yyyy-mm-dd", true)).toBe("1904-01-01");
    expect(formatCell(1, "yyyy-mm-dd", true)).toBe("1904-01-02");
  });

  it("converts an ExcelJS Date to its serial in the workbook's date system", () => {
    expect(formatCell(new Date(Date.UTC(2024, 0, 15)), "yyyy-mm-dd", false)).toBe("2024-01-15");
    expect(formatCell(new Date(Date.UTC(1904, 0, 2)), "yyyy-mm-dd", true)).toBe("1904-01-02");
  });

  it("passes strings through and renders empty and boolean values", () => {
    expect(formatCell("hello", "0.00", false)).toBe("hello");
    expect(formatCell(null, undefined, false)).toBe("");
    expect(formatCell(undefined, undefined, false)).toBe("");
    expect(formatCell(true, undefined, false)).toBe("TRUE");
  });

  it("joins the runs of a rich text value", () => {
    expect(formatCell({ richText: [{ text: "Bold " }, { text: "plain" }] }, undefined, false)).toBe("Bold plain");
  });

  it("shows a hyperlink's text", () => {
    expect(formatCell({ text: "Docs", hyperlink: "https://example.com" }, undefined, false)).toBe("Docs");
  });

  it("formats a formula's cached result with the cell's number format", () => {
    expect(formatCell({ formula: "A1*2", result: 3 }, "0.0", false)).toBe("3.0");
  });

  it("shows a formula with no cached result as empty", () => {
    expect(formatCell({ formula: "A1*2" }, undefined, false)).toBe("");
  });

  it("shows an error value as its Excel error code", () => {
    expect(formatCell({ error: "#DIV/0!" }, undefined, false)).toBe("#DIV/0!");
  });

  it("throws on a number format it cannot parse, naming the format and value", () => {
    expect(() => formatNumber(1234.5, "[bogus", false)).toThrow(/number format "\[bogus" cannot format 1234.5/);
  });
});
