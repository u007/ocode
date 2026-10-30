/* global Excel, Office */
// The real ExcelPort, written against Office.js (ExcelApi 1.8+). This is the ONLY file that needs a real
// Excel to verify; everything it serves is tested against FakePort. Every member marked UNVERIFIED relied on
// documentation, not on running in Excel; excel-addin/RESUME.md lists the checks to do on a desktop.
import { parseAddress, shiftFormula } from "./a1";
import type {
  CellValue, ChartInfo, ChartKind, ChartSpec, ExcelPort, FormatSpec, NameInfo, PivotInfo, PivotSpec, RangeData, SheetInfo, TableInfo, ValueType,
} from "./port";

/** "Sheet 1!A1:B2" or "'Sheet 1'!A1" -> {sheet, address}. */
export function splitQualified(a: string): { sheet: string; address: string } {
  const i = a.lastIndexOf("!");
  if (i < 0) return { sheet: "", address: a };
  let sheet = a.slice(0, i);
  if (sheet.startsWith("'") && sheet.endsWith("'")) sheet = sheet.slice(1, -1).replace(/''/g, "'");
  return { sheet, address: a.slice(i + 1) };
}

/** A box in points, as Office.js reports Chart and Range positions. */
export interface Box { left: number; top: number; width: number; height: number }

/** Boxes closer than this (points) along an edge are treated as touching, not overlapping. */
const EDGE_TOLERANCE_PT = 0.5;

/** Pure: true when two boxes overlap by more than EDGE_TOLERANCE_PT on both axes. */
export function boxesOverlap(a: Box, b: Box): boolean {
  return a.left < b.left + b.width - EDGE_TOLERANCE_PT && b.left < a.left + a.width - EDGE_TOLERANCE_PT
    && a.top < b.top + b.height - EDGE_TOLERANCE_PT && b.top < a.top + a.height - EDGE_TOLERANCE_PT;
}

/**
 * Edges a border set always covers. The inside lines exist only with more than one row (InsideHorizontal) or
 * column (InsideVertical); setting them on a single cell, row or column rejects the whole batch in Excel.
 */
export function bordersFor(rowCount: number, columnCount: number): string[] {
  const edges = ["EdgeTop", "EdgeBottom", "EdgeLeft", "EdgeRight"];
  if (rowCount > 1) edges.push("InsideHorizontal");
  if (columnCount > 1) edges.push("InsideVertical");
  return edges;
}

const CHART_TYPES: Record<ChartKind, string> = {
  pie: "Pie", column_clustered: "ColumnClustered", column_stacked: "ColumnStacked", bar_clustered: "BarClustered",
  line: "Line", area: "Area", scatter: "XYScatter",
};

export class OfficePort implements ExcelPort {
  async listSheets(): Promise<SheetInfo[]> {
    return Excel.run(async (ctx) => {
      const sheets = ctx.workbook.worksheets;
      sheets.load("items/name,items/visibility");
      await ctx.sync();
      const used = sheets.items.map((s) => {
        const u = s.getUsedRangeOrNullObject(true);
        u.load("address");
        return u;
      });
      await ctx.sync();
      return sheets.items.map((s, i) => ({
        name: s.name,
        visibility: s.visibility as SheetInfo["visibility"],
        usedAddress: used[i].isNullObject ? null : splitQualified(used[i].address).address,
      }));
    });
  }

  async activeSheet(): Promise<string> {
    return Excel.run(async (ctx) => {
      const s = ctx.workbook.worksheets.getActiveWorksheet();
      s.load("name");
      await ctx.sync();
      return s.name;
    });
  }

  async selection(): Promise<{ sheet: string; address: string }> {
    return Excel.run(async (ctx) => {
      const r = ctx.workbook.getSelectedRange();
      r.load("address");
      await ctx.sync();
      return splitQualified(r.address);
    });
  }

  async read(sheet: string, address: string): Promise<RangeData> {
    return Excel.run(async (ctx) => {
      const r = ctx.workbook.worksheets.getItem(sheet).getRange(address);
      r.load("address,values,formulas,numberFormat,valueTypes");
      await ctx.sync();
      return {
        address: splitQualified(r.address).address,
        values: r.values as CellValue[][],
        formulas: (r.formulas as unknown[][]).map((row) => row.map((f) => (f === null || f === undefined ? "" : String(f)))),
        numberFormats: r.numberFormat as string[][],
        valueTypes: r.valueTypes as ValueType[][],
      };
    });
  }

  async write(sheet: string, address: string, cells: CellValue[][], numberFormat?: string): Promise<void> {
    return Excel.run(async (ctx) => {
      const r = ctx.workbook.worksheets.getItem(sheet).getRange(address);
      // Set the number format FIRST so a "@" (text) format keeps "007" as text when the value lands.
      if (numberFormat) r.numberFormat = cells.map((row) => row.map(() => numberFormat));
      r.formulas = cells.map((row) => row.map((v) => (v === null ? "" : v)));
      await ctx.sync();
    });
  }

  /** UNVERIFIED: undo sets formats before values so a "@" format keeps a restored "007" as text. */
  async setNumberFormats(sheet: string, address: string, formats: string[][]): Promise<void> {
    return Excel.run(async (ctx) => {
      ctx.workbook.worksheets.getItem(sheet).getRange(address).numberFormat = formats;
      await ctx.sync();
    });
  }

  /**
   * Fill by computing each cell's formula ourselves (shiftFormula is unit tested) instead of relying on how
   * Excel treats a single formula assigned to a multi-cell range. UNVERIFIED for functions that spill.
   */
  async fillFormula(sheet: string, address: string, formula: string): Promise<void> {
    const rect = parseAddress(address)!;
    const grid: string[][] = [];
    for (let r = 0; r <= rect.r2 - rect.r1; r++) {
      const row: string[] = [];
      for (let c = 0; c <= rect.c2 - rect.c1; c++) row.push(shiftFormula(formula, r, c));
      grid.push(row);
    }
    return Excel.run(async (ctx) => {
      ctx.workbook.worksheets.getItem(sheet).getRange(address).formulas = grid;
      await ctx.sync();
    });
  }

  async clear(sheet: string, address: string, what: "contents" | "formats" | "all"): Promise<void> {
    return Excel.run(async (ctx) => {
      const r = ctx.workbook.worksheets.getItem(sheet).getRange(address);
      r.clear(what === "contents" ? Excel.ClearApplyTo.contents : what === "formats" ? Excel.ClearApplyTo.formats : Excel.ClearApplyTo.all);
      await ctx.sync();
    });
  }

  async format(sheet: string, address: string, spec: FormatSpec): Promise<void> {
    return Excel.run(async (ctx) => {
      const ws = ctx.workbook.worksheets.getItem(sheet);
      const r = ws.getRange(address);
      if (spec.bold !== undefined) r.format.font.bold = spec.bold;
      if (spec.fill) r.format.fill.color = spec.fill;
      if (spec.fontColor) r.format.font.color = spec.fontColor;
      if (spec.align) r.format.horizontalAlignment = spec.align === "left" ? "Left" : spec.align === "center" ? "Center" : "Right";
      if (spec.wrapText !== undefined) r.format.wrapText = spec.wrapText;
      if (spec.columnWidth !== undefined) r.format.columnWidth = spec.columnWidth; // UNVERIFIED unit (points vs characters)
      // One load and one sync serve both the border and the number-format decisions.
      if (spec.borders || spec.numberFormat) {
        r.load("rowCount,columnCount");
        await ctx.sync();
      }
      if (spec.borders) {
        for (const edge of bordersFor(r.rowCount, r.columnCount)) {
          r.format.borders.getItem(edge as Excel.BorderIndex).style = "Continuous";
        }
      }
      if (spec.numberFormat) {
        r.numberFormat = Array.from({ length: r.rowCount }, () => Array.from({ length: r.columnCount }, () => spec.numberFormat!));
      }
      if (spec.autofitColumns) r.format.autofitColumns();
      if (spec.freezeRows) ws.freezePanes.freezeRows(spec.freezeRows);
      await ctx.sync();
    });
  }

  async listTables(): Promise<TableInfo[]> {
    return Excel.run(async (ctx) => {
      const ts = ctx.workbook.tables;
      ts.load("items/name");
      await ctx.sync();
      const rs = ts.items.map((t) => { const r = t.getRange(); r.load("address"); const w = t.worksheet; w.load("name"); return { r, w }; });
      await ctx.sync();
      return ts.items.map((t, i) => ({ name: t.name, sheet: rs[i].w.name, address: splitQualified(rs[i].r.address).address }));
    });
  }

  async addTable(sheet: string, address: string, name: string | undefined, style: string): Promise<TableInfo> {
    return Excel.run(async (ctx) => {
      const t = ctx.workbook.worksheets.getItem(sheet).tables.add(address, true);
      if (name) t.name = name;
      t.style = style;
      const r = t.getRange();
      r.load("address");
      t.load("name");
      await ctx.sync();
      return { name: t.name, sheet, address: splitQualified(r.address).address };
    });
  }

  async listNames(): Promise<NameInfo[]> {
    return Excel.run(async (ctx) => {
      const ns = ctx.workbook.names;
      ns.load("items/name,items/formula");
      await ctx.sync();
      return ns.items.map((n) => ({ name: n.name, refersTo: n.formula }));
    });
  }

  /** UNVERIFIED: a chart's data range and anchor cell are not exposed directly, so they are returned empty. */
  async listCharts(sheet: string): Promise<ChartInfo[]> {
    return Excel.run(async (ctx) => {
      const cs = ctx.workbook.worksheets.getItem(sheet).charts;
      cs.load("items/name,items/chartType");
      await ctx.sync();
      const titles = cs.items.map((c) => { c.title.load("text"); return c.title; });
      await ctx.sync();
      return cs.items.map((c, i) => ({ name: c.name, sheet, type: String(c.chartType), title: titles[i].text, dataAddress: "", anchor: "" }));
    });
  }

  /**
   * UNVERIFIED: overlap is geometric, in one Excel.run. Office.js reports a chart's position in points (not its
   * anchor cell), so the target range's box is compared with each chart's box. Returns the first chart that overlaps.
   */
  async findChartOverlap(sheet: string, address: string): Promise<string | null> {
    return Excel.run(async (ctx) => {
      const ws = ctx.workbook.worksheets.getItem(sheet);
      const charts = ws.charts;
      charts.load("items/name,items/left,items/top,items/width,items/height");
      const target = ws.getRange(address);
      target.load("left,top,width,height");
      await ctx.sync();
      const box: Box = { left: target.left, top: target.top, width: target.width, height: target.height };
      const hit = charts.items.find((c) => boxesOverlap({ left: c.left, top: c.top, width: c.width, height: c.height }, box));
      return hit ? hit.name : null;
    });
  }

  async addChart(spec: ChartSpec): Promise<ChartInfo> {
    return Excel.run(async (ctx) => {
      const ws = ctx.workbook.worksheets.getItem(spec.sheet);
      const chart = ws.charts.add(CHART_TYPES[spec.kind] as Excel.ChartType, ws.getRange(spec.dataAddress), Excel.ChartSeriesBy.columns);
      if (spec.title) { chart.title.text = spec.title; chart.title.visible = true; } else chart.title.visible = false;
      chart.legend.visible = spec.showLegend ?? true;
      if (spec.kind !== "pie") {
        if (spec.categoryAxisTitle) { chart.axes.categoryAxis.title.text = spec.categoryAxisTitle; chart.axes.categoryAxis.title.visible = true; }
        if (spec.valueAxisTitle) { chart.axes.valueAxis.title.text = spec.valueAxisTitle; chart.axes.valueAxis.title.visible = true; }
      }
      if (spec.showPercentLabels) { chart.dataLabels.showPercentage = true; chart.dataLabels.showValue = false; chart.dataLabels.showCategoryName = false; }
      const a = parseAddress(spec.anchor)!;
      const end = ws.getRangeByIndexes(a.r1 - 1 + 15, a.c1 - 1 + 7, 1, 1); // 8 columns x 16 rows, matches CHART_COLS/ROWS
      chart.setPosition(spec.anchor, end);
      chart.load("name");
      await ctx.sync();
      return { name: chart.name, sheet: spec.sheet, type: spec.kind, title: spec.title ?? "", dataAddress: spec.dataAddress, anchor: spec.anchor };
    });
  }

  async listPivots(): Promise<PivotInfo[]> {
    return Excel.run(async (ctx) => {
      const ps = ctx.workbook.pivotTables;
      ps.load("items/name");
      await ctx.sync();
      const info = ps.items.map((p) => { const r = p.layout.getRange(); r.load("address"); return r; });
      await ctx.sync();
      return ps.items.map((p, i) => { const q = splitQualified(info[i].address); return { name: p.name, sheet: q.sheet, address: q.address }; });
    });
  }

  async addPivot(spec: PivotSpec): Promise<PivotInfo> {
    return Excel.run(async (ctx) => {
      const src = ctx.workbook.worksheets.getItem(spec.sourceSheet).getRange(spec.sourceAddress);
      const dest = ctx.workbook.worksheets.getItem(spec.destSheet).getRange(spec.destAddress);
      const pt = ctx.workbook.pivotTables.add(spec.name, src, dest);
      for (const f of spec.rows) pt.rowHierarchies.add(pt.hierarchies.getItem(f));
      for (const f of spec.columns) pt.columnHierarchies.add(pt.hierarchies.getItem(f));
      for (const f of spec.filters) pt.filterHierarchies.add(pt.hierarchies.getItem(f));
      const fn = { sum: "Sum", count: "Count", average: "Average", min: "Min", max: "Max" } as const;
      for (const v of spec.values) {
        const dh = pt.dataHierarchies.add(pt.hierarchies.getItem(v.field));
        dh.summarizeBy = fn[v.summarizeBy] as Excel.AggregationFunction;
      }
      await ctx.sync();
      return { name: spec.name, sheet: spec.destSheet, address: spec.destAddress };
    });
  }

  async calculationMode(): Promise<string> {
    return Excel.run(async (ctx) => {
      ctx.workbook.application.load("calculationMode");
      await ctx.sync();
      return String(ctx.workbook.application.calculationMode);
    });
  }

  async calculate(): Promise<void> {
    return Excel.run(async (ctx) => {
      ctx.workbook.application.calculate(Excel.CalculationType.full);
      await ctx.sync();
    });
  }

  /** Document settings live inside the .xlsx. UNVERIFIED: size limits, and whether Excel on the web persists them. */
  async getSetting(key: string): Promise<string | null> {
    const v = Office.context.document.settings.get(key);
    return typeof v === "string" ? v : null;
  }

  async setSetting(key: string, value: string): Promise<void> {
    Office.context.document.settings.set(key, value);
    await new Promise<void>((res, rej) => Office.context.document.settings.saveAsync((r) => (r.status === Office.AsyncResultStatus.Succeeded ? res() : rej(new Error(r.error.message)))));
  }
}
