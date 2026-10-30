import { Rect, cellAddress, cellCount, cols, formatRect, rows } from "../a1";
import type { CellValue, ValueType } from "../port";
import { DATA_NOTICE, ERROR_RE, ToolCtx, ToolError, checkAbort, errorsIn, isBlank, rectOfData, resolveRect, usedRect } from "./util";

export async function describeWorkbook(ctx: ToolCtx) {
  const [sheets, active, tables, names, pivots, calc, sel] = await Promise.all([
    ctx.port.listSheets(), ctx.port.activeSheet(), ctx.port.listTables(), ctx.port.listNames(), ctx.port.listPivots(),
    ctx.port.calculationMode(), ctx.port.selection(),
  ]);
  const out = [];
  for (const s of sheets) {
    const ch = s.visibility === "VeryHidden" ? [] : await ctx.port.listCharts(s.name);
    out.push({ name: s.name, visibility: s.visibility, used_range: s.usedAddress, charts: ch.map((c) => ({ name: c.name, type: c.type, title: c.title, data: c.dataAddress })) });
  }
  return { active_sheet: active, selection: sel, calculation_mode: calc, sheets: out, tables, named_ranges: names, pivots };
}

export async function getSelection(ctx: ToolCtx, a: { max_cells: number }) {
  const sel = await ctx.port.selection();
  return readPage(ctx, sel.sheet, sel.address, ["values", "formulas"], a.max_cells, 0);
}

type Include = "values" | "formulas" | "number_formats" | "value_types";

export async function readRange(ctx: ToolCtx, a: { sheet: string; address: string; include: Include[]; max_cells: number; offset_rows: number }) {
  return readPage(ctx, a.sheet, a.address, a.include, a.max_cells, a.offset_rows);
}

async function readPage(ctx: ToolCtx, sheet: string, address: string, include: Include[], maxCells: number, offsetRows: number) {
  const rect = await resolveRect(ctx, sheet, address);
  const totalRows = rows(rect);
  const perPage = Math.max(1, Math.floor(maxCells / cols(rect)));
  if (offsetRows >= totalRows) throw new ToolError(`offset_rows ${offsetRows} is past the end (the range has ${totalRows} rows)`);
  const r1 = rect.r1 + offsetRows;
  const r2 = Math.min(rect.r2, r1 + perPage - 1);
  checkAbort(ctx);
  const d = await ctx.port.read(sheet, formatRect({ ...rect, r1, r2 }));
  const origin: Rect = { ...rect, r1, r2 };
  const out: Record<string, unknown> = {
    data_notice: DATA_NOTICE, sheet, address: formatRect(rect), total_rows: totalRows, total_cols: cols(rect),
    offset_rows: offsetRows, rows_returned: r2 - r1 + 1, truncated: r2 < rect.r2,
  };
  if (r2 < rect.r2) out.next_offset_rows = offsetRows + (r2 - r1 + 1);
  if (include.includes("values")) out.values = d.values;
  if (include.includes("formulas")) {
    const f: Record<string, string> = {};
    d.formulas.forEach((row, i) => row.forEach((x, j) => { if (x.startsWith("=")) f[cellAddress(origin.c1 + j, origin.r1 + i)] = x; }));
    out.formulas = f;
  }
  if (include.includes("number_formats")) out.number_formats = d.numberFormats;
  if (include.includes("value_types")) out.value_types = d.valueTypes;
  return out;
}

// ---------------------------------------------------------------- profile

const NBSP = /[   ​]/g;
const CURRENCY_NUMBER = /^[-+(]?(?:[A-Za-z]{1,3}\s?|\p{Sc}\s?)?\d{1,3}(?:,\d{3})*(?:\.\d+)?%?\)?$|^[-+]?(?:[A-Za-z]{1,3}\s?|\p{Sc}\s?)?\d+(?:\.\d+)?%?$/u;
export const isNumericText = (s: string) => CURRENCY_NUMBER.test(s.replace(NBSP, " ").trim());
export const isDateText = (s: string) => {
  const t = s.replace(NBSP, " ").trim();
  return /^\d{4}-\d{2}-\d{2}([ T]\d{2}:\d{2}(:\d{2})?)?$/.test(t) || /^\d{1,2} [A-Za-z]{3,9},? \d{4}$/.test(t) || /^\d{1,2}[/.-]\d{1,2}[/.-]\d{2,4}$/.test(t);
};
export const isAmbiguousDateText = (s: string) => {
  const m = /^(\d{1,2})[/.-](\d{1,2})[/.-]\d{2,4}$/.exec(s.trim());
  return !!m && Number(m[1]) <= 12 && Number(m[2]) <= 12 && m[1] !== m[2];
};

export interface ColumnProfile {
  column: string;
  name: string;
  counts: { empty: number; string: number; number: number; boolean: number; error: number };
  distinct: number;
  min?: number;
  max?: number;
  text_numeric: number;
  text_date: number;
  ambiguous_day_month_dates: number;
  leading_trailing_space: number;
  double_space: number;
  nonbreaking_space: number;
  samples: CellValue[];
}

export function profileGrid(values: CellValue[][], types: ValueType[][], header: boolean, firstCol: number) {
  const head = header ? values[0] : null;
  const body = header ? values.slice(1) : values;
  const btypes = header ? types.slice(1) : types;
  const ncols = values[0]?.length ?? 0;
  const profiles: ColumnProfile[] = [];
  for (let j = 0; j < ncols; j++) {
    const p: ColumnProfile = {
      column: cellAddress(firstCol + j, 1).replace(/\d+$/, ""), name: head ? String(head[j]) : `col${j + 1}`,
      counts: { empty: 0, string: 0, number: 0, boolean: 0, error: 0 }, distinct: 0, text_numeric: 0, text_date: 0,
      ambiguous_day_month_dates: 0, leading_trailing_space: 0, double_space: 0, nonbreaking_space: 0, samples: [],
    };
    const seen = new Set<string>();
    for (let i = 0; i < body.length; i++) {
      const v = body[i][j];
      const t = btypes[i][j];
      if (t === "Empty" || isBlank(v)) { p.counts.empty++; continue; }
      seen.add(`${typeof v}:${v}`);
      if (t === "Error" || (typeof v === "string" && ERROR_RE.test(v))) p.counts.error++;
      else if (typeof v === "number") {
        p.counts.number++;
        p.min = p.min === undefined ? v : Math.min(p.min, v);
        p.max = p.max === undefined ? v : Math.max(p.max, v);
      } else if (typeof v === "boolean") p.counts.boolean++;
      else {
        p.counts.string++;
        if (isNumericText(v)) p.text_numeric++;
        if (isDateText(v)) p.text_date++;
        if (isAmbiguousDateText(v)) p.ambiguous_day_month_dates++;
        if (v !== v.trim()) p.leading_trailing_space++;
        if (/ {2,}/.test(v.trim())) p.double_space++;
        if (NBSP.test(v)) p.nonbreaking_space++;
        NBSP.lastIndex = 0;
      }
      if (p.samples.length < 3 && !p.samples.includes(v)) p.samples.push(v);
    }
    p.distinct = seen.size;
    profiles.push(p);
  }
  let dup = 0;
  const rowsSeen = new Set<string>();
  for (const r of body) {
    if (r.every(isBlank)) continue;
    const k = JSON.stringify(r.map((v) => (typeof v === "string" ? v.trim().toLowerCase() : v)));
    if (rowsSeen.has(k)) dup++;
    rowsSeen.add(k);
  }
  const blankRows = body.filter((r) => r.every(isBlank)).length;
  return { rows: body.length, columns: profiles, duplicate_rows_after_trim_and_case: dup, blank_rows: blankRows };
}

const PROFILE_MAX_ROWS = 5000;

export async function profileRange(ctx: ToolCtx, a: { sheet: string; address: string; header_row: boolean }) {
  const rect = await resolveRect(ctx, a.sheet, a.address);
  const sampled = rows(rect) > PROFILE_MAX_ROWS;
  const r: Rect = sampled ? { ...rect, r2: rect.r1 + PROFILE_MAX_ROWS - 1 } : rect;
  checkAbort(ctx);
  const d = await ctx.port.read(a.sheet, formatRect(r));
  return {
    data_notice: DATA_NOTICE, sheet: a.sheet, address: formatRect(rect), sampled_first_rows: sampled ? PROFILE_MAX_ROWS : undefined,
    ...profileGrid(d.values, d.valueTypes, a.header_row, r.c1),
  };
}

export async function checkErrors(ctx: ToolCtx, a: { sheet: string; address?: string }) {
  const rect = a.address ? await resolveRect(ctx, a.sheet, a.address) : await usedRect(ctx, a.sheet);
  if (!rect) return { sheet: a.sheet, errors: [], count: 0, note: "The sheet is empty." };
  if (cellCount(rect) > 200000) throw new ToolError("That range is too large to scan at once; pass a smaller address");
  const d = await ctx.port.read(a.sheet, formatRect(rect));
  const all = errorsIn(d, rectOfData(a.sheet, d), 200);
  return { sheet: a.sheet, address: formatRect(rect), count: all.length, errors: all, truncated: all.length >= 200 };
}
