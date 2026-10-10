# SQLite Browser — Design Spec

Date: 2026-10-05
Status: implemented (P1–P4)
Classification: architectural (new subsystem)

## Intent

A Prisma-Studio-like SQLite browser inside ocode's web/desktop file preview.
Open a `.sqlite`/`.sqlite3`/`.db`/`.db3` file in the preview pane and browse
tables, read/create/update/delete rows, run custom SQL, and add/alter schema —
with confirmation for destructive operations.

User decisions (2026-10-05):
1. Full CRUD + DDL, confirmation for destructive ops.
2. SQL editor + results table as the query UI, with a table list for browsing.
3. Only user-opened files (no implicit access to arbitrary DBs).

## Verified facts (2026-10-05)

- `modernc.org/sqlite v1.57.0` is already a dependency (pure Go, no cgo).
- `mode=ro` URI and `PRAGMA query_only=ON` both fail a write with
  `attempt to write a readonly database (8)`; reads still succeed. This is the
  409-escalation signal.
- modernc has **no** authorizer hook (only a comment noting its absence).
- Driver conn exposes `ColumnInfo(query) ([]sqlite.ColumnInfo, error)` via
  interface assertion. SELECT/RETURNING ⇒ ≥1 column; DML/DDL ⇒ 0 columns;
  syntax errors surface. `PRAGMA journal_mode=WAL` returns 1 column, so column
  count alone cannot gate writes — the readonly probe does.
- Remote proxy is a catch-all `/api/remote/{host}/api/{rest...}`, so new
  `/api/db/*` endpoints are proxied to the host's `ocode serve --remote`.
- `PUT /api/files/content` (`HandleSaveFileContent`) has no permission-mode
  gate: user-initiated web writes are the user's authority. Permission modes
  wrap the *agent's* tools, not user HTTP requests.
- Four middleware wrappers: `authMiddleware`, `mediaAuthMiddleware`,
  `healthMiddleware`, `pluginAuthMiddleware`. DB routes use `authMiddleware`.
- `allowedProjectRoots()` excludes remote entries; `fileContentRootFor`
  `EvalSymlinks` both sides before comparison.

## Architecture

### Package `internal/dbbrowse` (pure, no server deps)

- `Probe(path) (bool, error)` — sniff `SQLite format 3\0` header (first 16
  bytes). `.db` is ambiguous; encrypted/other-format files fall back.
- `ListTables(ctx, path) ([]Table, error)` — sorted by name; name, type
  (table/view), row estimate.
- `DescribeTable(ctx, path, table) (TableSchema, error)` — columns
  (name, decl_type, not_null, default, pk), indexes, foreign keys, and the
  original `CREATE` DDL from `sqlite_master`.
- `Query(ctx, path, sql, limit) (ResultSet, error)` — read-only open.
- `Exec(ctx, path, sql) (ExecResult, error)` — read-write open; RowsAffected /
  LastInsertId.
- `RowInsert/RowUpdate/RowDelete(ctx, path, table, key, values) (int64, error)`
  — parameterized; optimistic concurrency.
- `SchemaOps` — server-built quoted DDL for add-column / create-table /
  drop-table / create-index / drop-index.

JSON-safe values: null, integer/real as number, text as string, BLOB as
`{"$blob": true, "bytes": N, "preview": "<hex, capped>", "data": "<base64, capped>"}`.

### Endpoints (`internal/server/handler_db.go`)

| Route | Purpose |
|---|---|
| `GET  /api/db/info` | probe + sorted table list |
| `GET  /api/db/table` | paginated rows (`limit`/`offset`) |
| `POST /api/db/query` | SQL from the editor |
| `POST /api/db/row` | parameterized row insert/update/delete |
| `POST /api/db/schema` | guided DDL |

Registered in `registerRoutes()` behind `authMiddleware`. Path containment is
identical to `HandleFileContent` (`fileContentRootFor` + `containsDotDot` +
`containedIn`/`containedLexical`, resolve against `h.workDir`, never
`os.Getwd()`). No `h.mu` held across DB work.

### Safety model

- **Engine is the classifier.** Every SQL-editor statement first runs on a
  `mode=ro` connection with `PRAGMA query_only=ON`. A
  `readonly database` failure ⇒ HTTP **409** with the statement text; the client
  shows a confirm dialog and retries with `confirm:true` on a read-write
  connection. Defeats `WITH…DELETE…RETURNING`, `VACUUM`, writable CTEs.
- **Token-scan blocks**: `ATTACH`, `DETACH`, `VACUUM INTO` are refused outright
  (no authorizer hook). `PRAGMA journal_mode` / `writable_schema` require
  confirmation.
- **Multi-statement**: split on `;` respecting quotes/comments; execute in one
  transaction; confirm if any constituent is non-read-only.
- **Session-DB write guard**: refuse write escalation when `os.SameFile` matches
  any DB under `paths.GlobalDataDir()` (read-only browse stays allowed). The UI
  explains why.
- Per-request `ctx` timeout (~30s; engine interrupts), row cap 1000 default /
  10000 max, cell-size cap.
- **Optimistic concurrency**: declared PK first, then `rowid`; WHERE uses the
  original key values; require `RowsAffected==1` else 409. `WITHOUT ROWID` /
  views / neither ⇒ read-only. Generated columns excluded.
- **Authorization**: user-initiated (like file save), NOT gated by agent
  permission mode. Self-escalation is unreachable — only a header-sniffed
  SQLite file is touched; `.ocode/settings.json` is not SQLite.

### Frontend

- `previewKind.ts`: remove `.sqlite/.sqlite3/.db/.db3` from
  `NON_PREVIEWABLE_EXTS`; add `"sqlite"` kind (server sniff still gates render).
- New lazy `SQLiteViewer.tsx`: table list (searchable, row counts) + tabs
  **Data** (paginated grid, row CRUD), **Query** (Monaco SQL + results grid),
  **Schema** (columns/indexes/DDL + guided add-column/create/drop).
  Uses existing shadcn/Radix dialog primitives for confirmation. `revision`
  live-refresh must not discard unsaved grid/editor state.
- Non-SQLite file (sniff fails) ⇒ error pane with "Open externally".
- Works in sidebar preview and mobile `PreviewTabPage`.
- `api.dbInfo/dbTable/dbQuery/dbRow/dbSchema` in `web/src/api/client.ts`.
- Desktop app must be rebuilt (`web/dist` embedded).

### Remote

Endpoints reach the host through the existing catch-all proxy; the frontend
threads `projectHost` and the proxy admission uses `?project=`/`?path=`.

## Review correction (2026-10-05)

A security review found a sibling asymmetry: `HandleDBQuery` called
`resolveProjectFilePath` directly and skipped the unconditional
`pathWithinAllowedRoots` check that `HandleDBInfo`/`HandleDBTable` get through
`resolveDBRequest`. An absolute path outside the allowed roots was therefore
accepted by the query endpoint (verified live: `/etc/hosts` → 200 pre-fix, 400
post-fix). Fixed by composing both checks in `Handler.resolveDBPath` and routing
all three endpoints through it; regression `TestHandleDBQueryRejectsOutsideAllowedRoots`
(mutation-verified — the mutant returns 200).

## Phasing

- **P1 — shipped.** Read-only: `Probe`, `ListTables`, `DescribeTable`,
  `Query(ro)`, endpoints `info`/`table`/`query`, `SQLiteViewer` (browse, Data,
  Query, Schema view), mobile.
- **P2 — shipped.** `POST /api/db/row` with optimistic concurrency
  (exactly-one-row enforced inside a transaction; declared PK else `_rowid_`).
- **P3 — shipped.** `confirm:true` on `POST /api/db/query`; `ATTACH`/`DETACH`/
  `VACUUM INTO` refused; session-data-dir write guard.
- **P4 — shipped.** `POST /api/db/schema` guided DDL.

Remaining work (remote live validation, bundle doc page, FK enforcement, BLOB
editing, version-based concurrency) is recorded in `TODO.md`.

## Docs

- `docs/concepts/sqlite-browser.md` (bundle) — via context agent after P1.
- `skills/ocode-web/SKILL.md` + `CHANGES.md` — direct (not bundle pages).
