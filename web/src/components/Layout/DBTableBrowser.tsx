// Data view for one Postgres table: a paged grid with sort, a read-only filter,
// and row insert/update/delete. Row writes go through the server's keyed
// endpoint; the dialogs are the SQLite viewer's, with the Postgres converter.
// The parent renders this with key={table}, so sort, filter and paging reset per table.
//
// The toolbar stays mounted through a failed load, so a bad filter can be fixed
// in place. Only the grid area changes state (loading, error, rows).
import { ChevronLeft, ChevronRight, Loader2, Pencil, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import {
  api,
  type DBCell,
  type DBCellInput,
  type DBConnectBrowsePage,
  type DBTableSchema,
} from "../../api/client";
import { ConfirmDialog, RowEditorDialog } from "../Preview/SQLiteDialogs";
import { ResultGrid } from "../Preview/SQLiteViewer";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { pgInputToCell, toCell } from "./dbConnectCells";

const PAGE_SIZE = 100;

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export default function DBTableBrowser({
  surface,
  connection,
  table,
}: {
  surface: string;
  connection: string;
  table: string;
}) {
  const [page, setPage] = useState<DBConnectBrowsePage | null>(null);
  const [offset, setOffset] = useState(0);
  const [sort, setSort] = useState("");
  const [desc, setDesc] = useState(false);
  const [filterDraft, setFilterDraft] = useState("");
  const [filter, setFilter] = useState("");
  // Bumped after every write so the page reloads from the server.
  const [version, setVersion] = useState(0);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const [rowEdit, setRowEdit] = useState<{ index: number | null } | null>(null);
  const [rowBusy, setRowBusy] = useState(false);
  const [rowError, setRowError] = useState<string | null>(null);
  const [deleteIndex, setDeleteIndex] = useState<number | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(null);
    api
      .dbConnectRows(surface, connection, table, {
        sort,
        dir: desc ? "desc" : "asc",
        filter,
        limit: PAGE_SIZE,
        offset,
      })
      .then((res) => {
        if (!cancelled) setPage(res);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setPage(null);
          setLoadError(errorText(err));
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [surface, connection, table, sort, desc, filter, offset, version]);

  const keyless = page !== null && page.primaryKey.length === 0;

  // The key of a row is its primary-key values exactly as the server sent them,
  // so a bigint over 2^53 goes back as the same digits.
  const keyOf = (p: DBConnectBrowsePage, index: number): Record<string, unknown> => {
    const row = p.rows[index];
    const key: Record<string, unknown> = {};
    for (const name of p.primaryKey) {
      key[name] = row[p.columns.findIndex((c) => c.name === name)];
    }
    return key;
  };

  const schemaOf = (p: DBConnectBrowsePage): DBTableSchema => ({
    name: table,
    type: "table",
    columns: p.columns.map((c) => ({
      name: c.name,
      decl_type: c.type,
      not_null: false,
      pk: c.pk,
      generated: false,
    })),
    indexes: [],
    foreign_keys: [],
    ddl: "",
    rowid: false,
  });

  const initialFor = (p: DBConnectBrowsePage, index: number): Record<string, DBCell> => {
    const row = p.rows[index];
    const out: Record<string, DBCell> = {};
    p.columns.forEach((c, i) => {
      out[c.name] = toCell(row[i]);
    });
    return out;
  };

  const applyFilter = () => {
    setFilter(filterDraft);
    setOffset(0);
  };

  const toggleSort = (column: string) => {
    if (sort === column) {
      setDesc((d) => !d);
    } else {
      setSort(column);
      setDesc(false);
    }
    setOffset(0);
  };

  const saveRow = async (values: Record<string, DBCellInput>) => {
    if (!rowEdit || !page) return;
    setRowBusy(true);
    setRowError(null);
    try {
      if (rowEdit.index == null) {
        await api.dbConnectRow(surface, connection, "insert", table, {}, values);
        setNotice("Inserted 1 row.");
      } else {
        await api.dbConnectRow(surface, connection, "update", table, keyOf(page, rowEdit.index), values);
        setNotice("Updated 1 row.");
      }
      setRowEdit(null);
      setVersion((v) => v + 1);
    } catch (err: unknown) {
      setRowError(errorText(err));
    } finally {
      setRowBusy(false);
    }
  };

  const confirmDelete = async () => {
    if (deleteIndex == null || !page) return;
    setRowBusy(true);
    setActionError(null);
    try {
      await api.dbConnectRow(surface, connection, "delete", table, keyOf(page, deleteIndex), {});
      setNotice("Deleted 1 row.");
      setDeleteIndex(null);
      setVersion((v) => v + 1);
    } catch (err: unknown) {
      setActionError(errorText(err));
      setDeleteIndex(null);
    } finally {
      setRowBusy(false);
    }
  };

  const first = page && page.rows.length > 0 ? offset + 1 : 0;
  const last = page ? offset + page.rows.length : 0;
  const shownError = loadError ?? actionError;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <form
          className="flex min-w-0 flex-1 items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            applyFilter();
          }}
        >
          <Input
            aria-label="Filter rows"
            placeholder="Read-only condition, e.g. amount > 10"
            value={filterDraft}
            onChange={(e) => setFilterDraft(e.target.value)}
            className="h-8 min-w-0 flex-1 font-mono text-xs"
          />
          <Button type="submit" size="sm" variant="outline" className="h-8" disabled={loading}>
            Apply
          </Button>
        </form>
        <Button
          size="sm"
          className="h-8"
          disabled={rowBusy || page === null}
          onClick={() => {
            setRowError(null);
            setRowEdit({ index: null });
          }}
        >
          <Plus className="mr-1 h-3.5 w-3.5" /> Add row
        </Button>
      </div>

      {keyless && (
        <div className="text-[11px] text-muted-foreground">
          This table has no primary key, so its rows are read-only here.
        </div>
      )}
      {notice && <div className="text-xs text-muted-foreground">{notice}</div>}
      {shownError && (
        <div role="alert" className="rounded-md bg-destructive/10 p-2 text-xs text-destructive">
          {shownError}
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-border">
        {page === null ? (
          <div className="flex flex-1 items-center justify-center p-4 text-xs text-muted-foreground">
            {loading && (
              <>
                <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> Loading rows…
              </>
            )}
          </div>
        ) : page.columns.length > 0 ? (
          <ResultGrid
            result={{
              columns: page.columns.map((c) => ({ name: c.name, decl_type: c.type })),
              rows: page.rows.map((row) => row.map(toCell)),
              row_count: page.rows.length,
              truncated: false,
              elapsed_ms: 0,
            }}
            testId="dbconnect-rows"
            sortable
            sortBy={sort}
            sortDesc={desc}
            onSort={toggleSort}
            renderRowActions={
              keyless
                ? undefined
                : (ri) => (
                    <div className="flex items-center justify-end gap-1">
                      <button
                        type="button"
                        aria-label={`Edit row ${offset + ri + 1}`}
                        className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                        onClick={() => {
                          setRowError(null);
                          setRowEdit({ index: ri });
                        }}
                      >
                        <Pencil className="h-3 w-3" />
                      </button>
                      <button
                        type="button"
                        aria-label={`Delete row ${offset + ri + 1}`}
                        className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-destructive"
                        onClick={() => setDeleteIndex(ri)}
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  )
            }
          />
        ) : null}
      </div>

      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <span>
          {page && page.rows.length === 0 ? "No rows" : page ? `Rows ${first}–${last}` : ""}
          {sort ? ` · sorted by ${sort} ${desc ? "desc" : "asc"}` : ""}
        </span>
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            className="h-7"
            aria-label="Previous page"
            disabled={offset === 0 || loading}
            onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="h-7"
            aria-label="Next page"
            disabled={!page?.hasMore || loading}
            onClick={() => setOffset((o) => o + PAGE_SIZE)}
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {rowEdit && page && (
        <RowEditorDialog
          open
          schema={schemaOf(page)}
          initial={rowEdit.index == null ? null : initialFor(page, rowEdit.index)}
          busy={rowBusy}
          error={rowError}
          parseInput={pgInputToCell}
          onCancel={() => setRowEdit(null)}
          onSubmit={saveRow}
        />
      )}
      <ConfirmDialog
        open={deleteIndex != null}
        title="Delete this row?"
        message={
          <p>
            The row is deleted in its own transaction, and only if its primary key matches exactly one row. PostgreSQL has no undo here.
          </p>
        }
        confirmLabel="Delete row"
        busy={rowBusy}
        onCancel={() => setDeleteIndex(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  );
}
