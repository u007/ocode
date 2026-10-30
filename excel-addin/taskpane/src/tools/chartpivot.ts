import { Rect, cellAddress, colToLetters, formatRect, intersects, parseAddress, rows } from "../a1";
import type { ChartKind, ChartSpec, PivotSpec } from "../port";
import { ToolCtx, ToolError, checkAbort, isBlank, resolveRect, usedRect } from "./util";

/** A whole label that is a total: "Total", "Totals", "Grand Total", "Subtotal" or "Sub Total", optionally with a colon. */
const TOTAL_LABEL = /^(grand\s+total|sub\s*total|totals?)\s*:?$/i;
/** The chart footprint: the block a chart covers from its anchor cell. Office.js adds the same size in addChart. */
export const CHART_COLS = 8;
export const CHART_ROWS = 16;

export function chartFootprint(anchor: { c1: number; r1: number }): Rect {
  return { c1: anchor.c1, r1: anchor.r1, c2: anchor.c1 + CHART_COLS - 1, r2: anchor.r1 + CHART_ROWS - 1 };
}

export interface ChartPlan {
  rect: Rect;
  seriesColumns: number;
  warnings: string[];
}

/** Pure validation of a chart's source block; throws ToolError with an actionable message. */
export function validateChartData(
  kind: ChartKind, rect: Rect, values: unknown[][], types: string[][], opts: { includeTotalRows?: boolean } = {},
): ChartPlan {
  const warnings: string[] = [];
  const nrows = values.length;
  const ncols = values[0]?.length ?? 0;
  if (nrows < 2 || ncols < 2) throw new ToolError("A chart needs a header row, at least one data row, a category column and at least one series column");
  // Totals live at the bottom, so only a final row labelled as a total is an error. A total-looking label anywhere
  // else is only a warning. include_total_rows turns the final-row error into a warning too.
  for (let i = 1; i < nrows; i++) {
    const label = String(values[i][0] ?? "").trim();
    if (!TOTAL_LABEL.test(label)) continue;
    if (i === nrows - 1 && !opts.includeTotalRows) {
      const excl = formatRect({ ...rect, r2: rect.r1 + i - 1 });
      throw new ToolError(`Row ${rect.r1 + i} ("${label}") is inside the data range and would be charted as a data point. Use ${excl} to exclude it, or pass include_total_rows=true if it is a real category.`);
    }
    warnings.push(`Row ${rect.r1 + i} ("${label}") looks like a total; check it belongs in the chart.`);
  }
  const seriesCols = ncols - 1;
  for (let j = 1; j < ncols; j++) {
    if (isBlank(values[0][j] as never)) throw new ToolError(`Series column ${colToLetters(rect.c1 + j)} has no header; the first row must name every series`);
    for (let i = 1; i < nrows; i++) {
      if (!["Double", "Integer", "Empty"].includes(types[i][j])) {
        throw new ToolError(`${cellAddress(rect.c1 + j, rect.r1 + i)} is not numeric ("${String(values[i][j])}"). Series columns must hold numbers; if the header row is not the first row of data_address, include it as the first row.`);
      }
    }
  }
  for (let i = 1; i < nrows; i++) if (kind !== "scatter" && isBlank(values[i][0] as never)) throw new ToolError(`Category label is blank at ${cellAddress(rect.c1, rect.r1 + i)}`);
  if (kind === "pie") {
    if (seriesCols !== 1) throw new ToolError(`A pie chart shows one series; data_address has ${seriesCols}. Use two columns: labels and one value column.`);
    const nums = values.slice(1).map((r) => r[1]).filter((v): v is number => typeof v === "number");
    if (nums.some((v) => v < 0)) throw new ToolError("A pie chart cannot show negative values; use a bar chart");
    if (nums.every((v) => v === 0)) throw new ToolError("All values are zero, so a pie chart would be empty");
    if (nrows - 1 > 8) warnings.push(`${nrows - 1} slices is hard to read; consider a bar chart`);
    if (nums.some((v) => v === 0)) warnings.push("Zero-value categories get no slice but still appear in the legend");
  }
  return { rect, seriesColumns: seriesCols, warnings };
}

export async function createChart(ctx: ToolCtx, a: {
  sheet: string; data_address: string; type: ChartKind; title?: string; category_axis_title?: string; value_axis_title?: string;
  show_percent_labels?: boolean; show_legend: boolean; anchor?: string; include_total_rows?: boolean;
}) {
  const rect = await resolveRect(ctx, a.sheet, a.data_address);
  const d = await ctx.port.read(a.sheet, formatRect(rect));
  const plan = validateChartData(a.type, rect, d.values, d.valueTypes, { includeTotalRows: a.include_total_rows ?? false });

  const used = await usedRect(ctx, a.sheet);
  let anchor: Rect;
  if (a.anchor) {
    const p = parseAddress(a.anchor);
    if (!p) throw new ToolError(`anchor "${a.anchor}" is not a valid cell`);
    anchor = chartFootprint(p);
  } else {
    const col = Math.max(used?.c2 ?? rect.c2, rect.c2) + 2;
    anchor = chartFootprint({ c1: col, r1: rect.r1 });
  }
  // The chart covers a CHART_COLS x CHART_ROWS block from the anchor; it must not hide any data on the sheet.
  if (used && intersects(anchor, used)) {
    const free = { c1: used.c2 + 2, r1: used.r1 };
    // only complain when the footprint actually covers occupied cells
    const covered = await ctx.port.read(a.sheet, formatRect({ ...anchor, c2: Math.min(anchor.c2, used.c2), r2: Math.min(anchor.r2, used.r2), c1: Math.max(anchor.c1, used.c1), r1: Math.max(anchor.r1, used.r1) }));
    if (covered.values.some((r) => r.some((v) => !isBlank(v as never)))) {
      throw new ToolError(`The chart would cover data at ${formatRect(anchor)}. Choose an anchor in free space, for example ${cellAddress(free.c1, free.r1)}.`);
    }
  }
  // Asked of the port, not computed from anchors: Office.js does not report a chart's anchor cell.
  const overlap = await ctx.port.findChartOverlap(a.sheet, formatRect(anchor));
  if (overlap) throw new ToolError(`The chart would overlap the existing chart "${overlap}" at ${formatRect(anchor)}. Choose a different anchor.`);
  const spec: ChartSpec = {
    sheet: a.sheet, dataAddress: formatRect(rect), kind: a.type, title: a.title, categoryAxisTitle: a.category_axis_title,
    valueAxisTitle: a.value_axis_title, showPercentLabels: a.show_percent_labels ?? a.type === "pie", showLegend: a.show_legend,
    anchor: cellAddress(anchor.c1, anchor.r1),
  };
  checkAbort(ctx);
  const chart = await ctx.port.addChart(spec);
  const catCol = colToLetters(rect.c1);
  return {
    chart,
    series: Array.from({ length: plan.seriesColumns }, (_, k) => ({
      name_cell: `${colToLetters(rect.c1 + 1 + k)}${rect.r1}`,
      values: `${colToLetters(rect.c1 + 1 + k)}${rect.r1 + 1}:${colToLetters(rect.c1 + 1 + k)}${rect.r2}`,
      categories: `${catCol}${rect.r1 + 1}:${catCol}${rect.r2}`,
    })),
    data_points: rows(rect) - 1,
    warnings: plan.warnings,
    note: "Not undoable with undo_step. Series addresses above are derived from data_address.",
  };
}

export async function createPivot(ctx: ToolCtx, a: {
  source_sheet: string; source_address: string; destination_sheet: string; destination_address: string; name?: string;
  rows?: string[]; columns?: string[]; filters?: string[]; values: { field: string; summarize_by?: PivotSpec["values"][number]["summarizeBy"] }[];
}) {
  const src = await resolveRect(ctx, a.source_sheet, a.source_address);
  if (rows(src) < 2) throw new ToolError("The source needs a header row and at least one data row");
  const head = await ctx.port.read(a.source_sheet, formatRect({ ...src, r2: src.r1 }));
  const fields = head.values[0].map((v) => String(v ?? "").trim());
  if (fields.some((f) => f === "")) throw new ToolError("Every source column needs a header; the first row has blanks");
  const want = [...(a.rows ?? []), ...(a.columns ?? []), ...(a.filters ?? []), ...a.values.map((v) => v.field)];
  const missing = want.filter((f) => !fields.includes(f));
  if (missing.length) throw new ToolError(`Unknown field(s): ${[...new Set(missing)].join(", ")}. Available: ${fields.join(", ")}`);
  if (a.values.length === 0) throw new ToolError("At least one value field is required");
  const dest = parseAddress(a.destination_address);
  if (!dest) throw new ToolError(`destination_address "${a.destination_address}" is not a valid cell`);
  if (a.destination_sheet === a.source_sheet && intersects({ ...dest, c2: dest.c1 + 12, r2: dest.r1 + 25 }, src)) {
    throw new ToolError("The pivot would be placed over its own source data. Put it on another sheet or to the right of the data.");
  }
  const target = await ctx.port.read(a.destination_sheet, a.destination_address).catch(() => null);
  if (target && target.values[0]?.some((v) => !isBlank(v as never))) throw new ToolError(`The destination cell ${a.destination_address} is not empty`);
  const pivots = await ctx.port.listPivots();
  const name = a.name ?? `PivotTable${pivots.length + 1}`;
  if (pivots.some((p) => p.name === name)) throw new ToolError(`A pivot named "${name}" already exists`);
  checkAbort(ctx);
  const pivot = await ctx.port.addPivot({
    name, sourceSheet: a.source_sheet, sourceAddress: formatRect(src), destSheet: a.destination_sheet, destAddress: a.destination_address,
    rows: a.rows ?? [], columns: a.columns ?? [], filters: a.filters ?? [],
    values: a.values.map((v) => ({ field: v.field, summarizeBy: v.summarize_by ?? "sum" })),
  });
  return { pivot, note: "A real PivotTable. It refreshes with the source; not undoable with undo_step." };
}
