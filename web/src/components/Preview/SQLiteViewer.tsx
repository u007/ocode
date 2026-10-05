import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  ChevronLeft,
  ChevronRight,
  Database,
  ExternalLink,
  Loader2,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Search,
  Trash2,
} from "lucide-react";
import {
  api,
  ApiError,
  type DBCell,
  type DBColumnDef,
  type DBInfo,
  type DBResultSet,
  type DBTableResponse,
  type DBTableSchema,
} from "../../api/client";
import { cn } from "../../lib/utils";
import { ConfirmDialog, RowEditorDialog, SchemaDialog, type SchemaAction } from "./SQLiteDialogs";

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
function Cell({ value }: { value: DBCell }) {
  if (value === null) {
    return <span className="italic text-muted-foreground">NULL</span>;
  }
  if (typeof value === "object" && value !== null && "$blob" in value) {
    return (
      <span
        className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground"
        title={value.preview}
      >
        blob {value.bytes}B
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
}: {
  result: DBResultSet;
  testId: string;
  renderRowActions?: (rowIndex: number) => ReactNode;
}) {
  if (result.columns.length === 0) {
    return (
      <div className="p-3 text-xs text-muted-foreground">
        Statement executed — no rows returned.
      </div>
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-auto" data-testid={testId}>
      <table className="w-full border-collapse text-left">
        <thead className="sticky top-0 bg-muted/80 backdrop-blur">
          <tr>
            {result.columns.map((c, i) => (
              <th
                key={`${c.name}-${i}`}
                className="whitespace-nowrap border-b border-border px-2 py-1 text-[11px] font-semibold"
              >
                {c.name}
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
            <tr key={ri} className="odd:bg-muted/20">
              {row.map((cell, ci) => (
                <td
                  key={ci}
                  className="max-w-[24rem] truncate border-b border-border/50 px-2 py-1 align-top"
                >
                  <Cell value={cell} />
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
  const [schemaAction, setSchemaAction] = useState<SchemaAction | null>(null);
  const [schemaBusy, setSchemaBusy] = useState(false);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [pendingWrite, setPendingWrite] = useState<string | null>(null);

  const [queryText, setQueryText] = useState("SELECT * FROM ");
  const [queryResult, setQueryResult] = useState<DBResultSet | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [running, setRunning] = useState(false);

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

  // Load a page of the selected table.
  const loadTable = useCallback(
    (name: string, off: number) => {
      let cancelled = false;
      setTable(null);
      api
        .dbTable(path, name, { projectRoot, host: projectHost, limit: PAGE_SIZE, offset: off })
        .then((res) => {
          if (!cancelled) setTable(res);
        })
        .catch((e: unknown) => {
          if (!cancelled) setError(e instanceof Error ? e.message : String(e));
        });
      return () => {
        cancelled = true;
      };
    },
    [path, projectRoot, projectHost],
  );

  useEffect(() => {
    if (selected) return loadTable(selected, offset);
    setTable(null);
    return undefined;
  }, [selected, offset, loadTable, revision, bump]);

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

  const rowKey = useCallback(
    (row: DBCell[]): Record<string, DBCell> => {
      const obj: Record<string, DBCell> = {};
      keyColumns.forEach((name) => {
        const idx = table?.result.columns.findIndex((c) => c.name === name) ?? -1;
        if (idx >= 0) obj[name] = row[idx];
      });
      return obj;
    },
    [keyColumns, table],
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
          key: rowKey(row),
        });
        setNotice(`Deleted 1 row from ${selected}.`);
        setBump((b) => b + 1);
      },
    });
  };

  const saveRow = async (values: Record<string, DBCell>) => {
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
          key: rowKey(row),
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

      <div className="flex min-h-0 flex-1">
        {/* Table list */}
        <div className="flex w-40 shrink-0 flex-col border-r border-border">
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
                <span className="truncate">{t.name}</span>
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

        {/* Main pane */}
        <div className="flex min-h-0 flex-1 flex-col">
          {tab === "data" && (
            <>
              {!selected ? (
                <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
                  Select a table to browse its rows.
                </div>
              ) : (
                <>
                  <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1 text-xs">
                    <span className="font-medium">{selected}</span>
                    <span className="text-muted-foreground">
                      rows {table ? offset + 1 : offset}–
                      {table ? offset + table.result.row_count : offset}
                      {table?.result.truncated ? "+" : ""}
                    </span>
                    <div className="ml-auto flex items-center gap-1">
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
                  {table ? (
                    <ResultGrid
                      result={table.result}
                      testId="sqlite-data-grid"
                      renderRowActions={editable ? rowActions : undefined}
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
      <div className="flex shrink-0 items-center gap-1 border-t border-border px-2 py-0.5 text-[11px] text-muted-foreground">
        <RefreshCw className="h-3 w-3" />
        {editable ? "writes enabled" : "read-only"}
        {notice ? (
          <span className="ml-2 text-foreground" data-testid="sqlite-notice">
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
