// DB project tab: Postgres connector. Saved connections are global
// (ocodeconfig.json, encrypted); each panel unlocks them under its own surface
// id and locks them on close. Every statement runs read-only first; a write is
// shown to the user and runs only after an explicit confirmation.
//
// An environment-backed connection ("${DATABASE_URL}") carries no secret: its
// URL is read from the project's .env / .env.local at load time, with no master
// password. A literal URL stays sealed and needs the password that sealed it.
import { Database, Loader2, Lock, PanelLeft, PanelLeftClose, Play, Trash2, Unlock } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  api,
  type DBConnectConnection,
  type DBConnectQueryResult,
  type DBConnectTarget,
  type DBConnectUnlockFailure,
  type DBUnlockScope,
} from "../../api/client";
import { useResizableSidebar } from "../../hooks/useResizableSidebar";
import { isEnvTemplateURL as isEnvTemplate } from "../../lib/dbConnectTemplate";
import { cn } from "../../lib/utils";
import { ConfirmDialog } from "../Preview/SQLiteDialogs";
import { ResultGrid } from "../Preview/SQLiteResultGrid";
import { Button } from "../ui/button";
import { Input } from "../ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../ui/select";
import DBTableBrowser, { dbCallErrorText } from "./DBTableBrowser";
import { toResultSet } from "./dbConnectCells";

// Matches the server's default page size for the tables endpoint.
const TABLE_PAGE_SIZE = 100;

// Table list width bounds, and the room side by side needs beyond the list: the
// resize handle, then the narrowest data pane the toolbar and grid stay usable in.
const LIST_MIN_PX = 120;
const LIST_MAX_PX = 480;
const LIST_DEFAULT_PX = 160;
const HANDLE_PX = 4;
const DATA_MIN_PX = 320;

// Per-mount id for the unlock surface. The surface must be unique per panel, not
// only per project: two windows viewing the same project would otherwise share
// it, and closing one would revoke the other's grant (and close its pools).
//
// A module-scope counter alone is NOT enough: module scope is per DOCUMENT, so
// every browser window restarts at 1 and two windows on one project collide on
// `p1`. The document tag makes the id unique across windows too.
const dbPanelDocumentTag = (() => {
  const c = globalThis.crypto;
  if (c && typeof c.randomUUID === "function") return c.randomUUID().slice(0, 8);
  return Array.from({ length: 8 }, () => Math.floor(Math.random() * 16).toString(16)).join("");
})();
let dbPanelSurfaceSeq = 0;
function newDbPanelSurfaceID(): string {
  dbPanelSurfaceSeq += 1;
  return `${dbPanelDocumentTag}-${dbPanelSurfaceSeq}`;
}

export default function DBPanel({ projectPath, projectHost }: { projectPath?: string; projectHost?: string }) {
  // A remote project's calls carry its path as X-Ocode-Project. Without it the host
  // would act on its own default project, so refuse to render rather than misroute.
  if (projectHost && !projectPath) {
    throw new Error(`db panel: remote project on ${projectHost} has no project path`);
  }
  const surfaceID = useRef<string>("");
  if (!surfaceID.current) surfaceID.current = newDbPanelSurfaceID();
  // Stable for this panel's lifetime. The project is kept in the id for
  // diagnostics only; the per-mount suffix is what makes the grant ours.
  const surface = useMemo(
    () => `db-panel:${projectHost ?? ""}:${projectPath ?? ""}:${surfaceID.current}`,
    [projectHost, projectPath],
  );
  // Routes every dbconnect call. A remote project's calls go to its host's server,
  // which holds that project's .env, saved connections, grants and pools.
  const target = useMemo<DBConnectTarget | undefined>(
    () => (projectHost ? { host: projectHost, projectPath } : undefined),
    [projectHost, projectPath],
  );
  // The unlock's project scope: a remote project's path travels with its host.
  const unlockScope = useMemo<DBUnlockScope>(() => {
    if (projectHost && projectPath) return [projectPath, { host: projectHost, projectPath }];
    return projectPath ? [projectPath] : [];
  }, [projectHost, projectPath]);
  // The unlock-on-close cleanup reads the target through this ref, so it is keyed on
  // the surface alone: a target change must never lock the grant mid-session.
  const targetRef = useRef(target);
  useEffect(() => {
    targetRef.current = target;
  }, [target]);

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
  // Connections this attempt could not unlock. The ones that did are still
  // granted, so this is a warning, not a panel error.
  const [unlockFailures, setUnlockFailures] = useState<DBConnectUnlockFailure[]>([]);

  const [addName, setAddName] = useState("");
  const [addUrl, setAddUrl] = useState("");
  const [addPassword, setAddPassword] = useState("");
  // Only read on first setup, where the typed master password must be confirmed.
  const [addConfirm, setAddConfirm] = useState("");
  const [addTunnelEnabled, setAddTunnelEnabled] = useState(false);
  const [addTunnelHost, setAddTunnelHost] = useState("");
  const [addTunnelPort, setAddTunnelPort] = useState("");
  const [addTunnelRemoteHost, setAddTunnelRemoteHost] = useState("");
  const [addTunnelRemotePort, setAddTunnelRemotePort] = useState("");
  const [addTunnelIdentity, setAddTunnelIdentity] = useState("");
  const [addTunnelAuthMethod, setAddTunnelAuthMethod] = useState<"key" | "password" | "auto">("key");
  const [addTunnelPassword, setAddTunnelPassword] = useState("");
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
  // A template URL needs no master password, so the add form neither asks for
  // one nor blocks Save on it.
  const addIsTemplate = isEnvTemplate(addUrl.trim());

  const refresh = useCallback(async () => {
    const res = await api.dbConnectList(surface, target);
    // Normalise at the fetch boundary: the api client already turns a null
    // list into [], but a partial mock (or an older server) must not crash the
    // panel with an undefined list.
    const list = res?.connections ?? [];
    setConnections(list);
    setSelected((prev) => {
      if (prev && list.some((c) => c.name === prev)) return prev;
      return list[0]?.name ?? null;
    });
  }, [surface, target]);

  // offset 0 starts a fresh list; a later offset appends the next page.
  const loadTables = useCallback(
    async (connection: string, offset: number) => {
      const page = await api.dbConnectTables(surface, connection, TABLE_PAGE_SIZE, offset, target);
      setTables((prev) => (offset === 0 ? page.tables : [...prev, ...page.tables]));
      setTablesHasMore(page.hasMore);
    },
    [surface, target],
  );

  // Retryable load. A failure keeps `connections` null, so the loading branch
  // must render this error (and a Retry) instead of an endless spinner.
  const reload = useCallback(() => {
    setPanelError(null);
    refresh().catch((err: unknown) => setPanelError(dbCallErrorText(err, target)));
  }, [refresh]);

  useEffect(() => {
    reload();
  }, [reload]);

  // Drop the unlock grant when the panel goes away. A failed lock is only logged:
  // the grant also dies with the server process.
  useEffect(() => {
    return () => {
      const lockTarget = targetRef.current;
      api.dbConnectLock(surface, lockTarget).catch((err: unknown) => {
        console.warn("db panel: lock on close failed", dbCallErrorText(err, lockTarget));
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
    setUnlockFailures([]);
  }, [selected]);

  useEffect(() => {
    if (!selected || !unlocked) return;
    loadTables(selected, 0).catch((err: unknown) => setPanelError(dbCallErrorText(err, target)));
  }, [selected, unlocked, loadTables]);

  // A statement runs read-only first. The server answers 409 when it is a write;
  // that opens the confirmation. Only a confirmed run commits.
  const run = async (text: string, confirmed: boolean) => {
    if (!selected || !text.trim()) return;
    setBusy("run");
    setQueryError(null);
    setNotice(null);
    try {
      const res = await api.dbConnectQuery(surface, selected, text, confirmed, target);
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
        setQueryError(dbCallErrorText(err, target));
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
      setPanelError(dbCallErrorText(err, target));
    } finally {
      setBusy(null);
    }
  };

  // Unlocks every connection this password opens, for this panel's surface.
  const unlockWith = async (password: string) => {
    // The project path lets the server resolve an environment-backed
    // connection against THIS project's .env / .env.local. The password is
    // empty for that flow; only sealed connections need it. For a remote project
    // the same path goes to the host's server, which validates it against its own
    // allowed roots and reads the host's .env.
    try {
      const res = await api.dbConnectUnlock(surface, password, undefined, ...unlockScope);
      setUnlockFailures(res.failed);
      // Apply the server's answer locally as well, so a failed refresh below
      // cannot leave the panel claiming the opposite of what the server did:
      // granted connections become unlocked, failed ones lose the flag, and
      // anything the attempt did not mention keeps its previous state (the
      // password-less flow MERGES, so a sealed connection already unlocked
      // stays unlocked and is never listed in `unlocked`).
      const grantedNames = new Set(res.unlocked);
      const failedNames = new Set(res.failed.map((f) => f.name));
      setConnections((prev) =>
        prev
          ? prev.map((c) => ({
              ...c,
              unlocked: !failedNames.has(c.name) && (c.unlocked || grantedNames.has(c.name)),
            }))
          : prev,
      );
    } finally {
      // A rejected attempt can still have changed the grant, and the panel
      // must not keep claiming a connection is locked when the server has
      // it (or vice versa). Keep the original unlock error if this fails.
      await refresh().catch((err: unknown) => {
        console.warn("db panel: refresh after unlock failed", dbCallErrorText(err, target));
      });
    }
    // No explicit loadTables: the [selected, unlocked] effect above reloads
    // when the connection becomes unlocked, and doing both sent two requests.
  };

  const unlockConnection = () =>
    withBusy("unlock", async () => {
      // The password is empty for an environment-backed connection (see unlockWith).
      await unlockWith(current?.fromEnv ? "" : unlockPassword);
      setUnlockPassword("");
    });

  const addConnection = () =>
    withBusy("add", async () => {
      const name = addName.trim();
      const password = addPassword;
      // A template URL is saved in the clear and needs no password; a literal
      // URL is sealed under it. The server enforces the same rule.
      const literal = !addIsTemplate;
      const tunnel = addTunnelEnabled
        ? {
            host: addTunnelHost.trim(),
            port: addTunnelPort ? Number(addTunnelPort) : undefined,
            remote_host: addTunnelRemoteHost.trim(),
            remote_port: Number(addTunnelRemotePort),
            identity_file: addTunnelIdentity.trim() || undefined,
            auth_method: addTunnelAuthMethod,
            password: addTunnelAuthMethod === "password" ? addTunnelPassword : undefined,
          }
        : undefined;
      await api.dbConnectAdd(name, addUrl.trim(), password, target, tunnel);
      setAddUrl("");
      setAddPassword("");
      setAddConfirm("");
      setAddName("");
      setAddTunnelEnabled(false);
      setAddTunnelHost("");
      setAddTunnelPort("");
      setAddTunnelRemoteHost("");
      setAddTunnelRemotePort("");
      setAddTunnelIdentity("");
      setAddTunnelAuthMethod("key");
      setAddTunnelPassword("");
      // The connection set changed; a failure recorded for a name that was just
      // (re)added no longer describes the saved state.
      setUnlockFailures((prev) => prev.filter((f) => f.name !== name));
      await refresh();
      setSelected(name);
      // A literal URL was just sealed under this password, so it is unlocked
      // now. A template is not sealed: it loads through "Load from .env".
      if (literal) {
        // The save already committed and the form is cleared, so an unlock failure
        // must not read as a failed add: a retry would hit the duplicate-name error.
        try {
          await unlockWith(password);
        } catch (err: unknown) {
          console.warn("db panel: auto-unlock after add failed", name, dbCallErrorText(err, target));
          setPanelError(
            `Connection ${name} was saved, but unlocking it failed: ${dbCallErrorText(err, target)}. Unlock it manually with the master password.`,
          );
        }
      }
    });

  const lockConnections = () =>
    withBusy("lock", async () => {
      await api.dbConnectLock(surface, target);
      setTables([]);
      setTablesHasMore(false);
      setResult(null);
      setUnlockFailures([]);
      setConnections((prev) => (prev ? prev.map((c) => ({ ...c, unlocked: false })) : prev));
      await refresh();
    });

  const removeConnection = (name: string) =>
    withBusy("remove", async () => {
      await api.dbConnectRemove(name, target);
      setConfirmRemove(null);
      setUnlockFailures((prev) => prev.filter((f) => f.name !== name));
      await refresh();
    });

  if (connections === null) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-3 p-4 text-xs text-muted-foreground">
        {panelError ? (
          <>
            <div
              role="alert"
              className="max-w-xl rounded-md border border-destructive/40 bg-destructive/10 p-2 text-xs text-destructive"
            >
              {panelError}
            </div>
            <Button size="sm" variant="outline" onClick={reload}>
              Retry
            </Button>
          </>
        ) : (
          <div className="flex items-center">
            <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> Loading connections…
          </div>
        )}
      </div>
    );
  }

  // Only one add form renders at a time: the first-run form when nothing is saved,
  // the "Add another connection" form otherwise. The first save fixes the master
  // password, so its confirmation must match; later saves are checked by the server.
  const firstSetup = connections.length === 0;
  const addPasswordMismatch = firstSetup && !addIsTemplate && addPassword !== addConfirm;
  const addSaveDisabled =
    busy !== null || !addName || !addUrl || (!addIsTemplate && !addPassword) || addPasswordMismatch;

  return (
    <div
      className="flex h-full min-h-0 flex-col gap-3 overflow-auto p-4"
      data-project-path={projectPath}
      aria-busy={busy !== null}
    >
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
                    {c.fromEnv ? " (env)" : ""}
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

      {unlockFailures.length > 0 && (
        <div
          role="status"
          className="rounded-md border border-amber-500/40 bg-amber-500/10 p-2 text-xs text-amber-600"
        >
          <div className="font-medium">Some connections could not be unlocked:</div>
          <ul className="mt-1 list-disc space-y-0.5 pl-4">
            {unlockFailures.map((f) => (
              <li key={f.name}>
                <span className="font-mono">{f.name}</span>: {f.error}
              </li>
            ))}
          </ul>
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
            confirmPassword={addConfirm}
            firstSetup={true}
            onName={setAddName}
            onUrl={setAddUrl}
            onPassword={setAddPassword}
            onConfirmPassword={setAddConfirm}
            tunnelEnabled={addTunnelEnabled}
            tunnelHost={addTunnelHost}
            tunnelPort={addTunnelPort}
            tunnelRemoteHost={addTunnelRemoteHost}
            tunnelRemotePort={addTunnelRemotePort}
            tunnelIdentity={addTunnelIdentity}
            tunnelAuthMethod={addTunnelAuthMethod}
            tunnelPassword={addTunnelPassword}
            onTunnelEnabled={setAddTunnelEnabled}
            onTunnelHost={setAddTunnelHost}
            onTunnelPort={setAddTunnelPort}
            onTunnelRemoteHost={setAddTunnelRemoteHost}
            onTunnelRemotePort={setAddTunnelRemotePort}
            onTunnelIdentity={setAddTunnelIdentity}
            onTunnelAuthMethod={setAddTunnelAuthMethod}
            onTunnelPassword={setAddTunnelPassword}
          />
          <Button type="submit" size="sm" disabled={addSaveDisabled}>
            Save connection
          </Button>
        </form>
      )}

      {current && (
        <div className="flex items-center gap-2 text-xs">
          <span className="font-medium">{current.name}</span>
          {current.fromEnv && (
            <span
              className="rounded border border-border px-1 py-0.5 font-mono text-[10px] text-muted-foreground"
              title="The connection URL is read from this project's .env / .env.local when loaded — no stored secret. A value exported in the server's own environment takes precedence."
            >
              env
            </span>
          )}
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
          {current.fromEnv ? (
            <>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Database className="h-3.5 w-3.5" aria-hidden="true" /> {current.name} reads its URL from the
                project&apos;s <code className="font-mono">.env</code>
              </div>
              <form
                className="flex items-end gap-2"
                onSubmit={(e) => {
                  e.preventDefault();
                  void unlockConnection();
                }}
              >
                <Button type="submit" size="sm" disabled={busy !== null} aria-busy={busy === "unlock"}>
                  {busy === "unlock" ? (
                    <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Unlock className="mr-1 h-3.5 w-3.5" />
                  )}
                  Load from .env
                </Button>
              </form>
              <p className="text-[11px] text-muted-foreground">
                No password needed — nothing is stored or encrypted. The URL is read from{" "}
                <code className="font-mono">.env</code> / <code className="font-mono">.env.local</code> in the
                project root when you load it. A variable exported in the server&apos;s environment takes
                precedence.
              </p>
            </>
          ) : (
            <>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                <Lock className="h-3.5 w-3.5" aria-hidden="true" /> {current.name} is locked
              </div>
              {/* Outside the label: it must not become part of the field's accessible name. */}
              <p className="text-[11px] text-muted-foreground">
                The URL is encrypted and never shown again. Enter your master password to connect.
              </p>
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
                <Button
                  type="submit"
                  size="sm"
                  disabled={busy !== null || !unlockPassword}
                  aria-busy={busy === "unlock"}
                >
                  <Unlock className="mr-1 h-3.5 w-3.5" /> Unlock &amp; connect
                </Button>
              </form>
            </>
          )}
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
                confirmPassword={addConfirm}
                firstSetup={false}
                onName={setAddName}
                onUrl={setAddUrl}
                onPassword={setAddPassword}
                onConfirmPassword={setAddConfirm}
                tunnelEnabled={addTunnelEnabled}
                tunnelHost={addTunnelHost}
                tunnelPort={addTunnelPort}
                tunnelRemoteHost={addTunnelRemoteHost}
                tunnelRemotePort={addTunnelRemotePort}
                tunnelIdentity={addTunnelIdentity}
                tunnelAuthMethod={addTunnelAuthMethod}
                tunnelPassword={addTunnelPassword}
                onTunnelEnabled={setAddTunnelEnabled}
                onTunnelHost={setAddTunnelHost}
                onTunnelPort={setAddTunnelPort}
                onTunnelRemoteHost={setAddTunnelRemoteHost}
                onTunnelRemotePort={setAddTunnelRemotePort}
                onTunnelIdentity={setAddTunnelIdentity}
                onTunnelAuthMethod={setAddTunnelAuthMethod}
                onTunnelPassword={setAddTunnelPassword}
              />
              <Button type="submit" size="sm" variant="outline" disabled={addSaveDisabled}>
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
                <DBTableBrowser
                  key={activeTable}
                  surface={surface}
                  connection={current.name}
                  table={activeTable}
                  target={target}
                />
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
  confirmPassword,
  firstSetup,
  tunnelEnabled,
  tunnelHost,
  tunnelPort,
  tunnelRemoteHost,
  tunnelRemotePort,
  tunnelIdentity,
  tunnelAuthMethod,
  tunnelPassword,
  onName,
  onUrl,
  onPassword,
  onConfirmPassword,
  onTunnelEnabled,
  onTunnelHost,
  onTunnelPort,
  onTunnelRemoteHost,
  onTunnelRemotePort,
  onTunnelIdentity,
  onTunnelAuthMethod,
  onTunnelPassword,
}: {
  name: string;
  url: string;
  password: string;
  confirmPassword: string;
  firstSetup: boolean;
  tunnelEnabled: boolean;
  tunnelHost: string;
  tunnelPort: string;
  tunnelRemoteHost: string;
  tunnelRemotePort: string;
  tunnelIdentity: string;
  tunnelAuthMethod: "key" | "password" | "auto";
  tunnelPassword: string;
  onName: (v: string) => void;
  onUrl: (v: string) => void;
  onPassword: (v: string) => void;
  onConfirmPassword: (v: string) => void;
  onTunnelEnabled: (v: boolean) => void;
  onTunnelHost: (v: string) => void;
  onTunnelPort: (v: string) => void;
  onTunnelRemoteHost: (v: string) => void;
  onTunnelRemotePort: (v: string) => void;
  onTunnelIdentity: (v: string) => void;
  onTunnelAuthMethod: (v: "key" | "password" | "auto") => void;
  onTunnelPassword: (v: string) => void;
}) {
  // A URL that references an environment variable is not a secret, so the
  // password field is neither required nor meaningful for it.
  const template = isEnvTemplate(url.trim());
  // The first literal save creates the master password, so it is typed twice.
  const creating = firstSetup && !template;
  return (
    <div className="space-y-2">
      <label className="block space-y-1 text-xs font-medium text-muted-foreground">
        Name
        <Input value={name} onChange={(e) => onName(e.target.value)} className="h-8 text-xs" />
      </label>
      <div className="space-y-1">
        <label className="block text-xs font-medium text-muted-foreground">
          Connection URL
          <Input
            type={template ? "text" : "password"}
            autoComplete="off"
            placeholder={"postgres://user:pass@host:5432/db  or  ${DATABASE_URL}"}
            data-1p-ignore="true"
            data-lpignore="true"
            value={url}
            onChange={(e) => onUrl(e.target.value)}
            className="h-8 font-mono text-xs"
          />
        </label>
        {/* The hint sits OUTSIDE the label: text inside a label becomes part
            of the input's accessible name, and the field's name must stay
            "Connection URL". */}
        <p className="text-[11px] text-muted-foreground">
          Use <code className="font-mono">{"${DATABASE_URL}"}</code> to read the URL from the
          project&apos;s <code className="font-mono">.env</code> /{" "}
          <code className="font-mono">.env.local</code> (a value exported in the server environment
          wins). No password is needed for that, and the password must itself be a{" "}
          <code className="font-mono">{"${VAR}"}</code> reference — a literal password is refused
          because a template is stored unencrypted.
        </p>
      </div>
      <div className="space-y-1">
        <label className="block text-xs font-medium text-muted-foreground">
          {template
            ? "Master password (not needed for an environment URL)"
            : creating
              ? "Create master password"
              : "Master password (must match the one you created first)"}
          <Input
            type="password"
            autoComplete={creating ? "new-password" : "off"}
            value={password}
            onChange={(e) => onPassword(e.target.value)}
            disabled={template}
            className="h-8 text-xs"
          />
        </label>
        {creating && (
          <label className="block space-y-1 text-xs font-medium text-muted-foreground">
            Confirm master password
            <Input
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => onConfirmPassword(e.target.value)}
              className="h-8 text-xs"
            />
          </label>
        )}
        <p className="text-[11px] text-muted-foreground">
          {template
            ? "Nothing is stored or encrypted: the value is read from the project's .env files when the connection is loaded."
            : creating
              ? "Choose a new password now. It is not an existing account password. You will enter it each time you connect, and it cannot be recovered if forgotten."
              : "Required for a literal URL: it seals the value that is saved."}
        </p>
      </div>
      <div className="space-y-2 rounded-md border border-border p-2">
        <label className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <input type="checkbox" checked={tunnelEnabled} onChange={(e) => onTunnelEnabled(e.target.checked)} />
          Use SSH tunnel
        </label>
        {tunnelEnabled && (
          <div className="space-y-2">
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              SSH host
              <Input value={tunnelHost} onChange={(e) => onTunnelHost(e.target.value)} className="h-8 text-xs" />
            </label>
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              SSH port (optional)
              <Input value={tunnelPort} onChange={(e) => onTunnelPort(e.target.value)} className="h-8 text-xs" />
            </label>
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              Remote DB host
              <Input value={tunnelRemoteHost} onChange={(e) => onTunnelRemoteHost(e.target.value)} className="h-8 text-xs" />
            </label>
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              Remote DB port
              <Input value={tunnelRemotePort} onChange={(e) => onTunnelRemotePort(e.target.value)} className="h-8 text-xs" />
            </label>
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              Identity file (optional)
              <Input value={tunnelIdentity} onChange={(e) => onTunnelIdentity(e.target.value)} className="h-8 text-xs" />
            </label>
            <label className="block space-y-1 text-xs font-medium text-muted-foreground">
              Auth method
              <select value={tunnelAuthMethod} onChange={(e) => onTunnelAuthMethod(e.target.value as any)} className="h-8 w-full rounded-md border border-border bg-background px-2 text-xs">
                <option value="key">Key / agent</option>
                <option value="password">Password</option>
                <option value="auto">Auto</option>
              </select>
            </label>
            {tunnelAuthMethod === "password" && (
              <label className="block space-y-1 text-xs font-medium text-muted-foreground">
                SSH password
                <Input type="password" value={tunnelPassword} onChange={(e) => onTunnelPassword(e.target.value)} className="h-8 text-xs" />
              </label>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
