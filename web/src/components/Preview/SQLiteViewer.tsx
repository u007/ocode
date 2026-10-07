import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  ArrowDown,
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  Database,
  Download,
  ExternalLink,
  Gauge,
  Loader2,
  PanelLeft,
  PanelLeftClose,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Trash2,
  Undo2,
} from "lucide-react";
import {
  api,
  ApiError,
  type DBCell,
  type DBCellInput,
  type DBColumnDef,
  type DBInfo,
  type DBResultSet,
  type DBTableResponse,
  type DBTableSchema,
} from "../../api/client";
import { cn } from "../../lib/utils";
import {
  ConfirmDialog,
  RowEditorDialog,
  SchemaDialog,
  cellToInput,
  inputToCell,
  type SchemaAction,
} from "./SQLiteDialogs";
import BlobDialog from "./BlobDialog";
import { csvFilename, downloadCsv, resultToCsv } from "./csvExport";
import { useResizableSidebar } from "../../hooks/useResizableSidebar";

/**
 * Read-only SQLite browser for the preview pane (phase 1 of the SQLite
 * browser feature). Three surfaces over one database file:
 *
 *   Data   — a paginated grid of the selected table's rows.
 *   Query  — a SQL editor (plain textarea; Cmd/Ctrl+Enter runs) + results grid.
 *   Schema — columns, indexes, foreign keys and the original CREATE DDL.
 *
 * The server sniffs the SQLite header, so a `.db` that is not actually a SQLite
 * database renders the fallback pane instead of being handed to the engine.
 *
 * Live refresh: `revision` (bumped when the active session mutates this file)
 * refetches the table list and the current table's rows WITHOUT clearing the
 * query editor or the user's table selection.
 */

export interface SQLiteViewerProps {
  path: string;
  projectRoot?: string;
  projectHost?: string;
  revision?: number;
}

const PAGE_SIZE = 100;

type Tab = "data" | "query" | "schema";

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
function ResultGrid({
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

function SchemaPane({
  schema,
  onDropIndex,
}: {
  schema: DBTableSchema;
  onDropIndex?: (name: string) => void;
}) {
  return (
    <div className="min-h-0 flex-1 space-y-4 overflow-auto p-3 text-xs" data-testid="sqlite-schema">
      <section>
        <h4 className="mb-1 font-semibold">Columns</h4>
        <table className="w-full border-collapse text-left">
          <thead className="text-muted-foreground">
            <tr>
              <th className="border-b border-border px-2 py-1">Name</th>
              <th className="border-b border-border px-2 py-1">Type</th>
              <th className="border-b border-border px-2 py-1">Not null</th>
              <th className="border-b border-border px-2 py-1">Default</th>
              <th className="border-b border-border px-2 py-1">PK</th>
            </tr>
          </thead>
          <tbody>
            {schema.columns.map((c) => (
              <tr key={c.name}>
                <td className="border-b border-border/50 px-2 py-1 font-mono">
                  {c.name}
                  {c.generated ? (
                    <span className="ml-1 text-[10px] text-muted-foreground">generated</span>
                  ) : null}
                </td>
                <td className="border-b border-border/50 px-2 py-1">{c.decl_type || "—"}</td>
                <td className="border-b border-border/50 px-2 py-1">{c.not_null ? "yes" : ""}</td>
                <td className="border-b border-border/50 px-2 py-1 font-mono">
                  {c.default ?? ""}
                </td>
                <td className="border-b border-border/50 px-2 py-1">{c.pk || ""}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      {schema.indexes.length > 0 && (
        <section>
          <h4 className="mb-1 font-semibold">Indexes</h4>
          <ul className="space-y-0.5">
            {schema.indexes.map((ix) => (
              <li key={ix.name} className="flex items-center gap-2 font-mono">
                <span>
                  {ix.name}
                  {ix.unique ? " (unique)" : ""} — {ix.columns.join(", ")}
                </span>
                {onDropIndex ? (
                  <button
                    type="button"
                    aria-label={`Drop index ${ix.name}`}
                    onClick={() => onDropIndex(ix.name)}
                    className="rounded p-0.5 text-destructive hover:bg-muted"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      )}

      {schema.foreign_keys.length > 0 && (
        <section>
          <h4 className="mb-1 font-semibold">Foreign keys</h4>
          <ul className="space-y-0.5">
            {schema.foreign_keys.map((fk, i) => (
              <li key={i} className="font-mono">
                {fk.from} → {fk.table}.{fk.to}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section>
        <h4 className="mb-1 font-semibold">DDL</h4>
        <pre className="overflow-auto rounded bg-muted/40 p-2 font-mono text-[11px]">
          {schema.ddl || "-- no DDL available"}
        </pre>
      </section>
    </div>
  );
}

export default function SQLiteViewer({ path, projectRoot, projectHost, revision }: SQLiteViewerProps) {
  const [info, setInfo] = useState<DBInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [tab, setTab] = useState<Tab>("data");

  const [table, setTable] = useState<DBTableResponse | null>(null);
  const [offset, setOffset] = useState(0);

  // ── IDE state (filter / sort / selection / inline edit / maintenance) ─────
  // `filterDraft` is what the input holds; `filter` is what the request used, so
  // a half-typed expression never hits the database on every keystroke.
  const [filterDraft, setFilterDraft] = useState("");
  const [rowFilter, setRowFilter] = useState("");
  const [sortBy, setSortBy] = useState("");
  const [sortDesc, setSortDesc] = useState(false);
  const [filterError, setFilterError] = useState<string | null>(null);
  const [selectedRows, setSelectedRows] = useState<Set<number>>(new Set());
  const [editingCell, setEditingCell] = useState<{ row: number; col: number } | null>(null);
  const [inlineBusy, setInlineBusy] = useState(false);
  const [maintenanceBusy, setMaintenanceBusy] = useState(false);

  // ── Write state (P2–P4) ────────────────────────────────────────────────────
  // `bump` forces a refetch after a write; `notice` is the on-screen result.
  const [bump, setBump] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  const [rowEdit, setRowEdit] = useState<{ index: number | null } | null>(null);
  const [rowBusy, setRowBusy] = useState(false);
  const [rowError, setRowError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<{
    title: string;
    message: string;
    label: string;
    run: () => Promise<void>;
  } | null>(null);
  const [confirmBusy, setConfirmBusy] = useState(false);
  const [blobCell, setBlobCell] = useState<{ row: number; col: number } | null>(null);
  const [schemaAction, setSchemaAction] = useState<SchemaAction | null>(null);
  const [schemaBusy, setSchemaBusy] = useState(false);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [pendingWrite, setPendingWrite] = useState<string | null>(null);

  const [queryText, setQueryText] = useState("SELECT * FROM ");
  const [queryResult, setQueryResult] = useState<DBResultSet | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);

  // Table-list pane width: drag-to-resize + collapse, persisted to localStorage
  // (same hook as the app sidebar and Git file list). `width` is retained while
  // collapsed so re-expanding restores the previous size.
  const tablePane = useResizableSidebar({
    storageKey: "ocode.ui.sqlite-viewer.width",
    defaultWidth: 160,
    minWidth: 120,
    maxWidth: 480,
    collapsible: true,
  });

  // Load the database header + table list. Runs on path/host/revision change;
  // never clears the query editor or table selection.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .dbInfo(path, projectRoot, projectHost)
      .then((res) => {
        if (cancelled) return;
        setInfo(res);
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path, projectRoot, projectHost, revision, bump]);

  // Load a page of the selected table. A filter error is rendered next to the
  // filter box rather than replacing the whole pane: a rejected expression must
  // not look like "this table has no rows".
  const loadTable = useCallback(
    (name: string, off: number) => {
      let cancelled = false;
      setTable(null);
      setSelectedRows(new Set());
      setEditingCell(null);
      api
        .dbTable(path, name, {
          projectRoot,
          host: projectHost,
          limit: PAGE_SIZE,
          offset: off,
          filter: rowFilter || undefined,
          sort: sortBy || undefined,
          dir: sortDesc ? "desc" : "asc",
          // A count is only worth its second COUNT query once the user is
          // actually narrowing the view or reordering it.
          count: Boolean(rowFilter || sortBy),
        })
        .then((res) => {
          if (!cancelled) {
            setTable(res);
            setFilterError(null);
          }
        })
        .catch((e: unknown) => {
          if (cancelled) return;
          const msg = e instanceof Error ? e.message : String(e);
          // Only a filter/sort rejection is a filter problem; anything else is
          // the pane's own error state.
          if (rowFilter || sortBy) setFilterError(msg);
          else setError(msg);
        });
      return () => {
        cancelled = true;
      };
    },
    [path, projectRoot, projectHost, rowFilter, sortBy, sortDesc],
  );

  useEffect(() => {
    if (selected) return loadTable(selected, offset);
    setTable(null);
    return undefined;
  }, [selected, offset, loadTable, revision, bump]);

  // Apply the filter after a pause in typing, and always return to the first
  // page: page 7 of the old result set is meaningless under a new filter.
  useEffect(() => {
    const trimmed = filterDraft.trim();
    if (trimmed === rowFilter) return undefined;
    const t = setTimeout(() => {
      setRowFilter(trimmed);
      setOffset(0);
    }, 300);
    return () => clearTimeout(t);
  }, [filterDraft, rowFilter]);

  // Changing the filter or sort invalidates a row selection: the row indices it
  // referred to no longer mean the same rows.
  useEffect(() => {
    setSelectedRows(new Set());
  }, [rowFilter, sortBy, sortDesc]);

  // Clicking the active column flips the direction; clicking another column
  // starts ascending. Derived from the current values rather than an updater so
  // no state update happens inside another one.
  const toggleSort = useCallback(
    (column: string) => {
      setOffset(0);
      if (column === sortBy) setSortDesc((d) => !d);
      else {
        setSortBy(column);
        setSortDesc(false);
      }
    },
    [sortBy],
  );

  const runQuery = useCallback(() => {
    const sql = queryText.trim();
    if (!sql) return;
    setRunning(true);
    setQueryError(null);
    api
      .dbQuery(path, sql, { projectRoot, host: projectHost })
      .then((res) => setQueryResult(res))
      .catch((e: unknown) => {
        setQueryResult(null);
        if (e instanceof ApiError && e.status === 409) {
          // A write needs explicit confirmation: show the SQL and retry through
          // dbExec (POST /api/db/query with confirm:true).
          setPendingWrite(sql);
          return;
        }
        setQueryError(e instanceof Error ? e.message : String(e));
      })
      .finally(() => setRunning(false));
  }, [queryText, path, projectRoot, projectHost]);

  // ── Row identity + write handlers (P2–P4) ─────────────────────────────────

  // The columns that identify a row: the declared primary key, or the synthetic
  // _rowid_ the server adds for a table with none. Empty ⇒ the grid is
  // read-only (a view, or a WITHOUT ROWID table with no primary key).
  const keyColumns = useMemo(() => {
    if (!table) return [];
    const pk = table.schema.columns.filter((c) => c.pk > 0).map((c) => c.name);
    if (pk.length > 0) return pk;
    if (table.result.columns.some((c) => c.name === "_rowid_")) return ["_rowid_"];
    return [];
  }, [table]);
  const editable = keyColumns.length > 0 && table?.schema.type === "table";

  // Prefer the server's EXACT key for a row. Rebuilding one from the cells is
  // lossy for an integer id past 2^53 — JSON.parse has already rounded it, so
  // two adjacent ids become the same number and the mutation would hit the
  // neighbouring row. `row_keys` carries those values as strings; a null entry
  // means the server could not express that row's key, so we fall back.
  const rowKey = useCallback(
    (row: DBCell[], rowIndex?: number): Record<string, DBCell> => {
      if (rowIndex != null) {
        const exact = table?.row_keys?.[rowIndex];
        if (exact) return exact;
      }
      const obj: Record<string, DBCell> = {};
      keyColumns.forEach((name) => {
        const idx = table?.result.columns.findIndex((c) => c.name === name) ?? -1;
        if (idx >= 0) obj[name] = row[idx];
      });
      return obj;
    },
    [keyColumns, table],
  );

  // The size/truncation the grid already knows about a blob cell, used by the
  // dialog for its pre-fetch notice. The value itself is fetched by the dialog.
  const blobCellInfo = useCallback(
    (row: DBCell[], col: number): { bytes: number; truncated: boolean } => {
      const cell = row?.[col];
      if (cell && typeof cell === "object" && "$blob" in cell) {
        return { bytes: cell.bytes, truncated: Boolean(cell.truncated) };
      }
      return { bytes: 0, truncated: false };
    },
    [],
  );

  const rowByName = useCallback(
    (row: DBCell[]): Record<string, DBCell> => {
      const obj: Record<string, DBCell> = {};
      table?.result.columns.forEach((c, i) => {
        obj[c.name] = row[i];
      });
      return obj;
    },
    [table],
  );

  const askDeleteRow = (ri: number) => {
    if (!table || !selected) return;
    const row = table.result.rows[ri];
    setConfirm({
      title: `Delete row from ${selected}?`,
      message: "This permanently deletes the row.",
      label: "Delete",
      run: async () => {
        await api.dbRow(path, "delete", selected, {
          projectRoot,
          host: projectHost,
          key: rowKey(row, ri),
        });
        setNotice(`Deleted 1 row from ${selected}.`);
        setBump((b) => b + 1);
      },
    });
  };

  const saveRow = async (values: Record<string, DBCellInput>) => {
    if (!table || !selected || !rowEdit) return;
    setRowBusy(true);
    setRowError(null);
    try {
      if (rowEdit.index == null) {
        await api.dbRow(path, "insert", selected, { projectRoot, host: projectHost, values });
        setNotice(`Inserted 1 row into ${selected}.`);
      } else {
        const row = table.result.rows[rowEdit.index];
        await api.dbRow(path, "update", selected, {
          projectRoot,
          host: projectHost,
          key: rowKey(row, rowEdit.index),
          values,
        });
        setNotice(`Updated 1 row in ${selected}.`);
      }
      setRowEdit(null);
      setBump((b) => b + 1);
    } catch (e) {
      setRowError(e instanceof Error ? e.message : String(e));
    } finally {
      setRowBusy(false);
    }
  };

  // ── Inline cell editing ───────────────────────────────────────────────────
  // Double-click a cell to edit it in place; Enter commits, Escape abandons. A
  // commit reuses the row UPDATE endpoint with the row's key, so an inline edit
  // is the same optimistic-concurrency checked mutation as the dialog — a row
  // that changed underneath answers 409 instead of overwriting it.
  const commitCell = useCallback(
    async (rowIndex: number, colIndex: number, text: string) => {
      if (!table || !editingCell) return;
      const name = table.result.columns[colIndex]?.name;
      if (!name) return;
      const declType =
        table.schema.columns.find((c) => c.name === name)?.decl_type ??
        table.result.columns[colIndex]?.decl_type ??
        "";
      const row = table.result.rows[rowIndex];
      setInlineBusy(true);
      setRowError(null);
      try {
        await api.dbRow(path, "update", selected as string, {
          projectRoot,
          host: projectHost,
          key: rowKey(row, rowIndex),
          values: { [name]: inputToCell(text, declType) },
        });
        setNotice(`Updated ${name}.`);
        setEditingCell(null);
        setBump((b) => b + 1);
      } catch (e) {
        setRowError(e instanceof Error ? e.message : String(e));
      } finally {
        setInlineBusy(false);
      }
    },
    [table, editingCell, selected, path, projectRoot, projectHost, rowKey],
  );

  // ── Bulk delete ───────────────────────────────────────────────────────────
  // The confirmation names the row COUNT, not "the selected rows": a bulk delete
  // is the operation most likely to be applied without counting what it covers.
  const askDeleteSelected = () => {
    if (!table || !selected || selectedRows.size === 0) return;
    const rows = [...selectedRows].sort((a, b) => a - b);
    setConfirm({
      title: `Delete ${rows.length} row${rows.length === 1 ? "" : "s"} from ${selected}?`,
      message:
        rows.length === 1
          ? "This permanently deletes the row."
          : "This permanently deletes every selected row. A backup of the database is taken first.",
      label: "Delete",
      run: async () => {
        let deleted = 0;
        const failures: string[] = [];
        // Sequential on purpose: these are writes to one SQLite file, and a
        // burst of parallel writes is exactly how you get "database is locked".
        for (const ri of rows) {
          const row = table.result.rows[ri];
          if (!row) continue;
          try {
            await api.dbRow(path, "delete", selected as string, {
              projectRoot,
              host: projectHost,
              key: rowKey(row, ri),
            });
            deleted += 1;
          } catch (e) {
            failures.push(e instanceof Error ? e.message : String(e));
          }
        }
        // No explicit clear here: the refetch below rebuilds the page and
        // loadTable resets the selection, which is where indices stop being
        // meaningful. Clearing it twice would be a second source of truth.
        if (failures.length === 0) {
          setNotice(`Deleted ${deleted} row${deleted === 1 ? "" : "s"} from ${selected}.`);
        } else {
          // A partial delete must say so plainly: "Deleted 2 of 5" plus the
          // reason, never a bare success message over a half-applied change.
          setNotice(
            `Deleted ${deleted} of ${rows.length} row${rows.length === 1 ? "" : "s"}. ${failures[0]}`,
          );
        }
        setBump((b) => b + 1);
      },
    });
  };

  // ── Maintenance ───────────────────────────────────────────────────────────
  const runMaintenance = useCallback(
    async (op: "analyze" | "vacuum" | "integrity_check") => {
      setMaintenanceBusy(true);
      try {
        const res = await api.dbMaintenance(path, op, { projectRoot, host: projectHost });
        if (op === "integrity_check") {
          const rows = res.rows ?? [];
          const clean = rows.length === 1 && rows[0].toLowerCase() === "ok";
          setNotice(clean ? "Integrity check: ok." : `Integrity check found problems:\n${rows.join("\n")}`);
        } else if (op === "analyze") {
          setNotice("Analyzed — the table list now shows real row counts.");
          setBump((b) => b + 1);
        } else {
          setNotice("Vacuum complete — the file was rewritten to reclaim space.");
        }
      } catch (e) {
        setNotice(e instanceof Error ? e.message : String(e));
      } finally {
        setMaintenanceBusy(false);
      }
    },
    [path, projectRoot, projectHost],
  );

  const askVacuum = () => {
    // VACUUM rewrites the entire file, so it gets a confirmation like a drop.
    setConfirm({
      title: "Vacuum this database?",
      message: "This rewrites the whole file to reclaim unused space. It can take a while on a large database.",
      label: "Vacuum",
      run: async () => {
        await runMaintenance("vacuum");
      },
    });
  };

  const exportCsv = () => {
    if (!table || !selected) return;
    downloadCsv(csvFilename(selected), resultToCsv(table.result));
    setNotice(`Exported ${table.result.row_count} row(s) to CSV.`);
  };

  const runConfirm = async () => {
    if (!confirm) return;
    setConfirmBusy(true);
    try {
      await confirm.run();
      setConfirm(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setConfirm(null);
    } finally {
      setConfirmBusy(false);
    }
  };

  const askDropTable = () => {
    if (!selected) return;
    setConfirm({
      title: `Drop table ${selected}?`,
      message: "This permanently deletes the table and every row in it.",
      label: "Drop table",
      run: async () => {
        await api.dbSchema(path, "drop_table", { projectRoot, host: projectHost, table: selected });
        setNotice(`Dropped table ${selected}.`);
        setSelected(null);
        setBump((b) => b + 1);
      },
    });
  };

  const askDropIndex = (index: string) => {
    setConfirm({
      title: `Drop index ${index}?`,
      message: "This permanently deletes the index.",
      label: "Drop index",
      run: async () => {
        await api.dbSchema(path, "drop_index", { projectRoot, host: projectHost, index });
        setNotice(`Dropped index ${index}.`);
        setBump((b) => b + 1);
      },
    });
  };

  const submitSchema = async (payload: {
    column?: DBColumnDef;
    columns?: DBColumnDef[];
    table?: string;
    index?: string;
    indexColumns?: string[];
    unique?: boolean;
  }) => {
    if (!schemaAction) return;
    setSchemaBusy(true);
    setSchemaError(null);
    try {
      await api.dbSchema(path, schemaAction, {
        projectRoot,
        host: projectHost,
        table: payload.table ?? selected ?? undefined,
        column: payload.column,
        columns: payload.columns,
        index: payload.index,
        indexColumns: payload.indexColumns,
        unique: payload.unique,
      });
      setNotice("Schema updated.");
      setSchemaAction(null);
      setBump((b) => b + 1);
    } catch (e) {
      setSchemaError(e instanceof Error ? e.message : String(e));
    } finally {
      setSchemaBusy(false);
    }
  };

  const confirmQueryWrite = async () => {
    const sql = pendingWrite;
    if (!sql) return;
    setRunning(true);
    setQueryError(null);
    try {
      const res = await api.dbExec(path, sql, { projectRoot, host: projectHost });
      setNotice(`Statement applied — ${res.rows_affected} row(s) affected.`);
      setQueryResult(null);
      setPendingWrite(null);
      setBump((b) => b + 1);
    } catch (e) {
      setQueryError(e instanceof Error ? e.message : String(e));
      setPendingWrite(null);
    } finally {
      setRunning(false);
    }
  };

  const rowActions = (ri: number) => (
    <div className="flex items-center justify-end gap-1">
      <button
        type="button"
        aria-label={`Edit row ${ri + 1}`}
        onClick={() => {
          setRowError(null);
          setRowEdit({ index: ri });
        }}
        className="rounded p-0.5 hover:bg-muted"
      >
        <Pencil className="h-3 w-3" />
      </button>
      <button
        type="button"
        aria-label={`Delete row ${ri + 1}`}
        onClick={() => askDeleteRow(ri)}
        className="rounded p-0.5 text-destructive hover:bg-muted"
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  );

  const tables = useMemo(() => info?.tables ?? [], [info]);
  const visibleTables = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return tables;
    return tables.filter((t) => t.name.toLowerCase().includes(needle));
  }, [tables, filter]);

  if (loading && !info && !error) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Reading database…
      </div>
    );
  }

  if (error && !info) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-4 text-center text-xs text-muted-foreground" data-testid="sqlite-error">
        <Database className="h-6 w-6" />
        <p className="text-destructive">Could not open this database.</p>
        <p className="break-all">{error}</p>
      </div>
    );
  }

  if (info && !info.is_sqlite) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-center text-xs text-muted-foreground" data-testid="sqlite-fallback">
        <Database className="h-6 w-6" />
        <p>This file is not a SQLite database.</p>
        {!projectHost && (
          <button
            type="button"
            onClick={() => api.openFileWithOS(path, projectRoot).catch(() => {})}
            className="inline-flex items-center gap-1 rounded border border-border px-2 py-1 hover:bg-muted"
          >
            <ExternalLink className="h-3 w-3" /> Open externally
          </button>
        )}
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="sqlite-viewer">
      {/* Header: table tabs + refresh */}
      <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1">
        <button
          type="button"
          onClick={tablePane.toggleCollapsed}
          aria-label={tablePane.collapsed ? "Show table list" : "Hide table list"}
          aria-expanded={!tablePane.collapsed}
          title={tablePane.collapsed ? "Show table list" : "Hide table list"}
          data-testid="sqlite-toggle-table-list"
          className="mr-1 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          {tablePane.collapsed ? (
            <PanelLeft className="h-3.5 w-3.5" />
          ) : (
            <PanelLeftClose className="h-3.5 w-3.5" />
          )}
        </button>
        {(["data", "query", "schema"] as Tab[]).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            aria-pressed={tab === t}
            className={cn(
              "rounded px-2 py-0.5 text-xs capitalize",
              tab === t ? "bg-muted font-medium" : "text-muted-foreground hover:bg-muted/50",
            )}
          >
            {t}
          </button>
        ))}
        <div className="ml-auto flex items-center gap-1 text-[11px] text-muted-foreground">
          {tables.length} {tables.length === 1 ? "table" : "tables"}
        </div>
      </div>

      <div className="flex min-h-0 min-w-0 flex-1">
        {/* Table list: width is drag-resizable and persisted (`tablePane`).
            The header toggle collapses it to zero; `overflow-hidden` clips the
            content while the width animates. */}
        <div
          data-testid="sqlite-table-pane"
          className="flex shrink-0 flex-col overflow-hidden border-r border-border transition-[width] duration-100"
          style={{ width: tablePane.collapsed ? 0 : tablePane.width }}
        >
          <div className="relative shrink-0 p-1">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter tables"
              aria-label="Filter tables"
              className="w-full rounded border border-border bg-transparent py-0.5 pl-6 pr-1 text-xs"
            />
          </div>
          <div className="min-h-0 flex-1 overflow-auto" data-testid="sqlite-table-list">
            {visibleTables.map((t) => (
              <button
                key={t.name}
                type="button"
                onClick={() => {
                  setSelected(t.name);
                  setOffset(0);
                  setTab("data");
                }}
                className={cn(
                  "flex w-full items-center justify-between gap-1 px-2 py-0.5 text-left text-xs hover:bg-muted",
                  selected === t.name && "bg-muted",
                )}
                title={t.type}
              >
                <span className="truncate" title={t.name}>
                  {t.name}
                </span>
                <span className="shrink-0 text-[10px] text-muted-foreground">
                  {t.type === "view" ? "view" : t.rows >= 0 ? t.rows : ""}
                </span>
              </button>
            ))}
            {visibleTables.length === 0 && (
              <p className="p-2 text-[11px] text-muted-foreground">No tables.</p>
            )}
          </div>
        </div>

        {/* Drag handle between the table list and the main pane. Double-click
            restores the default width; hidden while collapsed. */}
        {!tablePane.collapsed && (
          <div
            ref={tablePane.handleRef}
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize table list"
            title="Drag to resize · double-click to reset"
            onPointerDown={tablePane.onPointerDown}
            onDoubleClick={tablePane.resetToDefault}
            className="w-1 shrink-0 cursor-col-resize touch-none bg-border hover:bg-accent active:bg-accent"
          />
        )}

        {/* Main pane */}
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {tab === "data" && (
            <>
              {!selected ? (
                <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
                  Select a table to browse its rows.
                </div>
              ) : (
                <>
                  <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border px-2 py-1 text-xs">
                    <span className="font-medium">{selected}</span>
                    <span className="text-muted-foreground">
                      rows {table ? offset + 1 : offset}–
                      {table ? offset + table.result.row_count : offset}
                      {table?.result.truncated ? "+" : ""}
                      {typeof table?.total === "number" ? (
                        <span data-testid="sqlite-total"> of {table.total}</span>
                      ) : null}
                    </span>
                    <input
                      value={filterDraft}
                      onChange={(e) => setFilterDraft(e.target.value)}
                      placeholder="WHERE …"
                      aria-label="Filter rows"
                      spellCheck={false}
                      className="min-w-[8rem] flex-1 rounded border border-border bg-transparent px-1.5 py-0.5 font-mono text-[11px]"
                    />
                    <div className="ml-auto flex items-center gap-1">
                      <button
                        type="button"
                        onClick={exportCsv}
                        disabled={!table}
                        data-testid="sqlite-export-csv"
                        title="Export this page as CSV"
                        className="inline-flex items-center gap-1 rounded border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted disabled:opacity-40"
                      >
                        <Download className="h-3 w-3" /> CSV
                      </button>
                      {editable && selectedRows.size > 0 ? (
                        <button
                          type="button"
                          onClick={askDeleteSelected}
                          data-testid="sqlite-delete-selected"
                          className="inline-flex items-center gap-1 rounded border border-destructive px-1.5 py-0.5 text-[11px] text-destructive hover:bg-muted"
                        >
                          <Trash2 className="h-3 w-3" /> Delete {selectedRows.size}
                        </button>
                      ) : null}
                      {editable ? (
                        <button
                          type="button"
                          onClick={() => {
                            setRowError(null);
                            setRowEdit({ index: null });
                          }}
                          data-testid="sqlite-add-row"
                          className="inline-flex items-center gap-1 rounded border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted"
                        >
                          <Plus className="h-3 w-3" /> Add row
                        </button>
                      ) : null}
                      <button
                        type="button"
                        aria-label="Previous page"
                        disabled={offset === 0}
                        onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                        className="rounded p-0.5 hover:bg-muted disabled:opacity-40"
                      >
                        <ChevronLeft className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        aria-label="Next page"
                        disabled={!table?.result.truncated}
                        onClick={() => setOffset(offset + PAGE_SIZE)}
                        className="rounded p-0.5 hover:bg-muted disabled:opacity-40"
                      >
                        <ChevronRight className="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                  {filterError ? (
                    <div
                      role="alert"
                      data-testid="sqlite-filter-error"
                      className="border-b border-border px-2 py-1 text-[11px] text-destructive"
                    >
                      {filterError}
                    </div>
                  ) : null}
                  {rowError ? (
                    <div
                      role="alert"
                      data-testid="sqlite-inline-error"
                      className="border-b border-border px-2 py-1 text-[11px] text-destructive"
                    >
                      {rowError}
                    </div>
                  ) : null}
                  {table ? (
                    <ResultGrid
                      result={table.result}
                      testId="sqlite-data-grid"
                      renderRowActions={editable ? rowActions : undefined}
                      sortable
                      sortBy={sortBy}
                      sortDesc={sortDesc}
                      onSort={toggleSort}
                      selectable={editable}
                      selectedRows={selectedRows}
                      onToggleRow={(ri) =>
                        setSelectedRows((prev) => {
                          const next = new Set(prev);
                          if (next.has(ri)) next.delete(ri);
                          else next.add(ri);
                          return next;
                        })
                      }
                      editing={editingCell}
                      inlineBusy={inlineBusy}
                      onStartEdit={(r, c) => {
                        setRowError(null);
                        setEditingCell({ row: r, col: c });
                      }}
                      onCommitEdit={commitCell}
                      onCancelEdit={() => {
                        setEditingCell(null);
                        setRowError(null);
                      }}
                      onOpenBlob={(r, c) => setBlobCell({ row: r, col: c })}
                    />
                  ) : (
                    <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
                      <Loader2 className="h-4 w-4 animate-spin" />
                    </div>
                  )}
                </>
              )}
            </>
          )}

          {tab === "query" && (
            <>
              <div className="shrink-0 border-b border-border p-1">
                <textarea
                  value={queryText}
                  onChange={(e) => setQueryText(e.target.value)}
                  onKeyDown={(e) => {
                    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                      e.preventDefault();
                      runQuery();
                    }
                  }}
                  spellCheck={false}
                  rows={4}
                  aria-label="SQL query"
                  data-testid="sqlite-query-input"
                  className="w-full resize-y rounded border border-border bg-transparent p-2 font-mono text-xs"
                />
                <div className="mt-1 flex items-center gap-2">
                  <button
                    type="button"
                    onClick={runQuery}
                    disabled={running}
                    data-testid="sqlite-run-query"
                    className="inline-flex items-center gap-1 rounded bg-primary px-2 py-0.5 text-xs text-primary-foreground hover:opacity-90 disabled:opacity-50"
                  >
                    {running ? (
                      <Loader2 className="h-3 w-3 animate-spin" />
                    ) : (
                      <Play className="h-3 w-3" />
                    )}
                    Run
                  </button>
                  <span className="text-[11px] text-muted-foreground">⌘/Ctrl+Enter</span>
                </div>
              </div>
              {queryError ? (
                <div className="p-3 text-xs text-destructive" data-testid="sqlite-query-error">
                  {queryError}
                </div>
              ) : queryResult ? (
                <ResultGrid result={queryResult} testId="sqlite-query-grid" />
              ) : (
                <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
                  Run a query to see results.
                </div>
              )}
            </>
          )}

          {tab === "schema" &&
            (table?.schema ? (
              <div className="flex min-h-0 flex-1 flex-col">
                <div className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border px-2 py-1 text-xs">
                  <button
                    type="button"
                    onClick={() => {
                      setSchemaError(null);
                      setSchemaAction("add_column");
                    }}
                    data-testid="sqlite-add-column"
                    className="rounded border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted"
                  >
                    Add column
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setSchemaError(null);
                      setSchemaAction("create_table");
                    }}
                    className="rounded border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted"
                  >
                    Create table
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setSchemaError(null);
                      setSchemaAction("create_index");
                    }}
                    className="rounded border border-border px-1.5 py-0.5 text-[11px] hover:bg-muted"
                  >
                    Create index
                  </button>
                  <button
                    type="button"
                    onClick={askDropTable}
                    className="rounded border border-border px-1.5 py-0.5 text-[11px] text-destructive hover:bg-muted"
                  >
                    Drop table
                  </button>
                </div>
                <SchemaPane schema={table.schema} onDropIndex={askDropIndex} />
              </div>
            ) : selected ? (
              <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
              </div>
            ) : (
              <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
                Select a table to see its schema.
              </div>
            ))}
        </div>
      </div>

      {/* Refresh is implicit on revision; expose an explicit control too. */}
      <div className="flex shrink-0 flex-wrap items-center gap-1 border-t border-border px-2 py-0.5 text-[11px] text-muted-foreground">
        <RefreshCw className="h-3 w-3" />
        {editable ? "writes enabled" : "read-only"}
        <button
          type="button"
          onClick={() => runMaintenance("analyze")}
          disabled={maintenanceBusy}
          data-testid="sqlite-analyze"
          title="Populate sqlite_stat1 so the table list shows real row counts"
          className="inline-flex items-center gap-0.5 rounded px-1 hover:bg-muted disabled:opacity-40"
        >
          <Gauge className="h-3 w-3" /> Analyze
        </button>
        <button
          type="button"
          onClick={() => runMaintenance("integrity_check")}
          disabled={maintenanceBusy}
          data-testid="sqlite-integrity"
          title="Check the file for corruption"
          className="inline-flex items-center gap-0.5 rounded px-1 hover:bg-muted disabled:opacity-40"
        >
          <ShieldCheck className="h-3 w-3" /> Check
        </button>
        <button
          type="button"
          onClick={askVacuum}
          disabled={maintenanceBusy}
          data-testid="sqlite-vacuum"
          title="Rewrite the file to reclaim unused space"
          className="inline-flex items-center gap-0.5 rounded px-1 hover:bg-muted disabled:opacity-40"
        >
          <Undo2 className="h-3 w-3" /> Vacuum
        </button>
        {notice ? (
          <span className="ml-2 whitespace-pre-line text-foreground" data-testid="sqlite-notice">
            {notice}
          </span>
        ) : null}
      </div>

      {confirm ? (
        <ConfirmDialog
          open
          title={confirm.title}
          message={confirm.message}
          confirmLabel={confirm.label}
          busy={confirmBusy}
          onCancel={() => setConfirm(null)}
          onConfirm={runConfirm}
        />
      ) : null}
      {rowEdit && table ? (
        <RowEditorDialog
          open
          schema={table.schema}
          initial={rowEdit.index == null ? null : rowByName(table.result.rows[rowEdit.index])}
          busy={rowBusy}
          error={rowError}
          onCancel={() => setRowEdit(null)}
          onSubmit={saveRow}
        />
      ) : null}
      {blobCell && table ? (
        <BlobDialog
          open
          path={path}
          table={selected ?? ""}
          column={table.result.columns[blobCell.col]?.name ?? ""}
          rowKey={rowKey(table.result.rows[blobCell.row], blobCell.row)}
          cell={blobCellInfo(table.result.rows[blobCell.row], blobCell.col)}
          editable={editable}
          projectRoot={projectRoot}
          projectHost={projectHost}
          onClose={() => setBlobCell(null)}
          onReplaced={() => setBump((b) => b + 1)}
        />
      ) : null}
      {schemaAction && table ? (
        <SchemaDialog
          open
          action={schemaAction}
          schema={table.schema}
          busy={schemaBusy}
          error={schemaError}
          onCancel={() => setSchemaAction(null)}
          onSubmit={submitSchema}
        />
      ) : null}
      <ConfirmDialog
        open={pendingWrite != null}
        title="Run this write?"
        message={<pre className="whitespace-pre-wrap font-mono text-[11px]">{pendingWrite}</pre>}
        confirmLabel="Run"
        destructive={false}
        busy={running}
        onCancel={() => setPendingWrite(null)}
        onConfirm={confirmQueryWrite}
      />
    </div>
  );
}
