import type { ReactNode } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";
import type { DBCell, DBResultSet } from "../../api/client";
import { cn } from "../../lib/utils";
import { cellToInput } from "./SQLiteDialogs";

/** One cell, rendered so NULL, numbers and BLOBs are visually distinct. */
function Cell({ value, onOpenBlob }: { value: DBCell; onOpenBlob?: () => void }) {
  if (value === null) {
    return <span className="italic text-muted-foreground">NULL</span>;
  }
  if (typeof value === "object" && value !== null && "$blob" in value) {
    // A blob is the one cell whose value the grid cannot show, so the chip is a
    // button that opens the viewer rather than a dead label.
    const label = `blob ${value.bytes}B`;
    if (onOpenBlob) {
      return (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onOpenBlob();
          }}
          data-testid="sqlite-blob-chip"
          title={value.truncated ? `${value.preview}… (preview only)` : value.preview}
          className="rounded bg-muted px-1 font-mono text-[10px] underline decoration-dotted hover:bg-muted/70"
        >
          {label}
        </button>
      );
    }
    return (
      <span
        className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground"
        title={value.preview}
      >
        {label}
      </span>
    );
  }
  const text = String(value);
  return (
    <span className="font-mono text-xs" title={text.length > 80 ? text : undefined}>
      {text.length > 200 ? `${text.slice(0, 200)}…` : text}
    </span>
  );
}

/** Shared result-grid renderer for the Data and Query tabs. */
export function ResultGrid({
  result,
  testId,
  renderRowActions,
  sortable,
  sortBy,
  sortDesc,
  onSort,
  selectable,
  selectedRows,
  onToggleRow,
  editing,
  onStartEdit,
  onCommitEdit,
  onCancelEdit,
  inlineBusy,
  rowLabel,
  onOpenBlob,
}: {
  result: DBResultSet;
  testId: string;
  renderRowActions?: (rowIndex: number) => ReactNode;
  sortable?: boolean;
  sortBy?: string;
  sortDesc?: boolean;
  onSort?: (column: string) => void;
  selectable?: boolean;
  selectedRows?: Set<number>;
  onToggleRow?: (rowIndex: number) => void;
  editing?: { row: number; col: number } | null;
  onStartEdit?: (row: number, col: number) => void;
  onCommitEdit?: (row: number, col: number, text: string) => void;
  onCancelEdit?: () => void;
  inlineBusy?: boolean;
  /** Test/debug label prefix for the row checkboxes, e.g. "Select row". */
  rowLabel?: string;
  /** Open the blob viewer for a cell (only meaningful for BLOB values). */
  onOpenBlob?: (row: number, col: number) => void;
}) {
  if (result.columns.length === 0) {
    return (
      <div className="p-3 text-xs text-muted-foreground">
        Statement executed — no rows returned.
      </div>
    );
  }
  return (
    <div className="min-h-0 min-w-0 flex-1 overflow-auto" data-testid={testId}>
      <table className="w-max min-w-full border-collapse text-left">
        <thead className="sticky top-0 bg-muted/80 backdrop-blur">
          <tr>
            {selectable ? (
              <th className="w-8 border-b border-border px-2 py-1" aria-label="Select rows" />
            ) : null}
            {result.columns.map((c, i) => (
              <th
                key={`${c.name}-${i}`}
                className="whitespace-nowrap border-b border-border px-2 py-1 text-[11px] font-semibold"
              >
                {sortable && onSort ? (
                  <button
                    type="button"
                    onClick={() => onSort(c.name)}
                    aria-label={`Sort by ${c.name}`}
                    className="inline-flex items-center gap-0.5 hover:underline"
                  >
                    {c.name}
                    {sortBy === c.name ? (
                      sortDesc ? (
                        <ArrowDown className="h-3 w-3" />
                      ) : (
                        <ArrowUp className="h-3 w-3" />
                      )
                    ) : null}
                  </button>
                ) : (
                  c.name
                )}
                {c.decl_type ? (
                  <span className="ml-1 font-normal text-muted-foreground">{c.decl_type}</span>
                ) : null}
              </th>
            ))}
            {renderRowActions ? (
              <th className="w-14 border-b border-border px-2 py-1" aria-label="Row actions" />
            ) : null}
          </tr>
        </thead>
        <tbody>
          {result.rows.map((row, ri) => (
            <tr key={ri} className={cn("odd:bg-muted/20", selectedRows?.has(ri) && "bg-primary/10")}>
              {selectable ? (
                <td className="border-b border-border/50 px-2 py-1 align-top">
                  <input
                    type="checkbox"
                    aria-label={`${rowLabel ?? "Select row"} ${ri + 1}`}
                    checked={selectedRows?.has(ri) ?? false}
                    onChange={() => onToggleRow?.(ri)}
                  />
                </td>
              ) : null}
              {row.map((cell, ci) => (
                <td
                  key={ci}
                  className="max-w-[24rem] truncate border-b border-border/50 px-2 py-1 align-top"
                >
                  {editing?.row === ri && editing.col === ci ? (
                    <input
                      autoFocus
                      aria-label={`Edit ${result.columns[ci]?.name ?? "cell"}`}
                      defaultValue={cellToInput(cell)}
                      disabled={inlineBusy}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          onCommitEdit?.(ri, ci, e.currentTarget.value);
                        } else if (e.key === "Escape") {
                          e.preventDefault();
                          onCancelEdit?.();
                        }
                      }}
                      onBlur={(e) => onCommitEdit?.(ri, ci, e.currentTarget.value)}
                      className="w-full rounded border border-primary bg-transparent px-1 font-mono text-xs outline-none"
                    />
                  ) : onStartEdit ? (
                    <span
                      role="button"
                      tabIndex={0}
                      onDoubleClick={() => onStartEdit(ri, ci)}
                      className="block cursor-text"
                    >
                      <Cell value={cell} onOpenBlob={() => onOpenBlob?.(ri, ci)} />
                    </span>
                  ) : (
                    <Cell value={cell} onOpenBlob={() => onOpenBlob?.(ri, ci)} />
                  )}
                </td>
              ))}
              {renderRowActions ? (
                <td className="border-b border-border/50 px-2 py-1 text-right align-top">
                  {renderRowActions(ri)}
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
      {result.rows.length === 0 && (
        <div className="p-3 text-xs text-muted-foreground">No rows.</div>
      )}
    </div>
  );
}
