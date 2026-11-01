import type { ChatMsg } from "./chatState";

/**
 * Compact token count for the context gauge: 999, 12.4k, 1M. Rounds to one decimal and drops a trailing ".0", and a
 * value that would print as "1000.0k" moves up to the M unit (999950 is "1M", not "1000k").
 */
export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 0) throw new RangeError(`token count must be a finite non-negative number, got ${n}`);
  const v = Math.round(n);
  if (v < 1000) return String(v);
  if (v < 999_950) return `${oneDecimal(v / 1000)}k`;
  return `${oneDecimal(v / 1_000_000)}M`;
}
const oneDecimal = (x: number) => (Math.round(x * 10) / 10).toFixed(1).replace(/\.0$/, "");

const CTX = /^\[ocode:excel\][\s\S]*?\[\/ocode:excel\]\s*/;

/** The workbook context block rides on every user message; the reader should only see what they typed. */
export const stripContext = (text: string) => text.replace(CTX, "");

/** One-line human summary of an Excel tool call for the transcript. */
export function summarizeTool(m: Pick<ChatMsg, "tool" | "args" | "output" | "status">): string {
  const name = (m.tool ?? "tool").replace(/^excel_/, "");
  let a: Record<string, any> = {};
  try { a = m.args ? JSON.parse(m.args) : {}; } catch { /* args may be partial while streaming */ }
  const where = a.sheet ? `${a.sheet}${a.address ? `!${a.address}` : a.data_address ? `!${a.data_address}` : ""}` : "";
  const verb: Record<string, string> = {
    describe_workbook: "Looked at the workbook", get_selection: "Read the selection", read_range: `Read ${where}`, profile_range: `Profiled ${where}`,
    check_errors: `Checked errors${where ? ` in ${where}` : ""}`, write_range: `Wrote ${where}`, fill_formula: `Filled ${where}`, format_range: `Formatted ${where}`,
    create_table: `Made a table at ${where}`, create_chart: `Added a ${a.type ?? ""} chart from ${where}`, create_pivot: "Created a PivotTable",
    checkpoint: "Listed changes", undo_step: `Undid ${a.step_id ?? "a step"}`,
  };
  let line = verb[name] ?? name;
  if (m.status === "done" && m.output && /^Excel bridge:|"ok":false|^Invalid arguments|not empty|cancelled/i.test(m.output.slice(0, 200))) line += " (refused)";
  return line;
}
