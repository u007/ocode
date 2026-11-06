---
type: Concept
title: DB Connector (P1) — 2026-10-08, status 2026-10-09
description: 'PostgreSQL connector: saved encrypted connections, read-only enforcement, confirmed writes, in-memory unlock grants, environment templates resolved per project, and the web DB panel. Includes the unlock path''s acceptance of the host''s HOME-expanded spelling of a remote (~) project path.'
resource: internal/dbconnect; internal/server/handler_dbconnect.go; internal/server/dbconnect_env.go; internal/config/db_connections.go; web/src/components/Layout/DBPanel.tsx
tags:
  - dbconnect
  - postgres
  - sqlite
  - env
  - security
timestamp: 2026-11-06T07:54:57Z
---
# DB Connector (P1) — 2026-10-08, status 2026-10-09

Purpose: connect to PostgreSQL via `pgx`, reuse `dbbrowse` for SQLite, save encrypted URLs.

The SQLite browser (`docs/concepts/sqlite-browser.md`) is the parity reference. Its §1-2 rules are the read/write split and explicit confirmation. Containment and the SQLite allowlist do not apply to Postgres; the read-only guarantee comes from the server's transaction mode instead.

## Shipped

### Saved connections

- `ocodeconfig.json` `db.connections`: list of `{name, driver, url}`. `driver` must be `postgres`. `url` is an encrypted envelope, never plaintext. Managed by `config.AddDBConnection(conn, verified)` and `config.RemoveDBConnection` under the ocodeconfig lock (`internal/config/db_connections.go`).
- `AddDBConnection` takes the saved set the caller verified the master password against. Under the lock it writes only if that set is unchanged, and otherwise returns `ErrDBConnectionsChanged` (HTTP 409). Argon2id runs outside the lock.
- `internal/dbconnect.SealURL(url, password)` validates the URL and seals it with `internal/encryption.EncryptJSON`. Each envelope carries its own 32-byte Argon2id salt, so the password alone is enough to open it.
- `internal/dbconnect.OpenURL(sealed, password)` decrypts it. A wrong password fails the AES-GCM tag check. There is no separate verifier.
- `internal/dbconnect.Open` uses the `pgx` driver name and extended protocol only (`QueryExecModeExec`), so one call carries one statement.

### Statements

- `internal/dbconnect.QueryReadOnly` runs one statement in a `READ ONLY` transaction, capped at 1000 rows. Column types are read from the driver, so `bytea` is sent in Postgres's `\x…` hex form.
- `internal/dbconnect.ExecWrite` runs one confirmed statement in a read-write transaction and commits it. It first describes the statement (an unnamed Parse + Describe, which executes nothing and refuses multi-statement input). A statement with no result columns runs through `Exec` and reports the affected-row count. A statement with columns (`INSERT … RETURNING`) runs through the same row scanner as a read, and `rows_affected` is the number of rows it returned.
- `internal/dbconnect.IsReadOnlyViolation` matches SQLSTATE 25006 with `errors.As` on `*pgconn.PgError`, never on message text.
- `internal/dbconnect.HasServerSideEffect` is a static pre-check for calls that a READ ONLY transaction does not refuse but that act on the server: `pg_terminate_backend`, `pg_cancel_backend`, `pg_(try_)advisory_*`, `pg_sleep*`, `pg_notify`, `pg_reload_conf`, `lo_*`, `set_config(…)`, `NOTIFY`, `LISTEN`, and `SET ROLE` / `SET SESSION AUTHORIZATION`. The match is case-insensitive and errs toward flagging.
- `internal/dbconnect.jsonSafeInt`: an `int64` outside ±(2^53−1) is sent as a decimal string, so a snowflake-style id is never rounded.
- `internal/dbconnect.ListTables(ctx, db, limit, offset)` returns one sorted page and `has_more`.

### Table data

- `internal/dbconnect.TableColumns` reads a table's columns and primary key from the catalog (`format_type` for the type), plus the row-editor metadata: `pg_attribute.attnotnull` → `not_null`; `pg_get_expr(pg_attrdef.adbin, adrelid)` → `default` (omitted when there is no expression); `attidentity <> ''` or any `pg_attrdef` row → `has_default` (an identity column is defaulted with no displayable expression); `attgenerated <> ''` → `generated` (a stored generated column). An empty result means no such table. Pinned by `TestPGTableColumnsCarryDefaultAndNullability`.
- `internal/dbconnect.BuildBrowseSQL` builds one page. The sort column must be one of the table's columns, and it is quoted only after that check. The direction is a boolean. The filter is the only free-form text: it sits on its own line inside parentheses, so a trailing `--` cannot comment out `ORDER BY` or `LIMIT`. Without a sort, rows are ordered by primary key; a table with no primary key has no stable order.
- `internal/dbconnect.BrowsePage` runs the page through `QueryReadOnly`, so a filter cannot write.
- Row writes (`BuildInsertSQL`, `BuildUpdateSQL`, `BuildDeleteSQL`, `ApplyRowChange`). Every value is a bound text parameter cast to its column's catalog type (`$n::numeric(10,2)`), so a bigint key stays exact. Identifiers are checked against the table's own columns before they are quoted. Update and delete use the primary key only; a table with no primary key is read-only. Update and delete run in a transaction and must match exactly one row. Zero matches is `ErrNoRowMatched` and more than one is `ErrManyRowsMatched`; both roll back and map to 409.

### HTTP surface

`internal/server/handler_dbconnect.go` and `handler_dbconnect_rows.go`, routes in `server.go`, all behind `authMiddleware`:

- `GET /api/dbconnect/connections?surface=` lists saved connections with an `unlocked` flag for the surface. Not paginated on purpose: a user saves a handful.
- `POST /api/dbconnect/connections` `{name, url, password}` seals a literal URL and saves it; the password is required for a literal URL only (a `${NAME}` template is stored in the clear and needs none). A template whose userinfo password or `password`/`sslpassword` value is not entirely a reference is refused (400) — a literal password would be persisted unencrypted. Returns 401 on a password mismatch and 409 if the set changed during the save. `DELETE /api/dbconnect/connections/{name}` removes.
- `POST /api/dbconnect/unlock` `{surface, password, project_path}` returns `{unlocked, failed}`. With a password, every saved envelope must open (a wrong password is 401 before any grant) and templates are resolved too. Without a password, only the `${NAME}` templates are granted and sealed connections stay locked; this path MERGES into the surface's grant, so it never revokes an already-unlocked sealed connection. A template needs `project_path` (an allowed project root) or a `session_id`; with neither it is reported in `failed` and is never resolved from the server's process environment. Other template failures are also per connection in `failed`, and the connections that did resolve are still granted. Each granted connection keeps one pooled handle, opened on its first request. A password unlock replaces the grant and closes the old handles; a password-less one merges.
- `POST /api/dbconnect/lock` `{surface}` drops the surface's grant and closes its pooled connections.
- `GET /api/dbconnect/tables?surface=&connection=&limit=&offset=` returns one page (`limit` 1-500, default 100) and `has_more`.
- `GET /api/dbconnect/rows?surface=&connection=&table=&sort=&dir=&filter=&limit=&offset=` returns one page of a table: `columns` (`name`, `type`, `pk`, plus `not_null`, `default`, `has_default`, and `generated`), `rows`, `has_more`, and `primary_key`. The metadata fields are OPTIONAL to the client: a remote project proxies to a host that may still run an older `ocode serve --remote`, so an absent `not_null`/`has_default`/`generated` reads as false and an absent `default` as none (the same additive rule as `row_keys`). An unknown table is 404; a bad sort, direction, or paging value is 400; a filter that writes is 400.
- `POST /api/dbconnect/row` `{surface, connection, op: insert|update|delete, table, key, values}` returns `{rows_affected}`. The body is decoded with `UseNumber` so a key's digits reach the database exactly as sent. Zero or several matches are 409.
- `POST /api/dbconnect/query` `{surface, connection, sql, confirm}` requires an unlocked grant (403 otherwise). See "Writes" below.

## Unlock model

- Grants are in memory only, keyed by client surface id, then connection name. They hold the plaintext URLs. They are never serialized, logged, or written to disk. They last until the surface locks or the server restarts.
- The surface id is UX state, not a security boundary. The auth token is.
- Saved connections are global, not directory-bound; they are not stored per project. Only `unlock` takes a `project_path`, and only so a `${NAME}` template can read that project's `.env` / `.env.local`. The path is validated against the server's allowed project roots. `dbUnlockProjectRoot` (`handler_dbconnect.go:185`) resolves it through `dbProjectRootFor` (`handler_dbconnect.go:213`), which tries the exact allowlist check first (`handler_dbconnect.go:214` `fileContentRootFor`) and, on a miss, retries the HOME-expanded spelling (`handler_dbconnect.go:217` `projects.ExpandHome`). That second try exists because the desktop saves a remote (SSH/WSL) project under the verbatim `~` form and proxies the unlock to the host's own server, whose registry holds the HOME-expanded path (`projects.Add` expands there); without it, every tilde-keyed remote project was rejected with `project_path is not an allowed project root`. The trust decision is unchanged: only a saved, allow-listed root can match, and expansion narrows the accepted set (`~user` forms and non-tilde paths pass through `ExpandHome` unchanged, so they cannot match). It mirrors the `resolveRegisteredProjectRoot` fallback already used by the git, upload and session-list endpoints. A remote project's calls are routed to its host's server (`/api/remote/<host>/api/dbconnect/*`), which validates the path against its own roots and reads the host's `.env`. A template with neither `project_path` nor a `session_id` is refused, never resolved from the process environment.
- A template carries no secret, so it needs no master password: a password-less unlock grants templates only and MERGES into the surface grant. A sealed connection still requires the password that sealed it; a password unlock replaces the grant.
- The master password therefore protects stored literal URLs only. A holder of the server auth token can add and load an environment connection and then read/write that database; the token is the real boundary (it already opens a terminal). The `from_env` flag marks the password-free connections.
- Plaintext URLs never appear in responses. Errors from `ParseURL` are generic, because Go's `url.Error` quotes the whole input including the password.

## Unlock model additions (review fixes)

- The in-memory grant is now bounded: a surface idle for 30 minutes (or beyond 64
  remembered surfaces) is retired with its pooled handle, and the client-supplied
  surface is length-bounded (256). The panel's surface id carries a per-DOCUMENT
  random tag as well as the per-mount counter, because a module counter restarts
  at 1 in every browser window.
- A template that stops resolving now LOSES its grant and pool: the password-less
  unlock drops it from the surface before the "nothing unlocked" 400, so a later
  query cannot keep hitting the old database.
- `project_path` is resolved and validated only when at least one saved connection
  is a template, so a sealed-only unlock works even when the project directory has
  moved.

## Reads never write

Every statement runs in a `READ ONLY` transaction first. Verified live against Postgres 16:

- A direct `DELETE` is refused with SQLSTATE 25006 ("cannot execute DELETE in a read-only transaction").
- `SELECT 1; SELECT 2` is refused: "cannot insert multiple commands into a prepared statement".
- `COMMIT; DELETE …`, `END; DELETE …`, `ROLLBACK; DELETE …`, and `SELECT 1; COMMIT; DELETE …` are refused by the same multi-statement rule, both unconfirmed and confirmed, so no statement reaches the DELETE outside the read-only transaction. Row count unchanged.
- `SET TRANSACTION READ WRITE` alone is accepted and changes nothing, because the transaction is rolled back and no write follows.

This is a server-enforced transaction, not a privilege boundary. A SQL function with side effects (for example via `dblink`) can still write if the connection's role allows it. For a hard guarantee, connect with a role that has no write grants.

A table filter that tries to write (`nextval('sq') > 0`) is refused with 400 "the filter must be a read-only expression". Injection shapes (`true) --`, `1=1); DELETE …`, an unterminated `/*`) fail as a syntax error and the row count is unchanged.

## Writes (confirmed)

The server asks Postgres whether a statement is a write. There is no lexical classifier.

1. The statement runs read-only. If the server refuses it with 25006, that is the write signal, and the statement has done nothing. A statement that `HasServerSideEffect` flags skips this step: it gets the same 409 before anything is sent, and only a confirmed retry runs it.
2. Without `confirm`, the handler returns **409**. The client opens `ConfirmDialog` (from `SQLiteDialogs`) with the SQL and states that the change is permanent. Cancel is the default action.
3. With `confirm: true` the statement is described, then run in a read-write transaction and committed. The response is `{rows_affected, columns, rows}`. The panel shows "N row(s) affected. Committed." and, for `RETURNING`, the returned rows.

Differences from the SQLite browser:

- **Single statement only, even when confirmed.** SQLite runs a confirmed batch in one transaction. Postgres runs exactly one statement, because the extended protocol refuses multiple statements. A deliberate difference, decided 2026-10-09: a batch would need a lexical splitter or the simple protocol, and the simple protocol lets `COMMIT; DELETE …` escape the transaction. Batches happen only if the user asks for them after hearing this.
- **No undo.** SQLite takes a pre-write snapshot. There is no equivalent here, so the dialog says so.
- **`rows_affected` counts returned rows for a statement that returns columns.** For `INSERT`/`UPDATE`/`DELETE … RETURNING` that is every changed row. For a `WITH … DELETE … SELECT` it is the rows the `SELECT` returned, not the rows deleted. The panel says "returned" for these results and "affected" only for a statement with no columns. Postgres's command tag names only the top-level statement, and reading the tag would need the native pgx write path, which decodes values differently. So the deleted count is not reported. Pinned by `TestPGDataModifyingCTEReportsReturnedRows`.
- **Statements that cannot run in a transaction return 400.** Examples: `VACUUM` (25001) and `CREATE INDEX CONCURRENTLY`. A statement that fails for another reason also returns 400 with the server's message.

## Environment templates

A connection URL may contain ${NAME} or $NAME references (docs/concepts/db-environment-templates.md). At unlock (handler_dbconnect.go:500 HandleDBConnectUnlock) the template is expanded against the project the request names — `project_path` for the project-scoped DB tab, `session_id` as the legacy fallback — and validated as a URL (dbconnect_env.go:101 ParseURL). Precedence is process environment > `.env.local` > `.env` (`dbEnvLookup`/`envfile.Lookup`), so an exported server variable shadows the project file. An unreadable `.env` (a differing duplicate, an unterminated quote, a permission error) is reported as `cannot read the project's env files: <wrapped error naming the path and line>` rather than a phantom missing variable (dbconnect_env.go:95). The resolved URL stays in the in-memory grant (`handler_dbconnect.go:635` merge for the environment flow, `handler_dbconnect.go:637` replacement for a password unlock); never returned, logged, or written (dbconnect_env.go:82). Only a braced reference with a shell-valid name makes a value a template (unresolved = error); a bare `$NAME` never does (expand.go:151 IsTemplate), so a literal p$ssword, pa$HOME or we$ird.db stays a plain value and is sealed. A template's credential must now be BRACED references only (`${VAR}`): a bare `$NAME` is refused, because an unset variable is kept as literal text by `ExpandEnv` and would be stored in the clear. The query scan also no longer stops at `#` and runs even when the URL has no `://`. Saved shape uses env_template (plaintext, internal/config/db_connections.go:35); literal URLs stay encrypted (db_connections.go:56 EncryptedURL). A template needs NO master password (a password-less unlock grants templates only); sealed connections still require the password, and a wrong password is 401 before any grant. A template needs a PROJECT: with neither `project_path` nor `session_id` it is reported in `failed` and never resolved from the server's process environment. A template whose password is literal is refused (TemplateHasLiteralSecret, expand.go:207), and ParseURL refuses a raw `#`, parses the query the way pgx does (split on `&`, percent-decode the key, bad escape fails closed), and refuses `service`/`servicefile`/`passfile`/`sslkey`/`sslcert`/`sslrootcert` (`dbconnect.go:160` redirect params, `dbconnect.go:189` ParseURL).

A saved template is protected against destruction by an older ocode binary: the template is duplicated under the separate top-level `db_env_templates` key (`internal/config/db_connections.go:35` `DBEnvTemplatesKey`), which an older binary writes back verbatim from its `Extra` map. The map is authoritative on load (`ocodeconfig.go:2611` `reconcileDBEnvTemplates`) and is rebuilt from the connections at save time (`ocodeconfig.go:3209`); when no template connection remains it is deleted (`ocodeconfig.go:3372`) so a stale map cannot resurrect one. Full mechanism: `docs/concepts/db-environment-templates.md` (§ "Forward-compatibility: the `db_env_templates` top-level key").


## Verified

Automated: `make test-postgres` runs `internal/dbconnect/pg_integration_test.go` and `internal/server/handler_dbconnect_pg_integration_test.go` (build tag `pgintegration`) against a throwaway `postgres:16` container started with the docker CLI. Each test gets its own database, dropped afterwards. `OCODE_TEST_POSTGRES_URL` (admin URL, in `.env.example`) selects the server; a missing variable fails the test. The CI `postgres` job runs the same suites with a service container.

The checks below were first run by hand against Postgres 16. The automated suites cover them.

- An unconfirmed INSERT returns 409, and the row count is unchanged. A confirmed INSERT commits.
- A confirmed `COMMIT; DELETE …` is refused with 400, and the row count is unchanged. A confirmed SELECT works.
- `VACUUM` returns 400.
- Describing `INSERT`, `DELETE`, `CREATE TABLE`, and `DROP TABLE` executes none of them.
- `INSERT … RETURNING *` and `SELECT *` return identical JSON for the same row. The row covers uuid, numeric, timestamptz, bytea, jsonb, boolean, and a bigint of 9007199254740993.
- An `INSERT … RETURNING` of 1005 rows reports `rows_affected: 1005`, `truncated: true`, and 1000 rows.
- Update and delete by the exact key at 9007199254740993 leave the row at 9007199254740992 unchanged. Verified with a distinguishing value.
- A missing key is 409 for update and delete. A table with no primary key refuses keyed writes with 400 and accepts inserts.
- Filter and sort: a sort on a non-column is 400, and so is a bad direction. Pages report `has_more`.

Browser pass (headless Chrome, isolated `HOME`, Postgres 16): add and unlock, table data view, sort by header, a write-attempt filter's error shown verbatim, a valid filter, edit of row 2 by exact key, insert of `9007199254740995`, and a confirmed delete. After each step the database was checked.

## DBPanel (web)

The DB connector is a **project-level top tab** (beside Assets), not a chat session sub-tab, because an environment-backed connection resolves against the active project. `web/src/components/Layout/DBPanel.tsx` and `DBTableBrowser.tsx`:

- Add form (first run and "Add another connection"). A `${NAME}` environment URL needs no master password (the field is disabled). A literal URL does need one, and the label depends on whether a connection is already saved:
  - **First setup** (no connection saved): **Create master password**, plus a **Confirm master password** field. Save stays disabled until both match. The hint says the password is new, not an existing account password, and cannot be recovered if forgotten. Both fields use `autoComplete="new-password"`.
  - **Later saves**: **Master password (must match the one you created first)**, `autoComplete="off"`. The server rejects a mismatch.
  - The URL input has `data-1p-ignore` and `data-lpignore`, so password managers do not offer to fill it.
- **Auto-unlock on save.** After a literal connection saves, the panel unlocks it with the password just typed (`unlockWith`, the same path as the Unlock button). It is then selected and its tables load. A template is not auto-unlocked. If that unlock fails, the connection stays saved and locked, and the error shows in the panel alert.
- **Locked card** (literal connections): the copy reads "The URL is encrypted and never shown again. Enter your master password to connect." The button is **Unlock & connect**. An environment-backed connection shows **Load from .env** instead of a password prompt. Per-panel unlock; the password is cleared on success.
- Table list: paginated (100 per page, "Load more tables" appends). Clicking a table opens its Data view.
- Right pane has a Data and a Query tab. The Data view (`DBTableBrowser`) has:
  - a read-only filter field, applied on Apply or Enter;
  - sortable headers (click to sort ascending, click again to flip);
  - Previous and Next paging;
  - Add row, Edit row, and Delete row. Edit and insert use the SQLite viewer's `RowEditorDialog`; delete uses `ConfirmDialog`. Both take the Postgres converter (`pgInputToCell`) instead of `inputToCell`, which would round a bigint through a JavaScript number.
  - Insert omits an empty NOT NULL column that has a default, so Postgres applies the column's `DEFAULT` (Postgres applies a default only when the column is OMITTED, never when `NULL` is passed). An empty NOT NULL column with no default blocks Save with an inline `Required: <names>` message and sends no request; an empty nullable column is sent as `NULL`. Update sends every field shown, so an explicit clear stays an explicit `NULL`. Generated columns are excluded from the editor entirely. A column label carries a red `not null` badge and `default <expr>` when present (an identity column shows a plain `default`).
  - **A UUID primary key is pre-filled on INSERT, but only with evidence.** `DBTableBrowser.schemaOf` (`web/src/components/Layout/DBTableBrowser.tsx:31`) computes a client-only `uuid_like` hint per column from the current page's own values with `columnHasUUIDIds` (`web/src/lib/dbIds.ts:60`): the column must be a textual primary key that is neither generated nor defaulted, and either its declared type is exactly `uuid` or every sampled non-empty value is a UUIDv4. An empty such field opens pre-filled with a fresh UUIDv4, visible and editable; a Generate button (`aria-label` "Generate UUID for <column>", `SQLiteDialogs.tsx:363`) regenerates it. A slug-like key (`admin`, `user`) has no UUID evidence, so it is NOT auto-filled and still blocks Save with `Required: …`. Nothing is written until Save; Cancel discards the generated value. The hint is derived from the returned rows and never comes from the server, so it also works against an older remote host (the `uuid_like` field in `web/src/api/client.ts:882` is client-only, not an `/api/dbconnect/rows` payload field). The schema is memoised per page (`DBTableBrowser.tsx:141`), so a parent re-render cannot reset the open dialog's draft.
  - keys are sent exactly as the server returned them, so a bigint over 2^53 goes back as the same digits;
  - a table with no primary key is read-only, and the view says so.
- The toolbar stays mounted through a failed load, so a bad filter can be corrected in place.
- The Query tab has the SQL editor with Cmd/Ctrl+Enter. Results use the shared `ResultGrid`. The truncated-result notice is shown; errors render verbatim.
- Collapsible, drag-resizable table pane via `useResizableSidebar`, using the storage key `ocode.ui.dbconnect-table-pane.width`.
- Narrow panels stack. A `ResizeObserver` on the table-and-data area measures its width. Stacking is decided on the list's minimum (120px) plus a 4px handle plus 320px for the data pane: below 444px the list sits above the data at full width, capped at 10rem high, and the resize handle is hidden. Side by side, the list is drawn at the smaller of its saved width and the room left beside the data, so a wide saved width cannot lock the layout. The saved width and the collapsed state are never changed by the layout.
- A 409 from a SQL run opens the write confirmation. Only the confirmed retry sends `confirm: true`.
- Surface id is `db-panel:<host>:<projectPath>:<per-mount id>`, so two clients viewing the same project do not share (and revoke) each other's grant. App keys the panel by `host:path` and mounts it only for a project whose DB tab has been visited. Unmount calls `/api/dbconnect/lock` for this panel's surface.
- Calls go through `api.dbConnect*` in `web/src/api/client.ts`. A local project hits this server. A remote project's calls all go through its reverse proxy (`/api/remote/<host>/api/dbconnect/*`, with `X-Ocode-Project` set), so the host's own server resolves the `.env`, owns the saved connections, unlock grants and pools, and dials the database. A `localhost` URL therefore means the host's database. The host must run a version with the dbconnect routes; there is no version negotiation. A remote call that answers 404 or 405 shows `Remote host <host> answered HTTP <status> for the database connector: <message>`, and other errors (400, 409, ...) are shown verbatim. Lock and remove go through `fetchEmpty`, which throws the same `ApiError` (with the HTTP status) as the JSON calls, so their 404/405 get the same host-naming text. The `DBConnectTarget` argument on each call carries the host and project path; a local project passes none. Unlock takes its project as a `DBUnlockScope` tuple: `[projectPath]` for a local project, `[projectPath, { host, projectPath }]` for a remote one. A remote path without its host, or a host without its path, is a compile error and a runtime throw, so a remote path never reaches the wrong server. The panel refuses to render a remote project with no project path, rather than let the host act on its default project.
- A failed connection-list load shows the error and a Retry (not an endless spinner). A partial unlock shows the `failed` connections in a warning while the granted ones are usable; the password form is absent for `from_env` connections.
- Shared cell conversion (`dbConnectCells.ts`): `toCell`, `pgInputToCell`, and `toResultSet`.
- Unit tests: `DBPanel.test.tsx` (31 cases, including auto-unlock on save and its failure, first-setup confirmation, the locked-card copy, the password-less environment flow, partial failures, retry and the remote project's routing to its host), `DBTableBrowser.test.tsx` (16 cases, including the richer column metadata and the UUID-format hint), `SQLiteDialogs.test.tsx` (the shared row editor's insert/update rules, including UUID pre-fill and the Generate button), and `dbIds.test.ts` (7 cases pinning the UUIDv4 shape/version/variant and the detection rule).

## Known limits

- A `json` column value is shown as its JSON text in the grid. An `int8` beyond 2^53 is sent as a string.
- The grid scrolls inside its pane. Its columns are not fitted to the pane.
- The browser pass for the stacked layout was manual. No automated test checks the layout itself.
- No pre-write snapshot or undo. Postgres has no file to copy, and no sound undo exists for arbitrary remote SQL.

## Security notes

- Encrypted `db.connections` entries are part of `ocodeconfig.json`, so they sync to the hub with the rest of that file (see `internal/sync`). Ciphertext only; the master password is never stored.
- `internal/encrypt` was removed on 2026-10-09. It was untracked and had no importers. A backup copy lives in the session scratchpad only.
