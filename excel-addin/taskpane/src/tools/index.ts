import { createChart, createPivot } from "./chartpivot";
import { checkErrors, describeWorkbook, getSelection, profileRange, readRange } from "./read";
import type { ToolCtx } from "./util";
import { checkpointList, createTable, fillFormula, formatRange, undoStep, writeRange } from "./write";

export { Journal } from "./journal";
export type { ToolCtx } from "./util";
export { ToolError } from "./util";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
export type ToolFn = (ctx: ToolCtx, args: any) => Promise<unknown>;

/** Names match bridge/tools.json (contract test enforces both directions). */
export const TOOLS: Record<string, ToolFn> = {
  checkpoint: checkpointList,
  check_errors: checkErrors,
  create_chart: createChart,
  create_pivot: createPivot,
  create_table: createTable,
  describe_workbook: describeWorkbook,
  fill_formula: fillFormula,
  format_range: formatRange,
  get_selection: getSelection,
  profile_range: profileRange,
  read_range: readRange,
  undo_step: undoStep,
  write_range: writeRange,
};

