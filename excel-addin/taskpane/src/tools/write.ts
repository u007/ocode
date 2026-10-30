import { Rect, cellAddress, cellCount, cols, formatRect, intersects, parseAddress, rows } from "../a1";
import type { CellValue, FormatSpec, RangeData } from "../port";
import { MAX_CHECKPOINT_CELLS, Journal, Step, jsonBytes, stepRect } from "./journal";
import {
  DATA_NOTICE, ToolCtx, ToolError, checkAbort, errorsIn, gridDims, isBlank, isFormula, normalized, rectOfData, resolveRect,
} from "./util";

const READBACK_CELLS = 200;

/**
 * Put the prior values and formats back after a checkpoint failed. Returns the error to throw: either the write was
 * rolled back, or (if the rollback failed too) the message says plainly which cells keep the new values.
 */
async function rollBack(
  ctx: ToolCtx, sheet: string, addr: string, prior: CellValue[][], priorFormats: string[][], cause: unknown,
): Promise<ToolError> {
  const why = cause instanceof Error ? cause.message : String(cause);
  try {
    await ctx.port.setNumberFormats(sheet, addr, priorFormats);
    await ctx.port.write(sheet, addr, prior);
  } catch (e) {
    const again = e instanceof Error ? e.message : String(e);
    return new ToolError(`The write to ${sheet}!${addr} was applied but could NOT be checkpointed for undo (${why}), and rolling it back also failed (${again}). The cells at ${sheet}!${addr} keep the new values and no undo step exists for them. Tell the user and check or fix those cells by hand.`);
  }
  return new ToolError(`The write to ${sheet}!${addr} was rolled back because it could not be checkpointed for undo (${why}). Nothing was changed. Retry the write.`);
}

/** Shared by write_range and fill_formula: refuse silently destructive writes, checkpoint, write, read back. */
async function checkpointedWrite(
  ctx: ToolCtx, tool: string, sheet: string, rect: Rect, overwrite: boolean, replaceFormulas: boolean,
  incoming: CellValue[][] | null, incomingBytes: number, apply: () => Promise<void>,
) {
  if (cellCount(rect) > MAX_CHECKPOINT_CELLS) {
    throw new ToolError(`${cellCount(rect)} cells is too many to checkpoint in one step (limit ${MAX_CHECKPOINT_CELLS}). Write in smaller blocks so every change can be undone.`);
  }
  checkAbort(ctx);
  const addr = formatRect(rect);
  const before = await ctx.port.read(sheet, addr);
  const prior = normalized(before);
  const priorFormats = before.numberFormats;

  const occupied: string[] = [];
  const formulaLoss: string[] = [];
  prior.forEach((row, i) => row.forEach((v, j) => {
    const at = cellAddress(rect.c1 + j, rect.r1 + i);
    if (!isBlank(v)) occupied.push(at);
    const next = incoming ? incoming[i]?.[j] : "=";
    if (isFormula(v) && !(typeof next === "string" && next.startsWith("="))) formulaLoss.push(at);
  }));
  if (formulaLoss.length && !replaceFormulas) {
    throw new ToolError(`This would replace ${formulaLoss.length} formula cell(s) with constants (${formulaLoss.slice(0, 5).join(", ")}${formulaLoss.length > 5 ? ", ..." : ""}). Write formulas instead, or pass replace_formulas=true if the user asked for values.`);
  }
  if (occupied.length && !overwrite) {
    throw new ToolError(`${occupied.length} target cell(s) are not empty (${occupied.slice(0, 5).join(", ")}${occupied.length > 5 ? ", ..." : ""}). Pass overwrite=true only if replacing them is intended.`);
  }

  // Estimate the persisted step before touching a cell: before image, after image, and both format grids.
  const priorBytes = jsonBytes(prior);
  await ctx.journal.prepare(Math.max(2 * priorBytes, priorBytes + incomingBytes) + 2 * jsonBytes(priorFormats));

  checkAbort(ctx);
  await apply();
  let after: RangeData;
  let step: Step;
  try {
    after = await ctx.port.read(sheet, addr);
    step = await ctx.journal.record({
      tool, sheet, address: addr, before: prior, after: normalized(after), beforeFormats: priorFormats, afterFormats: after.numberFormats,
    });
  } catch (e) {
    throw await rollBack(ctx, sheet, addr, prior, priorFormats, e);
  }
  const errs = errorsIn(after, rectOfData(sheet, after));
  const small = cellCount(rect) <= READBACK_CELLS;
  return {
    data_notice: DATA_NOTICE, step_id: step.id, sheet, address: addr,
    cells_written: cellCount(rect),
    /** true when the step holds format images, so undo restores the number formats too */
    formats_restored: step.beforeFormats !== undefined,
    evaluated: small ? after.values : undefined,
    formulas: small ? Object.fromEntries(after.formulas.flatMap((r, i) => r.map((f, j) => [cellAddress(rect.c1 + j, rect.r1 + i), f] as const)).filter(([, f]) => f.startsWith("="))) : undefined,
    first_rows: small ? undefined : after.values.slice(0, 3),
    new_errors: errs,
    note: errs.length ? "Some cells evaluate to errors. Fix them before reporting success." : undefined,
  };
}

export async function writeRange(ctx: ToolCtx, a: { sheet: string; address: string; values: CellValue[][]; number_format?: string; overwrite: boolean; replace_formulas: boolean }) {
  const { rows: nr, cols: nc } = gridDims(a.values);
  if (nr === 0 || nc === 0) throw new ToolError("values is empty");
  const given = parseAddress(a.address);
  if (!given) throw new ToolError(`"${a.address}" is not a valid A1 address`);
  const single = given.c1 === given.c2 && given.r1 === given.r2;
  if (!single && (cols(given) !== nc || rows(given) !== nr)) {
    throw new ToolError(`address ${a.address} is ${rows(given)}x${cols(given)} but values is ${nr}x${nc}. Give the top-left cell alone, or an address that matches the data.`);
  }
  const rect: Rect = { sheet: a.sheet, c1: given.c1, r1: given.r1, c2: given.c1 + nc - 1, r2: given.r1 + nr - 1 };
  return checkpointedWrite(ctx, "write_range", a.sheet, rect, a.overwrite, a.replace_formulas, a.values, jsonBytes(a.values), () =>
    ctx.port.write(a.sheet, formatRect(rect), a.values, a.number_format));
}

export async function fillFormula(ctx: ToolCtx, a: { sheet: string; address: string; formula: string; overwrite: boolean }) {
  if (!a.formula.startsWith("=")) throw new ToolError("formula must start with '='");
  const rect = await resolveRect(ctx, a.sheet, a.address);
  return checkpointedWrite(ctx, "fill_formula", a.sheet, rect, a.overwrite, true, null, cellCount(rect) * (jsonBytes(a.formula) + 1), () =>
    ctx.port.fillFormula(a.sheet, formatRect(rect), a.formula));
}

const HEX = /^#[0-9A-Fa-f]{6}$/;

export async function formatRange(ctx: ToolCtx, a: {
  sheet: string; address: string; bold?: boolean; fill?: string; font_color?: string; number_format?: string; align?: "left" | "center" | "right";
  autofit_columns?: boolean; column_width?: number; wrap_text?: boolean; borders?: boolean; freeze_rows?: number;
}) {
  for (const [k, v] of [["fill", a.fill], ["font_color", a.font_color]] as const) {
    if (v !== undefined && !HEX.test(v)) throw new ToolError(`${k} must look like #RRGGBB, got "${v}"`);
  }
  const rect = await resolveRect(ctx, a.sheet, a.address);
  const spec: FormatSpec = {
    bold: a.bold, fill: a.fill, fontColor: a.font_color, numberFormat: a.number_format, align: a.align, autofitColumns: a.autofit_columns,
    columnWidth: a.column_width, wrapText: a.wrap_text, borders: a.borders, freezeRows: a.freeze_rows,
  };
  const applied = Object.entries(spec).filter(([, v]) => v !== undefined);
  if (applied.length === 0) throw new ToolError("No formatting option was given");
  checkAbort(ctx);
  await ctx.port.format(a.sheet, formatRect(rect), spec);
  return { sheet: a.sheet, address: formatRect(rect), applied: Object.fromEntries(applied), note: "Formatting is not recorded for undo; values were not touched." };
}

export async function createTable(ctx: ToolCtx, a: { sheet: string; address: string; name?: string; style: string }) {
  const rect = await resolveRect(ctx, a.sheet, a.address);
  if (rows(rect) < 2) throw new ToolError("A table needs a header row and at least one data row");
  const d = await ctx.port.read(a.sheet, formatRect({ ...rect, r2: rect.r1 }));
  const heads = d.values[0].map((v) => String(v ?? "").trim());
  const empty = heads.map((h, i) => (h === "" ? cellAddress(rect.c1 + i, rect.r1) : "")).filter(Boolean);
  if (empty.length) throw new ToolError(`The header row has empty cells (${empty.join(", ")}). Every table column needs a name.`);
  const dupes = heads.filter((h, i) => heads.findIndex((x) => x.toLowerCase() === h.toLowerCase()) !== i);
  if (dupes.length) throw new ToolError(`Duplicate column names: ${[...new Set(dupes)].join(", ")}`);
  for (const t of await ctx.port.listTables()) {
    const tr = parseAddress(t.address);
    if (t.sheet === a.sheet && tr && intersects(tr, rect)) throw new ToolError(`The range overlaps the existing table "${t.name}" (${t.address})`);
  }
  checkAbort(ctx);
  return { table: await ctx.port.addTable(a.sheet, formatRect(rect), a.name, a.style) };
}

export async function checkpointList(ctx: ToolCtx, a: { limit: number }) {
  const steps = await ctx.journal.list(a.limit);
  return {
    steps: steps.map((s) => ({ step_id: s.id, tool: s.tool, sheet: s.sheet, address: s.address, at: s.at, undone: s.undone ?? false })),
    note: "Formatting, tables, charts and pivots are not in this list and cannot be undone here.",
  };
}

export async function undoStep(ctx: ToolCtx, a: { step_id: string }) {
  const step: Step | undefined = await ctx.journal.get(a.step_id);
  if (!step) throw new ToolError(`No recorded step "${a.step_id}". Use checkpoint to list steps.`);
  if (step.undone) throw new ToolError(`Step ${step.id} was already undone`);
  const rect = stepRect(step);
  checkAbort(ctx);
  const now = normalized(await ctx.port.read(step.sheet, step.address));
  const conflicts = Journal.conflicts(step, rect, now);
  if (conflicts.length) {
    throw new ToolError(`Not undone: ${conflicts.length} cell(s) changed since step ${step.id} (${conflicts.slice(0, 5).map((c) => c.address).join(", ")}${conflicts.length > 5 ? ", ..." : ""}). Nothing was modified. Ask the user before overwriting their edits.`);
  }
  // Formats go first: a text format must be in place before a string such as "007" lands, or Excel re-parses it.
  // Steps recorded before formats were kept have no beforeFormats and skip this.
  if (step.beforeFormats) await ctx.port.setNumberFormats(step.sheet, step.address, step.beforeFormats);
  // Empty before-cells are written back as "" so they end up empty again.
  await ctx.port.write(step.sheet, step.address, step.before);
  await ctx.journal.markUndone(step.id);
  return { restored: step.id, sheet: step.sheet, address: step.address, formats_restored: step.beforeFormats !== undefined };
}
