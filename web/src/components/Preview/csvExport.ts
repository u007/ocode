import type { DBResultSet, DBCell } from "../../api/client";

/**
 * CSV export for the SQLite browser's grids.
 *
 * These are pure string helpers plus one DOM call, kept out of SQLiteViewer.tsx
 * so the escaping rules can be tested directly — a CSV that silently drops a
 * quoted field is a data-corruption bug, not a formatting nit.
 */

/** Characters that force a field to be quoted per RFC 4180. */
const MUST_QUOTE = /[",\r\n]/;

/**
 * Render one cell as a CSV field.
 *
 * NULL becomes the empty string: writing the word "null" would be
 * indistinguishable from the four-character string in the export, which is
 * exactly the distinction the user opened the grid to see. A BLOB becomes a
 * marker — the base64 payload is binary and would corrupt the file's shape.
 */
export function quoteCsvField(value: DBCell | undefined): string {
  // An absent cell and a NULL cell are the same thing in a CSV: both are an
  // empty field. `undefined` is accepted so a caller can pass a sparse row
  // straight through.
  if (value === null || value === undefined) return "";
  if (typeof value === "object" && "$blob" in value) {
    return quote(`[blob ${value.bytes}B]`);
  }
  return quote(String(value));
}

function quote(s: string): string {
  return MUST_QUOTE.test(s) ? `"${s.split('"').join('""')}"` : s;
}

/** Join one row of cells into a CSV line (no trailing newline). */
export function cellToCsv(row: DBCell[]): string {
  return row.map(quoteCsvField).join(",");
}

/**
 * Serialise a result set: the column names as a header row, then one line per
 * row. The file ends with a newline so the last row is complete for a reader
 * that splits on it.
 */
export function resultToCsv(result: DBResultSet): string {
  const lines = [result.columns.map((c) => quote(c.name)).join(",")];
  for (const row of result.rows) lines.push(cellToCsv(row));
  return `${lines.join("\n")}\n`;
}

/**
 * Offer `text` as a file download.
 *
 * The object URL is revoked on a timer rather than immediately: Safari (and the
 * desktop app's WKWebView) navigates to about:blank when the URL it just
 * clicked is revoked synchronously, which reads as the download failing.
 */
export function downloadCsv(filename: string, text: string): void {
  const blob = new Blob([text], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/**
 * Build a download filename from the table name, keeping it filesystem-safe and
 * adding a UTC timestamp so repeated exports of the same table do not collide in
 * the Downloads folder.
 */
export function csvFilename(table: string): string {
  const safe = table.replace(/[^A-Za-z0-9._-]+/g, "_") || "export";
  const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
  return `${safe}-${stamp}.csv`;
}