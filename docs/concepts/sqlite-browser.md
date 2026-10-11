---
type: Concept
title: SQLite Browser / DB IDE (preview pane)
description: |-
  Design decisions and invariants for the web/desktop SQLite browser: server-side modernc.org/sqlite engine, containment and read-only allowlist, rowid row identity, BLOB prefix vs full-byte handling, pre-write snapshots, and grid/IDE behaviors that must not regress. Fuller implementation checklist in skills/ocode-web/SKILL.md item 64.
tags:
  - web
  - sqlite
  - dbbrowse
  - preview
  - viewer
  - blob
  - csv
  - server
  - security
timestamp: 2026-10-07T05:55:15Z
resource: internal/tool/sqlite.go; internal/dbbrowse/dbbrowse.go; internal/dbbrowse/page.go; internal/dbbrowse/write.go; internal/dbbrowse/blob.go; internal/dbbrowse/rowkeys.go; internal/server/handler_db.go; internal/server/handler_db_blob.go; web/src/api/client.ts; web/src/components/Preview/SQLiteViewer.tsx; web/src/components/Preview/SQLiteDialogs.tsx; web/src/components/Preview/BlobDialog.tsx; web/src/components/Preview/blobPreview.ts; web/src/components/Preview/csvExport.ts
---
# SQLite Browser / DB IDE (preview pane)

The web/desktop preview pane renders `.sqlite` / `.sqlite3` / `.db` / `.db3` files with an interactive browser (Data / Query / Schema tabs) that can page, filter, sort, run confirmed SQL, edit rows, and view/download/replace BLOB cells. This page records **why** each choice was made and **which invariants must not regress**. The step-by-step implementation checklist lives in `skills/ocode-web/SKILL.md` item 64 — read that for the mechanical "where is the code" detail; this page is the rationale.

## 1. The engine is server-side, never WASM

The database is a **real file on disk** — possibly remote over SSH, possibly large, possibly in WAL mode — and writes must persist to that file. So browsing/querying/writing happens in Go over `modernc.org/sqlite` (already a dependency, no cgo). `sql.js`/WASM is not used: it would require shipping the whole file to the browser, cannot persist writes back, and cannot honour the same containment boundary.

`internal/dbbrowse` is a **pure package** (no `internal/server`, no HTTP, no permission code). The server layer owns containment and the write guard; `dbbrowse` owns SQL correctness.

- **Reads** open the file `mode=ro` with `query_only(1)` set, and the DSN deliberately does **not** carry `_txlock=immediate` or `journal_mode(WAL)` — copying the session store's DSN here would take a write lock on `BEGIN` and mutate the file. A read path must never write.
- **Writes** are a *separate, explicit* read-write path. A read-only connection can still succeed at some `PRAGMA` assignments (e.g. `journal_mode`), so "it's on a read-only connection" is not a safety argument. The only way `dbbrowse.Exec` is reached is `POST /api/db/query` with `confirm:true`, after the client has shown a confirmation dialog.

**Consequence to preserve:** never unify read and write onto one connection "for convenience", and never treat token-scanning for DML as the gate. The engine + allowlist classify; the explicit user confirmation authorises.

## 2. Containment and security model

Every DB endpoint routes through **`Handler.resolveDBPath`**, which composes two independent checks:

1. File-content containment (`resolveProjectFilePath`, the same helper `HandleFileContent` uses).
2. An **unconditional** `pathWithinAllowedRoots` check — applied even for an absolute path with no `project_root`, because this surface gains write capability.

The path is resolved **through symlinks**, so a symlink cannot dodge the allowed-roots check. The asymmetry bug to avoid: calling `resolveProjectFilePath` alone leaves some endpoints accepting a path others reject; `resolveDBPath` exists so no endpoint can drift.

**Writes additionally pass `Handler.dbWriteGuard`:**
- non-SQLite file or missing file → **400** (this also stops a write from *creating* a stray database);
- anything under ocode's own data dir (`paths.GlobalDataDir()`) → **400**, resolved so a symlink cannot dodge it.

### The read-only allowlist

`validateReadOnlyStatement` is an **allowlist**, not a denylist: exactly one statement, no `;`, no `ATTACH`/`DETACH`, and the leading keyword must be a read (`SELECT`/`WITH`/`PRAGMA`/`EXPLAIN`). The SQL is noise-stripped first so an `ATTACH` hidden in a comment or string cannot slip through.

A **user filter for the grid is untrusted SQL**, so it is validated by wrapping it in `SELECT 1 WHERE (<filter>)` and running *that* through the same allowlist (`filterExpression`). A smuggled second statement or an `ATTACH` is refused. A **read-only subquery stays legal on purpose** — containment rests on `ATTACH` being impossible plus `query_only(1)`, not on the filter being a literal comparison.

A **sort column is admitted only if the table actually has it** (or it is the synthetic rowid column), and then quoted (`sortClause`). Quoting alone keeps the SQL well-formed while still allowing `ORDER BY` to name another table's column or an expression — so existence-check *then* quote, never quote-only.

A rejected filter/sort is a **400, not a 500**: it is caller input, and a 500 would read as "the database is broken".

`ATTACH`/`DETACH`/`VACUUM INTO` are refused outright by `dbbrowse.BlockedWriteStatement` on the write path too — there is no authorizer hook, so this is a static refusal.

## 3. Row identity

The key for a row edit is the **declared primary key** (`TableSchema.KeyColumns`); if the table has none, the true `rowid` is projected as a synthetic **`_rowid_`** column (`dbbrowse.RowIDColumn`) aliased so a literal `rowid` column cannot shadow it. A view, or a `WITHOUT ROWID` table with no PK, stays read-only.

UPDATE/DELETE must affect **exactly one row**, enforced **inside a transaction** (`execExact`). In autocommit the rows are already changed before `RowsAffected` is checked, so a multi-row key could not be rolled back — the transaction is what makes the rollback possible. Zero or many matches is a **409** (the optimistic-concurrency contract; the editor stays open with the error visible).

**Both paging paths must preserve the rowid projection.** `TablePageFiltered` (filter/sort/count) and the unfiltered `TablePage` are different functions; `PageOptions.IncludeRowID` forces the filtered path even with no filter/sort/count. Otherwise a PK-less rowid table loses the very column its row edits are keyed on.

### Exact row keys — why keys travel as strings

The grid hands the browser a row's key as **JSON**, and `JSON.parse` turns every JSON integer into a `float64`. Past 2^53 two *adjacent* ids collapse to the same number, so a key rebuilt from the grid's cells silently addresses the **neighbouring** row. Snowflake-style and user-assigned 64-bit ids exceed 2^53 routinely, so this is real, not hypothetical.

That makes it a **data-integrity** bug, not a cosmetic one: when the rounded neighbour exists the mutation hits the wrong row; when it does not, the user gets a confusing **409 "no row matched"** for a row they can plainly see.

The fix carries the key exactly end-to-end:

- `internal/dbbrowse/rowkeys.go` — `ExactValue(any) *string` renders one driver value as an exact decimal string (`nil` for SQL NULL, `"1"`/`"0"` for booleans); `RowKeys(schema TableSchema, page ResultSet) []map[string]*string` returns one key per row, **index-aligned** with `page.Rows`. It reads the key columns from the page's own columns, so the synthetic `_rowid_` case needs no special-casing by the caller, and it returns `nil` for a view or when a key column is not in the projection (a rowid table without `IncludeRowID`).
- `GET /api/db/table` gained a **`row_keys`** field, index-aligned with `result.rows` and omitted entirely when the table has no addressable key.
- The client's `rowKey(row, rowIndex?)` prefers `table.row_keys[rowIndex]` when it is non-null and otherwise falls back to the cell-derived key (an older remote server, or a null entry). All five mutations pass their row index: single delete, bulk delete, row-dialog update, inline cell edit, and the BLOB dialog.

Two invariant-shaped rules a future change must not break:

- **NULL stays NULL.** `ExactValue` returns `nil` for SQL NULL, never `""` — the predicate for `nil` is `IS NULL` while `""` is an equality, so flattening the two addresses nothing.
- **A partial key is never emitted.** A row whose key cannot be expressed exactly (e.g. an unexpressible component such as a BLOB) yields a **nil entry** and the caller falls back. A partial key could match a *different* row — the exact failure this path exists to prevent.

**No server-side coercion exists or is needed.** SQLite's type affinity converts a numeric TEXT parameter back to INTEGER for the comparison, so `WHERE id = '9007199254740993'` matches exactly; this was verified empirically before implementing and confirmed live (updating via the exact string key changed 2^53+1 and left 2^53 untouched).

**Residual:** the grid still **displays** the rounded value, because the cell itself arrives as a JSON number. Only the key path is fixed.

## 4. BLOB handling

The grid row carries only the **first 8 KB** of any value — a *prefix*, for display. This shapes everything:

- **Download is a separate endpoint** (`GET /api/db/blob`) that streams the full raw bytes, never base64 through JSON, with `Content-Disposition: attachment` and `X-Content-Type-Options: nosniff` (a blob can be any bytes; the browser must not re-interpret it). The `{bytes, truncated}` the grid holds is used only for an "only a preview is loaded" notice — never presented as the value.
- **A NULL cell answers 204, not 200-with-empty-body.** "Nothing stored" and "an empty file" are different, and a 200-with-empty-body would make the client save a zero-byte file. `ReadBlob` keeps the distinction in Go too: SQL NULL is `nil`, a zero-length blob is a non-nil empty slice.
- **TEXT stored in a BLOB column is refused**, not saved as bytes (`ErrNotABlob`). Silently coercing text→bytes is a data-corruption class.
- **The row key arrives as JSON in a query parameter and MUST be decoded with `json.Decoder.UseNumber`** (`decodeRowKey`). A plain `Unmarshal` rounds an integer id beyond 2^53 to a float64, the `WHERE` matches nothing, and it surfaces as "this row has no blob". (The browser itself cannot *send* such an id — `JSON.stringify` already rounds it — but the Go defence matters for non-browser callers.)
- **`ReadBlob` resolves key multiplicity BEFORE judging the column type.** Checking as it iterates let the first match's column type mask an ambiguous key, so an ambiguous key over a TEXT-holding blob column reported a column-type problem instead of the real ambiguity.
- **Inline rendering is a magic-byte allowlist for raster images only** (`SniffBlobMediaType` + `IsInlineSafeMediaType`). SVG and HTML are deliberately **not** treated as images because they execute script — an inline SVG in a preview pane is an XSS surface.
- **Upload sends the raw bytes as the request body**, capped before reading, so a large upload cannot be buffered unbounded.

## 5. Write safety

- **Every mutation takes a `VACUUM INTO` sibling snapshot first**: `dbbrowse.Backup` writes `<file>.db.bak`. The snapshot goes to a temp sibling first and is renamed into place, so a failed `VACUUM INTO` never costs the previous backup; the displaced `.db.bak` is rotated to `<file>.db.bak.<unixnano>` (newest 5 kept) so a second edit cannot overwrite the pre-mistake state. `VACUUM INTO` produces a consistent single-file snapshot that **includes rows still in the `-wal` sidecar** — a plain file copy can miss them. It runs **before** the statement, never after, or it would record the change it exists to protect against.
- **`COUNT(*)` is opt-in** (`PageOptions.WithCount`; the handler sets it only when it will send `total`). `HandleDBTable` maps only `dbbrowse.IsBadPageInput` errors (unknown sort/filter column, SQLITE_ERROR, non-read-only SQL) to 400; every other failure (locked, corrupt, I/O, timeout) is a logged 500.
- **`Backup` probes the file first** (`dbbrowse.Probe`). `sql.Open` is lazy and `VACUUM INTO` will happily snapshot an empty database it just created, so without the probe a typo'd path would report a successful backup of nothing.
- **There is currently NO restore UI.** The `.db.bak` is the user's return point; recovery is manual (copy it back). Do not describe the snapshot as "undo" in UI copy without also shipping a restore path.
- **Error mapping is direction-sensitive:** a failed **write** with no row matched maps to **409**; a failed **read** of the same shape maps to **404**. This is `writeBlobError`/`dbbrowseQueryError`; the distinction matters because a write failure is a conflict the user can retry, a read failure is a missing resource.

## 6. Grid / IDE behaviors worth preserving

- **A rejected filter is rendered next to the filter box** (`data-testid="sqlite-filter-error"`), never as an empty pane. "No rows" and "your filter is invalid" must not look alike.
- **Changing the filter or sort resets to page 1.** Staying on page N of a new result set shows an arbitrary window.
- **CSV export writes NULL as the empty field, not the text `null`** (otherwise the export cannot distinguish NULL from the string `null`); a BLOB becomes `[blob NB]` rather than base64 noise. The object URL is revoked on a **deferred timer**, not synchronously — Safari/WKWebView navigates to `about:blank` when a URL is revoked immediately after the click that used it.
- **Maintenance maps an allowlisted `op` string to a package-constant statement** (`analyze` / `vacuum` / `integrity_check`), never to user text, so an unrecognised op is refused rather than falling through to something executable. `analyze` and `vacuum` are writes and pass `dbWriteGuard`; `integrity_check` is a read-only PRAGMA that reports its problem lines (a healthy DB answers exactly `["ok"]`).
- **Every list field on a `/api/db/*` response is a JSON array, never `null`.** `encoding/json` renders a nil slice as `null`, and the viewer dereferences lists unguarded, so one nil slice crashes the pane. `dbbrowse` allocates every list it builds; the web client also normalises null lists once at the fetch boundary (`normalizeDBInfo` / `normalizeDBTable` / `normalizeDBResult`) because a **remote** project proxies to a host that may still run the old server.
- **A `revision` live-refresh must not clear the query editor or the table selection** — the viewer refetches `dbInfo` and the current page but keeps `queryText` and `selected`.
- **The table list is a drag-resizable, collapsible pane** (120–480 px, double-click resets) with an always-visible header toggle that collapses it to zero width. Width and collapsed state persist per browser under `ocode.ui.sqlite-viewer.width` and `ocode.ui.sqlite-viewer.width.collapsed` via the shared `useResizableSidebar` hook (same as the app sidebar and Git file list). The result grid (Data and Query tabs) scrolls on **both** axes: `ResultGrid`'s container is `min-w-0 overflow-auto` and its table is `w-max min-w-full`, so a wide table overflows horizontally while still filling a narrow pane. The `min-width: 0` on the viewer body/main pane/grid is load-bearing — without it the flex chain stretches the pane instead of the grid scrolling.

## 7. Agent tools (`sqlite_schema` / `sqlite_query` / `sqlite_exec`)

`internal/tool/sqlite.go` exposes the same engine to the model. They exist so a DB question is a typed `{path, sql}` call rather than a Python/`sqlite3` script: the auto-permission judge can classify the former directly, while a script's effect must be inferred from its source and a source over `maxInterpreterSourceBytes` (48 KB) is refused as `truncated_or_unknown`.

- **Same invariants as sections 1, 2 and 5, not a new policy.** Reads go through `dbbrowse.Query`/`ListTables`/`DescribeTable` (read-only twice over); `sqlite_exec` is the explicit read-write path and takes `dbbrowse.Backup` BEFORE `dbbrowse.Exec`. A backup failure aborts the write.
- **Path:** `confinedPath` (allowed roots, symlink-resolved) then `dbbrowse.Probe`, so a typo or non-SQLite file errors instead of making the driver create a stray database. `sqliteWriteGuard` mirrors `Handler.dbWriteGuard` (no writes under `paths.GlobalDataDir()`).
- **Refused on the write path:** `BlockedWriteStatement` (ATTACH/DETACH/VACUUM INTO) and `WritePragma` (writable_schema, journal_mode assignments). The web editor allows the latter after a confirm dialog; the tool refuses them outright because there is no per-call UI to carry that nuance.
- **Permissions:** all three are in `pathScopedTools` (the `path` arg drives the out-of-scope / sensitive-path checks). `sqlite_schema`/`sqlite_query` are default-allow and in `isReadOnlyTool`; `sqlite_exec` defaults to ask (like `delete`) so the judge or user sees the SQL. Plan/debug modes allow the two reads and block `sqlite_exec`.
- **Output is capped for the model**, not the browser: default 100 rows, max 1000, `truncated` says more exist.
- Not wired into sub-agent tool lists on purpose; add the names to an agent's `Tools` if a sub-agent needs DB access.

## Must-not-regress summary

1. Engine stays server-side Go `modernc.org/sqlite`; no WASM/sql.js.
2. Reads are `mode=ro` + `query_only`; the only write path is the confirmed `POST /api/db/query` (and row/DDL/maintenance endpoints) behind `dbWriteGuard`.
3. Every DB endpoint goes through `resolveDBPath` (containment **plus** unconditional allowed-roots, symlink-resolved).
4. Filters and sorts are validated by the read-only allowlist; sort columns are existence-checked then quoted.
5. Row edits key on PK-else-`_rowid_` and require exactly one row, inside a transaction; both paging paths keep the rowid projection.
6. BLOBs: prefix in the grid, full bytes on a separate download endpoint, NULL ≠ empty, `UseNumber` for keys, inline allowlist excludes SVG/HTML.
7. Snapshot (`<file>.db.bak`) is taken before every mutation; there is no restore UI yet.
8. 400 for bad caller input, 409 for write conflicts, 404 for missing reads, 204 for NULL blobs.
9. Null lists never reach the client; normalise once at the fetch boundary.

## See also

- `skills/ocode-web/SKILL.md` item 64 — the fuller implementation checklist (function-by-function, mutation-pinned rules).
- `concepts/pdf-viewer-zoom-find.md` — the precedent for documenting one preview viewer's decisions and gotchas.
- `concepts/web-server-project-scoping.md` — allowed roots and per-session project resolution that `resolveDBPath` builds on.
- `concepts/data-storage-layout.md` — why writes under `paths.GlobalDataDir()` are refused.