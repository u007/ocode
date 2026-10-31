// The only surface that touches Excel. Everything above it (tools, journal, validation) is pure
// logic tested against FakePort; only OfficePort needs a real Excel to verify (see RESUME.md).

export type CellValue = string | number | boolean | null;
export type ValueType = "Empty" | "String" | "Double" | "Integer" | "Boolean" | "Error" | "Unknown";

export interface RangeData {
  /** Address as Excel reports it, without the sheet name, e.g. "A1:F24" */
  address: string;
  values: CellValue[][];
  /** "=..." for formula cells, otherwise the constant rendered as text ("" for empty) */
  formulas: string[][];
  numberFormats: string[][];
  valueTypes: ValueType[][];
}

export interface SheetInfo {
  name: string;
  visibility: "Visible" | "Hidden" | "VeryHidden";
  /** Used range address (values only) or null for an empty sheet */
  usedAddress: string | null;
}

export interface TableInfo { name: string; sheet: string; address: string }
export interface NameInfo { name: string; refersTo: string }
export interface ChartInfo { name: string; sheet: string; type: string; title: string; dataAddress: string; anchor: string }
export interface PivotInfo { name: string; sheet: string; address: string }

export interface FormatSpec {
  bold?: boolean;
  fill?: string;
  fontColor?: string;
  numberFormat?: string;
  align?: "left" | "center" | "right";
  autofitColumns?: boolean;
  columnWidth?: number;
  wrapText?: boolean;
  borders?: boolean;
  freezeRows?: number;
}

export type ChartKind = "pie" | "column_clustered" | "column_stacked" | "bar_clustered" | "line" | "area" | "scatter";

export interface ChartSpec {
  sheet: string;
  dataAddress: string;
  kind: ChartKind;
  title?: string;
  categoryAxisTitle?: string;
  valueAxisTitle?: string;
  showPercentLabels?: boolean;
  showLegend?: boolean;
  anchor: string;
}

export interface PivotSpec {
  name: string;
  sourceSheet: string;
  sourceAddress: string;
  destSheet: string;
  destAddress: string;
  rows: string[];
  columns: string[];
  filters: string[];
  values: { field: string; summarizeBy: "sum" | "count" | "average" | "min" | "max" }[];
}

export interface ExcelPort {
  listSheets(): Promise<SheetInfo[]>;
  activeSheet(): Promise<string>;
  selection(): Promise<{ sheet: string; address: string }>;
  /** Activate `sheet` and select `address` so the user sees it. Changes no value, format or cell. */
  select(sheet: string, address: string): Promise<void>;
  read(sheet: string, address: string): Promise<RangeData>;
  /** Strings starting with "=" are formulas, everything else is a constant. */
  write(sheet: string, address: string, cells: CellValue[][], numberFormat?: string): Promise<void>;
  /** Set each cell's number format from a grid the same shape as the address (used by undo, before the values). */
  setNumberFormats(sheet: string, address: string, formats: string[][]): Promise<void>;
  /** Write one formula into `address` with relative references adjusting per cell (like Ctrl+Enter / fill). */
  fillFormula(sheet: string, address: string, formula: string): Promise<void>;
  clear(sheet: string, address: string, what: "contents" | "formats" | "all"): Promise<void>;
  format(sheet: string, address: string, spec: FormatSpec): Promise<void>;
  listTables(): Promise<TableInfo[]>;
  addTable(sheet: string, address: string, name: string | undefined, style: string): Promise<TableInfo>;
  listNames(): Promise<NameInfo[]>;
  listCharts(sheet: string): Promise<ChartInfo[]>;
  /**
   * Name of an existing chart on `sheet` whose bounding box overlaps the cell range `address`, or null.
   * Ranges that only touch along an edge do not overlap. Works from chart positions because Office.js does not
   * expose a chart's anchor cell.
   */
  findChartOverlap(sheet: string, address: string): Promise<string | null>;
  addChart(spec: ChartSpec): Promise<ChartInfo>;
  listPivots(): Promise<PivotInfo[]>;
  addPivot(spec: PivotSpec): Promise<PivotInfo>;
  calculationMode(): Promise<string>;
  calculate(): Promise<void>;
  getSetting(key: string): Promise<string | null>;
  setSetting(key: string, value: string): Promise<void>;
}
