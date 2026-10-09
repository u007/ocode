// DB session sub-tab: Postgres connector. Saved connections are global
// (ocodeconfig.json, encrypted); each panel unlocks them under its own surface
// id and locks them on close. Every statement runs read-only first; a write is
// shown to the user and runs only after an explicit confirmation.
import { Database, Loader2, Lock, PanelLeft, PanelLeftClose, Play, Trash2, Unlock } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  api,
  type DBConnectConnection,
  type DBConnectQueryResult,
} from "../../api/client";
import { useResizableSidebar } from "../../hooks/useResizableSidebar";
import { cn } from "../../lib/utils";
import { ConfirmDialog } from "../Preview/SQLiteDialogs";
import { ResultGrid } from "../Preview/SQLiteViewer";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import DBTableBrowser from "./DBTableBrowser";
import { toResultSet } from "./dbConnectCells";

// Matches the server's default page size for the tables endpoint.
const TABLE_PAGE_SIZE = 100;

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// Table list width bounds, and the room side by side needs beyond the list: the
// resize handle, then the narrowest data pane the toolbar and grid stay usable in.
const LIST_MIN_PX = 120;
const LIST_MAX_PX = 480;
const LIST_DEFAULT_PX = 160;
const HANDLE_PX = 4;
const DATA_MIN_PX = 320;

export default function DBPanel({ sessionId }: { sessionId: string }) {
  // Stable per panel so the server's unlock grant belongs to this panel only.
  const surface = useMemo(() => `db-panel:${sessionId}`, [sessionId]);

  // Table-list pane width: drag-to-resize + collapse, persisted per panel kind.
  const tablePane = useResizableSidebar({
    storageKey: "ocode.ui.dbconnect-table-pane.width",
    defaultWidth: LIST_DEFAULT_PX,
    minWidth: LIST_MIN_PX,
    maxWidth: LIST_MAX_PX,
    collapsible: true,
  });

  // Measures the table-and-data area, not the viewport: the panel sits in a pane the
  // user can resize. Stacking is decided on the narrowest list, so a wide saved width
  // never locks the layout. Side by side, the list is clamped at render time to the
  // room that is left. Neither the saved width nor the collapsed state is changed.
  const [paneEl, setPaneEl] = useState<HTMLDivElement | null>(null);
  const [paneWidth, setPaneWidth] = useState<number | null>(null);
  useEffect(() => {
    if (!paneEl) return;
    const observer = new ResizeObserver(([entry]) => setPaneWidth(entry.contentRect.width));
    observer.observe(paneEl);
    return () => observer.disconnect();
  }, [paneEl]);
  const stacked =
    paneWidth !== null &&
    !tablePane.collapsed &&
    paneWidth < LIST_MIN_PX + HANDLE_PX + DATA_MIN_PX;
  const listWidth =
    paneWidth === null || stacked
      ? tablePane.width
      : Math.min(tablePane.width, paneWidth - HANDLE_PX - DATA_MIN_PX);

  const [connections, setConnections] = useState<DBConnectConnection[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [panelError, setPanelError] = useState<string | null>(null);

  const [addName, setAddName] = useState("");
  const [addUrl, setAddUrl] = useState("");
  const [addPassword, setAddPassword] = useState("");
  const [unlockPassword, setUnlockPassword] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null);

  const [tables, setTables] = useState<string[]>([]);
  const [tablesHasMore, setTablesHasMore] = useState(false);
  // The open table (Data view) and which right-pane tab is showing.
  const [activeTable, setActiveTable] = useState<string | null>(null);
  const [view, setView] = useState<"data" | "query">("data");
  const [sql, setSql] = useState("");
  const [result, setResult] = useState<DBConnectQueryResult | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  // The statement the server refused as a write, awaiting the user's confirmation.
  const [pendingWrite, setPendingWrite] = useState<string | null>(null);

  const current = connections?.find((c) => c.name === selected) ?? null;
  const unlocked = current?.unlocked ?? false;

  const refresh = useCallback(async () => {
    const res = await api.dbConnectList(surface);
    setConnections(res.connections);
    setSelected((prev) => {
      if (prev && res.connections.some((c) => c.name === prev)) return prev;
      return res.connections[0]?.name ?? null;
    });
  }, [surface]);

  // offset 0 starts a fresh list; a later offset appends the next page.
  const loadTables = useCallback(
    async (connection: string, offset: number) => {
      const page = await api.dbConnectTables(surface, connection, TABLE_PAGE_SIZE, offset);
      setTables((prev) => (offset === 0 ? page.tables : [...prev, ...page.tables]));
      setTablesHasMore(page.hasMore);
    },
    [surface],
  );

  useEffect(() => {
    refresh().catch((err: unknown) => setPanelError(errorText(err)));
  }, [refresh]);

  // Drop the unlock grant when the panel goes away. A failed lock is only logged:
  // the grant also dies with the server process.
  useEffect(() => {
    return () => {
      api.dbConnectLock(surface).catch((err: unknown) => {
        console.warn("db panel: lock on close failed", errorText(err));
      });
    };
  }, [surface]);

  // Switching connection clears everything tied to the previous one.
  useEffect(() => {
    setTables([]);
    setTablesHasMore(false);
    setResult(null);
    setQueryError(null);
    setNotice(null);
    setPendingWrite(null);
    setSql("");
    setActiveTable(null);
    setView("data");
    setUnlockPassword("");
    setConfirmRemove(null);
  }, [selected]);

  useEffect(() => {
    if (!selected || !unlocked) return;
    loadTables(selected, 0).catch((err: unknown) => setPanelError(errorText(err)));
  }, [selected, unlocked, loadTables]);

  // A statement runs read-only first. The server answers 409 when it is a write;
  // that opens the confirmation. Only a confirmed run commits.
  const run = async (text: string, confirmed: boolean) => {
    if (!selected || !text.trim()) return;
    setBusy("run");
    setQueryError(null);
    setNotice(null);
    try {
      const res = await api.dbConnectQuery(surface, selected, text, confirmed);
      setPendingWrite(null);
      setResult(res);
      if (res.rowsAffected !== null) {
        // With columns, the count is the rows the statement returned. Postgres does not
        // report the rows a data-modifying CTE changed, so "affected" would overstate it.
        setNotice(
          res.columns.length > 0
            ? `${res.rowsAffected} row(s) returned. Committed.`
            : `${res.rowsAffected} row(s) affected. Committed.`,
        );
      }
    } catch (err: unknown) {
      setResult(null);
      if (err instanceof ApiError && err.status === 409 && !confirmed) {
        setPendingWrite(text);
      } else {
        setPendingWrite(null);
        setQueryError(errorText(err));
      }
    } finally {
      setBusy(null);
    }
  };

  // Opening a table shows it in the Data view. The browser reads the table through
  // the server's keyed endpoints, not through the SQL editor.
  const openTable = (table: string) => {
    setActiveTable(table);
    setView("data");
  };

  const withBusy = async (label: string, fn: () => Promise<void>) => {
    setBusy(label);
    setPanelError(null);
    try {
      await fn();
    } catch (err: unknown) {
      setPanelError(errorText(err));
    } finally {
      setBusy(null);
    }
  };

  const addConnection = () =>
    withBusy("add", async () => {
      const name = addName.trim();
      await api.dbConnectAdd(name, addUrl.trim(), addPassword);
      setAddUrl("");
      setAddPassword("");
      setAddName("");
      await refresh();
      setSelected(name);
    });

  const unlockConnection = () =>
    withBusy("unlock", async () => {
      await api.dbConnectUnlock(surface, unlockPassword);
      setUnlockPassword("");
      await refresh();
      if (selected) await loadTables(selected, 0);
    });

  const lockConnections = () =>
    withBusy("lock", async () => {
      await api.dbConnectLock(surface);
      setTables([]);
      setTablesHasMore(false);
      setResult(null);
      await refresh();
    });

  const removeConnection = (name: string) =>
    withBusy("remove", async () => {
      await api.dbConnectRemove(name);
      setConfirmRemove(null);
      await refresh();
    });

  if (connections === null) {
    return (
      <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
        <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> Loading connections…
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-auto p-4" data-session-id={sessionId}>
      <div className="flex flex-wrap items-center gap-2">
        <Database className="h-4 w-4" aria-hidden="true" />
        <h2 className="text-sm font-semibold">PostgreSQL</h2>
        {connections.length > 0 && (
          <div className="ml-auto flex items-center gap-2">
            <Select value={selected ?? undefined} onValueChange={setSelected}>
              <SelectTrigger aria-label="Connection" className="h-8 w-48 text-xs">
                <SelectValue placeholder="Choose a connection" />
              </SelectTrigger>
              <SelectContent>
                {connections.map((c) => (
                  <SelectItem key={c.name} value={c.name} className="text-xs">
                    {c.name}
                    {c.unlocked ? " (unlocked)" : ""}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </div>

      {panelError && (
        <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive">
          {panelError}
        </div>
      )}

      {connections.length === 0 && (
        <form
          className="max-w-xl space-y-3 rounded-lg border border-border bg-card p-4"
          onSubmit={(e) => {
            e.preventDefault();
            void addConnection();
          }}
        >
          <h3 className="text-sm font-medium">Add a PostgreSQL connection</h3>
          <AddFields
            name={addName}
            url={addUrl}
            password={addPassword}
            onName={setAddName}
            onUrl={setAddUrl}
            onPassword={setAddPassword}
          />
          <Button type="submit" size="sm" disabled={busy !== null || !addName || !addUrl || !addPassword}>
            Save connection
          </Button>
        </form>
      )}

      {current && (
        <div className="flex items-center gap-2 text-xs">
          <span className="font-medium">{current.name}</span>
          {unlocked && (
            <Button size="sm" variant="outline" className="ml-auto h-7" disabled={busy !== null} onClick={() => void lockConnections()}>
              <Lock className="mr-1 h-3 w-3" /> Lock
            </Button>
          )}
          {confirmRemove === current.name ? (
            <Button size="sm" variant="destructive" className="h-7" disabled={busy !== null} onClick={() => void removeConnection(current.name)}>
              Confirm remove
            </Button>
          ) : (
            <Button
              size="sm"
              variant="ghost"
              className={cn("h-7", !unlocked && "ml-auto")}
              aria-label="Remove connection"
              disabled={busy !== null}
              onClick={() => setConfirmRemove(current.name)}
            >
              <Trash2 className="h-3 w-3" />
            </Button>
          )}
        </div>
      )}

      {connections.length > 0 && current && !unlocked && (
        <div className="max-w-xl space-y-3 rounded-lg border border-border bg-card p-4">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Lock className="h-3.5 w-3.5" aria-hidden="true" /> {current.name} is locked
          </div>
          <form
            className="flex items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void unlockConnection();
            }}
          >
            <label className="flex-1 space-y-1 text-xs font-medium text-muted-foreground">
              Master password
              <Input
                type="password"
                autoComplete="off"
                value={unlockPassword}
                onChange={(e) => setUnlockPassword(e.target.value)}
                className="h-8 text-xs"
              />
            </label>
            <Button type="submit" size="sm" disabled={busy !== null || !unlockPassword}>
              <Unlock className="mr-1 h-3.5 w-3.5" /> Unlock
            </Button>
          </form>
          <details className="text-xs text-muted-foreground">
            <summary className="cursor-pointer">Add another connection</summary>
            <form
              className="mt-2 space-y-2"
              onSubmit={(e) => {
                e.preventDefault();
                void addConnection();
              }}
            >
              <AddFields
                name={addName}
                url={addUrl}
                password={addPassword}
                onName={setAddName}
                onUrl={setAddUrl}
                onPassword={setAddPassword}
              />
              <Button type="submit" size="sm" variant="outline" disabled={busy !== null || !addName || !addUrl || !addPassword}>
                Save connection
              </Button>
            </form>
          </details>
        </div>
      )}

      {current && unlocked && (
        <div ref={setPaneEl} className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-border">
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-2 py-1">
            <button
              type="button"
              onClick={tablePane.toggleCollapsed}
              aria-label={tablePane.collapsed ? "Show table list" : "Hide table list"}
              aria-expanded={!tablePane.collapsed}
              title={tablePane.collapsed ? "Show table list" : "Hide table list"}
              className="mr-1 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              {tablePane.collapsed ? <PanelLeft className="h-3.5 w-3.5" /> : <PanelLeftClose className="h-3.5 w-3.5" />}
            </button>
            <span className="text-xs text-muted-foreground">Data &amp; query</span>
          </div>

          <div className={cn("flex min-h-0 min-w-0 flex-1", stacked && "flex-col")}>
            {/* Table list: width is drag-resizable and persisted; collapsing sets it to zero.
                Stacked, it takes the full width with a capped height instead. */}
            <div
              data-testid="dbconnect-table-pane"
              data-layout={stacked ? "stacked" : "side"}
              className={cn(
                "flex shrink-0 flex-col overflow-hidden transition-[width] duration-100",
                stacked ? (tablePane.collapsed ? "h-0" : "max-h-40 w-full border-b border-border") : "border-r border-border",
              )}
              style={stacked ? undefined : { width: tablePane.collapsed ? 0 : listWidth }}
            >
              <div className="border-b border-border px-2 py-1 text-[11px] font-semibold text-muted-foreground">Tables</div>
              <ul className="min-h-0 flex-1 overflow-auto p-1 text-xs" aria-label="Tables">
                {tables.length === 0 && <li className="p-2 text-muted-foreground">No tables in public schema.</li>}
                {tables.map((t) => (
                  <li key={t}>
                    <button
                      type="button"
                      onClick={() => openTable(t)}
                      className="w-full truncate rounded px-2 py-1 text-left font-mono hover:bg-muted"
                    >
                      {t}
                    </button>
                  </li>
                ))}
              </ul>
              {tablesHasMore && (
                <div className="shrink-0 p-1">
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 w-full text-xs"
                    disabled={busy !== null}
                    onClick={() =>
                      void withBusy("tables", () => loadTables(current.name, tables.length))
                    }
                  >
                    Load more tables
                  </Button>
                </div>
              )}
            </div>

            {!tablePane.collapsed && !stacked && (
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

            <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2 p-2">
              {activeTable && (
                <div role="tablist" aria-label="View" className="flex items-center gap-1 text-xs">
                  {(["data", "query"] as const).map((v) => (
                    <button
                      key={v}
                      type="button"
                      role="tab"
                      aria-selected={view === v}
                      onClick={() => setView(v)}
                      className={cn(
                        "rounded px-2 py-0.5 capitalize",
                        view === v ? "bg-muted font-medium" : "text-muted-foreground hover:bg-muted/50",
                      )}
                    >
                      {v}
                    </button>
                  ))}
                  <span className="ml-2 truncate font-mono text-muted-foreground">{activeTable}</span>
                </div>
              )}
              {activeTable && view === "data" ? (
                <DBTableBrowser key={activeTable} surface={surface} connection={current.name} table={activeTable} />
              ) : (
              <>
              <textarea
                aria-label="SQL"
                value={sql}
                onChange={(e) => setSql(e.target.value)}
                onKeyDown={(e) => {
                  if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                    e.preventDefault();
                    void run(sql, false);
                  }
                }}
                placeholder="SELECT …  (Cmd/Ctrl+Enter runs)"
                rows={4}
                className="w-full resize-y rounded-md border border-border bg-background px-3 py-2 font-mono text-xs focus:outline-none focus:ring-2 focus:ring-ring/30"
              />
              <div className="flex items-center gap-2">
                <Button size="sm" disabled={busy !== null || !sql.trim()} onClick={() => void run(sql, false)}>
                  {busy === "run" ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <Play className="mr-1 h-3.5 w-3.5" />}
                  Run
                </Button>
                {result?.truncated && (
                  <span className="text-[11px] text-muted-foreground">Showing the first 1000 rows.</span>
                )}
              </div>
              {notice && <div className="text-xs text-muted-foreground">{notice}</div>}
              {queryError && (
                <pre className="whitespace-pre-wrap rounded-md bg-destructive/10 p-2 font-mono text-xs text-destructive">
                  {queryError}
                </pre>
              )}
              {result && (
                <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-border">
                  <ResultGrid
                    result={toResultSet(result.columns, result.rows, result.truncated)}
                    testId="dbconnect-result"
                  />
                </div>
              )}
              </>
              )}
            </div>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={pendingWrite !== null}
        title="Run this write?"
        message={
          <div className="space-y-2">
            <pre className="whitespace-pre-wrap font-mono text-[11px]">{pendingWrite}</pre>
            <p>
              This commits to the database as soon as it runs. PostgreSQL has no undo here, so the change cannot be reverted from ocode.
            </p>
          </div>
        }
        confirmLabel="Run and commit"
        busy={busy === "run"}
        onCancel={() => setPendingWrite(null)}
        onConfirm={() => {
          if (pendingWrite !== null) void run(pendingWrite, true);
        }}
      />
    </div>
  );
}

function AddFields({
  name,
  url,
  password,
  onName,
  onUrl,
  onPassword,
}: {
  name: string;
  url: string;
  password: string;
  onName: (v: string) => void;
  onUrl: (v: string) => void;
  onPassword: (v: string) => void;
}) {
  return (
    <div className="space-y-2">
      <label className="block space-y-1 text-xs font-medium text-muted-foreground">
        Name
        <Input value={name} onChange={(e) => onName(e.target.value)} className="h-8 text-xs" />
      </label>
      <label className="block space-y-1 text-xs font-medium text-muted-foreground">
        Connection URL
        <Input
          type="password"
          autoComplete="off"
          placeholder="postgres://user:pass@host:5432/db"
          value={url}
          onChange={(e) => onUrl(e.target.value)}
          className="h-8 font-mono text-xs"
        />
      </label>
      <label className="block space-y-1 text-xs font-medium text-muted-foreground">
        Master password (encrypts the URL)
        <Input
          type="password"
          autoComplete="off"
          value={password}
          onChange={(e) => onPassword(e.target.value)}
          className="h-8 text-xs"
        />
      </label>
    </div>
  );
}
