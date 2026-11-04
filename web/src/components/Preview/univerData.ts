import * as XLSX from "xlsx";
import type { Cell, Workbook, Worksheet } from "exceljs";
import {
  BooleanNumber,
  BorderStyleTypes,
  CellValueType,
  LocaleType,
  WorksheetHiddenState,
  type ICellData,
  type IColumnData,
  type IRange,
  type IRowData,
  type IStyleData,
  type IWorkbookData,
  type IWorksheetData,
} from "@univerjs/core";
import {
  colWidthPx,
  errorMessage,
  formatCell,
  isNumericValue,
  parseThemePalette,
  rowHeightPx,
  univerCellStyle,
  DEFAULT_THEME_PALETTE,
} from "./xlsxStyle";
import { isDelimitedTextPath } from "../../lib/previewKind";

/**
 * Converts parsed spreadsheets into Univer `IWorkbookData`. No Univer runtime
 * is loaded here: only the data shapes and enum values, so this module is
 * unit-testable without a canvas.
 */

/**
 * Largest file the viewer will parse. ExcelJS loads the whole workbook into
 * memory and SheetJS reads the whole text, so a bigger file is refused before
 * any parsing. 15 MiB.
 */
export const MAX_FILE_BYTES = 15 * 1024 * 1024;
/** Rows converted per sheet. A sheet with more shows its first rows and a notice. */
export const MAX_ROWS = 50000;
/** Columns converted per sheet. A sheet with more shows its first columns and a notice. */
export const MAX_COLS = 200;
/**
 * Rows SheetJS is asked to parse: one past the cap, so a longer sheet can be
 * detected (its parsed range then has more than MAX_ROWS rows).
 */
export const SHEETJS_READ_ROWS = MAX_ROWS + 1;

/** Auto-fitted CSV/XLS columns: at least the Excel default width, at most 400px. */
const AUTO_WIDTH_MIN_PX = colWidthPx(undefined);
const AUTO_WIDTH_MAX_PX = 400;
/** Approximate advance per character and horizontal padding for auto-fit. */
const AUTO_CHAR_PX = 7;
const AUTO_PAD_PX = 10;
const DEFAULT_ROW_HEIGHT_PX = rowHeightPx(15);
/** Univer needs at least one row and column to draw a grid; an empty sheet gets exactly one. */
const EMPTY_GRID = 1;
const HEADER_FILL = "#e7ecf3";
const HEADER_RULE = "#9aa5b1";

export interface ConvertedSheet {
  id: string;
  name: string;
  /** No cells at all. The viewer shows "(empty sheet)" instead of a grid. */
  isEmpty: boolean;
  /** The sheet has more rows than MAX_ROWS; only the first MAX_ROWS are in `data`. */
  rowCapped: boolean;
  /** The sheet has more columns than MAX_COLS; only the first MAX_COLS are in `data`. */
  colCapped: boolean;
}

export interface ConvertedWorkbook {
  sheets: ConvertedSheet[];
  data: IWorkbookData;
}

/** Throws when a file is above MAX_FILE_BYTES. The message is shown in the viewer. */
export function assertWithinFileLimit(bytes: number): void {
  if (bytes > MAX_FILE_BYTES) {
    const mib = (bytes / (1024 * 1024)).toFixed(1);
    throw new Error(`the spreadsheet is ${mib} MiB; the viewer refuses files above ${MAX_FILE_BYTES / (1024 * 1024)} MiB`);
  }
}

/** Univer's cell and row/column maps are sparse and keyed by 0-based index. */
interface SheetBody {
  rowCount: number;
  columnCount: number;
  cellData: Record<number, Record<number, ICellData>>;
  rowData: Record<number, Partial<IRowData>>;
  columnData: Record<number, Partial<IColumnData>>;
  mergeData: IRange[];
}

interface BuiltSheet {
  name: string;
  isEmpty: boolean;
  rowCapped: boolean;
  colCapped: boolean;
  body: SheetBody;
}

/** Shares one `styles` table across the workbook: identical styles get one id. */
class StyleRegistry {
  readonly styles: Record<string, IStyleData> = {};
  private readonly ids = new Map<string, string>();

  idFor(style: IStyleData): string {
    const key = JSON.stringify(style);
    const existing = this.ids.get(key);
    if (existing !== undefined) return existing;
    const id = `s${this.ids.size + 1}`;
    this.ids.set(key, id);
    this.styles[id] = style;
    return id;
  }
}

/**
 * Runs one cell's conversion and, on failure, rethrows with the sheet and cell
 * address so the viewer can name the exact cell and bad value.
 */
function atCell<T>(sheetName: string, address: string, build: () => T): T {
  try {
    return build();
  } catch (e) {
    throw new Error(`sheet "${sheetName}" cell ${address}: ${errorMessage(e)}`);
  }
}

function assemble(sheets: BuiltSheet[], styles: StyleRegistry): ConvertedWorkbook {
  const sheetData: Record<string, Partial<IWorksheetData>> = {};
  const summaries: ConvertedSheet[] = sheets.map((sheet, i) => {
    const id = `sheet-${i + 1}`;
    sheetData[id] = worksheetData(id, sheet);
    return { id, name: sheet.name, isEmpty: sheet.isEmpty, rowCapped: sheet.rowCapped, colCapped: sheet.colCapped };
  });
  return {
    sheets: summaries,
    data: {
      id: "ocode-spreadsheet",
      name: "Spreadsheet",
      appVersion: "ocode",
      locale: LocaleType.EN_US,
      styles: styles.styles,
      sheetOrder: summaries.map((s) => s.id),
      sheets: sheetData,
    },
  };
}

function worksheetData(id: string, sheet: BuiltSheet): Partial<IWorksheetData> {
  const { body } = sheet;
  return {
    id,
    name: sheet.name,
    tabColor: "",
    hidden: WorksheetHiddenState.VISIBLE,
    freeze: { xSplit: 0, ySplit: 0, startRow: 0, startColumn: 0 },
    rowCount: body.rowCount === 0 ? EMPTY_GRID : body.rowCount,
    columnCount: body.columnCount === 0 ? EMPTY_GRID : body.columnCount,
    zoomRatio: 1,
    scrollTop: 0,
    scrollLeft: 0,
    defaultColumnWidth: AUTO_WIDTH_MIN_PX,
    defaultRowHeight: DEFAULT_ROW_HEIGHT_PX,
    mergeData: body.mergeData,
    cellData: body.cellData,
    rowData: body.rowData,
    columnData: body.columnData,
    rowHeader: { width: 46 },
    columnHeader: { height: 20 },
    showGridlines: BooleanNumber.TRUE,
    rightToLeft: BooleanNumber.FALSE,
  };
}

/** Inclusive 1-based bounds of a merged block, as ExcelJS records them. */
interface MergeBounds {
  top: number;
  left: number;
  bottom: number;
  right: number;
}

function isMergeBounds(value: unknown): value is MergeBounds {
  if (typeof value !== "object" || value === null) return false;
  return ["top", "left", "bottom", "right"].every((k) => typeof Reflect.get(value, k) === "number");
}

/**
 * Merged blocks from ExcelJS's merge table. Read once per sheet from
 * `_merges` (ExcelJS keeps no public list of ranges without rebuilding the
 * whole model). A malformed entry throws.
 */
function mergeBoundsOf(ws: Worksheet): MergeBounds[] {
  const table: unknown = Reflect.get(ws, "_merges");
  if (typeof table !== "object" || table === null) {
    throw new Error(`sheet "${ws.name}" has no readable merge table (ExcelJS internals changed)`);
  }
  return Object.values(table).map((entry: unknown) => {
    if (!isMergeBounds(entry)) throw new Error(`sheet "${ws.name}" has a malformed merge entry`);
    return entry;
  });
}

/** ExcelJS theme part (theme1.xml), or undefined when the workbook has none. */
function themeXmlOf(wb: Workbook): string | undefined {
  const themes: unknown = Reflect.get(wb, "_themes");
  if (themes === undefined) return undefined;
  if (typeof themes !== "object" || themes === null) throw new Error("ExcelJS theme table has an unexpected shape");
  const theme1: unknown = Reflect.get(themes, "theme1");
  if (typeof theme1 !== "string") throw new Error("ExcelJS theme table has no theme1 XML");
  return theme1;
}

interface ExcelJsContext {
  palette: readonly string[];
  date1904: boolean;
  styles: StyleRegistry;
}

function excelJsSheet(ws: Worksheet, ctx: ExcelJsContext): BuiltSheet {
  const rowCount = Math.min(ws.rowCount, MAX_ROWS);
  const columnCount = Math.min(ws.columnCount, MAX_COLS);

  // ExcelJS copies a master's value into every cell it covers, so covered cells
  // are skipped or the merged text would be drawn more than once. Merges that
  // start past the caps are dropped; a merge that crosses them is clipped.
  const mergeData: IRange[] = [];
  const covered = new Set<string>();
  for (const m of mergeBoundsOf(ws)) {
    if (m.top > rowCount || m.left > columnCount) continue;
    const bottom = Math.min(m.bottom, rowCount);
    const right = Math.min(m.right, columnCount);
    mergeData.push({ startRow: m.top - 1, startColumn: m.left - 1, endRow: bottom - 1, endColumn: right - 1 });
    for (let r = m.top; r <= bottom; r++) {
      for (let c = m.left; c <= right; c++) {
        if (r !== m.top || c !== m.left) covered.add(`${r},${c}`);
      }
    }
  }

  const columnData: Record<number, Partial<IColumnData>> = {};
  for (let c = 1; c <= columnCount; c++) {
    const col = ws.getColumn(c);
    const data: Partial<IColumnData> = { w: colWidthPx(col.width) };
    if (col.hidden) data.hd = BooleanNumber.TRUE;
    columnData[c - 1] = data;
  }

  const rowData: Record<number, Partial<IRowData>> = {};
  const cellData: Record<number, Record<number, ICellData>> = {};
  for (let r = 1; r <= rowCount; r++) {
    const row = ws.getRow(r);
    const info: Partial<IRowData> = {};
    if (row.height !== undefined) info.h = rowHeightPx(row.height);
    if (row.hidden) info.hd = BooleanNumber.TRUE;
    if (Object.keys(info).length > 0) rowData[r - 1] = info;

    row.eachCell({ includeEmpty: false }, (cell: Cell, c: number) => {
      if (c > columnCount || covered.has(`${r},${c}`)) return;
      const data = atCell(ws.name, cell.address, () => excelJsCell(cell, ctx));
      if (cellData[r - 1] === undefined) cellData[r - 1] = {};
      cellData[r - 1][c - 1] = data;
    });
  }

  return {
    name: ws.name,
    isEmpty: ws.rowCount === 0,
    rowCapped: ws.rowCount > MAX_ROWS,
    colCapped: ws.columnCount > MAX_COLS,
    body: { rowCount, columnCount, cellData, rowData, columnData, mergeData },
  };
}

function excelJsCell(cell: Cell, ctx: ExcelJsContext): ICellData {
  const text = formatCell(cell.value, cell.numFmt, ctx.date1904);
  const style = univerCellStyle(
    { font: cell.font, fill: cell.fill, border: cell.border, alignment: cell.alignment },
    ctx.palette,
    isNumericValue(cell.value),
  );
  const data: ICellData = { s: ctx.styles.idFor(style) };
  if (text !== "") {
    data.v = text;
    data.t = CellValueType.STRING;
  }
  return data;
}

/**
 * Converts an ExcelJS workbook (already loaded) into Univer data. Dates are
 * formatted with the workbook's own date system, so a 1904 workbook renders
 * the dates it stores.
 */
export function workbookFromExcelJs(wb: Workbook): ConvertedWorkbook {
  const styles = new StyleRegistry();
  const palette = parseThemePalette(themeXmlOf(wb));
  const date1904 = wb.properties.date1904 === true;
  const ctx: ExcelJsContext = { palette, date1904, styles };
  const sheets = wb.worksheets.map((ws) => excelJsSheet(ws, ctx));
  return assemble(sheets, styles);
}

/** Header-row style for CSV/TSV: bold, lightly filled, with a rule under it. */
function csvHeaderStyle(): IStyleData {
  return {
    ...univerCellStyle({}, DEFAULT_THEME_PALETTE, false),
    bl: BooleanNumber.TRUE,
    bg: { rgb: HEADER_FILL },
    cl: { rgb: "#000000" },
    bd: { b: { s: BorderStyleTypes.THIN, cl: { rgb: HEADER_RULE } } },
  };
}

/**
 * Converts a SheetJS workbook (CSV/TSV or legacy .xls) into Univer data. Text
 * only: SheetJS gives display text per cell. Columns are auto-fitted to their
 * longest value (clamped), so nothing is clipped by default. For CSV/TSV the
 * first row is styled as a header.
 *
 * Callers must read with `sheetRows: SHEETJS_READ_ROWS` so an over-long sheet
 * shows up as a parsed range longer than MAX_ROWS.
 */
export function workbookFromSheetJs(wb: XLSX.WorkBook, firstRowIsHeader: boolean): ConvertedWorkbook {
  const styles = new StyleRegistry();
  const headerId = firstRowIsHeader ? styles.idFor(csvHeaderStyle()) : undefined;
  const sheets = wb.SheetNames.map((name) => sheetJsSheet(name, wb.Sheets[name], styles, headerId));
  return assemble(sheets, styles);
}

function isCellObject(value: unknown): value is XLSX.CellObject {
  return typeof value === "object" && value !== null && "t" in value;
}

function sheetJsSheet(name: string, ws: XLSX.WorkSheet, styles: StyleRegistry, headerId: string | undefined): BuiltSheet {
  const ref = ws["!ref"];
  if (ref === undefined) {
    return {
      name,
      isEmpty: true,
      rowCapped: false,
      colCapped: false,
      body: { rowCount: 0, columnCount: 0, cellData: {}, rowData: {}, columnData: {}, mergeData: [] },
    };
  }
  // Counted from A1, not from the first used cell: a sheet whose data starts at
  // C3 keeps its leading empty rows and columns, so the offset is preserved.
  const range = XLSX.utils.decode_range(ref);
  const totalRows = range.e.r + 1;
  const totalCols = range.e.c + 1;
  const rowCount = Math.min(totalRows, MAX_ROWS);
  const columnCount = Math.min(totalCols, MAX_COLS);

  const cellData: Record<number, Record<number, ICellData>> = {};
  const widths: number[] = Array.from({ length: columnCount }, () => 0);
  for (const key of Object.keys(ws)) {
    if (key.startsWith("!")) continue;
    const address = XLSX.utils.decode_cell(key);
    if (address.r >= rowCount || address.c >= columnCount) continue;
    const cell: unknown = ws[key];
    if (!isCellObject(cell)) continue;
    const text = XLSX.utils.format_cell(cell);
    const isHeader = headerId !== undefined && address.r === 0;
    const style = isHeader ? headerId : styles.idFor(univerCellStyle({}, DEFAULT_THEME_PALETTE, cell.t === "n"));
    const data: ICellData = { s: style };
    if (text !== "") {
      data.v = text;
      data.t = CellValueType.STRING;
      widths[address.c] = Math.max(widths[address.c], text.length);
    }
    if (cellData[address.r] === undefined) cellData[address.r] = {};
    cellData[address.r][address.c] = data;
  }

  const columnData: Record<number, Partial<IColumnData>> = {};
  widths.forEach((chars, c) => {
    columnData[c] = { w: Math.min(AUTO_WIDTH_MAX_PX, Math.max(AUTO_WIDTH_MIN_PX, chars * AUTO_CHAR_PX + AUTO_PAD_PX)) };
  });

  return {
    name,
    isEmpty: false,
    rowCapped: totalRows > MAX_ROWS,
    colCapped: totalCols > MAX_COLS,
    body: { rowCount, columnCount, cellData, rowData: {}, columnData, mergeData: [] },
  };
}

/**
 * Converts a CSV/TSV document already decoded to a string (the split-view
 * editor's live text). SheetJS handles both delimiters.
 */
export function workbookFromDelimitedText(text: string): ConvertedWorkbook {
  assertWithinFileLimit(new TextEncoder().encode(text).byteLength);
  return workbookFromSheetJs(XLSX.read(text, { type: "string", sheetRows: SHEETJS_READ_ROWS }), true);
}

const OOXML_WORKBOOK = /\.(xlsx|xlsm)$/i;

/**
 * Converts a spreadsheet file's bytes by extension: `.xlsx`/`.xlsm` through
 * ExcelJS (formatting kept), CSV/TSV decoded as UTF-8 first, anything else
 * (legacy `.xls`) through SheetJS.
 */
export async function workbookFromFileBytes(path: string, buf: ArrayBuffer): Promise<ConvertedWorkbook> {
  // Refused before any parser runs: ExcelJS and SheetJS hold the whole file.
  assertWithinFileLimit(buf.byteLength);
  // Delimited text has no binary magic, so it must be decoded as UTF-8
  // before SheetJS sees it: `type: "array"` reads raw bytes as latin-1
  // and mojibakes every non-ASCII cell. See isDelimitedTextPath.
  if (isDelimitedTextPath(path)) {
    return workbookFromSheetJs(XLSX.read(new TextDecoder().decode(buf), { type: "string", sheetRows: SHEETJS_READ_ROWS }), true);
  }
  if (OOXML_WORKBOOK.test(path)) {
    // Dynamic so ExcelJS stays out of the main bundle.
    const { Workbook: ExcelWorkbook } = await import("exceljs");
    const wb = new ExcelWorkbook();
    await wb.xlsx.load(buf);
    return workbookFromExcelJs(wb);
  }
  return workbookFromSheetJs(XLSX.read(buf, { type: "array", sheetRows: SHEETJS_READ_ROWS }), false);
}
