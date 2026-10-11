// Shared cell conversion for the Postgres connector. Values arrive as the
// server's JSON: scalars as-is, integers beyond 2^53 as strings, json and array
// values as objects. The grid needs DBCell, which is a scalar or a blob.
import type { DBCell, DBResultSet } from "../../api/client";

// A JSON scalar goes to the grid as-is; an object or array (json columns) is shown as its JSON text.
export function toCell(v: unknown): DBCell {
  if (v === null || v === undefined) return null;
  if (typeof v === "string" || typeof v === "number" || typeof v === "boolean") return v;
  return JSON.stringify(v);
}

// Typed text in the row editor is stored as typed. inputToCell is not used here:
// it coerces by declared type through a JavaScript number, which rounds a bigint.
export function pgInputToCell(text: string): DBCell {
  return text === "" ? null : text;
}

export function toResultSet(columns: string[], rows: unknown[][], truncated: boolean): DBResultSet {
  return {
    columns: columns.map((name) => ({ name, decl_type: "" })),
    rows: rows.map((row) => row.map(toCell)),
    row_count: rows.length,
    truncated,
    elapsed_ms: 0,
  };
}
