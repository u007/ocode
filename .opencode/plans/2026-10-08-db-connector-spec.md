# DB Connector — Spec (P1)
Status: approved (2026-10-08) | Scope: SQLite reuse + PGX + master-password encryption

1. Reuse `internal/dbbrowse` for SQLite (`.sqlite`/`.db`).
2. Add `internal/dbconnect` with `pgx` for PostgreSQL (`postgres://` URL import).
3. Web DB tab beside Git (`TopTabs.tsx` / session sub-tabs): connection picker, table list, SQL editor, results.
4. Connections stored in `ocodeconfig.json#db.connections` with AES-GCM encrypted `url` via master-password-derived key.
