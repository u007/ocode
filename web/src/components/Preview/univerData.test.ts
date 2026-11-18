import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { Workbook, type BorderStyle, type Worksheet } from "exceljs";
import * as XLSX from "xlsx";
import JSZip from "jszip";

const MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
const RELS =
  "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const PKG_RELS = "http://schemas.openxmlformats.org/package/2006/relationships";

import {
  BooleanNumber,
  BorderStyleTypes,
  CellValueType,
  HorizontalAlign,
  WrapStrategy,
  type ICellData,
  type IStyleData,
  type IWorkbookData,
} from "@univerjs/core";
import {
  assertWithinFileLimit,
  MAX_COLS,
  MAX_FILE_BYTES,
  MAX_ROWS,
  prefixDrawingNamespaces,
  SHEETJS_READ_ROWS,
  workbookFromExcelJs,
  workbookFromFileBytes,
  workbookFromSheetJs,
} from "./univerData";

const STYLED_FIXTURE = join(__dirname, "__fixtures__", "styled.xlsx");
/** 1x1 transparent PNG. */
const ONE_PX_PNG_BASE64 =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==";

async function loadStyledFixture(): Promise<Workbook> {
  const wb = new Workbook();
  await wb.xlsx.load(
    new Uint8Array(readFileSync(STYLED_FIXTURE)).buffer as ArrayBuffer,
  );
  return wb;
}

/** A fresh in-memory ExcelJS workbook with one sheet named `name`. */
function memoryBook(name: string): { wb: Workbook; ws: Worksheet } {
  const wb = new Workbook();
  return { wb, ws: wb.addWorksheet(name) };
}

function firstSheet(data: IWorkbookData) {
  return data.sheets[data.sheetOrder[0]];
}

/** The converted cell at a 1-based row/column of the first sheet. */
function cellAt(
  data: IWorkbookData,
  row: number,
  col: number,
): ICellData | undefined {
  return firstSheet(data).cellData?.[row - 1]?.[col - 1];
}

function styleOf(data: IWorkbookData, cell: ICellData | undefined): IStyleData {
  if (cell === undefined) throw new Error("expected a converted cell");
  if (typeof cell.s !== "string") throw new Error("expected a style id");
  const style = data.styles[cell.s];
  if (style === undefined || style === null)
    throw new Error(`no style ${cell.s}`);
  return style;
}

describe("workbookFromExcelJs on the styled fixture", () => {
  it("applies bold, italic, red colour and 14pt size to A1", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    const a1 = cellAt(data, 1, 1);
    expect(a1?.v).toBe("Bold red");
    const style = styleOf(data, a1);
    expect(style.bl).toBe(BooleanNumber.TRUE);
    expect(style.it).toBe(BooleanNumber.TRUE);
    expect(style.cl).toEqual({ rgb: "#ff0000" });
    expect(style.fs).toBe(14);
  });

  it("fills B1 with its RGB background colour", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    expect(styleOf(data, cellAt(data, 1, 2)).bg).toEqual({ rgb: "#ffff00" });
  });

  it("resolves the theme-4 tint 0.4 fill on C1 to Excel's colour", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    // Office 2007 accent1 (4F81BD) lightened by 0.4 is #95b3d7, the colour Excel shows.
    expect(styleOf(data, cellAt(data, 1, 3)).bg).toEqual({ rgb: "#95b3d7" });
  });

  it("sets each column's width from the sheet, and Excel's default when unset", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    const columns = firstSheet(data).columnData;
    expect(columns?.[0]?.w).toBe(180); // 25 * 7 + 5
    expect(columns?.[1]?.w).toBe(89); // 12 * 7 + 5
    expect(columns?.[2]?.w).toBe(64); // no width set: Excel default
  });

  it("merges A3:B3 into one block and leaves the covered cell out", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    expect(firstSheet(data).mergeData).toEqual([
      { startRow: 2, startColumn: 0, endRow: 2, endColumn: 1 },
    ]);
    expect(cellAt(data, 3, 1)?.v).toBe("merged");
    // ExcelJS repeats the master value in the covered cell; it must not be kept.
    expect(cellAt(data, 3, 2)).toBeUndefined();
  });

  it("hides column D without dropping its data", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    expect(firstSheet(data).columnData?.[3]?.hd).toBe(BooleanNumber.TRUE);
    expect(cellAt(data, 1, 4)?.v).toBe("hidden");
  });

  it("formats a number with its number format", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    expect(cellAt(data, 2, 1)?.v).toBe("1,234.50");
  });

  it("wraps and centres B2, and uses the 40pt row height", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    const b2 = cellAt(data, 2, 2);
    const style = styleOf(data, b2);
    expect(b2?.v).toBe("wrap me long text here");
    expect(style.tb).toBe(WrapStrategy.WRAP);
    expect(style.ht).toBe(HorizontalAlign.CENTER);
    expect(firstSheet(data).rowData?.[1]?.h).toBeCloseTo(53.333, 3);
  });

  it("draws the thin border on C2", async () => {
    const { data } = workbookFromExcelJs(await loadStyledFixture());
    const bd = styleOf(data, cellAt(data, 2, 3)).bd;
    expect(bd?.t).toEqual({ s: BorderStyleTypes.THIN, cl: { rgb: "#000000" } });
    expect(bd?.l).toEqual({ s: BorderStyleTypes.THIN, cl: { rgb: "#000000" } });
  });

  it("reports the sheet by name with no truncation", async () => {
    const { sheets } = workbookFromExcelJs(await loadStyledFixture());
    expect(sheets).toEqual([
      {
        id: "sheet-1",
        name: "Styled",
        isEmpty: false,
        rowCapped: false,
        colCapped: false,
      },
    ]);
  });
});

describe("workbookFromExcelJs merges, styles and hidden rows", () => {
  it("clips a merged block to the visible caps and skips every covered cell", () => {
    const { ws } = memoryBook("Merge");
    ws.getCell("B2").value = "block";
    ws.mergeCells("B2:C3");
    const { data } = workbookFromExcelJs(ws.workbook);
    expect(firstSheet(data).mergeData).toEqual([
      { startRow: 1, startColumn: 1, endRow: 2, endColumn: 2 },
    ]);
    expect(cellAt(data, 2, 2)?.v).toBe("block");
    expect(cellAt(data, 2, 3)).toBeUndefined();
    expect(cellAt(data, 3, 2)).toBeUndefined();
  });

  it("shares one style id between cells with the same style", () => {
    const { ws } = memoryBook("Dedupe");
    ws.getCell("A1").font = { bold: true };
    ws.getCell("A2").font = { bold: true };
    const { data } = workbookFromExcelJs(ws.workbook);
    expect(cellAt(data, 1, 1)?.s).toBe(cellAt(data, 2, 1)?.s);
  });

  it("marks hidden rows and keeps row heights", () => {
    const { ws } = memoryBook("Rows");
    ws.getCell("A1").value = "shown";
    ws.getRow(2).height = 30;
    ws.getRow(2).hidden = true;
    ws.getCell("A2").value = "gone";
    const { data } = workbookFromExcelJs(ws.workbook);
    expect(firstSheet(data).rowData?.[1]).toEqual({
      h: 40,
      hd: BooleanNumber.TRUE,
    });
    expect(firstSheet(data).rowData?.[0]).toBeUndefined();
  });

  it("marks a workbook with no cells as an empty sheet", () => {
    const { ws } = memoryBook("Blank");
    const { sheets, data } = workbookFromExcelJs(ws.workbook);
    expect(sheets[0].isEmpty).toBe(true);
    expect(firstSheet(data).rowCount).toBe(1);
  });
});

describe("workbookFromExcelJs dates, through ExcelJS's own reader", () => {
  /**
   * Writes numeric cells with their formats, reads the bytes back with a fresh
   * ExcelJS workbook (the viewer's path), and returns the displayed text.
   */
  async function roundTrip(
    values: Array<[number, string]>,
    date1904 = false,
  ): Promise<string[]> {
    const wb = new Workbook();
    wb.properties.date1904 = date1904;
    const ws = wb.addWorksheet("Dates");
    values.forEach(([v, fmt], i) => {
      ws.getCell(i + 1, 1).value = v;
      ws.getCell(i + 1, 1).numFmt = fmt;
    });
    const read = new Workbook();
    await read.xlsx.load(await wb.xlsx.writeBuffer());
    const { data } = workbookFromExcelJs(read);
    return values.map((_, i) => String(cellAt(data, i + 1, 1)?.v));
  }

  it("shows 1900-system serials as Excel does, including the phantom 29 Feb 1900 and time-only cells", async () => {
    const shown = await roundTrip([
      [0.5, "h:mm"],
      [1.25, "[h]:mm"],
      [1, "yyyy-mm-dd"],
      [59, "yyyy-mm-dd"],
      [60, "yyyy-mm-dd"],
      [61, "yyyy-mm-dd"],
      [45306, "yyyy-mm-dd"],
    ]);
    expect(shown).toEqual([
      "12:00",
      "30:00",
      "1900-01-01",
      "1900-02-28",
      "1900-02-29",
      "1900-03-01",
      "2024-01-15",
    ]);
  });

  it("shows 1904-system serials with the 1904 epoch", async () => {
    expect(
      await roundTrip(
        [
          [0, "yyyy-mm-dd"],
          [1, "yyyy-mm-dd"],
        ],
        true,
      ),
    ).toEqual(["1904-01-01", "1904-01-02"]);
  });

  it("keeps the 1904 epoch for Date values built in memory", () => {
    const { wb, ws } = memoryBook("Dates1904");
    wb.properties.date1904 = true;
    ws.getCell("A1").value = new Date(Date.UTC(1904, 0, 1));
    ws.getCell("A1").numFmt = "yyyy-mm-dd";
    ws.getCell("A2").value = new Date(Date.UTC(1904, 0, 2));
    ws.getCell("A2").numFmt = "yyyy-mm-dd";
    const { data } = workbookFromExcelJs(wb);
    expect(cellAt(data, 1, 1)?.v).toBe("1904-01-01");
    expect(cellAt(data, 2, 1)?.v).toBe("1904-01-02");
  });
});

describe("workbookFromExcelJs fails loudly", () => {
  it("names the cell and value when a theme index is out of range", () => {
    const { ws } = memoryBook("Bad");
    ws.getCell("A1").value = "x";
    ws.getCell("A1").fill = {
      type: "pattern",
      pattern: "solid",
      fgColor: { theme: 99 },
    };
    expect(() => workbookFromExcelJs(ws.workbook)).toThrow(
      /sheet "Bad" cell A1: theme index 99/,
    );
  });

  it("names the cell and value when a border style is unknown", () => {
    const { ws } = memoryBook("Bad");
    ws.getCell("B2").value = "x";
    const bogus: string = "wiggly";
    ws.getCell("B2").border = { top: { style: bogus as BorderStyle } };
    expect(() => workbookFromExcelJs(ws.workbook)).toThrow(
      /sheet "Bad" cell B2: unknown border style "wiggly"/,
    );
  });

  it("names the cell and format when a number format cannot be parsed", () => {
    const { ws } = memoryBook("Bad");
    ws.getCell("A1").value = 1234.5;
    ws.getCell("A1").numFmt = "[bogus";
    expect(() => workbookFromExcelJs(ws.workbook)).toThrow(
      /sheet "Bad" cell A1: number format "\[bogus"/,
    );
  });

  it("names the cell when an ARGB colour is malformed", () => {
    const { ws } = memoryBook("Bad");
    ws.getCell("C3").value = "x";
    ws.getCell("C3").font = { color: { argb: "red" } };
    expect(() => workbookFromExcelJs(ws.workbook)).toThrow(
      /sheet "Bad" cell C3: invalid ARGB colour "red"/,
    );
  });
});

describe("workbookFromExcelJs truncation", () => {
  it("caps columns at MAX_COLS and reports the sheet as column-capped", () => {
    const { ws } = memoryBook("Wide");
    ws.getCell(1, MAX_COLS).value = "last kept";
    ws.getCell(1, MAX_COLS + 1).value = "dropped";
    const { sheets, data } = workbookFromExcelJs(ws.workbook);
    expect(sheets[0].colCapped).toBe(true);
    expect(firstSheet(data).columnCount).toBe(MAX_COLS);
    expect(cellAt(data, 1, MAX_COLS)?.v).toBe("last kept");
    expect(cellAt(data, 1, MAX_COLS + 1)).toBeUndefined();
  });

  it("caps rows at MAX_ROWS and reports the sheet as row-capped", () => {
    const { ws } = memoryBook("Tall");
    ws.getCell(MAX_ROWS, 1).value = "last kept";
    ws.getCell(MAX_ROWS + 1, 1).value = "dropped";
    const { sheets, data } = workbookFromExcelJs(ws.workbook);
    expect(sheets[0].rowCapped).toBe(true);
    expect(firstSheet(data).rowCount).toBe(MAX_ROWS);
    expect(cellAt(data, MAX_ROWS, 1)?.v).toBe("last kept");
    expect(cellAt(data, MAX_ROWS + 1, 1)).toBeUndefined();
  });
});

describe("workbookFromSheetJs (CSV/TSV and .xls)", () => {
  it("styles the first row of CSV as a header and leaves data rows plain", () => {
    const data = workbookFromSheetJs(
      XLSX.read("name,qty\napples,3\n", { type: "string" }),
      true,
    ).data;
    expect(cellAt(data, 1, 1)?.v).toBe("name");
    const header = styleOf(data, cellAt(data, 1, 1));
    expect(header.bl).toBe(BooleanNumber.TRUE);
    expect(header.bg).toEqual({ rgb: "#e7ecf3" });
    // A filled cell gets black text, as in univerCellStyle; dark mode must not turn it light.
    expect(header.cl).toEqual({ rgb: "#000000" });
    expect(styleOf(data, cellAt(data, 2, 1)).bl).toBeUndefined();
  });

  it("does not style a first row as a header when the caller says it is not one", () => {
    const data = workbookFromSheetJs(
      XLSX.read("name,qty\napples,3\n", { type: "string" }),
      false,
    ).data;
    expect(styleOf(data, cellAt(data, 1, 1)).bl).toBeUndefined();
  });

  it("auto-fits columns to their longest value, clamped to 64-400px", () => {
    const csv = `a,${"x".repeat(20)},${"y".repeat(80)}\n1,2,3\n`;
    const { data } = workbookFromSheetJs(
      XLSX.read(csv, { type: "string" }),
      true,
    );
    const columns = firstSheet(data).columnData;
    expect(columns?.[0]?.w).toBe(64); // "a" is shorter than the default width
    expect(columns?.[1]?.w).toBe(150); // 20 * 7 + 10
    expect(columns?.[2]?.w).toBe(400); // clamped to the maximum
  });

  it("right-aligns numeric cells and leaves text left", () => {
    const { data } = workbookFromSheetJs(
      XLSX.read("1,text\n", { type: "string" }),
      false,
    );
    expect(cellAt(data, 1, 1)?.t).toBe(CellValueType.STRING);
    expect(styleOf(data, cellAt(data, 1, 1)).ht).toBe(HorizontalAlign.RIGHT);
    expect(styleOf(data, cellAt(data, 1, 2)).ht).toBeUndefined();
  });

  it("keeps the offset of a sheet whose data starts at C3", () => {
    const ws = XLSX.utils.aoa_to_sheet([]);
    XLSX.utils.sheet_add_aoa(ws, [["x"]], { origin: "C3" });
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, ws, "Offset");
    const { data } = workbookFromSheetJs(wb, false);
    expect(cellAt(data, 3, 3)?.v).toBe("x");
    expect(cellAt(data, 1, 1)).toBeUndefined();
  });

  it("marks a sheet with no cells as empty", () => {
    const wb = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(wb, XLSX.utils.aoa_to_sheet([]), "Empty");
    expect(workbookFromSheetJs(wb, false).sheets[0].isEmpty).toBe(true);
  });

  it("caps rows at MAX_ROWS, reporting the sheet as row-capped", () => {
    const lines: string[] = [];
    for (let i = 0; i <= MAX_ROWS; i++) lines.push(`r${i}`);
    const wb = XLSX.read(lines.join("\n"), {
      type: "string",
      sheetRows: SHEETJS_READ_ROWS,
    });
    const { sheets, data } = workbookFromSheetJs(wb, false);
    expect(sheets[0].rowCapped).toBe(true);
    expect(firstSheet(data).rowCount).toBe(MAX_ROWS);
    expect(cellAt(data, MAX_ROWS, 1)?.v).toBe(`r${MAX_ROWS - 1}`);
    expect(cellAt(data, MAX_ROWS + 1, 1)).toBeUndefined();
  });

  it("does not flag a sheet of exactly MAX_ROWS rows as capped", () => {
    const lines: string[] = [];
    for (let i = 0; i < MAX_ROWS; i++) lines.push(`r${i}`);
    const wb = XLSX.read(lines.join("\n"), {
      type: "string",
      sheetRows: SHEETJS_READ_ROWS,
    });
    expect(workbookFromSheetJs(wb, false).sheets[0].rowCapped).toBe(false);
  });

  it("caps columns at MAX_COLS, reporting the sheet as column-capped", () => {
    const row = Array.from({ length: MAX_COLS + 1 }, (_, i) => `c${i}`).join(
      ",",
    );
    const { sheets, data } = workbookFromSheetJs(
      XLSX.read(row, { type: "string" }),
      false,
    );
    expect(sheets[0].colCapped).toBe(true);
    expect(firstSheet(data).columnCount).toBe(MAX_COLS);
    expect(cellAt(data, 1, MAX_COLS)?.v).toBe(`c${MAX_COLS - 1}`);
    expect(cellAt(data, 1, MAX_COLS + 1)).toBeUndefined();
  });
});

describe("assertWithinFileLimit", () => {
  it("accepts a file of exactly the limit", () => {
    expect(() => assertWithinFileLimit(MAX_FILE_BYTES)).not.toThrow();
  });

  it("refuses a file one byte over the limit, with the size in the message", () => {
    expect(() => assertWithinFileLimit(MAX_FILE_BYTES + 1)).toThrow(
      /refuses files above 15 MiB/,
    );
  });
});

describe("workbookFromExcelJs images", () => {
  it("extracts floating images", async () => {
    const wb = new Workbook();
    const ws = wb.addWorksheet("Sheet1");
    const buf = Buffer.from(ONE_PX_PNG_BASE64, "base64");
    const id = wb.addImage({ buffer: buf as any, extension: "png" });
    ws.addImage(id, { tl: { col: 0, row: 0 }, ext: { width: 10, height: 10 } });
    const conv = workbookFromExcelJs(wb);
    expect(conv.images.length).toBe(1);
    expect(conv.images[0].sheetName).toBe("Sheet1");
    expect(conv.images[0].dataUrl.startsWith("data:image/png;base64,")).toBe(
      true,
    );
    expect(conv.images[0].extWidth).toBe(10);
    expect(conv.images[0].extHeight).toBe(10);
    expect(conv.images[0].toCol).toBeNull();
    expect(conv.images[0].toRow).toBeNull();
  });

  it("returns no images for a workbook without drawings", async () => {
    const wb = new Workbook();
    wb.addWorksheet("Sheet1");
    const conv = workbookFromExcelJs(wb);
    expect(conv.images).toEqual([]);
  });

  it("SheetJS path returns no images", () => {
    const wb = XLSX.utils.book_new();
    const sheet = XLSX.utils.aoa_to_sheet([
      ["a", "b"],
      [1, 2],
    ]);
    XLSX.utils.book_append_sheet(wb, sheet, "Sheet1");
    const conv = workbookFromSheetJs(wb, true);
    expect(conv.images).toEqual([]);
  });
});

/** An xlsx with one PNG drawn at A1, written by ExcelJS (`xdr:`-prefixed drawing). */
async function workbookWithImageBytes(): Promise<ArrayBuffer> {
  const wb = new Workbook();
  const ws = wb.addWorksheet("Sheet1");
  const id = wb.addImage({ base64: ONE_PX_PNG_BASE64, extension: "png" });
  ws.addImage(id, { tl: { col: 0, row: 0 }, ext: { width: 10, height: 10 } });
  return new Uint8Array(await wb.xlsx.writeBuffer()).buffer as ArrayBuffer;
}

/** Drops the `xdr:` prefix from every drawing part, as a default-namespace writer would. */
async function stripDrawingPrefix(buf: ArrayBuffer): Promise<ArrayBuffer> {
  const zip = await JSZip.loadAsync(buf);
  for (const part of zip.file(/^xl\/drawings\/[^/]+\.xml$/)) {
    const xml = await part.async("string");
    zip.file(
      part.name,
      xml.replace(/xmlns:xdr=/g, "xmlns=").replace(/<(\/?)xdr:/g, "<$1"),
    );
  }
  return zip.generateAsync({ type: "arraybuffer" });
}

describe("prefixDrawingNamespaces", () => {
  it("returns the identical buffer when no drawing needs rewriting", async () => {
    const buf = await workbookWithImageBytes();
    expect(await prefixDrawingNamespaces(buf)).toBe(buf);
  });

  it("loads a default-namespace drawing that plain ExcelJS rejects", async () => {
    const stripped = await stripDrawingPrefix(await workbookWithImageBytes());
    await expect(new Workbook().xlsx.load(stripped)).rejects.toThrow();

    const rewritten = await prefixDrawingNamespaces(stripped);
    expect(rewritten).not.toBe(stripped);

    const conv = await workbookFromFileBytes("x.xlsx", stripped);
    expect(conv.images).toHaveLength(1);
    expect(conv.images[0].sheetName).toBe("Sheet1");
    expect(conv.images[0].extWidth).toBe(10);
    expect(conv.images[0].extHeight).toBe(10);
  });

  it("extractDrawingExtentsBySheet reads the real EMU size and skips creationId ext nodes", async () => {
    const JSZip = (await import("jszip")).default;
    const zip = new JSZip();
    zip.file(
      "xl/drawings/drawing1.xml",
      `<?xml version="1.0"?>
      <xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
        <xdr:twoCellAnchor><xdr:pic>
          <xdr:nvPicPr><xdr:cNvPr id="1"><a:extLst><a:ext uri="creationId"/></a:extLst></xdr:cNvPr></xdr:nvPicPr>
          <xdr:spPr><a:xfrm><a:ext cx="952500" cy="476250"/></a:xfrm></xdr:spPr>
        </xdr:pic></xdr:twoCellAnchor>
      </xdr:wsDr>`,
    );
    const buf = await zip.generateAsync({ type: "arraybuffer" });
    const { extractDrawingExtentsBySheet } = await import("./univerData");
    const values = [...(await extractDrawingExtentsBySheet(buf)).values()].flat();
    expect(values).toEqual([
      { width: 100, height: 50 },
    ]);
  });

  it("pairs extents with multiple pictures in drawing order", async () => {
    const JSZip = (await import("jszip")).default;
    const zip = new JSZip();
    zip.file(
      "xl/drawings/drawing1.xml",
      `<?xml version="1.0"?>
      <xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
        <xdr:twoCellAnchor><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="1"/></xdr:nvPicPr>
          <xdr:spPr><a:xfrm><a:ext cx="190500" cy="95250"/></a:xfrm></xdr:spPr></xdr:pic></xdr:twoCellAnchor>
        <xdr:twoCellAnchor><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2"/></xdr:nvPicPr>
          <xdr:spPr><a:xfrm><a:ext cx="476250" cy="238125"/></a:xfrm></xdr:spPr></xdr:pic></xdr:twoCellAnchor>
      </xdr:wsDr>`,
    );
    const buf = await zip.generateAsync({ type: "arraybuffer" });
    const { extractDrawingExtentsBySheet } = await import("./univerData");
    const values = [...(await extractDrawingExtentsBySheet(buf)).values()].flat();
    expect(values).toEqual([
      { width: 20, height: 10 },
      { width: 50, height: 25 },
    ]);
  });

  it("maps extents to sheets through relationships, not drawing archive order", async () => {
    const JSZip = (await import("jszip")).default;
    const zip = new JSZip();
    zip.file(
      "xl/workbook.xml",
      `<?xml version="1.0"?>
      <workbook xmlns="${MAIN}" xmlns:r="${RELS}"><sheets>
        <sheet name="Alpha" sheetId="1" r:id="rId1"/>
        <sheet name="Beta" sheetId="2" r:id="rId2"/>
      </sheets></workbook>`,
    );
    zip.file(
      "xl/_rels/workbook.xml.rels",
      `<?xml version="1.0"?>
      <Relationships xmlns="${PKG_RELS}">
        <Relationship Id="rId1" Type="${RELS}/worksheet" Target="worksheets/sheet1.xml"/>
        <Relationship Id="rId2" Type="${RELS}/worksheet" Target="worksheets/sheet2.xml"/>
      </Relationships>`,
    );
    zip.file(
      "xl/worksheets/sheet1.xml",
      `<worksheet xmlns="${MAIN}"><sheetData/></worksheet>`,
    );
    zip.file(
      "xl/worksheets/_rels/sheet1.xml.rels",
      `<?xml version="1.0"?>
      <Relationships xmlns="${PKG_RELS}">
        <Relationship Id="rId1" Type="${RELS}/drawing" Target="../drawings/drawing2.xml"/>
      </Relationships>`,
    );
    zip.file(
      "xl/worksheets/sheet2.xml",
      `<worksheet xmlns="${MAIN}"><sheetData/></worksheet>`,
    );
    zip.file(
      "xl/worksheets/_rels/sheet2.xml.rels",
      `<?xml version="1.0"?>
      <Relationships xmlns="${PKG_RELS}">
        <Relationship Id="rId1" Type="${RELS}/drawing" Target="../drawings/drawing1.xml"/>
      </Relationships>`,
    );
    // Archive order is the reverse of the sheet order.
    zip.file(
      "xl/drawings/drawing1.xml",
      `<?xml version="1.0"?>
      <xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
        <xdr:twoCellAnchor><xdr:pic><xdr:spPr><a:xfrm><a:ext cx="190500" cy="95250"/></a:xfrm></xdr:spPr></xdr:pic></xdr:twoCellAnchor>
      </xdr:wsDr>`,
    );
    zip.file(
      "xl/drawings/drawing2.xml",
      `<?xml version="1.0"?>
      <xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
        <xdr:twoCellAnchor><xdr:pic><xdr:spPr><a:xfrm><a:ext cx="952500" cy="476250"/></a:xfrm></xdr:spPr></xdr:pic></xdr:twoCellAnchor>
      </xdr:wsDr>`,
    );
    const buf = await zip.generateAsync({ type: "arraybuffer" });
    const { extractDrawingExtentsBySheet } = await import("./univerData");
    const bySheet = await extractDrawingExtentsBySheet(buf);
    expect(bySheet.get("Alpha")).toEqual([{ width: 100, height: 50 }]);
    expect(bySheet.get("Beta")).toEqual([{ width: 20, height: 10 }]);
  });
});
