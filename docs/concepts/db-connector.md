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

- `internal/dbconnect.TableColumns` reads a table's columns and primary key from the catalog (`format_type` for the type). An empty result means no such table.
- `internal/dbconnect.BuildBrowseSQL` builds one page. The sort column must be one of the table's columns, and it is quoted only after that check. The direction is a boolean. The filter is the only free-form text: it sits on its own line inside parentheses, so a trailing `--` cannot comment out `ORDER BY` or `LIMIT`. Without a sort, rows are ordered by primary key; a table with no primary key has no stable order.
- `internal/dbconnect.BrowsePage` runs the page through `QueryReadOnly`, so a filter cannot write.
- Row writes (`BuildInsertSQL`, `BuildUpdateSQL`, `BuildDeleteSQL`, `ApplyRowChange`). Every value is a bound text parameter cast to its column's catalog type (`$n::numeric(10,2)`), so a bigint key stays exact. Identifiers are checked against the table's own columns before they are quoted. Update and delete use the primary key only; a table with no primary key is read-only. Update and delete run in a transaction and must match exactly one row. Zero matches is `ErrNoRowMatched` and more than one is `ErrManyRowsMatched`; both roll back and map to 409.

### HTTP surface

`internal/server/handler_dbconnect.go` and `handler_dbconnect_rows.go`, routes in `server.go`, all behind `authMiddleware`:

- `GET /api/dbconnect/connections?surface=` lists saved connections with an `unlocked` flag for the surface. Not paginated on purpose: a user saves a handful.
- `POST /api/dbconnect/connections` `{name, url, password}` verifies the password against the saved set, seals, and saves. Returns 401 on a mismatch and 409 if the set changed during the save. `DELETE /api/dbconnect/connections/{name}` removes.
- `POST /api/dbconnect/unlock` `{surface, password}` opens every saved envelope. Any failure rejects the whole unlock (401). Each granted connection keeps one pooled handle, opened on its first request. Replacing the grant closes the old handles.
- `POST /api/dbconnect/lock` `{surface}` drops the surface's grant and closes its pooled connections.
- `GET /api/dbconnect/tables?surface=&connection=&limit=&offset=` returns one page (`limit` 1-500, default 100) and `has_more`.
- `GET /api/dbconnect/rows?surface=&connection=&table=&sort=&dir=&filter=&limit=&offset=` returns one page of a table: `columns` (name, type, pk), `rows`, `has_more`, and `primary_key`. An unknown table is 404; a bad sort, direction, or paging value is 400; a filter that writes is 400.
- `POST /api/dbconnect/row` `{surface, connection, op: insert|update|delete, table, key, values}` returns `{rows_affected}`. The body is decoded with `UseNumber` so a key's digits reach the database exactly as sent. Zero or several matches are 409.
- `POST /api/dbconnect/query` `{surface, connection, sql, confirm}` requires an unlocked grant (403 otherwise). See "Writes" below.

## Unlock model

- Grants are in memory only, keyed by client surface id, then connection name. They hold the plaintext URLs. They are never serialized, logged, or written to disk. They last until the surface locks or the server restarts.
- The surface id is UX state, not a security boundary. The auth token is.
- Connections are global, not directory-bound, so the endpoints take no project param.
- Plaintext URLs never appear in responses. Errors from `ParseURL` are generic, because Go's `url.Error` quotes the whole input including the password.

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

`web/src/components/Layout/DBPanel.tsx` and `DBTableBrowser.tsx`:

- Add form (first run and "Add another connection"). Connection picker. Per-panel unlock; the password is cleared on success.
- Table list: paginated (100 per page, "Load more tables" appends). Clicking a table opens its Data view.
- Right pane has a Data and a Query tab. The Data view (`DBTableBrowser`) has:
  - a read-only filter field, applied on Apply or Enter;
  - sortable headers (click to sort ascending, click again to flip);
  - Previous and Next paging;
  - Add row, Edit row, and Delete row. Edit and insert use the SQLite viewer's `RowEditorDialog`; delete uses `ConfirmDialog`. Both take the Postgres converter (`pgInputToCell`) instead of `inputToCell`, which would round a bigint through a JavaScript number.
  - keys are sent exactly as the server returned them, so a bigint over 2^53 goes back as the same digits;
  - a table with no primary key is read-only, and the view says so.
- The toolbar stays mounted through a failed load, so a bad filter can be corrected in place.
- The Query tab has the SQL editor with Cmd/Ctrl+Enter. Results use the shared `ResultGrid`. The truncated-result notice is shown; errors render verbatim.
- Collapsible, drag-resizable table pane via `useResizableSidebar`, using the storage key `ocode.ui.dbconnect-table-pane.width`.
- Narrow panels stack. A `ResizeObserver` on the table-and-data area measures its width. Stacking is decided on the list's minimum (120px) plus a 4px handle plus 320px for the data pane: below 444px the list sits above the data at full width, capped at 10rem high, and the resize handle is hidden. Side by side, the list is drawn at the smaller of its saved width and the room left beside the data, so a wide saved width cannot lock the layout. The saved width and the collapsed state are never changed by the layout.
- A 409 from a SQL run opens the write confirmation. Only the confirmed retry sends `confirm: true`.
- Surface id is `db-panel:<sessionId>`. Unmount calls `/api/dbconnect/lock`.
- Calls go through `api.dbConnect*` in `web/src/api/client.ts`. They are not host-threaded: connections are global, so a remote-project session never routes them through `?host=`.
- Shared cell conversion (`dbConnectCells.ts`): `toCell`, `pgInputToCell`, and `toResultSet`.
- Unit tests: `DBPanel.test.tsx` (15 cases) and `DBTableBrowser.test.tsx` (10 cases).

## Known limits

- A `json` column value is shown as its JSON text in the grid. An `int8` beyond 2^53 is sent as a string.
- The grid scrolls inside its pane. Its columns are not fitted to the pane.
- The browser pass for the stacked layout was manual. No automated test checks the layout itself.
- No pre-write snapshot or undo. Postgres has no file to copy, and no sound undo exists for arbitrary remote SQL.

## Security notes

- Encrypted `db.connections` entries are part of `ocodeconfig.json`, so they sync to the hub with the rest of that file (see `internal/sync`). Ciphertext only; the master password is never stored.
- `internal/encrypt` was removed on 2026-10-09. It was untracked and had no importers. A backup copy lives in the session scratchpad only.
